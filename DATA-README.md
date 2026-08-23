# Amiga Constellation — data guide

The site is one self-contained HTML file, but **all of its content lives in an
external, human-editable data source**. You never need to touch the HTML to
change what the site says.

## The files

| File | What it is |
|---|---|
| `Amiga-Constellation.html` | The site. Double-click to open — works alone, no webserver. |
| `amiga-data.js` | **The editable sidecar.** All 330 entries plus eras, categories, domains and story routes, as one pretty-printed object. When this file sits next to the HTML, it **overrides** the copy embedded in the page. |
| `data/amiga-data.json` | The same content as pure JSON — the canonical source used by the build script. |
| `build.py` + `src/` | Regenerates the HTML and the sidecar from the JSON (only needed if you want to re-embed your edits into the single file). |
| `images/` | Optional. Drop pictures here and point entries at them with `"image": "images/a500.jpg"`. |

## How to edit (the easy way — no tools needed)

1. Open `amiga-data.js` in any text editor.
2. Change anything — fix a description, add an entry, update a status.
3. Save, and reload `Amiga-Constellation.html` in the browser. Done.

The page logs which data source it used to the browser console:
`[amiga] data source: external sidecar` or `embedded`.

If you delete or rename `amiga-data.js`, the page silently falls back to the
copy embedded inside the HTML — so the single file always keeps working, and
you can't break it permanently. (Note: your browser may cache the sidecar;
force-reload with Ctrl/Cmd-Shift-R if an edit doesn't show.)

## How to re-embed your edits into the single file (optional)

If you want the standalone HTML itself to carry your changes (e.g. to share
just one file), copy your edits into `data/amiga-data.json` and run:

```
python3 build.py
```

This validates the data (unique ids, no dangling `related` references, tour
steps that exist) and regenerates both `Amiga-Constellation.html` and
`amiga-data.js`.

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

## Ground rules the build enforces

- every `id` unique; every `related` id and tour step id must exist;
- `year` should be the first public release (announcement vs retail often
  differ — pick one convention and note the other in `what`);
- keep CoffinOS/PiMiga-style grey-zone projects without `url` on purpose;
- if a fact isn't verified against a primary source, set `"unverified": true`
  rather than deleting the caveat.

## Adding a picture

Every entry can carry one image, and none of them have to. Set `image` to:

| Value | When to use it |
|---|---|
| `https://…` | a picture already hosted somewhere you trust |
| `images/a500.jpg` or `./images/a500.jpg` | a file you drop in an `images/` folder next to the HTML — keeps working offline |
| `data:image/jpeg;base64,…` | when you want the site to stay a single self-contained file |

```jsonc
{ "id": "a500", "name": "Amiga 500", …,
  "image": "images/a500.jpg",
  "imageCredit": "Photo: Bill Bertram, CC BY-SA 2.5" }
```

The picture fills the plate on the catalogue card, the front page and the top
of the detail drawer. Leave `image` out and the entry keeps its hand-drawn
pictogram — which is also what you get back if the URL ever fails to load, so
a dead link can never leave a broken frame on the page.

Snapshot: research verified August 2026.
