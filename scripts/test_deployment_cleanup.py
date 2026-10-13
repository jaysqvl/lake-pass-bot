"""Destructive-operation boundary tests using a stateful Docker model."""

import copy
import json
import os
import stat
import tempfile
import unittest
from pathlib import Path

from deployment_cleanup import SafetyError, capture, cleanup, journal_lock, track, write_state


OLD = "a" * 64
NEW = "b" * 64
OTHER = "c" * 64
OLD_IMAGE = "sha256:" + "1" * 64
NEW_IMAGE = "sha256:" + "2" * 64
OTHER_IMAGE = "sha256:" + "3" * 64


def container(cid, image, name, status="running", health="healthy"):
    state = {"Status": status}
    if health is not None:
        state["Health"] = {"Status": health}
    return {"Id": cid, "Image": image, "Name": "/" + name,
            "State": state, "Config": {"Image": image}, "HostConfig": {}, "Mounts": []}


class ModelDocker:
    host = None

    def __init__(self):
        self.rows = {OLD: container(OLD, OLD_IMAGE, "service")}
        self.image_rows = {OLD_IMAGE: {"Id": OLD_IMAGE, "RepoTags": ["app:old"], "RepoDigests": ["app@" + OLD_IMAGE]},
                           NEW_IMAGE: {"Id": NEW_IMAGE, "RepoTags": ["app:new"]},
                           OTHER_IMAGE: {"Id": OTHER_IMAGE, "RepoTags": ["unrelated:old"]}}
        self.calls = []
        self.shared_root = False
        self.fail_image = False
        self.swap_on_list = False

    def container(self, ref, missing_ok=False):
        result = self.rows.get(ref) or next((c for c in self.rows.values() if c["Name"] == "/" + ref), None)
        if result is None and not missing_ok:
            raise SafetyError("Missing container")
        return copy.deepcopy(result)

    def containers(self):
        if self.swap_on_list:
            self.rows[NEW]["Image"] = OTHER_IMAGE
            self.swap_on_list = False
        return copy.deepcopy(list(self.rows.values()))

    def image(self, ref, missing_ok=False):
        result = self.image_rows.get(ref)
        if result is None and not missing_ok:
            raise SafetyError("Missing image")
        return copy.deepcopy(result)

    def images(self):
        return copy.deepcopy(list(self.image_rows.values()))

    def bind_dependency(self, rows, retired):
        return self.shared_root

    def run(self, args):
        self.calls.append(args)
        if args[:2] == ["container", "rm"]:
            self.rows.pop(args[2])
        elif args[:2] == ["image", "rm"]:
            iid = args[2]
            if self.fail_image:
                raise SafetyError("native image removal refused multiple references")
            if any(c["Image"] == iid for c in self.rows.values()):
                raise AssertionError("Attempt to delete referenced image")
            self.image_rows.pop(iid)
        else:
            raise AssertionError("Unexpected destructive operation: " + repr(args))
        return ""


