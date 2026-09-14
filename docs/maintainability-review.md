# Engineering review — September 13, 2026

This review covers the 0.6.2 baseline and the audit changes described below.
Later releases replaced saved-request editing and automatic queueing with
per-visit jobs. See [the current booking flow](lakes.md#booking-a-visit) and
[development guidelines](../CONTRIBUTING.md) for the resulting lifecycle; this
review does not establish the quality of those later changes.

This review examined the application, tests, and build/deployment tooling at
`063f68260d06fb4933735e4415f6a05cd20c5336` (0.6.2), including the recent lake and
account settings work. It rechecked the September 5 review against current code.
The concerns in [this discussion of generated code](https://news.ycombinator.com/item?id=49654229)
informed the review: readable contracts, ownership of state, useful failure tests,
and accidental coupling mattered more than line counts or replacement frameworks.

## Findings addressed

| Area | Problem and change |
| --- | --- |
| Settings validation | Lake defaults were validated through a fictional booking with placeholder identity, date, and vehicle. Lake settings now validate their own fields. Bookings share that policy while retaining required identity and vehicle checks. Account and booking preparation timing use one small private policy value. |
| Command-line startup | Unknown commands initialized directories, an encryption key, and SQLite before returning usage. Some commands silently accepted extra arguments. Command and flag parsing now finish before runtime initialization; malformed commands leave appdata untouched. |
| Worker coordination | The frame dispatcher accepted twelve arguments, including output pointers and a mutex around a map owned by one event loop. A private run state now owns challenges, replies, and confirmation state. Provider goroutines publish replies through a channel; the loop remains the sole state owner. |
| Status privacy | The redaction expression consumed delimiters and could miss the second of two adjacent OTP codes. Redaction now processes complete digit runs. A regression exercises the returned result, durable event hook, and live status stream through the coordinator. |
| Browser recovery | Routine helpers caught every exception, hiding programming errors as missing controls or unavailable passes. They now recover from Playwright failures specifically. Cleanup and the uncertain final-confirmation boundary retain broader handling. Generator-throw lambdas were replaced with named functions or explicit test side effects. |
| Browser fixtures | Synthetic provider pages were embedded in Go, including dense markup and occurrence-count string replacements. Readable HTML templates now use named values for vehicle labels, cart quantity, and privacy scenarios. The real browser scenarios remain intact. |
| Web forms | Two templates separately implemented the same input/select/checkbox policy. They now share one readable form-controls partial. Advanced lake settings use an explicit section flag instead of depending on a heading's display text. |
| Container verification | Tests extracted Bash functions with regular expressions, rebuilt source strings, and substituted a fake Docker function. HTTP smoke behavior now lives in an ordinary Python module with direct HTTP fixture tests. Bash retains container setup, resource and browser checks, key relocation, and restart orchestration. |
| Release tooling and dead code | Release metadata is parsed as TOML; only the updater's comment annotation needs a textual check. Removed unused store entry points and corrected the deployment variable count in the migration guide. |

The CLI, adjacent-code, and swallowed-programming-error regressions reproduced
the failures against the previous implementation. An isolated native-app run of
the extracted HTTP smoke helper also caught a test-client bug: hostname probes
were accidentally authenticated. Those probes now explicitly omit cookies, with
a regression covering that distinction.

## Boundaries retained

The review covered configuration and migration compatibility; domain values and
destination dispatch; engine scheduling and maintenance; store admission,
ownership, leases, and recovery; worker coordination and subprocess transport;
OTP adapters, authentication, encryption, egress, and locking; web routes, forms,
account/session handling, and event streams; the Python provider lifecycle;
browser fixtures; Docker/Compose, CI, release scripts, and their tests.

Several existing choices remain appropriate:

- Reservation uniqueness and job admission belong in SQLite transactions and
  constraints. An in-memory scan cannot replace that authority. Reservations
  must survive uncertain confirmation outcomes and retained-job cleanup.
- The pairing-history pre-check remains for older booking-linked pairing jobs.
  The newer unique index covers profile-only pairing jobs; treating that index
  as a complete replacement would change legacy admission behavior.
- Go owns credentials and durable state. The Python worker receives bounded
  protocol messages and owns one browser session. Yodel login, release waiting,
  reauthentication, and trace suspension share that session's lifecycle;
  splitting them solely to shorten a file would obscure it.
- The two OTP adapters retain their distinct pagination semantics. Egress policy
  and message matching are the useful common boundaries.
- Stream admission, write deadlines, cancellation deadlines, and the durable
  confirmation barrier address concrete failures and remain explicit.
- The lake catalogue contains destination policy. Per-account lake defaults,
  global preparation/browser defaults, and the default OTP source have separate
  owners. Saved jobs and booking requests retain their existing semantics.
- Hostname checks remain an optional private-hosting restriction. CSRF/session
  validation and the explicit public-HTTPS boundary remain independent.

No generic repository layer, dependency-injection framework, browser inheritance
hierarchy, or new database schema was introduced. Deployment topology, existing
data, and preserved pre-rebrand history are unchanged by this review.

## Verification and limits

| Check | What it establishes |
| --- | --- |
| Go vet and full race suite | Policy, HTTP behavior, persistence, concurrency, and subprocess regressions. |
| Locked Python unit suite and pinned Ruff | Worker/configuration/error handling regressions and static checks. |
| Go integration suite with real Chromium and race detection | Go-to-worker-to-browser behavior against the synthetic provider: approval/cancellation, false-confirmation rejection, cart/vehicle errors, OTP, and privacy. |
| Executed client event tests and rendered form tests | Form metadata, event handling, and request behavior. These are not a visual redesign review. |
| Release tests and isolated native-app HTTP smoke | Form parsing, setup/login, session and Network-setting persistence across a process restart. |
| CI Docker job | Built Linux container, Compose contracts, resource limits, browser operation, encrypted state, key relocation, and volume/bind persistence through recreation. Local HTTP fixtures do not substitute for this job. |

The commands are in [the development guidelines](../CONTRIBUTING.md) and
[browser integration guide](../integration/README.md). CI results on the reviewed
commit are the evidence for the built container; an image build or native HTTP
check alone is insufficient.

Passing synthetic integration does not establish that a live Yodel reservation
was issued. This review performs no live booking or production data migration.
The implementation is easier to inspect and has stronger failure tests; that is
a bounded engineering result, not a certification that no defects remain.

Review committed test fixtures and scripts with the same readability standards
as application code, and verify behavior at the boundary actually being changed.
