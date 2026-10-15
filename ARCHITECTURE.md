# Architecture

Amiga Constellation as one Go application with SQLite. It serves the existing
catalogue UI unchanged in look and behaviour, adds public suggestions with +1
votes, and a private owner panel for review and direct catalogue management.
Implements `NewPLAN.md`. No deployment is performed; VPS files are prepared only.

## Stack and key decisions

| Decision | Choice | Why |
|---|---|---|
| Language / HTTP | Go 1.27, `net/http` with 1.22 method patterns, `html/template` | No framework; stdlib covers routing, templates, embedding |
| Database | SQLite via `modernc.org/sqlite` (pure Go), WAL, `foreign_keys=ON`, single writer | cgo-free static Linux binary; one instance, light writes |
| Frontend | Existing vanilla JS split into classic scripts (no bundler, no ESM); CSS custom-property themes (see Themes) | Keeps the `vm`-based layout tests and zero build step |
| Auth | Argon2id password + TOTP (`pquerna/otp`), server-side sessions, hashed recovery codes | One owner, no external IdP |
| Spam | Cloudflare Turnstile + signed browser id cookie + Origin check + rate limits + honeypot + timing | Layered; Turnstile is skipped only when `APP_ENV != production` and no keys are set |
| Images | Owner-triggered fetch only; validated with stdlib decoders (`image/{jpeg,png,gif}`, `x/image/webp`); stored as original bytes | No re-encoding, no ImageMagick; SSRF guarded by DNS + redirect checks |
| Catalogue cache | In-memory JSON bytes + sha256 ETag, rebuilt after any publish | Filtering and layout stay in the browser; no polling |
| Seed | `data/amiga-data.json` + `images/` import once into an empty DB; never re-applied | SQLite is the live source of truth after init |
| Identity of entries | Entry `id` slugs preserved; each entry row stores the full entry object as JSON (`doc`) | Preserves every content field verbatim; vocab-validated in Go |

## Layout

```
cmd/amiga/main.go            CLI: serve | import | export | owner init|reset|recovery|unlock | backup
internal/config              env parsing (AMIGA_* vars), defaults, production checks
internal/store               sqlite open, migrations, typed queries (one package, several files)
internal/catalog             entry model + validation against meta vocab, seed import, export, cache/ETag
internal/auth                owner account, argon2id, TOTP, sessions, login throttle, recovery codes
internal/suggest             suggestion types, diff/snapshot logic, votes, duplicate detection, rate limits
internal/images              safe fetcher (SSRF guards, limits), staging, publish to media dir
internal/turnstile           siteverify client (hostname/action checks), fail-closed policy
internal/web                 http server: router, middleware, handlers (public, suggestions, admin), templates
web/static/js/*.js           data.js, catalog.js, detail.js, lineage.js, suggest.js, admin.js (load order matters)
web/static/css/styles.css    design system (Museum light/dark tokens)
web/static/css/workbench.css Workbench Mode token and component overrides
web/templates/*.html         index (SPA shell), suggestions list/new/detail, admin pages
data/amiga-data.json         seed content (read once)
images/                      seed images (copied once into the media dir)
tests/lineage-layout.cjs     existing 40 layout scenarios, now loading the split scripts
deploy/                      systemd unit, env example, reverse-proxy notes, backup script
```

Runtime data lives in `AMIGA_DATA_DIR` (default `./runtime`, git-ignored):
`amiga.db`, `media/` (served at `/images/`), `staging/` (fetched but unpublished images).

Constructors are explicit: `store.Open(path)`, `catalog.New(store)`,
`auth.New(store, cfg)`, `images.New(cfg, store)`, `web.New(cfg, store, catalog, logger) (*Server, error)`. No globals, no DI container.

In development a missing `AMIGA_SECRET` is generated once into `<data_dir>/secret`; production requires `AMIGA_SECRET` and `AMIGA_BASE_URL`.

## Data model (SQLite)

