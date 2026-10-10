# Amiga Constellation

A field guide to the Amiga ecosystem: 333 entries and 422 lineage links spanning
1984–2026. One Go binary serves the catalogue, accepts public suggestions with +1
support, and gives the owner a private panel to review them and manage the content.
SQLite is the live source of truth; the JSON in `data/` is only the seed.

## Run it locally

```bash
go run ./cmd/amiga serve
```

Opens on <http://127.0.0.1:8080>. On first start an empty database is created in
`./runtime/`, the seed (`data/amiga-data.json` and `images/`) is imported once, and a
development secret is generated. Turnstile is skipped in development when no keys are
set. Create the owner account, then sign in at `/admin/login`:

```bash
go run ./cmd/amiga owner init
```

## Commands

| Command | What it does |
|---|---|
| `amiga serve` | start the server (`AMIGA_ADDR`, default `:8080`) |
| `amiga import [--replace] <file.json>` | validated import; without `--replace` only into an empty catalogue |
| `amiga export [file]` | the live catalogue as JSON, same shape as the seed |
| `amiga backup <dir>` | consistent database copy plus media |
| `amiga owner init\|reset-password\|reset-totp\|recovery-codes` | owner account and local recovery |

Configuration is environment-only; see `deploy/amiga.env.example`. Runtime data and
secrets live in `AMIGA_DATA_DIR` and are git-ignored.

## The files

| Path | What it is |
|---|---|
| `cmd/amiga`, `internal/` | the Go application (see `ARCHITECTURE.md`) |
| `web/static/js/*.js` | the catalogue UI as classic scripts: `data.js` boots, then core, catalog, detail, lineage, app, actions |
| `web/static/css/styles.css` | the design system, both themes, plus admin and suggestion sections |
| `web/templates/` | the page shell and the server-rendered suggestion and admin pages |
| `data/amiga-data.json` | seed content, read once; afterwards edit through `/admin` or `amiga import --replace` |
| `images/` | seed pictures, copied once into the runtime media directory |
| `tests/lineage-layout.cjs` | 40 layout scenarios for the lineage map |
| `deploy/` | systemd unit, environment example, reverse-proxy configs, backup script |
| `DATA-README.md` | the entry schema and editing rules |

## Suggestions and review

Visitors propose new entries, changes, removals and images from `/suggestions/new` or
from the three actions in every entry dialog. Valid proposals are public immediately as
Pending, listed at `/suggestions` with search, filters and newest/most-supported sorting,
and can be +1'd once per browser while pending. Protection: Turnstile, a signed browser
cookie, Origin checks, honeypot and timing checks, and per-browser and per-IP limits.

The owner reviews the queue at `/admin/queue`: current entry, proposed differences and a
final preview, then Accept, Edit & accept, Reject, Mark duplicate or Hide spam. Each
acceptance is one transaction that updates the entry, records a revision and a moderation
entry, and freezes the proposal; a second click is refused. Suggested images are fetched
only when the owner asks, validated, and published as a local copy.

## How the catalogue is modelled

Everything is content, including the vocabularies. `meta` in the JSON defines the
eras, architectures, statuses, kinds and category groups; add a value there and it
appears in the filter rail automatically. Nothing about the taxonomy is hardcoded in
the UI. Entries declare `related` as their *ancestors*; children are derived at load,
as is each entry's era from its `year`. Filtering is five-dimensional, and each
facet's counts are computed with its own dimension released — so a facet never
offers you a choice that would return nothing.

The three views are the front page (`renderOverview`), the catalogue accordion
(`renderCatalog`), and the canvas lineage map (`graphDraw`/`graphLayout`), which
can be arranged in five ways and carries the guided routes. Clusters uses
circular summaries inside rectangular regions; Every Entry keeps the radial
constellation. Each entry belongs to one cluster per arrangement. Architecture
uses the first listed (primary) platform, while the filter rail matches any
supported platform. Small named clusters are retained rather than merged.

The five visible Arrange buttons support click, Tab, arrow keys, Home and End.
Changing arrangement keeps search and facets, closes any open cluster board,
and ends an active route. Regions stack on narrow screens; dense Kind panels
extend vertically rather than shrink their text. Scroll or drag to explore
Clusters, and use the zoom buttons or Ctrl+scroll to zoom. Every Entry retains
its scroll-to-zoom behavior.

