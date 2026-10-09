# Frontend development

The application UI lives in `web/frontend`. It uses React 19, TypeScript, Vite,
Tailwind 4, React Router, TanStack Query, Lucide icons, and locally owned
shadcn-style components built on Radix primitives. These are the same core
frameworks and component conventions used by Jotist. Application pages must use
this frontend; do not reintroduce HTMX or Go HTML templates.

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
