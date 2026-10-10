# Production readiness and ordered backlog

Assessment date: 2026-10-10 UTC. This is a bounded review of the current source,
tests, GitHub controls, release evidence, and one existing installation. It is
not a certification or an exhaustive bug/security audit.

Lake Pass Bot has a substantial tested foundation for a trusted, self-hosted
installation. It is not yet proven for unattended live booking or accepted for
general enterprise deployment. Successful login, synthetic checkout tests, and
container health do not establish that the provider issued a pass.

## Verified foundation

| Area | Evidence and boundary |
| --- | --- |
| CI | Five required checks: Go, frontend, Python, browser integration, and Docker. Checks include race tests, browser journeys, dependency audits, image smoke, and HIGH/CRITICAL image vulnerability rejection. See [CI](../.github/workflows/ci.yml). |
| Branch protection | At review time, `main` required an up-to-date PR and those five GitHub Actions checks, including for administrators. Required approving reviews were **zero**; independent human review was not enforced. |
| Image publication | The publisher validates the release tag/source SHA, builds `linux/amd64`, smoke-tests the exact digest, checks vulnerabilities including unfixed findings, creates and verifies signed provenance/SBOM/scan attestations, and verifies version/commit/latest promotion. [Publication workflow](../.github/workflows/release-image.yml). |
| Deployment | Installation is operator-triggered. Publishing an image does not deploy it. Manual Portainer deployment is an intentional supported model; a GitHub deploy runner or automatic deployment is not required. [Deployment procedure](release-and-deployment.md). |
| Booking safeguards | Tests exercise atomic duplicate admission, durable reservations, manual approval, cancellation, interrupted-job recovery, and ambiguous confirmation becoming `outcome_unknown`. Integration uses the real coordinator, worker and Chromium with fake provider servers. [Integration boundaries](../integration/README.md). |
| Accounts and secrets | Ownership guards, CSRF/origin checks, session revocation, encrypted credential storage, redacted logging, and public HTTPS mode exist. Their explicit limits include no application MFA, a shared trusted worker runtime, and a default key beside the database. [Security scope](../SECURITY.md). |
| UI | Desktop/mobile browser coverage exercises setup, lake configuration, booking snapshots, cancellation, settings, password replacement and member isolation. The blue palette and screenshots are restored. This is not coverage of every failure and accessibility case. [Frontend verification](frontend.md). |
| Development | `LAKE_PASS_DEV_AUTO_LOGIN=true` skips interactive login only in the disposable fixture. Restart/session recovery and CSRF rejection are tested. Production authentication is unchanged. |

