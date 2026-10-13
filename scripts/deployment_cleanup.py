#!/usr/bin/env python3
"""Retire recorded deployment resources after explicit, verified acceptance.

Python 3.9+, standard library only. Runs Docker locally or through SSH; Python
is not required on the Docker host. It never prunes a host, removes volumes,
deletes application files, or force-removes a container/image.
"""

import argparse
from contextlib import contextmanager
import json
import os
import re
import shlex
import subprocess
import tempfile
import uuid
from pathlib import Path


class SafetyError(RuntimeError):
    pass


def identity(value, image=False):
    pattern = r"sha256:[a-f0-9]{64}" if image else r"[a-f0-9]{64}"
    if not isinstance(value, str) or not re.fullmatch(pattern, value):
        raise SafetyError("Use a full immutable " + ("image" if image else "container") + " ID")
    return value


class Docker:
    def __init__(self, host=None):
        if host and (host.startswith("-") or not re.fullmatch(r"[A-Za-z0-9_.@-]+", host)):
            raise SafetyError("Invalid SSH host alias")
        self.host = host

    def run(self, args, missing_ok=False):
        command = ["docker", *args]
        if self.host:
            command = ["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
                       "--", self.host, shlex.join(command)]
        result = subprocess.run(command, text=True, capture_output=True, timeout=180)
        if result.returncode:
            if missing_ok and any(s in result.stderr for s in ("No such image", "No such object", "No such container")):
                return None
            raise SafetyError("Docker operation failed: " + result.stderr.strip()[:500])
        return result.stdout

    def container(self, reference, missing_ok=False):
        data = self.run(["container", "inspect", reference], missing_ok)
        return json.loads(data)[0] if data is not None else None

    def containers(self):
        ids = self.run(["container", "ls", "-aq", "--no-trunc"]).split()
        if not ids:
            return []
        return json.loads(self.run(["container", "inspect", *ids]))

    def image(self, reference, missing_ok=False):
        data = self.run(["image", "inspect", reference], missing_ok)
        return json.loads(data)[0] if data is not None else None

    def images(self):
        ids = sorted(set(self.run(["image", "ls", "-aq", "--no-trunc"]).split()))
        return json.loads(self.run(["image", "inspect", *ids])) if ids else []

    def bind_dependency(self, containers, retired):
        roots = [v for v in (retired.get("GraphDriver", {}).get("Data") or {}).values()
                 if isinstance(v, str) and v.startswith("/")]
        if retired.get("GraphDriver", {}).get("Name") == "btrfs":
            root = json.loads(self.run(["info", "--format", "{{json .DockerRootDir}}"])).rstrip("/")
            mount_file = root + "/image/btrfs/layerdb/mounts/" + identity(retired["Id"]) + "/mount-id"
            mount_id = self.host_run(["cat", "--", mount_file]).strip()
            if not re.fullmatch(r"[a-f0-9]{64}", mount_id):
                raise SafetyError("Cannot verify retired Btrfs root filesystem")
            roots.append(root + "/btrfs/subvolumes/" + mount_id)
        sources = [m.get("Source", "") for c in containers if c["Id"] != retired["Id"]
                   for m in c.get("Mounts") or [] if m.get("Source", "").startswith("/")]
        if not roots or not sources:
            return False
        # -ef compares filesystem identity, including bind-mount aliases. Walk
        # source ancestors so a mount of a child of the old root is protected.
        script = "set -eu\nfound=false\n"
        for root in roots:
            script += "if [ -d " + shlex.quote(root) + " ]; then found=true; fi\n"
        script += 'if [ "$found" != true ]; then exit 42; fi\n'
        for source in sources:
            script += "p=" + shlex.quote(source.rstrip("/")) + "\n"
            script += 'p=$(readlink -f -- "$p")\n'
            script += "while [ -n \"$p\" ] && [ \"$p\" != / ]; do\n"
            for root in roots:
                script += "if [ \"$p\" -ef " + shlex.quote(root) + " ]; then printf referenced; exit 0; fi\n"
            script += "p=${p%/*}\ndone\n"
        return self.host_run(["sh", "-s"], input_text=script).strip() == "referenced"

    def host_run(self, args, input_text=None):
        command = args
        if self.host:
            command = ["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
                       "--", self.host, shlex.join(args)]
        result = subprocess.run(command, input=input_text, text=True, capture_output=True, timeout=60)
        if result.returncode:
            raise SafetyError("Cannot verify host filesystem references")
        return result.stdout