- `entries(id PK, doc JSON, rev INTEGER, ord INTEGER, deleted_at, created_at, updated_at)`. `doc` is the entry object exactly as in the seed (compacted, key order kept). `ord` is the document position so export reproduces seed order (an unchanged export is byte-identical to `data/amiga-data.json`). Lineage links are `doc.related` (ancestors); no separate link table.
- `entry_revisions(id, entry_id, rev, before JSON, after JSON, actor, reason, suggestion_id, created_at)`, unique `(entry_id, rev)`. One row per publication, including deletes and restores; `before` is NULL when the entry was absent (create, restore), `after` is NULL for a delete.
- `catalog_meta(key PK, doc JSON)` for `meta`, `domains`, `tours`, `_readme`. Edited via JSON import/export only in v1.
- `suggestions(id, type, entry_id, title, summary, payload JSON, snapshot JSON, base_rev, reason, sources JSON, contact, status, duplicate_of, decision_note, internal_note, final JSON, voter_id, ip_hash, created_at, decided_at)`.
  Types: `new`, `change`, `remove`, `image`. Statuses: `pending`, `accepted`, `accepted_edited`, `rejected`, `duplicate`, `hidden`.
  `payload` holds only changed fields for `change`; `snapshot` is the entry at proposal time; `base_rev` detects staleness.
- `votes(suggestion_id, voter_id, created_at, PK(suggestion_id, voter_id))`; counts are `COUNT(*)`, frozen by refusing changes once status is not pending.
- `moderation_log(id, suggestion_id, entry_id, action, detail JSON, created_at)`.
- `images(id, suggestion_id, entry_id, source_url, source_page, credit, path, content_type, width, height, bytes, sha256, created_at)` — provenance for every published image.
- `owner(id=1, username, password_hash, totp_secret, recovery_codes JSON, created_at)`, `sessions(token_hash PK, created_at, expires_at, last_seen)`, `login_attempts(key, at)`.
- `rate_events(key, kind, at)` pruned on write; `settings(key PK, value)` for `submissions_paused`, `voting_paused`.
- `pageviews(id, at, day, path, visitor, referrer)`, indexed on `day` and `(visitor, at)`; cookie-free analytics, pruned after 400 days.

Schema conventions: STRICT tables, timestamps are INTEGER Unix seconds (UTC), JSON columns are TEXT checked with `json_valid`. Migrations are embedded SQL files (`internal/store/migrations/NNNN_*.sql`) tracked with `PRAGMA user_version`, each applied in its own transaction.

## HTTP surface