The five Lineage arrangements are:

- **Domain:** Hardware, System software, and Practice & stewardship.
- **Era:** four panels in chronological order.
- **Architecture:** 68k & PowerPC, FPGA, and Host & other platforms.
- **Status:** Current (shipping, active, limited), Unreleased (planned,
  prototype), and Historic.
- **Kind:** Machines & components (original, replica, fpga, appliance,
  compatible, component, prototype); Software & environments (os, amigalike,
  oslike, emulator, shell, toolkit, launcher, tool, app); and People,
  preservation & ideas (archive, org, media, event, concept, other).

## Reading an object

Objects in Overview, Catalogue, and Lineage open in a centered modal dialog.
The page underneath keeps its scroll position, filters, expanded sections,
selection, and map camera. On desktop the hero pairs the full image with the
title and introduction; on narrow screens it stacks them. Images keep their
proportions, and missing images use the existing category pictogram.

Related objects open in the same dialog. Its Back button restores the preceding
object and reading position; this history lasts until the dialog is closed.
Close, Escape, or clicking the backdrop returns focus to the originating object.
During a guided route, reading related objects leaves the current stop intact.
Global keyboard shortcuts are suspended while reading.

In Overview and Catalogue, visible card images and titles fly into the hero
over about 700ms and fly back on close. Objects opened from Lineage skip the
flight: the dialog blooms out of the clicked star or card in about 600ms, with
a brief flare at that point, and collapses back toward it on close. Timings
live in `MOTION` in `detail.js`. Reduced-motion preferences disable these animations.
The dialog makes the background inert, wraps Tab focus within its controls,
and requires a browser with HTML dialog support; there are no additional
libraries or network dependencies.

## Verifying a change

```bash
go test -race ./...
node tests/lineage-layout.cjs
```

The Go tests cover migrations, seed fidelity, validation, the catalogue cache, every
proposal type, voting and its uniqueness under concurrency, rate limits, Turnstile
failures, authentication and throttling, stale approvals, removal dependencies, restore,
image-fetch restrictions and transaction rollback. The layout test checks all five
arrangements at four widths with Essentials on and off.

Manual checks after a UI change: all three views in both themes, a guided route, the
detail dialog (Back, Escape, backdrop, focus return, Tab containment), keyboard operation
of the Arrange buttons, and a narrow screen. The console must show the entry/link counts
and no `[data]` warnings.

## Measurements

Recorded on the development machine (Apple Silicon, macOS, Go 1.27, development mode, SQLite
in WAL mode); see `ARCHITECTURE.md` for the method. The load generator ran on the same machine
and competed for CPU, so request rates are a lower bound.

| Measurement | Result |
| --- | --- |
| Migration fidelity | seeded 333 entries, 422 links, 159 images; `amiga export` is byte-identical to `data/amiga-data.json` (279,379 bytes) |
| Restart persistence | an imported edit survives a restart; no re-seed |
| Backup restoration | backup 0.17 s (db 0.7 MB + 159 media files, 76 MB); restored catalogue is intact and the edit persists |
| Idle memory (RSS) | 32.7 MB median of three samples over 30 s (target under 64 MB); 0.0 % CPU |
| RSS after 1.1 M requests | 45.3 MB |
| Linux binary | 15.8 MB (`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w"`) |
| `GET /api/catalog`, 50 clients, 10 s | 30,200 req/s; p50 1.4 ms, p99 4.4 ms (234 KB body, uncompressed) |
| Same with `If-None-Match` (304) | 79,200 req/s; p50 0.37 ms, p99 2.4 ms |
| Concurrent voting, 200 browsers | 150 returned 200 and 50 returned 429 (per-IP limit of 150/hour); count 150, equal to the successes |
| Concurrent voting, one browser | 50 simultaneous PUTs add exactly 1 vote (3 rounds, new browser each) |
| Image approval timing | not measured: the fetcher refuses a loopback image server (`image host resolves to a non-public address`), which is the intended SSRF guard |
