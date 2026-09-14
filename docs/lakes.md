# Lake settings and booking providers

Lake Pass Bot separates a destination's rules from the service used to book it.
The lake catalog currently offers **Buntzen Lake**, booked through **Yodel**.
Adding an entry does not by itself establish that another destination works.

## Where settings live

All preferences and resources below belong to the signed-in account, including
when that account is an administrator. Saving personal defaults does not change
another account or the server's deployment configuration.

| Page | Settings and resources |
| --- | --- |
| **Home** | A visual overview of your lake connections, booking requests, and jobs. Open a lake to complete setup or manage its connection. |
| **OTP sources** | BlueBubbles or Twilio configuration, connection checks, pairing, and the default source for newly queued jobs. |
| **Settings** | Preparation and sign-in deadlines, availability check window, retry delays, and final confirmation preferences shared across lakes; browser defaults for new sign-ins; account management. |
| **Lakes → a lake** | Connection status, provider sign-in setup, the account to use for bookings, vehicle keyword, release schedule, default pass preferences, and booking URLs. For Buntzen Lake, the connection uses Yodel. |
| **Bookings** | A Book action for each ready lake, followed by a visit date and pass choices. |
| **Jobs** | Queued and completed booking attempts, progress, approval, cancellation, and retained history. |

Provider sign-ins are managed from their lake page. Buntzen Lake contains its
Yodel connection, including sign-in names, mobile numbers, enabled state, and
the action to connect. Home presents lake status and links back to that setup;
it does not expose Yodel account management as a global task. Accounts without
a configured lake connection are directed to Lakes.

The sole enabled account connected to a lake is used automatically. When several
accounts are available, select **Use for bookings** on one account's card on the
lake page. Its card then shows **Used for bookings**. A disabled preferred
account requires attention; it does not silently switch to another identity.
Accounts must belong to the signed-in user and the selected lake and provider.
Sharing a provider does not connect an account to another lake automatically.

Existing sign-in IDs and credentials remain separate; the app does not merge
identities or reset existing sessions. The sign-in form edits only the name,
mobile number, and enabled state. New sign-ins receive an internally managed
approved login URL and the account's browser defaults. Editing an existing sign-in preserves its saved
browser and login configuration. Saving an older sign-in fills a missing login
URL from the approved provider defaults and clears any retired executable-path
override. If its mobile number was not migrated, re-enter the number to enable it.

OTP sources are independent account resources. The first source becomes the
default; choose **Make default** on another source to change it. Newly queued
jobs capture that source, and subsequent preference changes do not reroute jobs
already queued. Multiple Yodel sign-ins may use the same owned source. Browser
profile and inbox locks still serialize work on shared resources, and sources
cannot be selected from another account. Legacy callers without an account
preference retain their existing source association.

## Booking a visit

Set up the connection and vehicle on the lake page, then open **Bookings** and
choose **Book** for that lake. The form asks only for the visit date and pass
preferences. It shows the selected account for context and links to lake setup;
it has no account selector, vehicle override, or advanced release and URL fields.

Pressing **Book** creates a job and opens its page. Before the pass release,
the job follows the configured preparation and release window. After release,
it starts as soon as a worker is available and always requires manual approval.
Future release jobs use **Settings → Booking confirmation**, which defaults to
manual approval and can be changed to automatic confirmation. The new flow
does not create another reusable preset or saved request to maintain.

## Defaults and existing bookings

Lake defaults include the vehicle keyword, local timezone, release time and
number of days before the visit, pass preference order, and pass URLs.
Preparation, retry timing, and final confirmation preferences belong to the
account's global **Settings**. Supported pass types and provider identity remain
defined by the catalog. Custom URLs must still use an operator-approved origin.

New sign-ins copy the account's browser defaults. Each new booking captures the
current lake and account defaults together with the chosen date and passes.
Saving or resetting preferences never rewrites existing sign-ins or queued jobs. **Reset saved preferences** restores the lake defaults
while preserving the chosen booking account and global settings.

The old saved-request UI and automatic queueing have been removed. Upgrades
disable legacy scheduling flags while retaining job inputs, job history, and
reservation records. Existing queued jobs still run at their saved time and can
be cancelled from Jobs. `SCHEDULES_ENABLED` is no longer used. Each new visit
starts with **Book** and captures current settings.

Advanced CLI checks remain available for existing request IDs; see
[Common commands](../README.md#common-commands).

## Current boundaries

- `internal/destinations/catalog.go` defines the lake catalog and resolves stable
  lake IDs. `internal/destinations/buntzen.go` owns this lake's URLs, supported
  passes, local timezone, and release defaults.
- Sign-ins persist their lake and provider identity. Lake settings store an
  optional preferred `booking_profile_id`, protected by ownership and lake
  checks. An explicitly selected account cannot be deleted until that choice
  is deliberately changed or cleared. Account and lake settings are scoped by
  `user_id`; legacy profile vehicle fields remain for migration compatibility.
- New booking actions atomically save a `kind = 'snapshot'` booking request and
  its job. Legacy requests retain `kind = 'saved'`. Snapshots capture the lake,
  account, vehicle, date, pass order, release rules, and timing; they are cleaned
  up with their final retained job. Reservation records remain independent of
  that cleanup. The engine dispatches the saved lake and provider to the worker,
  and queued jobs use the captured release policy.
- `actions/src/lake_pass_actions/lakes/` defines destination-specific pass labels
  and matching rules. Its Buntzen module owns those choices.
- `actions/src/lake_pass_actions/providers/yodel/` owns Yodel browser behavior:
  authentication, calendar selection, vehicles, cart checks, and confirmation.
  The worker selects an allowlisted provider and rejects mismatched lake/provider
  combinations before browser work.

Unknown explicit lake IDs are rejected. Missing IDs are accepted only through the
compatibility path for records and callers predating lake selection. Sign-ins
currently represent Yodel identities; the engine rejects a destination using a
provider incompatible with that sign-in. Sign-in jobs operate without a booking
request and cannot reserve a pass. New sign-in forms accept only catalog lake IDs
that match the sign-in provider; return links are generated from that catalog
context. The compatibility `/profiles` index redirects to Lakes.

## Adding a lake using Yodel

1. Add a Go catalog entry with a stable ID, visible name, provider ID, URL defaults,
   supported passes, and release rules. Register it in the catalog list.
2. Add a corresponding Python lake definition and register it in the worker
   catalog, keeping IDs and pass keys consistent across both processes.
3. Confirm sign-in compatibility and approved provider origins. The lake
   catalog must not bypass the operator's outbound-origin policy.
4. Exercise selection, persistence, release timing, and provider dispatch in
   tests. Use isolated Playwright fixtures for its date/pass/vehicle UI and retain
   the cart, confirmation, duplicate-attempt, and unknown-outcome safeguards.
5. Document support only after checking the real provider flow. A synthetic test
   or successful login is not evidence that a pass was issued.

## Adding a booking provider

Implement a separate provider adapter and register it with the worker. Extend
sign-in configuration, provider policy, engine compatibility checks, and OTP
composition for that provider before enabling it in the catalog. Preserve the
versioned worker protocol and keep provider credentials out of booking requests.

The existing sign-in/date reservation guard remains deliberately conservative.
If future providers permit distinct same-day lake bookings, design and migrate
that rule explicitly; adding a lake must not silently weaken duplicate-booking
protection or allow a retry after an uncertain confirmation.
