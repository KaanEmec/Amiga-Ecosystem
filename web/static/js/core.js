/* ==========================================================================
   The Amiga Constellation — core.js
   data prep, vocab labels, state, filtering, DOM helpers,
   icons, pictograms, plates, the rainbow mark and the wordmark
   Load order (classic scripts sharing one global scope, no modules):
   data.js -> core.js -> catalog.js -> detail.js -> lineage.js -> app.js
   ========================================================================== */
'use strict';

/* ---------------------------------------------------------------- data prep
   All content lives in window.AMIGA_DATA — loaded from the editable
   amiga-data.js sidecar when present, otherwise from the embedded copy. */
var RAW = window.AMIGA_DATA || {meta:{}, entries:[], domains:[], tours:[]};
console.log('[amiga] data source: ' + (window.AMIGA_DATA_SOURCE || 'unknown'));

var M = (function(m){
  return {
    eras: m.eras || [],
    archs: m.architectures || [],
    statuses: m.statuses || [],
    kinds: m.kinds || [],
    groups: m.groups || [],
    theses: (m.theses || []).map(function(t){ return {t:t.title, b:t.body}; }),
    cautions: (m.cautions || []).map(function(c){ return [c.title, c.body]; })
  };
})(RAW.meta || {});

var ALL = (RAW.entries || []).map(function(r){
  return {
    id:r.id, n:r.name, y:r.year, cat:r.category, kind:r.kind, st:r.status,
    arch:r.arch || [], wt:r.weight || 1, by:r.by || '', d:r.what || '',
    w:r.why || '', ver:r.version, u:r.url, unv:r.unverified ? 1 : 0,
    img:r.image || '', imgCredit:r.imageCredit || '',
    imgFit:r.imageFit || 'cover', imgPos:r.imagePosition || '50% 50%',
    imgBg:r.imageBackground || '',
    rel:(r.related || []).slice(), tags:(r.tags || []).slice()
  };
});

var BY_ID = {};
ALL.forEach(function(e){ BY_ID[e.id] = e; });

/* index lookups */
var CAT_LABEL = {}, CAT_GROUP = {}, CAT_ORDER = [];
M.groups.forEach(function(g){
  g.cats.forEach(function(c){
    CAT_LABEL[c.id] = c.label; CAT_GROUP[c.id] = g.label; CAT_ORDER.push(c.id);
  });
});
var KIND_LABEL = {}; M.kinds.forEach(function(k){ KIND_LABEL[k.id] = k.label; });
var ARCH_LABEL = {}; M.archs.forEach(function(a){ ARCH_LABEL[a.id] = a.label; });
var ST_LABEL = {}, ST_TONE = {};
M.statuses.forEach(function(s){ ST_LABEL[s.id] = s.label; ST_TONE[s.id] = s.tone; });

/* derive era + children, and warn loudly in the console about bad data */
var problems = [];
ALL.forEach(function(e){
  e.children = [];
  for(var i=0;i<M.eras.length;i++){
    if(e.y >= M.eras[i].from && e.y <= M.eras[i].to){ e.era = M.eras[i].id; break; }
  }
  if(!e.era) problems.push('no era for '+e.id+' (year '+e.y+')');
  if(!CAT_LABEL[e.cat]) problems.push('unknown cat "'+e.cat+'" on '+e.id);
  if(!KIND_LABEL[e.kind]) problems.push('unknown kind "'+e.kind+'" on '+e.id);
  if(!ST_LABEL[e.st]) problems.push('unknown status "'+e.st+'" on '+e.id);
  (e.arch||[]).forEach(function(a){ if(!ARCH_LABEL[a]) problems.push('unknown arch "'+a+'" on '+e.id); });
  e._s = (e.n+' '+(e.by||'')+' '+(e.d||'')+' '+(e.w||'')+' '+(e.ver||'')+' '+(e.tags||[]).join(' ')).toLowerCase();
});
ALL.forEach(function(e){
  (e.rel||[]).forEach(function(pid){
    var p = BY_ID[pid];
    if(!p){ problems.push('dangling relation '+e.id+' -> '+pid); return; }
    p.children.push(e.id);
  });
});
if(problems.length) console.warn('[data] '+problems.length+' issue(s):\n'+problems.join('\n'));

