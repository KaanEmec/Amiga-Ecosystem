#!/usr/bin/env python3
"""Build the Amiga Constellation.

Reads the canonical data from data/amiga-data.json and produces:
  Amiga-Constellation.html  — self-contained, double-clickable page with the
                              data embedded as a fallback
  amiga-data.js             — editable sidecar; when it sits next to the HTML
                              it OVERRIDES the embedded data (edit + reload,
                              no rebuild needed)
"""
import json, pathlib, sys

ROOT = pathlib.Path(__file__).parent
SRC = ROOT / "src"
DATA = ROOT / "data" / "amiga-data.json"
OUT_HTML = ROOT / "Amiga-Constellation.html"
OUT_SIDE = ROOT / "amiga-data.js"


def read(name):
    p = SRC / name
    if not p.exists():
        sys.exit("missing source file: %s" % p)
    return p.read_text(encoding="utf-8")


# ---- data ----
try:
    doc = json.loads(DATA.read_text(encoding="utf-8"))
except Exception as e:
    sys.exit("data/amiga-data.json is not valid JSON: %s" % e)

for key in ("meta", "entries", "domains", "tours"):
    if key not in doc:
        sys.exit("amiga-data.json is missing the '%s' section" % key)

ids = [e.get("id") for e in doc["entries"]]
if len(ids) != len(set(ids)):
    dupes = sorted({i for i in ids if ids.count(i) > 1})
    sys.exit("duplicate entry ids: %s" % ", ".join(dupes))
idset = set(ids)
bad = [(e["id"], r) for e in doc["entries"] for r in e.get("related", []) if r not in idset]
if bad:
    sys.exit("dangling 'related' references: %s" % ", ".join("%s->%s" % b for b in bad))
bad_img = [e["id"] for e in doc["entries"]
           if e.get("image") and not str(e["image"]).startswith(("http://", "https://", "data:", "./", "images/"))]
if bad_img:
    sys.exit("entries with an unusable 'image' value (need a URL, a data: URI or a relative path): %s"
             % ", ".join(bad_img))

for t in doc["tours"]:
    for st in t["steps"]:
        if st["id"] not in idset:
            sys.exit("tour '%s' references unknown entry '%s'" % (t["id"], st["id"]))

# `</` must not appear literally inside a <script> block
data_js = json.dumps(doc, ensure_ascii=False, separators=(",", ":")).replace("</", "<\\/")

css = read("styles.css")
app = read("app.js")
if "</script" in app.lower():
    sys.exit("refusing to build: app.js contains a literal </script>")

# ---- sidecar (pretty-printed so it is pleasant to edit by hand) ----
side = ("/* Amiga Constellation — editable data sidecar.\n"
        "   Keep this file next to Amiga-Constellation.html and it overrides the\n"
        "   data embedded in the page: edit, save, reload the page. Delete it and\n"
        "   the page falls back to its embedded copy. The same content lives in\n"
        "   data/amiga-data.json (the canonical source used to rebuild the HTML).\n"
        "   See the _readme key below for the field guide. */\n"
        "window.AMIGA_DATA = "
        + json.dumps(doc, ensure_ascii=False, indent=2).replace("</", "<\\/")
        + ";\nwindow.AMIGA_DATA_SOURCE = 'external sidecar (amiga-data.js)';\n")
OUT_SIDE.write_text(side, encoding="utf-8")

html = """<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<title>Amiga Constellation</title>
<meta name="description" content="A field guide to the Amiga ecosystem: hardware, operating systems, shells, emulators, distributions and preservation projects, 1984 to 2026.">
<meta name="color-scheme" content="light dark">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Instrument+Serif:ital@0;1&amp;family=Public+Sans:wght@400;500;600;700&amp;family=JetBrains+Mono:wght@400;500;600&amp;display=swap">
<style>
%s
</style>
</head>
<body>
<noscript>
  <div style="padding:40px;max-width:640px;margin:0 auto;font-family:sans-serif;color:#14161A;background:#F2F3F5">
    <h1>Amiga Constellation</h1>
    <p>This atlas is interactive and needs JavaScript enabled. Everything runs locally in
    your browser &mdash; nothing is sent anywhere.</p>
  </div>
</noscript>
<script src="amiga-data.js"></script>
<script>
/* Embedded fallback: used only when the amiga-data.js sidecar is absent. */
if(!window.AMIGA_DATA){
  window.AMIGA_DATA = %s;
  window.AMIGA_DATA_SOURCE = 'embedded';
}
</script>
<script>
%s
</script>
</body>
</html>
""" % (css, data_js, app)

OUT_HTML.write_text(html, encoding="utf-8")
print("built %s (%.0f KB)  +  %s (%.0f KB)  —  %d entries, %d tours, %d domains"
      % (OUT_HTML.name, OUT_HTML.stat().st_size/1024,
         OUT_SIDE.name, OUT_SIDE.stat().st_size/1024,
         len(doc["entries"]), len(doc["tours"]), len(doc["domains"])))