def container_record(container):
    return {"id": identity(container["Id"]), "image": identity(container["Image"], True)}


def deployment_record(host, service, old, image_digests=()):
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]*", service):
        raise SafetyError("Use an exact container name")
    if old["Name"] != "/" + service:
        raise SafetyError("Container name did not match")
    record = container_record(old)
    return {"schema": 1, "deployment_id": str(uuid.uuid4()), "host": host,
            "service": service, "previous": record, "containers": [record],
            "images": [record["image"]], "previous_image_digests": list(image_digests),
            "status": "captured"}


def capture(docker, service):
    old = docker.container(service)
    image = docker.image(old["Image"])
    return deployment_record(docker.host, service, old, image.get("RepoDigests") or [])


def track(docker, state, container_id=None, image_id=None):
    """Record only IDs the caller owns; never infer ownership from a host diff."""
    if container_id:
        record = container_record(docker.container(identity(container_id)))
        if record not in state["containers"]:
            state["containers"].append(record)
        image_id = record["image"] if image_id is None else image_id
    if image_id:
        iid = identity(image_id, True)
        if docker.image(iid)["Id"] != iid:
            raise SafetyError("Image identity changed")
        if iid not in state["images"]:
            state["images"].append(iid)
    return state


def check_current(docker, state, container_id, image_id):
    current = docker.container(state["service"])
    if current["Id"] != container_id or current["Image"] != image_id:
        raise SafetyError("Verified deployment identity changed; cleanup stopped")
    if current["State"]["Status"] != "running":
        raise SafetyError("Verified deployment is not running")
    health = current["State"].get("Health", {}).get("Status")
    if health is not None and health != "healthy":
        raise SafetyError("Verified deployment is not healthy; cleanup stopped")
    return current


def image_ancestors(images, referenced):
    parents = {im["Id"]: im.get("Parent") for im in images}
    protected = set(referenced)
    for iid in list(referenced):
        seen = set()
        while parents.get(iid) and iid not in seen:
            seen.add(iid)
            iid = parents[iid]
            protected.add(iid)
    return protected


def container_dependency(containers, retired):
    """A stopped ID may still be configured by another container."""
    names = (retired["Id"], retired["Id"][:12], retired["Name"].lstrip("/"))
    for other in containers:
        if other["Id"] == retired["Id"]:
            continue
        config = other.get("HostConfig") or {}
        references = list(config.get("VolumesFrom") or []) + list(config.get("Links") or [])
        references += [config.get("NetworkMode", ""), config.get("PidMode", ""), config.get("IpcMode", "")]
        for ref in references:
            head = ref.removeprefix("container:").split(":", 1)[0].lstrip("/")
            if head in names:
                return True
        roots = [v for v in (retired.get("GraphDriver", {}).get("Data") or {}).values()
                 if isinstance(v, str) and v.startswith("/")]
        for mount in other.get("Mounts") or []:
            source = mount.get("Source", "").rstrip("/")
            if any(source == root.rstrip("/") or source.startswith(root.rstrip("/") + "/") for root in roots):
                return True
    return False