var ERA_OF = {}; M.eras.forEach(function(x){ ERA_OF[x.id] = x; });
function isLive(e){ return e.st === 'shipping' || e.st === 'active'; }
function eraColor(id){ var i = M.eras.findIndex(function(x){return x.id===id;}); return 'var(--e'+(i+1)+')'; }
function eraTint(id){ var i = M.eras.findIndex(function(x){return x.id===id;}); return 'var(--e'+(i+1)+'-t)'; }

/* ---------------------------------------------------------------- state */
var S = {
  view: 'overview',
  q: '',
  f: { arch:{}, st:{}, era:{}, cat:{}, kind:{} },
  sort: 'year',
  ess: false,
  sel: null,
  railOpen: false,
  open: { g:{}, c:{} },
  openedFirst: 0,
  catalogInitialized: false,
  filterSig: null
};

/* Facets and search narrow by DIMMING what does not match; the map needs this
   count to decide what to grey out. Essentials narrows by removing nodes
   outright, so the map already expresses it structurally and must not dim on
   it as well -- it has its own caption wording for that state. */
function dimmingCount(){
  var n = 0;
  Object.keys(S.f).forEach(function(k){ n += Object.keys(S.f[k]).length; });
  return n + (S.q ? 1 : 0);
}
/* Everything the Reset button clears, Essentials included. */
function activeCount(){ return dimmingCount() + (S.ess ? 1 : 0); }
function facetOn(dim){ return Object.keys(S.f[dim]); }

/* one predicate per dimension so facet counts can exclude their own dimension */
function testDim(e, dim){
  var on = facetOn(dim);
  if(!on.length) return true;
  if(dim === 'arch') return (e.arch||[]).some(function(a){ return S.f.arch[a]; });
  if(dim === 'st')   return !!S.f.st[e.st];
  if(dim === 'era')  return !!S.f.era[e.era];
  if(dim === 'cat')  return !!S.f.cat[e.cat];
  if(dim === 'kind') return !!S.f.kind[e.kind];
  return true;
}
function testQ(e){
  if(!S.q) return true;
  var terms = S.q.toLowerCase().split(/\s+/).filter(Boolean);
  return terms.every(function(t){ return e._s.includes(t); });
}
var DIMS = ['arch','st','era','cat','kind'];
function matches(e, skipDim){
  if(S.ess && (e.wt || 0) < 5) return false;
  if(!testQ(e)) return false;
  for(var i=0;i<DIMS.length;i++){
    if(DIMS[i] === skipDim) continue;
    if(!testDim(e, DIMS[i])) return false;
  }
  return true;
}
function results(){ return ALL.filter(function(e){ return matches(e); }); }

/* counts for a facet dimension, computed with that dimension released */
function facetCounts(dim, key){
  var pool = ALL.filter(function(e){ return matches(e, dim); });
  var out = {};
  pool.forEach(function(e){
    var vals = (dim === 'arch') ? (e.arch||[]) : [e[key||dim]];
    vals.forEach(function(v){ out[v] = (out[v]||0) + 1; });
  });
  return out;
}

/* ---------------------------------------------------------------- dom utils */
function el(tag, cls, txt){
  var n = document.createElement(tag);
  if(cls) n.className = cls;
  if(txt != null) n.textContent = txt;
  return n;
}
function $(s){ return document.querySelector(s); }
var ICON = {
  check:'<svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M2.5 6.4 4.7 8.6 9.5 3.6"/></svg>',
  arrow:'<svg class="arrow" viewBox="0 0 16 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M1 6h13M10 2l4 4-4 4"/></svg>',
  chev:'<svg class="chev" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4.5 2.5 8.5 6l-4 3.5"/></svg>',
  search:'<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"><circle cx="6.8" cy="6.8" r="4.4"/><path d="M10.2 10.2 14 14"/></svg>',
  x:'<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M4 4l8 8M12 4l-8 8"/></svg>',
  menu:'<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"><path d="M2 4h12M2 8h12M2 12h12"/></svg>',
  fit:'<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="M2 5.5V2h3.5M10.5 2H14v3.5M14 10.5V14h-3.5M5.5 14H2v-3.5"/></svg>',
  plus:'<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"><path d="M8 3v10M3 8h10"/></svg>',
  suggest:'<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M2.5 3.5h11v7.5H7l-3 2.5V11H2.5z"/><path d="M8 5.4v3.7M6.15 7.25h3.7"/></svg>',
  minus:'<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"><path d="M3 8h10"/></svg>'
};

