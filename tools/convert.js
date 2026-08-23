/* One-time conversion: existing JS data files -> canonical amiga-data.json */
const fs = require('fs');
global.window = {};
['meta.js','data-urls.js','data-1-hardware.js','data-2-upgrades.js','data-3-software.js','data-4-tech.js','data-5-brand.js']
  .forEach(f => new Function('window', fs.readFileSync('src/'+f,'utf8'))(global.window));

const app = fs.readFileSync('src/app.js','utf8');
function extract(name){
  const start = app.indexOf('var '+name+' = [');
  const open = app.indexOf('[', start);
  let depth = 0, i = open;
  for(; i < app.length; i++){
    if(app[i]==='[') depth++;
    if(app[i]===']') { depth--; if(!depth) break; }
  }
  return eval(app.slice(open, i+1));
}
const DOMAINS = extract('DOMAINS');
const TOURS = extract('TOURS');

const w = global.window;
const ALL = [].concat(w.DATA_HARDWARE, w.DATA_UPGRADES, w.DATA_SOFTWARE, w.DATA_TECH, w.DATA_BRAND);
const URLS = w.URLS || {};

const entries = ALL.map(e => {
  const o = {
    id: e.id,
    name: e.n,
    year: e.y,
    category: e.cat,
    kind: e.kind,
    status: e.st,
    arch: e.arch || [],
    weight: e.wt || 1,
    by: e.by || '',
    what: e.d || '',
    why: e.w || ''
  };
  if (e.ver) o.version = e.ver;
  if (URLS[e.id]) o.url = URLS[e.id];
  if (e.rel && e.rel.length) o.related = e.rel;
  if (e.tags && e.tags.length) o.tags = e.tags;
  if (e.unv) o.unverified = true;
  return o;
});

const M = w.META;
const doc = {
  _readme: [
    "Amiga Constellation — canonical data file. Everything the site shows lives here.",
    "HOW TO EDIT: change this JSON (or the amiga-data.js sidecar next to the HTML,",
    "which contains the same object) and reload the page. Field guide:",
    "  entry.id         unique slug; 'related' lists refer to these ids",
    "  entry.year       first public release/announcement year (sets the era)",
    "  entry.category   one of meta.groups[].cats[].id",
    "  entry.kind       one of meta.kinds[].id",
    "  entry.status     one of meta.statuses[].id",
    "  entry.arch       any of meta.architectures[].id (first one = primary)",
    "  entry.weight     1-5 significance; 5 = 'Essentials only' landmark",
    "  entry.what/why   description and significance; entry.version = detail line",
    "  entry.url        verified official page (omit if none); entry.unverified flags shaky facts",
    "  entry.related    ids this entry descends from (children are derived automatically)",
    "domains = graph clusters for the default view; tours = guided story routes.",
    "Snapshot: research verified August 2026."
  ],
  meta: {
    eras: M.eras,
    architectures: M.archs,
    statuses: M.statuses,
    kinds: M.kinds,
    groups: M.groups,
    theses: M.theses.map(t => ({title: t.t, body: t.b})),
    cautions: M.cautions.map(c => ({title: c[0], body: c[1]}))
  },
  domains: DOMAINS,
  tours: TOURS,
  entries: entries
};

fs.writeFileSync('data/amiga-data.json', JSON.stringify(doc, null, 2) + '\n');
console.log('entries:', entries.length, '| domains:', DOMAINS.length, '| tours:', TOURS.length,
  '| with url:', entries.filter(e=>e.url).length);