Public: `GET /` (SPA shell, rendered once at startup, `no-cache` + ETag), `/static/*` (every template links CSS/JS through the `asset` template function: path + `?v=` + 8 hex of the file's hash, served as the same bytes and cached immutable; bare paths for a day), `/images/*`, `GET /api/catalog` (ETag, 304),
`GET /suggestions`, `/suggestions/new`, `/suggestions/{id}` (server-rendered),
`POST /suggestions/new` (the only way to submit), `GET /api/suggestions` (list with search/filter/sort/page),
`PUT|DELETE /api/suggestions/{id}/vote`, `GET /api/entries/search?q=` (duplicate hints), `GET /healthz`.

Admin (session + CSRF token in forms, Origin checked on every mutation):
`/admin/login`, `/admin` (dashboard), `/admin/queue`, `POST /admin/queue/bulk` (accept / reject / hide selected), `/admin/queue/images` (image triage), `/admin/suggestions/{id}` (current / diff / final preview + decisions:
`POST …/accept` (`edit=1` for Edit & accept), `…/reject`, `…/duplicate`, `…/hide`, `…/preview` (triggers the image fetch)),
`/admin/suggestions/import` (bulk-load proposals as pending), `/admin/staged/{name}` (owner-only staged preview), `/admin/staged/suggestion/{id}` (stages a pending image suggestion on demand, then serves it), `/admin/entries`, `/admin/entries/{id}` (edit, remove with dependency summary, restore),
`/admin/entries/new`, `/admin/stats` (visitors), `/admin/settings` (pauses, hidden spam).

`/api/catalog` returns the same object shape as `data/amiga-data.json` (`meta`, `domains`, `tours`, `entries`),
excluding deleted entries and the `_readme` field guide (kept in exports). `data.js` fetches it before the rest of the UI boots.

## Invariants

- Every publication (accept, direct edit, remove, restore) runs in one transaction: entry update + revision + moderation log + suggestion status, then cache invalidation. Status transitions are guarded by `WHERE status='pending'` so a second click is a no-op.
- A `change` or `remove` proposal whose `base_rev` is older than the entry's current `rev` is flagged stale; the admin must confirm after seeing the newer revision.
- Removing an entry also strips it from other entries' `related`, from tour steps, and from the removal dependency summary shown before confirmation; all affected changes are written in the same transaction.
- Votes: one per `(suggestion, voter_id)`; the signed cookie is the voter id; IP is only stored as a salted hash for rate limiting.
- Image fetch: HTTPS/HTTP only, resolve DNS first and reject private/loopback/link-local ranges, re-check on each redirect (max 3), 8 MB cap, 16 MP cap, one fetch at a time (mutex). Failure leaves the proposal pending.
- Seed import runs only when `entries` is empty. `import --replace` is the explicit, validated way to overwrite; it soft-deletes entries missing from the file and restores deleted ones present in it. A running server must be restarted to see a CLI import (separate process, separate cache).
- Seed images: only image files (`jpg jpeg png gif webp avif`) are copied from `images/` to `media/` (SVG, Markdown and hidden files are skipped); existing files are never overwritten.

### Owner panel and publication

- Publication primitives live on `*catalog.Catalog`: `Create`, `Update`, `Remove`, `Restore`, `RestoreAs`, `Dependencies`. Each loads the whole catalogue inside one IMMEDIATE transaction, applies the change in memory, re-runs full-document `Validate` against the live vocab (so related links and tour steps can never dangle), then writes entries, `entry_revisions` (before/after), `moderation_log` and, for removals, the tours meta; the cache is invalidated after commit. Optional `TxHook`s run inside the same transaction (suggestion review flips its status there; a hook error rolls everything back).
- Optimistic concurrency: `Update`/`Remove` take the rev the caller edited and fail with `*catalog.StaleError` (`errors.Is(err, catalog.ErrStale)`). The edit form re-renders the owner's edits with the current rev, so saving again is an explicit, reviewed overwrite.
- Removal strips the id from every live entry's `related` (each a new revision; key dropped when empty) and from tour steps, in the same transaction. Restore re-publishes the last document; it does not re-add stripped links or tour stops. If the document would dangle, Restore fails with `*ValidationError` and the panel offers `RestoreAs` without the missing links. Edits keep JSON key order and untouched value bytes (`catalog.Patch`). An identical `Update` writes nothing (`Publication.Unchanged`).
- Auth: argon2id (64 MiB, t=3, p=2, PHC string; verification bounded to 256 MiB) with one hash at a time; TOTP SHA1/6/30 s ±1 step, and the last accepted step is stored (`settings.owner_totp_last_step`) so a code, or any older one, is never accepted twice; 10 recovery codes (50 bits, sha256-hashed, burned on use) are accepted in the code field. Unknown users still pay a dummy argon2 verification. Throttle: 5 failures / 15 min per HMAC-hashed IP and 100 globally, checked before hashing; success clears the IP's failures. The IP comes from the same helper as the public side (`visitorIP`: TCP peer, or the last `X-Forwarded-For` hop with `AMIGA_TRUST_PROXY=1`).
- Sessions: 32 random bytes, sha256 stored, cookie `amiga_admin` (HttpOnly, SameSite=Strict, Path=/admin, Secure in production), 12 h idle / 7 d absolute, new token on every login (a presented old one is deleted); password or TOTP reset signs out everywhere. CSRF: hidden `csrf` field = HMAC(secret, session hash), checked on every admin mutation together with `Sec-Fetch-Site` (must be same-origin when sent) and `Origin` (must equal `AMIGA_BASE_URL`, or the request host in development). Login is Origin-checked only. Admin and public mutations share one check (`sameOriginMutation`); only the admin side lets requests with neither header through, since it also requires the CSRF token (or, for login, has no session to ride on). All `/admin` responses carry `Cache-Control: no-store` and `X-Robots-Tag: noindex`; every unknown `/admin/*` path redirects anonymous visitors to the login page.
- Local recovery is the CLI: `owner init` (env `AMIGA_OWNER_USER` / `AMIGA_OWNER_PASSWORD` or prompts), `owner reset-password`, `owner reset-totp`, `owner recovery-codes`, `owner unlock` (clears `login_attempts`).
- Image fetcher (`images.Fetcher`): no environment proxy; the dialer resolves the host itself and dials only a vetted address (all answers must be public; blocks loopback, RFC 1918, CGNAT, link-local and metadata, ULA, multicast, unspecified, documentation, NAT64 and 6to4); redirects are re-vetted by the same dialer plus a scheme/credential check (max 3). Each attempt has a 10 s timeout; HTTP 429 or 503 is retried once after 2 s (or the server's `Retry-After`, capped at 10 s). The User-Agent is `AmigaConstellation-ImageFetcher/1.0 (+AMIGA_BASE_URL)` when the base URL is set (Wikimedia asks for a contact URL). Bytes are sniffed and decoded with `image.DecodeConfig` (format must match the sniffed type). Staged files are `staging/<128-bit hex>.<ext>` (0640); `Publish` re-reads and re-hashes the staged file before renaming it to `media/suggested/<entry>-<sha8>.<ext>`, and records provenance in `images` inside the catalogue transaction (path relative to the media dir; returned as `images/suggested/...`). The panel's "Fetch & attach" checks the rev before downloading.
- Analytics (`internal/web/analytics.go`): a middleware records each 2xx/304 `GET` of `/` and `/suggestions*` after the response is written, skipping empty and bot-like User-Agents. The catalogue is a single page, so `detail.js` calls the `AMIGA_DETAIL_VIEW` hook on every detail render and `actions.js` posts `entry=<id>` to `POST /api/view` (same-origin + `X-Requested-With: fetch`, id `^[a-z0-9-]{1,64}$`), recorded as the virtual path `/#entry=<id>`. No cookie: `visitor` = first 16 hex of sha256(secret ‖ UTC day ‖ client IP (`visitorIP`) ‖ User-Agent), so ids rotate daily and a returning visitor counts once per day. `referrer` is the Referer host only (empty for direct or same-site), query strings are dropped, a failed write is only logged, and rows older than 400 days are pruned at most hourly on the request path. A session starts when a visitor's previous view is absent or over 30 minutes earlier (`LAG` window). `/admin/stats` shows today / 7 / 30 days, a 30-day table and top pages and referrers.
- Template partials (`web/templates/_*.html`) are embedded with an explicit `templates/_*.html` pattern, since directory embeds skip `_` files. Admin import goes through the global 1 MB request-body limit (the seed is ~280 KB).

### Review and decisions

- Every decision is guarded twice: the handler refuses a non-pending suggestion up front ("already decided", 409), and the publication's `TxHook` runs `DecideSuggestion` + a `moderation_log` row (`suggestion_<status>`) inside the catalog transaction, so a racing second click rolls the whole publication back. Reject and duplicate use the same two statements in a plain transaction; hide works from any status, keeps a blank note as the existing one, and never touches the catalogue.
- What Accept publishes: `change` = the live document with the payload applied (`null` removes), so newer edits to other fields survive; `new` = the payload rebuilt through the entry form (seed key order) plus the owner's weight (default 2). `weight` and `unverified` are owner-only: setting them on Accept is not an edit. Edit & accept is stored as `accepted_edited` only when the published document differs by value from plain Accept with the same owner fields; `payload` is never rewritten, `final` holds what was published.
- Staleness is `entry.rev > base_rev` for change/remove/image; acceptance then needs `reviewed=1` (shown with the newer revisions' diffs and per-field "changed since" marks), and the form's `rev` still goes to `Update`/`Remove`, so a change made while reviewing is a plain stale error. Removal acceptance shows the same dependency partial and needs the same `confirm=1` as direct removal.
- Image previews are staged only on an owner POST and kept in an in-memory map per suggestion (a restart forgets them; the files stay in the private staging dir). `/admin/staged/{name}` serves only names currently in that map, through `Fetcher.OpenStaged` (`os.Root` on the staging dir, sandbox CSP). Accept without a preview fetches first; the rev is checked before anything is fetched or moved. `Publish` moves the file into `media/suggested` just before the catalog transaction and returns a `Published` whose `Record` writes the `images` row as a `TxHook` inside it; if the publication fails (stale, validation, already decided) `Undo` removes the file it moved, so nothing public or in `images` is left behind. Reject, duplicate and hide discard the staged file; `serve` deletes staged files older than 24 h at startup.
- Decisions are functions shared by the single and bulk paths: `reviewAcceptDecide(ctx, sg, reviewAcceptOptions)`, `reviewRejectDecide`, `reviewHideDecide`, `reviewStagePreview`; a `*reviewRefusal` (stale, needs confirmation, image not fetched) is an owner-actionable refusal. `POST /admin/queue/bulk` (`ids`, at most 100; `action` accept/reject/hide) runs them one by one: Accept uses `Defaults` (the review page's pre-filled owner fields, weight 2 for new, current rev) and never confirms, so stale proposals and removals are skipped, as are already-decided ones; a per-item error is recorded and the loop continues. Items not started within 25 s are skipped as "time limit" (one fetch with its retry can take ~30 s; the server's write timeout is 60 s). The redirect carries only counts and `id.code` pairs (`bulk`, `n`, `skip`), and the notice is rebuilt from fixed texts, so nothing submitted is reflected.
- `/admin/queue/images` lists pending image suggestions as cards (newest first, 30 per page), current and proposed image side by side: the live entry's image (a local `images/…` path under `/images/`, or the https URL it already uses publicly) and the proposal via `GET /admin/staged/suggestion/{id}`, never the remote URL. That route serves the suggestion's staged copy, staging it first with `reviewStagePreview` when there is none (a per-server semaphore plus the fetcher's own serialise concurrent requests, so each image is fetched once; a queued request gets a 3-minute write deadline); a failed fetch answers 502 in plain text, so the card shows its "preview failed" alt text. A side effect on GET is acceptable here because the admin cookie is SameSite=Strict (only the owner's own same-site pages trigger it) and the only effect is a file in the private staging dir, through the same guarded fetcher. Accept then takes the staged copy from the map without fetching again. Card Accept/Reject post to the single-item routes with a `back` path; `reviewDone` redirects there only when it is a clean same-site path under `/admin/`. The bulk bar is sticky at the bottom of the viewport.
- Duplicates must name an existing, different suggestion that is neither a duplicate nor hidden; votes are not merged.
- `/admin/suggestions/import` takes a multipart `proposals` file holding a JSON array of `suggest.Proposal` objects and files each through `Service.CreateOwner` (the same validation as `Create`, without the pause or rate limits; `voter_id` "owner-import", no IP hash); every item is attempted and the page lists each one's new suggestion or its problems, within the 1 MB body limit.

### Suggestions and voting

- `suggest.Service` (`New(store, catalog, cfg, clock)`) validates against the live vocabulary parsed from the catalogue cache (rebuilt when its ETag changes). Visitors submit `suggest.Fields`, which has no `weight`, `unverified` or `image*` keys, so those are dropped on decode. `new`: required catalogue fields via `catalog.CheckEntry` with a placeholder importance; `payload` is the proposed entry object in seed key order without `weight` (the owner sets it on acceptance); the id is derived from the name when blank and must not exist in `entries` (deleted rows included). `change`: `payload` holds only changed keys, with `null` meaning "remove this field"; the merged entry must validate; `snapshot` + `base_rev` are recorded. `remove`/`image` also store `snapshot` + `base_rev` and require a reason; image `payload` is `{"image_url","credit","source_page"?}`. `title` is the entry name; `summary` is derived when blank.
- `final` (set by the owner) is rendered publicly as an object of fields: the published entry for new/change, the published image fields for image, NULL for an accepted removal. `accepted_edited` is shown as "Accepted with edits", distinct from "Accepted". Store primitives for the owner: `Queries.DecideSuggestion` (guarded by `status='pending'`, returns `store.ErrNotPending`; run it in the publication transaction) and `Queries.HideSuggestion` (any status).
- Votes: `ToggleVote(ctx, id, Visitor, on)` runs status check (pending only; hidden is not found), rate check, insert-or-ignore / delete and count in one IMMEDIATE transaction; resolved suggestions keep their count and return 409. Rate limits live in `rate_events` (keys `v:<browser id>` and `ip:<hash>`, kinds `suggest`/`vote`), checked and recorded in the same transaction as the action, pruned after 24 h on every write; only successful submissions and effective vote changes count. Defaults 5/day and 30/hour per browser, 25/day and 150/hour per IP, and at most 20 +1s per IP per suggestion per day (key `ip:<hash>:s:<id>`, kind `vote_suggestion`, `AMIGA_RATE_VOTE_SUGGESTION_IP`), since browser ids are free to mint (`AMIGA_RATE_*`). IPv6 clients are grouped by /64.
- Identity: cookie `amiga_vid` = base64url(16 random bytes ‖ HMAC-SHA256(secret, "vid" ‖ bytes)), HttpOnly, SameSite=Lax, Path=/, 1 year, Secure in production; issued lazily on the public suggestion pages and required for every mutation. IP is stored only as hex sha256(secret ‖ ip). `AMIGA_TRUST_PROXY=1` takes the client IP from the last `X-Forwarded-For` hop (set it behind the reverse proxy; the admin login throttle uses it too).
- Every mutation must pass `Sec-Fetch-Site` (same-origin when sent) and `Origin` (equal to `AMIGA_BASE_URL`, or the request origin in development); a request with neither is refused. The JSON API also requires `X-Requested-With: fetch`; votes send the Turnstile token in `X-Turnstile-Token`. Suggestions are submitted only through the HTML form (there is no JSON create endpoint). The HTML form adds a honeypot (`website`) and a signed `form_ts` (3 s to 24 h) and re-renders with the visitor's input, a fresh token and widget on any failure.
- Turnstile: keys set → siteverify (5 s timeout, success + hostname of `AMIGA_BASE_URL` + action `suggest`/`vote`); no keys outside production → skipped with a startup warning and no widget; production without keys is rejected by `config.Load`. Widgets are rendered explicitly by `suggest.js` (votes use an invisible execute-on-demand widget, reset after each token). Votes need JavaScript; the form works without it except for Turnstile.
- `GET /api/entries/search` compares the query with at most the 100 newest pending suggestions and is limited in memory (fixed one-minute window) to 60 requests per browser id and 600 per IP; over the limit it answers 429. Request logs carry no client address.
- Pauses: settings `submissions_paused` / `voting_paused` = "1" refuse with 503 and a banner. Pending image suggestions show the URL as a link, never an `<img>`. `/#entry=<id>` opens that entry's dialog (`actions.js`).

## Themes

Three user-selectable themes: **Museum Mode Light**, **Museum Mode Dark** and
**Workbench Mode** (Amiga Workbench 1.3 look with a faint static interlace).

| Decision | Choice | Why |
|---|---|---|
| Selection model | One attribute, `<html data-theme="light|dark|workbench">`, stored in `localStorage["amiga-theme"]`; unset = Museum Mode Dark | Reuses the existing key and attribute; no second axis to keep in sync |
| Control | One `theme.js` (blocking, in `<head>`, shared by the SPA shell and the suggestion layout) applies the stored theme before first paint and exposes `AmigaTheme.mount(button)`: a "Screen Mode" icon button (sun / moon / pixel Workbench glyph for the current mode) with a "Theme" label and caret, opening a small popover with that glyph, name and tagline per mode; `role=menu`, arrow keys, Esc, click-outside | Three options need a picker; one shared script removes the duplicated toggle code and the load-time flash |
| Workbench CSS | `web/static/css/workbench.css`, every rule scoped to `:root[data-theme="workbench"]`, loaded after `styles.css` on the SPA shell and the public suggestion layout | Overrides the Museum tokens and a few components only; `styles.css` stays the design system, admin pages are untouched |
| Dark bleed | `workbench.css` sets `color-scheme: light` and redefines every token, so the dark `prefers-color-scheme` block in `styles.css` cannot show through | Same specificity, later file wins; tokens are the only contract |
| Interlace | `body::after` fixed overlay, `pointer-events:none`, `repeating-linear-gradient` darkening one of every two CSS pixels at .07 opacity; off under `forced-colors: active` and `prefers-contrast: more` | Static, cheap, no animation, no canvas work |
| Canvas | `lineage.js` reads colours via `cssv()` already; fonts now come from three tokens `--f-canvas-disp` / `--f-canvas-mono` / `--f-canvas-sans` (the last replaces the hardcoded `'Archivo'` label face; defined in `styles.css`, overridden in `workbench.css`) and the theme change triggers `GRAPH.draw()` | Keeps layout geometry identical, so the 40 layout tests still pass |
| Typeface | Bundled `web/static/fonts/topaz-unicode-ks13.woff` (Topaz Unicode KS13 by Screwtape, ISC, licence file beside it) for display text at 16px multiples only; small mono text and canvas labels keep a system monospace | The only Workbench 1.3 Topaz under a redistribution-safe licence; an 8x8 bitmap face is unreadable below 16px |
| Icons | A few Workbench 1.3 icon PNGs from `WBIcons/` copied to `web/static/img/wb/`, shown only in Workbench Mode with `image-rendering: pixelated` at integer scale; `Sel` variant for active state | Inventory in `WBIcons/README.md`; rights are not verified, see note |

Origin: `WBIcons/amiga.css` is the appearance study the theme file is derived from
(selector `data-skin="amiga"` became `data-theme="workbench"`, unused optional
components dropped). The study and the icon inventory stay in `WBIcons/` as reference only.

Note on icon rights: no redistribution licence has been verified for the icon files.
They ship in Workbench Mode on the owner's decision; remove `web/static/img/wb/` and
the icon block in `workbench.css` to withdraw them without touching anything else.

## Verification

- `go test ./...` with `-race`; `node tests/lineage-layout.cjs` must keep passing 40 cases.
- Manual browser pass over all three themes, three views, routes, dialog, keyboard, narrow width.
- Idle memory target under 64 MB; measured numbers recorded in `README.md`.

How the numbers in `README.md` were measured: a fresh data directory, the binary built with `go build ./cmd/amiga`, and `AMIGA_ADDR=127.0.0.1:8095` in development mode. Fidelity compares `amiga export` with `data/amiga-data.json` using `cmp`; persistence imports an edited export with `--replace`, restarts, and checks `/api/catalog`; restoration runs `amiga backup`, deletes `amiga.db*` and `media/`, copies the backup back and restarts. Idle RSS is `ps -o rss=` sampled three times over 30 s after startup and one catalogue request. Throughput uses a small Go client (50 keep-alive connections, 10 s, no compression) on the same machine, once plain and once with the current `ETag` in `If-None-Match`. Voting uses 200 separate cookie jars (each given an `amiga_vid` by `GET /suggestions`) firing `PUT /api/suggestions/{id}/vote` together, then 50 concurrent PUTs from one jar with `AMIGA_RATE_VOTE_HOUR_IP` raised to 1000 so the per-IP ceiling does not mask the one-vote-per-browser result. Image approval cannot be timed against a loopback server because the fetcher refuses non-public addresses; it needs a public image host. The backup does not include `<data_dir>/secret`, which signs cookies and form tokens; copy it separately if sessions and visitor ids must survive a restore.
