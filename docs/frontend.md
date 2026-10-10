# Frontend development

The application UI lives in `web/frontend`. It uses React 19, TypeScript, Vite,
Tailwind 4, React Router, TanStack Query, Lucide icons, and locally owned
shadcn-style components built on Radix primitives. These are the same core
frameworks and component conventions used by Jotist. Application pages must use
this frontend; do not reintroduce HTMX or Go HTML templates.

## v0.8.0 interface changes and flow compatibility

The migration in [PR #106](https://github.com/jaysqvl/lake-pass-bot/pull/106)
also introduced a visual redesign. React did not require changing the appearance;
the new sidebar, mobile menu, typography, cards, forms, sign-in screen, and job
layout are separate presentation decisions included in that release.

Compared with v0.7.3, the main workflows map as follows:

| Area | Retained behavior | Visible or interaction change |
| --- | --- | --- |
| Navigation | Home, Lakes, Bookings, Jobs, OTP sources, Settings, and account pages remain accessible at their existing browser URLs. | The sidebar order is now Home, Lakes, Bookings, Jobs, OTP sources, Settings. Account access sits at the bottom; small screens use a collapsible menu. |
| Lake setup | Choose a default OTP source, add a lake account, sign in or pair, then save vehicle and booking preferences. Multiple accounts still use an explicit booking account. | Connection and preference cards are restyled; their actions use the same server handlers. |
| Booking | Choose a lake, visit date, and up to three distinct ranked passes; Book creates a job. Existing jobs retain their saved settings when defaults change. | Forms and validation are rendered by React. Validation keeps the submitted draft and focuses its error. |
| Jobs and approval | Live progress, OTP/pairing candidates, booking review, final approval, and cancellation remain separate actions. The engine still enforces admission and approval. | The progress, details, and event panels are restyled. Failed decisions remain visible in the job; uncertain decisions are not retried automatically. |
| Account and administration | Username/password changes, member access, password resets, ownership checks, and deletion rules remain server enforced. | Member deletion first reveals the username confirmation form. It becomes available after the account is disabled and active jobs have finished. |

The Go booking engine, control hub, storage/schema, and Python provider code were
not changed by the UI migration. Automatic queueing and the old saved-request
editor had already been retired before v0.8.0; that was not part of this redesign.

There are technical compatibility changes: JavaScript is now required for the
application UI, and direct HTTP clients must use the `/api` prefix for application
requests. Normal browser links and reloads retain their URLs. The running
container still serves both the UI and API from one Go service.

The follow-up compatibility review restored the dark navy and blue palette from
v0.7.3 while retaining the React layout and workflows. It also restored the
keyboard **Skip to content** link omitted by the redesign and added a browser
check that activates it and verifies focus reaches the main content.

Verification combines frontend/API component tests, the Go web tests, and a
desktop/mobile browser journey using the real Go API with disposable data.
Pairing and final-approval client behavior are covered by component tests; the
browser journey does not start workers or complete a real Yodel checkout. These
checks support workflow compatibility, not a guarantee of successful bookings
against the live provider.

## Visual identity and screenshots

Preserve the app's dark navy surfaces and blue accents when changing frontend
frameworks or components. The shared theme in `src/index.css` uses background
`#10151c`, cards `#171e28`, text `#e8edf4`, and primary blue `#80b8fa`. Status
colours remain semantic: green for success, amber for warnings, and red for
errors. The ticket-and-waves favicon retains its blue identity.

README images must show the current app using synthetic data. After a visible
change, run the isolated browser journey and copy its `workspace.jpg`,
`booking.jpg`, and `jobs.jpg` outputs to `docs/screenshots`. Inspect both desktop
and mobile output before publishing. Do not substitute mockups or real account,
OTP, or reservation data.

## Build and run

Install Node.js 24 and Go 1.27. Run `make build` at the repository root to install
locked frontend dependencies, build assets, and create `bin/lake-pass-bot`.
The Go embed directive reads `internal/web/dist`; generated assets are ignored
by Git. Rebuild the frontend before building a binary after UI changes.

Docker performs the frontend build in a pinned Node stage and copies the output
into the Go build stage. The running image has no Node server. There is no
service worker or offline cache: HTML and API responses use `no-store`, while
hashed Vite assets are immutable.

For hot reload, start the Go service on port 8080, then run `npm run dev` inside
`web/frontend`. Vite proxies `/api` and `/healthz` while preserving the incoming
Host, so the existing browser-origin and CSRF checks continue to apply.

## API and authentication

The Go service owns validation, authorization, booking admission, and durable
state. `/api` prefixes the application routes: for example `/api/lakes/buntzen`
and `/api/jobs/42/decision`. JSON responses carry a resource name (`Page`), its
typed data (`Data`), and the running server's build identity (`Build`). Types in
`src/lib/types.ts` describe these responses. Client routes keep their normal
URLs, including reloads and deep links.

Mutations use POST with URL-encoded fields, the server-provided CSRF token, and
same-origin cookies. Session and CSRF cookies remain HttpOnly and SameSite
Strict. Keep tokens in memory; never store them in localStorage. Successful form
mutations return an explicit navigation target to the React client, which follows
with a GET and preserves section anchors. Other HTTP clients receive a 303 within
`/api`. Validation errors return the draft with
status 422 and focus the form error. Requests and decisions are never retried
automatically after an uncertain result.

Jobs receive live state and replayable durable events over EventSource. A
decision is confirmed only by a direct 204 from its decision endpoint. Clear
transient OTPs and pairing candidates on disconnect, completion, session
expiry, navigation, and when the page is hidden. Keep booking review details and
uncertain-outcome guidance visible before final approval or a manual retry.

## Verification

Inside `web/frontend`, run `npm test`, `npm run build`, and
`npm audit --audit-level=moderate`. Install Chromium with
`npx playwright install chromium`, then run `npm run test:browser`.

The browser suite starts `scripts/ui-fixture` on loopback port 18092 with a
temporary database and a deterministic test setup token. This command never
starts the job engine, so queued jobs cannot launch Python workers or contact a
booking provider. It tests setup, write-only credentials, booking validation and
snapshots, cancellation over SSE, network settings, mobile navigation, password
replacement, and account isolation. Screenshots and failure traces are written
to the ignored `web/frontend/test-results` directory.

Run the Go, Python, release, and provider browser checks described in
`CONTRIBUTING.md` for changes crossing those boundaries. The container HTTP
smoke checks the JSON API, hardened cookies, persisted network settings, server
build identity, and embedded asset delivery. Synthetic tests do not prove that
a live Yodel booking succeeded.

CI uses the pinned `actions/setup-node` commit in `.github/workflows/ci.yml` to
provide Node.js 24. Repositories with a selected-action allowlist must permit
that exact commit. Keep the full-SHA pinning requirement and update the allowlist
entry when upgrading the action.