/* ------------------------------------------------------- pictograms
   Hand-drawn 24x24 stroke icons — one per kind of thing. They give every
   card and drawer a visual identity with zero external images. */
var PICT = {
  wedge:   'M2.5 14.5 14 9.5l7.5 2.8V16L2.5 18Zm2.7.1 9.3-3.8',
  bigbox:  'M4.5 5h15v14h-15zM4.5 9.5h15M7 13h4.5M16.5 12.6h.9',
  pad:     'M7.5 9h9a4.3 4.3 0 0 1 4.3 4.3v.9a2.9 2.9 0 0 1-5 2l-1.6-1.6H9.8L8.2 16.2a2.9 2.9 0 0 1-5-2v-.9A4.3 4.3 0 0 1 7.5 9ZM8 12.4h3.2M9.6 10.8v3.2M15.6 11.6h.9M17.6 13.4h.9',
  chip:    'M7.5 7.5h9v9h-9zM10 7.5V4.5M13.5 7.5V4.5M10 19.5v-3M13.5 19.5v-3M7.5 10h-3M7.5 13.5h-3M19.5 10h-3M19.5 13.5h-3',
  card:    'M3.5 8h17v8h-17zM6.5 16v2.4h4.4V16M6.5 11.2h3.2M12 11.2h3.2',
  floppy:  'M5 4.5h10.6L19 7.9v11.6H5ZM8.3 4.5v4.8h6.8V4.5M8.3 13h7.4v6.5H8.3z',
  screen:  'M3.5 5h17v11.6h-17zM3.5 8.2h17M5.6 6.6h1.4M8.4 6.6h1.4M9.5 19.8h5M12 16.6v3.2',
  term:    'M3.5 5h17v13.6h-17zM6.5 9.2l3.2 2.9-3.2 2.9M11.5 15h5.5',
  emuwin:  'M3.5 5.5h17v12h-17zM10 8.6l5.4 2.9-5.4 2.9z',
  layers:  'M12 3.5l8.8 4.7L12 12.9 3.2 8.2ZM3.2 13l8.8 4.7 8.8-4.7',
  braces:  'M9.2 4.5c-2.4 0-1.7 3-3.7 3.4 2 .4 1.3 3.4 3.7 3.4M9.2 11.3c-2.4 0-1.7 3-3.7 3.4 2 .4 1.3 3.4 3.7 3.4M14.8 4.5c2.4 0 1.7 3 3.7 3.4-2 .4-1.3 3.4-3.7 3.4M14.8 11.3c2.4 0 1.7 3 3.7 3.4-2 .4-1.3 3.4-3.7 3.4',
  star:    'M12 4.2l2.3 4.9 5.2.7-3.8 3.7.9 5.3L12 16.3l-4.6 2.5.9-5.3L4.5 9.8l5.2-.7Z',
  archive: 'M4 8.4h16V19H4zM2.6 4.6h18.8v3.8H2.6zM9.8 12h4.4',
  bubble:  'M4 5h16v10.4H8.4L4 19.4Z M7.5 8.4h9M7.5 11.2h6',
  book:    'M4 5.4c3-1.5 5-1.5 8 0 3-1.5 5-1.5 8 0v13.2c-3-1.5-5-1.5-8 0-3-1.5-5-1.5-8 0ZM12 5.4v13.2',
  flag:    'M6 3.8v16.4M6 5h11.4l-2.6 3.1 2.6 3.1H6',
  bulb:    'M9.4 17.6h5.2M10.3 20.2h3.4M12 3.6a5.9 5.9 0 0 1 3.9 10.3c-.8.7-1 1.5-1 2.4h-5.8c0-.9-.2-1.7-1-2.4A5.9 5.9 0 0 1 12 3.6Z'
};
var PICT_BY_CAT = {
  'hw-classic':'wedge','hw-ng':'bigbox','hw-fpga':'chip','hw-accel':'chip',
  'hw-gfx':'card','hw-audio':'card','hw-storage':'floppy','hw-io':'card',
  'hw-repair':'chip','os':'screen','shell':'term','distro':'layers','tech':'chip',
  'emu':'emuwin','dev':'braces','soft':'star','pres':'archive','comm':'bubble','brand':'flag'
};
function pictFor(e){
  if(e.kind === 'concept') return 'bulb';
  if(e.kind === 'event') return 'flag';
  if(e.kind === 'media') return 'book';
  if(e.kind === 'org') return 'bubble';
  if(e.kind === 'archive') return 'archive';
  if(e.kind === 'launcher') return 'pad';
  if((e.tags||[]).includes('console')) return 'pad';
  if((e.tags||[]).includes('bigbox')) return 'bigbox';
  if(e.cat === 'hw-classic' && (e.tags||[]).includes('chipset')) return 'chip';
  return PICT_BY_CAT[e.cat] || 'bulb';
}
function pictSvg(e){
  return '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" '+
    'stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="'+
    PICT[pictFor(e)]+'"/></svg>';
}
function thumbEl(e, size){
  var d = el('span','thumb' + (size === 'lg' ? ' lg' : ''));
  d.style.setProperty('--era', eraColor(e.era));
  d.innerHTML = pictSvg(e);
  return d;
}