def cleanup(docker, state, container_id, image_id, verified=False, dry_run=False, save=None):
    """The caller must first pass its app/version/data/doctor acceptance checks.

    Cleanup failure is separate from deployment failure: never roll a healthy
    accepted service back because a retired resource could not be deleted.
    """
    identity(container_id)
    identity(image_id, True)
    if not verified:
        raise SafetyError("Project acceptance checks must pass before cleanup")
    if state.get("schema") != 1 or state.get("host") != docker.host:
        raise SafetyError("Deployment journal does not match this Docker host")
    check_current(docker, state, container_id, image_id)
    report = {"status": "dry_run" if dry_run else "complete", "removed_containers": [],
              "removed_images": [], "protected_containers": [], "protected_images": [], "errors": []}
    state["cleanup"] = report

    def persist():
        if save:
            save(state)

    try:
        for recorded in state["containers"]:
            cid = identity(recorded["id"])
            expected = identity(recorded["image"], True)
            check_current(docker, state, container_id, image_id)
            retired = docker.container(cid, missing_ok=True)
            if retired is None:
                continue
            if cid == container_id:
                report["protected_containers"].append(cid)
                continue
            if retired["Image"] != expected or retired["State"]["Status"] not in ("exited", "created"):
                raise SafetyError("Recorded retired container changed or is still running")
            others = docker.containers()
            if container_dependency(others, retired) or docker.bind_dependency(others, retired):
                raise SafetyError("Another container depends on a retired container")
            check_current(docker, state, container_id, image_id)
            if not dry_run:
                docker.run(["container", "rm", cid])
            report["removed_containers"].append(cid)
            persist()
        for recorded in state["images"]:
            iid = identity(recorded, True)
            check_current(docker, state, container_id, image_id)
            simulated = set(report["removed_containers"]) if dry_run else set()
            rows = [c for c in docker.containers() if c["Id"] not in simulated]
            protected = image_ancestors(docker.images(), {c["Image"] for c in rows})
            if iid in protected:
                report["protected_images"].append(iid)
                continue
            old = docker.image(iid, missing_ok=True)
            if old is None:
                continue
            tags = set(old.get("RepoTags") or [])
            if any(c.get("Config", {}).get("Image") in tags for c in rows):
                report["protected_images"].append(iid)
                continue
            check_current(docker, state, container_id, image_id)
            if any(c["Image"] == iid for c in docker.containers() if c["Id"] not in simulated):
                raise SafetyError("Image became referenced during cleanup")
            # Delete by immutable ID only. Never resolve a moving tag for a
            # destructive call. Native Docker refuses shared/multi-tag images;
            # report that blocker instead of forcing or broadly pruning them.
            if not dry_run and docker.image(iid, missing_ok=True) is not None:
                docker.run(["image", "rm", iid])
                if docker.image(iid, missing_ok=True) is not None:
                    raise SafetyError("Retired image remains after removal")
            report["removed_images"].append(iid)
            persist()
        check_current(docker, state, container_id, image_id)
    except (SafetyError, subprocess.SubprocessError) as error:
        report["status"] = "blocked"
        report["errors"].append(str(error))
        persist()
        raise SafetyError(str(error)) from error
    state["status"] = report["status"]
    persist()
    return report


def write_state(path, state, create=False):
    path = Path(path)
    if path.is_symlink():
        raise SafetyError("Journal must not be a symlink")
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    data = json.dumps(state, indent=2) + "\n"
    if create:
        fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        with os.fdopen(fd, "w") as file:
            file.write(data)
        return
    fd, temporary = tempfile.mkstemp(dir=path.parent, prefix=".deployment-")
    try:
        with os.fdopen(fd, "w") as file:
            file.write(data)
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


@contextmanager
def journal_lock(path):
    """Concurrent callers must not lose each other's tracked resource IDs."""
    lock = Path(str(path) + ".lock")
    try:
        fd = os.open(lock, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    except FileExistsError as error:
        raise SafetyError("Deployment journal is locked; inspect the owner before retrying") from error
    try:
        with os.fdopen(fd, "w") as file:
            file.write(str(os.getpid()))
        yield
    finally:
        lock.unlink()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ssh-host", help="SSH host alias; omit for local Docker")
    sub = parser.add_subparsers(dest="command", required=True)
    begin = sub.add_parser("capture")
    begin.add_argument("--service", required=True)
    begin.add_argument("--state", type=Path, required=True)
    record = sub.add_parser("track")
    record.add_argument("--state", type=Path, required=True)
    record.add_argument("--container-id")
    record.add_argument("--image-id")
    end = sub.add_parser("finish")
    end.add_argument("--state", type=Path, required=True)
    end.add_argument("--container-id", required=True)
    end.add_argument("--image-id", required=True)
    end.add_argument("--verified", action="store_true", help="App/version/data acceptance checks passed")
    end.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    docker = Docker(args.ssh_host)
    if args.command == "capture":
        state = capture(docker, args.service)
        write_state(args.state, state, create=True)
        result = state
    else:
        with journal_lock(args.state):
            state = json.loads(args.state.read_text())
            if state.get("host") != docker.host:
                raise SafetyError("Journal host does not match --ssh-host")
            if args.command == "track":
                if not (args.container_id or args.image_id):
                    raise SafetyError("Track requires an owned container or image ID")
                result = track(docker, state, args.container_id, args.image_id)
                write_state(args.state, state)
            else:
                result = cleanup(docker, state, args.container_id, args.image_id, args.verified,
                                 args.dry_run, save=lambda value: write_state(args.state, value))
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    try:
        main()
    except (SafetyError, OSError, ValueError, subprocess.SubprocessError) as error:
        raise SystemExit("Deployment cleanup stopped: " + str(error))