The issue inventory returned no open issues. One dependency PR, [#104](https://github.com/jaysqvl/lake-pass-bot/pull/104),
remained open at review time. Neither an empty issue list nor absence of source
TODO markers establishes absence of defects. The older maintainability review
explicitly covers the 0.6.2 baseline, not all later changes.

## Delivery verification

The current blue UI update is being released as 0.8.1. Its deployment receipt
must record the published source SHA, accepted registry digest, actual running
image/version, health response, and comparison of preserved runtime/data/key
state. A merged PR or published tag alone does not complete this task.

Development previews must open without a login when demonstrating changes. The
production installation must continue to require authentication. Use separate
synthetic data and never load production profiles into the fixture.

## Backlog in dependency order

Each task needs an owner and evidence attached before it is marked complete.
The acceptance criteria below are proposed release criteria, not claims that
the work has already happened. Do not run live reservations or disruptive
production drills without choosing their target and window deliberately.

### LP-001: Prove a real immediate checkout

**Priority: blocks claims of proven live booking.** Follow [live testing](live-testing.md)
with manual approval and an intentionally selected available date/pass/vehicle.
Record the app version, job outcome and provider confirmation matching those
inputs. Inspect the provider wallet after any uncertain outcome before retrying.
Cancel an unused test pass through the provider. Keep credentials, codes and
personal confirmation details out of public artifacts.

Done when one deliberate live checkout produces a matching issued pass and the
application reports the same result. Login and dry run are insufficient.

### LP-002: Prove release-time operation separately

**Priority: blocks claims of proven scheduled booking; depends on LP-001.**
Exercise session preparation, OTP timing, release polling, ranked fallback
passes and final approval at a real release window. Record clock/timezone,
release timing and actual provider inventory. A sold-out result can validate
safe behavior but does not prove successful release-time reservation.

Done when a selected live release attempt has an explainable result, correct
timing and no duplicate reservation; a successful pass must match the job.

### LP-003: Demonstrate recovery and backup restoration

**Priority: before relying on the installation unattended.** Define recovery
time and acceptable data loss targets. Automate private backups of consistent
SQLite state, profiles and matching keys, with off-host retention and a separately
protected key copy. Restore into an isolated installation with workers disabled.
Test supported upgrade and rollback procedures against disposable state.

In an isolated deployment, exercise process termination before/after confirmation,
provider timeout, expired login/OTP, unavailable database and disk exhaustion.
Preserve unknown-outcome reservations and require provider reconciliation;
never automatically repeat a possibly completed confirmation.

Done when a restore drill verifies decryption, records, profile state and the
agreed recovery targets, and fault drills demonstrate the documented outcomes.
Existing unit/integration recovery tests remain valuable but do not replace this
installation-level evidence.

### LP-004: Add operational visibility and actionable alerts

**Priority: before unattended production.** Structured redacted logs and durable
job events exist. `/healthz` checks database availability; it is not proof of
provider connectivity, worker readiness or successful bookings. No operational
metrics/alert integration was found in the reviewed application surfaces.

Add bounded counters/timing and alerts for failed or unknown bookings, missed
release windows, unavailable OTP/provider connections, worker failures, storage
pressure and unhealthy/stopped service. Keep user identifiers, OTPs and credentials
out of metric labels and alerts. Define a small incident/runbook procedure.

Done when injected failures produce useful alerts and an operator can identify
the affected job and next action without exposing secrets or requiring debug logs.

### LP-005: Complete UI and provider compatibility regression coverage

**Priority: before a wider pilot.** Extend the tested matrix to expired sessions,
duplicate submissions, missing resources, API/network outages, validation drafts,
reconnects, mobile access and keyboard/screen-reader workflows. Every recoverable
failure should retain a useful app page and a clear next action. Document material
UI/flow changes and refresh synthetic screenshots with each visual release.

Add a repeatable, non-reserving live provider compatibility check and an adapter
change procedure. Synthetic markup fixtures do not establish compatibility with
future provider markup. Verify first-run installation on a clean supported host;
state `linux/amd64` support explicitly. Add other architectures only when tested.

Done when the agreed regression matrix passes and live compatibility failures
are visible before a release-time booking is silently missed.

### LP-006: Establish the enterprise deployment/access contract

**Priority: before enterprise acceptance; requirements depend on deployment.**
Decide whether this remains a trusted single-organization tool or must support
hostile tenants. The present shared UID/appdata/worker trust model must not be
represented as tenant isolation. Define the deployment's TLS/network restrictions,
SSO/MFA requirements, access lifecycle, key custody, secret rotation, retention
and privacy requirements. Add durable administrative/security audit records
separate from booking progress where required.

Enforce independent PR review for consequential changes, define dependency
triage/support/security response owners, and review the pending dependency update.
Make approved-image identity and attestation verification part of installation
receipts; ordinary Portainer updates do not perform attestation verification.
Document provider-automation suitability, license/dependency review and support
expectations with the adopting organization.

Done when the intended organization accepts the documented trust model and the
required controls have been implemented and tested. SSO, multi-tenant isolation,
HA and additional architecture support should follow concrete requirements;
they are not automatic prerequisites for every private installation.

## Practical release decision

A controlled private pilot can proceed with manual approval, current backups,
visible job monitoring and the documented trust boundaries. Broader unattended
use should wait for LP-001 through LP-004. Enterprise acceptance additionally
needs the regression evidence and agreed controls in LP-005/LP-006. Manual
deployment can remain the chosen CD model throughout.