/* A catalogue plate. The pictogram is the standing state; when an entry
   carries an `image` URL the photograph covers it. If that URL cannot load
   — offline, moved, blocked — the img removes itself and the pictogram is
   simply still there, so the page never shows a broken frame. */
function plateEl(e, size){
  var d = el('div','plate' + (size ? ' plate-' + size : '') + (e.img ? ' plate-has-img' : ''));
  d.style.setProperty('--era', eraColor(e.era));
  d.style.setProperty('--era-t', eraTint(e.era));
  var g = el('span','plate-pict'); g.innerHTML = pictSvg(e);
  d.appendChild(g);
  if(e.img){
    var im = document.createElement('img');
    im.className = 'plate-img';
    im.alt = e.n; im.loading = 'lazy'; im.decoding = 'async';
    im.style.objectFit = e.imgFit;
    im.style.objectPosition = e.imgPos;
    if(e.imgBg) im.style.backgroundColor = e.imgBg;
    im.addEventListener('error', function(){
      if(im.parentNode){
        im.parentNode.classList.remove('plate-has-img');
        im.parentNode.removeChild(im);
      }
    });
    im.src = e.img;
    d.appendChild(im);
  }
  var n = el('span','plate-n');
  n.textContent = plateNo(e);
  d.appendChild(n);
  return d;
}
/* Plate numbers read section·index, the way a museum catalogue numbers its
   objects: stable, and a quick way to cite one entry to someone else. */
function pad2(n){ return (n < 10 ? '0' : '') + n; }
var PLATE_NO = {};
(function(){
  var sect = {}, i = 0, seen = {};
  M.groups.forEach(function(g){ g.cats.forEach(function(c){ sect[c.id] = ++i; }); });
  ALL.slice().sort(function(a,b){ return a.y - b.y || a.n.localeCompare(b.n); })
     .forEach(function(e){
       seen[e.cat] = (seen[e.cat] || 0) + 1;
       PLATE_NO[e.id] = pad2(sect[e.cat] || 0) + '.' + pad2(seen[e.cat]);
     });
})();
function plateNo(e){ return PLATE_NO[e.id] || ''; }

/* ------------------------------------------------------- the rainbow mark
   The Amiga double-tick, traced from the official artwork: two identical
   checkmarks offset by 51.5 units, each arm at 32 degrees from vertical, cut
   flat at the top and bottom. The spectrum runs along the PATH — purple at
   the short arm's tip, mint at the vertex, red at the long arm's tip — so
   each arm carries its own gradient, both in user space, which is what makes
   the second tick read a step further round the spectrum than the first.
   Hand-authored, so it needs no asset and survives with no network. */