class CleanupTests(unittest.TestCase):
    def setUp(self):
        self.docker = ModelDocker()
        self.state = capture(self.docker, "service")
        self.docker.rows[OLD]["Name"] = "/service-rollback"
        self.docker.rows[OLD]["State"]["Status"] = "exited"
        self.docker.rows[NEW] = container(NEW, NEW_IMAGE, "service")

    def finish(self, **options):
        return cleanup(self.docker, self.state, NEW, NEW_IMAGE, verified=True, **options)

    def test_success_removes_only_recorded_retired_resources(self):
        self.docker.rows[OTHER] = container(OTHER, OTHER_IMAGE, "other", "created")
        report = self.finish()
        self.assertEqual(report["status"], "complete")
        self.assertNotIn(OLD, self.docker.rows)
        self.assertNotIn(OLD_IMAGE, self.docker.image_rows)
        self.assertEqual(set(self.docker.rows), {NEW, OTHER})
        self.assertEqual(self.docker.calls, [["container", "rm", OLD], ["image", "rm", OLD_IMAGE]])

    def test_acceptance_marker_is_required(self):
        with self.assertRaisesRegex(SafetyError, "acceptance"):
            cleanup(self.docker, self.state, NEW, NEW_IMAGE)
        self.assertEqual(self.docker.calls, [])

    def test_unhealthy_or_wrong_current_identity_never_deletes(self):
        for change in ("unhealthy", "starting", "exited", "wrong-image", "wrong-id"):
            with self.subTest(change=change):
                docker = copy.deepcopy(self.docker)
                if change in ("unhealthy", "starting"):
                    docker.rows[NEW]["State"]["Health"]["Status"] = change
                elif change == "exited":
                    docker.rows[NEW]["State"]["Status"] = change
                elif change == "wrong-image":
                    docker.rows[NEW]["Image"] = OTHER_IMAGE
                else:
                    docker.rows[NEW]["Id"] = OTHER
                with self.assertRaises(SafetyError):
                    cleanup(docker, copy.deepcopy(self.state), NEW, NEW_IMAGE, verified=True)
                self.assertEqual(docker.calls, [])

    def test_still_running_retired_container_is_not_stopped_or_removed(self):
        self.docker.rows[OLD]["State"]["Status"] = "running"
        with self.assertRaisesRegex(SafetyError, "still running"):
            self.finish()
        self.assertEqual(self.docker.calls, [])

    def test_created_or_exited_other_container_protects_its_image(self):
        for state in ("created", "exited", "running"):
            with self.subTest(state=state):
                docker = copy.deepcopy(self.docker)
                docker.rows[OTHER] = container(OTHER, OLD_IMAGE, "other", state)
                result = cleanup(docker, copy.deepcopy(self.state), NEW, NEW_IMAGE, verified=True)
                self.assertIn(OLD_IMAGE, result["protected_images"])
                self.assertIn(OLD_IMAGE, docker.image_rows)
                self.assertIn(OTHER, docker.rows)

    def test_other_container_ancestor_image_is_preserved(self):
        self.docker.image_rows[NEW_IMAGE]["Parent"] = OLD_IMAGE
        self.assertIn(OLD_IMAGE, self.finish()["protected_images"])
        self.assertIn(OLD_IMAGE, self.docker.image_rows)

    def test_configured_tag_and_shared_bind_root_are_protected(self):
        self.docker.rows[NEW]["Config"]["Image"] = "app:old"
        self.assertIn(OLD_IMAGE, self.finish()["protected_images"])
        self.assertIn(OLD_IMAGE, self.docker.image_rows)
        self.setUp()
        self.docker.shared_root = True
        with self.assertRaisesRegex(SafetyError, "depends"):
            self.finish()
        self.assertEqual(self.docker.calls, [])

    def test_namespace_and_volumes_from_dependencies_block_removal(self):
        for config in ({"VolumesFrom": [OLD + ":ro"]}, {"NetworkMode": "container:" + OLD}, {"IpcMode": "container:service-rollback"}):
            with self.subTest(config=config):
                docker = copy.deepcopy(self.docker)
                docker.rows[NEW]["HostConfig"] = config
                with self.assertRaisesRegex(SafetyError, "depends"):
                    cleanup(docker, copy.deepcopy(self.state), NEW, NEW_IMAGE, verified=True)
                self.assertEqual(docker.calls, [])

    def test_resource_tracking_does_not_adopt_unrelated_host_images(self):
        track(self.docker, self.state, image_id=NEW_IMAGE)
        self.finish()
        self.assertIn(OTHER_IMAGE, self.docker.image_rows)
        self.assertIn(NEW_IMAGE, self.docker.image_rows)

    def test_replaced_current_service_interrupts_cleanup(self):
        self.docker.swap_on_list = True
        with self.assertRaisesRegex(SafetyError, "identity changed"):
            self.finish()
        self.assertEqual(self.docker.calls, [])

    def test_moved_alias_cannot_cause_removal_of_a_different_image(self):
        self.docker.image_rows[OLD_IMAGE]["RepoTags"] = []
        self.docker.image_rows[NEW_IMAGE]["RepoTags"].append("app:old")
        self.finish()
        self.assertIn(NEW_IMAGE, self.docker.image_rows)
        self.assertIn("app:old", self.docker.image_rows[NEW_IMAGE]["RepoTags"])
        self.assertNotIn(["image", "rm", "app:old"], self.docker.calls)

    def test_partial_cleanup_can_retry_without_rolling_back_current(self):
        self.docker.fail_image = True
        with self.assertRaises(SafetyError):
            self.finish()
        self.assertNotIn(OLD, self.docker.rows)
        self.assertIn(NEW, self.docker.rows)
        self.assertEqual(self.state["cleanup"]["status"], "blocked")
        self.docker.fail_image = False
        self.assertEqual(self.finish()["status"], "complete")

    def test_dry_run_makes_no_mutation_and_includes_now_retired_image(self):
        result = self.finish(dry_run=True)
        self.assertEqual(result["status"], "dry_run")
        self.assertIn(OLD_IMAGE, result["removed_images"])
        self.assertEqual(self.docker.calls, [])
        self.assertIn(OLD, self.docker.rows)

    def test_full_ids_and_matching_host_required(self):
        with self.assertRaisesRegex(SafetyError, "immutable"):
            cleanup(self.docker, self.state, NEW[:12], NEW_IMAGE, verified=True)
        self.state["host"] = "another-host"
        with self.assertRaisesRegex(SafetyError, "host"):
            self.finish()
        self.assertEqual(self.docker.calls, [])

    def test_journal_is_private_and_capture_cannot_overwrite(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "deployment.json"
            write_state(path, self.state, create=True)
            if os.name != "nt":
                self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
            with self.assertRaises(FileExistsError):
                write_state(path, self.state, create=True)
            write_state(path, self.state)
            self.assertEqual(json.loads(path.read_text())["deployment_id"], self.state["deployment_id"])

    def test_concurrent_journal_access_fails_without_losing_resources(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "state.json"
            with journal_lock(path):
                with self.assertRaisesRegex(SafetyError, "locked"):
                    with journal_lock(path):
                        self.fail("Concurrent caller acquired the journal")
            self.assertFalse(Path(str(path) + ".lock").exists())


if __name__ == "__main__":
    unittest.main()
