# Amiga Constellation — data guide

All content lives in SQLite once the application has started. `data/amiga-data.json`
is the **seed**: it is imported exactly once, into an empty database, and never
re-applied on restart or upgrade.

## How to edit

| Want to | Do |
|---|---|
| Fix an entry, add one, remove or restore one | sign in at `/admin` and use the entries pages; every change records a revision |
| Change vocabularies, routes, domains or narrative | `amiga export > catalogue.json`, edit, then `amiga import --replace catalogue.json` (validated; refuses broken data) |
| Add a picture | paste the URL in the entry's image field in `/admin` and use *Fetch & attach*, or accept a visitor's image suggestion; the file is stored locally under `images/suggested/` |
| Restore an older version | open the entry's revision history in `/admin` and choose *Restore* |

The validation rules are the ones the old build script enforced, applied on every write:
unique ids, `related` ids and tour steps that resolve, vocab values that exist in `meta`,
`weight` 1–5, and `image` values that are a URL, a `data:` URI or a relative path.

## Entry schema

```jsonc
{
  "id": "winuae",              // unique slug — "related" lists point at these
  "name": "WinUAE",
  "year": 1997,                // first public release; determines the era band
  "category": "emu",           // one of meta.groups[].cats[].id
  "kind": "emulator",          // one of meta.kinds[].id
  "status": "active",          // one of meta.statuses[].id
  "arch": ["x86"],             // meta.architectures[].id — first one is primary
  "weight": 5,                 // 1–5 significance; 5 = shows in "Essentials only"
  "by": "Toni Wilen",          // maker / maintainer
  "what": "…",                 // description ("What it is")
  "why": "…",                  // significance ("Why it matters")
  "version": "6.0.3 (Mar 2026)",  // optional detail line
  "url": "https://www.winuae.net",// optional verified official page
  "image": "images/winuae.png",// optional picture for the plate (see below)
  "imageCredit": "Screenshot, WinUAE project",  // optional credit line
  "related": ["uae"],          // ids this DESCENDS FROM (children are derived)
  "tags": ["open"],            // free-form
  "unverified": true           // optional: flags shaky facts with a visible notice
}
```

Everything else is content too:

- `meta.eras / architectures / statuses / kinds / groups` — the filter
  vocabularies. Add a value here and it appears in the filter rail.
- `domains` — the clusters of the default Lineage view (`cats` lists category ids).
- `tours` — the guided story routes: `{id, label, steps:[{id, note}]}`.
- `meta.theses / cautions` — the Overview page's narrative sections.

## Ground rules the application enforces

- every `id` unique; every `related` id and tour step id must exist;
- `year` should be the first public release (announcement vs retail often
  differ — pick one convention and note the other in `what`);
- keep CoffinOS/PiMiga-style grey-zone projects without `url` on purpose;
- if a fact isn't verified against a primary source, set `"unverified": true`
  rather than deleting the caveat.

## Pictures

Every entry can carry one image. `image` is `images/<path>` for a local file served from
the media directory, or an `https://` URL. Entries without a picture keep their category
pictogram, which is also the fallback when an image fails to load. `imageCredit` holds the
credit line; the source URL and page of fetched images are kept in the `images` table.

Seed snapshot: research verified August 2026.
