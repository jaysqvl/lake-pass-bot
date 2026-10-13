# Working on lake-pass-bot

Follow the repository's contribution and deployment documentation. Keep changes scoped and preserve other work in progress.

## Deployment completion and cleanup

A Docker/NAS deployment is incomplete until application acceptance **and** cleanup have been checked. Read `docs/release-and-deployment.md` and use `scripts/deployment_cleanup.py`; do not invent an unbounded prune command.

- Before replacement, capture the exact running container/image with the cleanup helper. Preserve the actual manager configuration, mounts, ports, database/key state and a matching recovery backup. Record an immutable image digest or retained build artifact outside Docker's writable layers.
- Track only the exact IDs of containers/images created by this deployment or its qualification work. Do not adopt resources merely because their names share a project prefix or because they appeared during a host-wide before/after diff.
- Wait for the Portainer stack operation to settle, then verify the intended image/revision, healthcheck, doctor, UI and persisted requests/jobs/reservations. Use read-only provider checks and the authorized OTP connection test. A healthy container does not prove a completed booking; do not book a pass merely to qualify cleanup.
- Keep the previous container available until these checks pass. After acceptance, stop disposable owned canaries and finish cleanup: remove recorded superseded stopped containers and images with no remaining container references. Running, stopped and Created containers all count as references. Preserve shared images and container filesystem dependencies.
- Cleanup failure is a separate outcome: keep the accepted service running, record the blocker and retry safely. Failed or unverified deployment keeps recovery resources. Never describe a preview or a blocked cleanup as completed.
- Do not create indefinite rollback tags, cutover holders or full model/runtime copies inside container writable layers. Use the captured immutable identity plus protected external backups for recovery after acceptance. Any temporary qualification copy needs an owner and a cleanup step.
- Never use global `docker system/image/container/volume prune`, forced image removal, `rm -v`, or manual Docker/Btrfs storage deletion for routine deployment cleanup. Persistent appdata, models, volumes, keys, backups, VMs and ISOs are outside this cleanup.
- The deployment receipt must identify the accepted image/container, checks performed, IDs actually removed, protected resources and any cleanup blocker. Do not call the deployment fully complete while cleanup remains unreviewed.

## Verification for cleanup changes

Run `python3 -m unittest discover -s scripts -p test_deployment_cleanup.py -v`. Keep the standard-library helper compatible with Python 3.9+. It runs on the client through SSH; Python is not required on the NAS.