var MARK_GEO = [
  {s:'M0.0 182.0 65.1 286.0 87.3 286.0 87.3 259.3 39.0 182.0Z',
   l:'M87.3 259.3 87.3 286.0 109.6 286.0 288.5 0.0 249.5 0.0Z'},
  {s:'M51.5 182.0 116.6 286.0 138.8 286.0 138.8 259.3 90.5 182.0Z',
   l:'M138.8 259.3 138.8 286.0 161.1 286.0 340.0 0.0 301.0 0.0Z'}
];
var MARK_LONG = [[0,'#62D2B4'],[.02,'#6FD4AA'],[.10,'#8BD58A'],[.18,'#A8D56E'],
  [.26,'#C8D551'],[.34,'#E7D535'],[.42,'#FBC725'],[.50,'#F8A229'],[.58,'#F37E2F'],
  [.66,'#F15932'],[.74,'#E93C36'],[.82,'#D22D3D'],[1,'#BF1E46']];
var MARK_SHORT = [[0,'#663AB4'],[.092,'#623CB6'],[.181,'#5E41B8'],[.267,'#554CC1'],
  [.356,'#4858C9'],[.442,'#3E64CF'],[.531,'#3672D4'],[.65,'#3D9BC6'],[.767,'#68BD9E'],
  [1,'#79CCAC']];
function markStops(list, id){
  return '<linearGradient id="'+id+'" gradientUnits="userSpaceOnUse" ' +
    (id.includes('-l') ? 'x1="-92" y1="147.1" x2="95.6" y2="-152.9"'
                           : 'x1="81.8" y1="130.8" x2="167.6" y2="268"') + '>' +
    list.map(function(st){ return '<stop offset="'+st[0]+'" stop-color="'+st[1]+'"/>'; }).join('') +
    '</linearGradient>';
}
function rainbowMark(h, id){
  id = id || 'rb';
  return '<svg class="rbmark" viewBox="0 0 340 286" height="'+h+'" role="img" '+
    'aria-label="Amiga" preserveAspectRatio="xMidYMid meet">'+
    '<defs>'+markStops(MARK_SHORT, id+'-s')+markStops(MARK_LONG, id+'-l')+'</defs>'+
    MARK_GEO.map(function(g){
      return '<path d="'+g.s+'" fill="url(#'+id+'-s)"/>'+
             '<path d="'+g.l+'" fill="url(#'+id+'-l)"/>';
    }).join('')+
    '</svg>';
}

/* ------------------------------------------------------- pixel wordmark
   A 5x7 bitmap face, drawn as SVG rects. Hand-authored so the Amiga
   character survives with no webfont and no network. */
var GLYPHS = {
  A:['01110','10001','10001','11111','10001','10001','10001'],
  M:['10001','11011','10101','10101','10001','10001','10001'],
  I:['11111','00100','00100','00100','00100','00100','11111'],
  G:['01110','10001','10000','10111','10001','10001','01111'],
  C:['01110','10001','10000','10000','10000','10001','01110'],
  O:['01110','10001','10001','10001','10001','10001','01110'],
  N:['10001','11001','10101','10011','10001','10001','10001'],
  S:['01111','10000','10000','01110','00001','00001','11110'],
  T:['11111','00100','00100','00100','00100','00100','00100'],
  E:['11111','10000','10000','11110','10000','10000','11111'],
  L:['10000','10000','10000','10000','10000','10000','11111'],
  ' ':['00000','00000','00000','00000','00000','00000','00000']
};
function wordmark(text, px){
  var W = 0, parts = [], x = 0;
  for(var i=0;i<text.length;i++){
    var g = GLYPHS[text[i]]; if(!g) continue;
    for(var r=0;r<7;r++) for(var c=0;c<5;c++){
      if(g[r][c]==='1') parts.push('<rect x="'+(x+c)+'" y="'+r+'" width="1" height="1"/>');
    }
    x += 6;
  }
  W = x - 1;
  return '<svg id="wordmark" viewBox="0 0 '+W+' 7" width="'+(W*px)+'" height="'+(7*px)+
         '" role="img" aria-label="'+text+'" shape-rendering="crispEdges" fill="currentColor">'+
         parts.join('')+'</svg>';
}
