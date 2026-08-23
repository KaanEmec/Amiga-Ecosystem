# Amiga Constellation

A field guide to the Amiga ecosystem: 330 entries and 419 lineage links spanning
1984–2026, as one self-contained page. No webserver, no build step to *use*, no
network — double-click `Amiga-Constellation.html` and it works, offline, with the
web fonts degrading to system faces.

## Don't edit the HTML

`Amiga-Constellation.html` and `amiga-data.js` are **build products**. Editing them
directly works until the next build overwrites your changes. Edit the sources:

| Want to change | Edit | Then |
|---|---|---|
| What the site says | `data/amiga-data.json` | `python3 build.py` |
| How it behaves | `src/app.js` | `python3 build.py` |
| How it looks | `src/styles.css` | `python3 build.py` |

```bash
python3 build.py
```

That validates the data and regenerates both outputs. It is the only build step,
and it needs nothing but Python 3.

## The files

| File | What it is |
|---|---|
| `Amiga-Constellation.html` | **The site.** Generated. Carries the data embedded as a fallback. |
| `amiga-data.js` | **Generated sidecar.** When it sits next to the HTML it *overrides* the embedded copy. |
| `data/amiga-data.json` | **Canonical content.** The source of truth for every entry, era, category and route. |
| `src/app.js` | The application: three views, faceted filtering, the canvas lineage map. |
| `src/styles.css` | The design system — palette, type, layout, both themes. |
| `build.py` | Validates the data and writes the two generated files. |
| `DATA-README.md` | The content editor's guide: entry schema, how to add a picture, ground rules. |
| `images/` | Optional pictures. Entries without one keep their hand-drawn pictogram. |

## The sidecar trick

The page prefers `amiga-data.js` and falls back to the copy embedded in the HTML.
So a content editor can change a description, save, and reload — no toolchain. Delete
the sidecar and the single file still works. The console says which source loaded:

```
[amiga] data source: external sidecar (amiga-data.js)
[amiga] 330 entries, 419 lineage links.
```

`build.py` regenerates both from the same JSON, so they never disagree. Run it when
you want the standalone HTML itself to carry your edits.

## Architecture in one paragraph

Everything is content, including the vocabularies. `meta` in the JSON defines the
eras, architectures, statuses, kinds and category groups; add a value there and it
appears in the filter rail automatically. Nothing about the taxonomy is hardcoded in
the UI. Entries declare `related` as their *ancestors*; children are derived at load,
as is each entry's era from its `year`. Filtering is five-dimensional, and each
facet's counts are computed with its own dimension released — so a facet never
offers you a choice that would return nothing.

The three views are the front page (`renderOverview`), the catalogue accordion
(`renderCatalog`), and the canvas lineage map (`graphDraw`/`graphLayout`), which
re-clusters six ways and carries the guided routes.

## What `build.py` enforces

The build refuses to write a broken site:

- every entry `id` unique
- every `related` id resolves to a real entry
- every tour step points at an entry that exists
- every `image` is a URL, a `data:` URI, or a relative path
- `app.js` contains no literal `</script>` that would close the inlined block

Bad data fails the build with a named error rather than shipping a page that warns
in the console.

## Verifying a change

There is no test runner. The checks that matter:

1. `python3 build.py` — the data validation above.
2. Open the page; the console must show the entry/link counts and **no `[data]`
   warnings**. `app.js` warns loudly about unknown categories, kinds, statuses,
   architectures, missing eras and dangling relations.
3. Click through all three views in both themes, and at least one guided route.

The design system's contrast ratios are recorded as comments in `src/styles.css`
next to the values they describe. The 36 solid foreground/background pairs that can
be computed from the stylesheet all clear WCAG AA in both themes; translucent
colours, gradients and canvas drawing are not machine-checkable and were judged by
eye. If you change a colour, re-measure it.
