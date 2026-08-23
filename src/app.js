/* ==========================================================================
   The Amiga Constellation — application
   ========================================================================== */
(function(){
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
  chev:'<svg class="chev" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4.5 2.5 8.5 6l-4 3.5"/></svg>',
  search:'<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"><circle cx="6.8" cy="6.8" r="4.4"/><path d="M10.2 10.2 14 14"/></svg>',
  x:'<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M4 4l8 8M12 4l-8 8"/></svg>',
  menu:'<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"><path d="M2 4h12M2 8h12M2 12h12"/></svg>',
  sun:'<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><circle cx="8" cy="8" r="3.1"/><path d="M8 1v1.6M8 13.4V15M1 8h1.6M13.4 8H15M3.1 3.1l1.1 1.1M11.8 11.8l1.1 1.1M12.9 3.1l-1.1 1.1M4.2 11.8l-1.1 1.1"/></svg>',
  fit:'<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="M2 5.5V2h3.5M10.5 2H14v3.5M14 10.5V14h-3.5M5.5 14H2v-3.5"/></svg>',
  plus:'<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"><path d="M8 3v10M3 8h10"/></svg>',
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
  var d = el('div','plate' + (size ? ' plate-' + size : ''));
  d.style.setProperty('--era', eraColor(e.era));
  d.style.setProperty('--era-t', eraTint(e.era));
  var g = el('span','plate-pict'); g.innerHTML = pictSvg(e);
  d.appendChild(g);
  if(e.img){
    var im = document.createElement('img');
    im.className = 'plate-img';
    im.alt = e.n; im.loading = 'lazy'; im.decoding = 'async';
    im.addEventListener('error', function(){ if(im.parentNode) im.parentNode.removeChild(im); });
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

/* ---------------------------------------------------------------- shell */
function buildShell(){
  document.body.innerHTML =
  '<div id="app">'+
    '<a class="skip" href="#stage">Skip to the collection</a>'+
    '<header class="topbar">'+
      '<div class="brand">'+
        rainbowMark(26,'rb-top')+
        '<span class="wm"><b>Amiga</b> <i>Constellation</i></span>'+
        '<span class="sub">A field guide</span>'+
      '</div>'+
      '<nav class="viewtoggle" role="group" aria-label="View">'+
        '<button id="v-overview" aria-pressed="false">Overview</button>'+
        '<button id="v-catalog"  aria-pressed="false">Catalogue</button>'+
        '<button id="v-graph"    aria-pressed="false">Lineage</button>'+
      '</nav>'+
      '<div class="searchwrap">'+ICON.search+
        '<input id="search" type="search" placeholder="Search machines, systems, projects, people…" autocomplete="off" spellcheck="false" aria-label="Search">'+
        '<button id="searchclear" hidden aria-label="Clear search">×</button>'+
      '</div>'+
      '<div class="topright">'+
        '<button class="iconbtn" id="railtoggle" aria-label="Filters">'+ICON.menu+'</button>'+
        '<button class="iconbtn" id="themebtn" aria-label="Toggle light and dark">'+ICON.sun+'</button>'+
      '</div>'+
    '</header>'+
    '<div class="body">'+
      '<aside class="rail" id="rail" aria-label="Filters"></aside>'+
      '<main class="stage" id="stage" tabindex="-1">'+
        '<section class="pane" id="pane-overview" hidden></section>'+
        '<section class="pane" id="pane-catalog" hidden>'+
          '<div class="stagebar">'+
            '<div class="chips" id="chips"></div>'+
            '<button class="btn ghost gsm" id="expandall" type="button">Expand all</button>'+
            '<button class="btn ghost gsm" id="collapseall" type="button">Collapse all</button>'+
            '<button class="btn ghost gsm" id="cess" type="button" aria-pressed="false"></button>'+
            '<div class="sortwrap"><label for="sort">Sort</label>'+
              '<select id="sort">'+
                '<option value="year">Year, oldest first</option>'+
                '<option value="year-desc">Year, newest first</option>'+
                '<option value="name">Name A–Z</option>'+
                '<option value="weight">Significance</option>'+
              '</select></div>'+
          '</div>'+
          '<div class="scroll" id="cat-scroll"></div>'+
        '</section>'+
        '<section class="pane" id="pane-graph" hidden>'+
          '<div class="gbar">'+
            '<label class="glbl" for="groupby">Arrange</label>'+
            '<select id="groupby" aria-label="Arrange clusters by"></select>'+
            '<div class="gdetail" role="group" aria-label="Level of detail">'+
              '<button id="glvl-c" type="button" aria-pressed="true">Clusters</button>'+
              '<button id="glvl-a" type="button" aria-pressed="false">Every entry</button>'+
            '</div>'+
            '<button class="gchip gess" id="gess" type="button" aria-pressed="false">Essentials</button>'+
            '<button class="gchip" id="gopenall" type="button" hidden>Open all</button>'+
            '<button class="gchip" id="gcloseall" type="button" hidden>Close all</button>'+
            /* the six routes live behind one disclosure instead of six chips
               that wrapped onto a second line and ate the map */
            '<details class="groutes" id="groutes">'+
              '<summary class="gchip" id="groutesbtn">Routes</summary>'+
              '<div class="groutes-p"><div class="gtours" id="gtours"></div></div>'+
            '</details>'+
          '</div>'+
          '<div class="graphwrap">'+
            '<canvas id="gcanvas"></canvas>'+
            /* the running count is a caption over the map, not a bar: it is
               information about the view, and it costs no layout height */
            '<div class="gcap">'+
              '<span class="gcount" id="gcount"></span>'+
              '<div class="glegend" id="glegend"></div>'+
            '</div>'+
            '<div class="gtools">'+
              '<button class="iconbtn" id="gzin" aria-label="Zoom in">'+ICON.plus+'</button>'+
              '<button class="iconbtn" id="gzout" aria-label="Zoom out">'+ICON.minus+'</button>'+
              '<button class="iconbtn" id="gfit" aria-label="Fit to view">'+ICON.fit+'</button>'+
            '</div>'+
            '<div id="gtip" aria-live="polite"></div>'+

            '<div class="tourbar" id="tourbar" hidden>'+
              '<div class="tourhead">'+
                '<span class="tourtitle" id="tourtitle"></span>'+
                '<span class="tourstep mono" id="tourstep"></span>'+
                '<button class="iconbtn toursm" id="tourexit" aria-label="End route">'+ICON.x+'</button>'+
              '</div>'+
              '<div class="tourname" id="tourname"></div>'+
              '<div class="tournote" id="tournote"></div>'+
              '<div class="tourctl">'+
                '<button class="btn ghost gsm" id="tourprev">← Back</button>'+
                '<button class="btn gsm" id="tournext">Next →</button>'+
                '<button class="btn ghost gsm" id="tourinfo">Details</button>'+
              '</div>'+
            '</div>'+
          '</div>'+
          /* the board sits BESIDE the map, not inside it, so opening a cluster
             lets the control bar scroll away with the content */
          '<div class="lfocus" id="lfocus" hidden></div>'+
        '</section>'+
        '<div class="scrim" id="scrim"></div>'+
        '<aside class="drawer" id="drawer" role="complementary" aria-label="Details" aria-hidden="true"></aside>'+
      '</main>'+
    '</div>'+
  '</div>';
}

/* ---------------------------------------------------------------- rail */
function facet(title, dim, items, key, help){
  var d = el('details','facet'); d.open = true;
  var sum = document.createElement('summary');
  sum.innerHTML = ICON.chev + '<span>' + title + '</span>' +
                  '<span class="on" data-on-for="'+dim+'" hidden>0</span>';
  d.appendChild(sum);
  var body = el('div','facetbody');
  if(help){ body.appendChild(el('div','facethelp', help)); }
  items.forEach(function(it){
    if(it.groupLabel){ body.appendChild(el('div','grouplbl', it.groupLabel)); return; }
    var b = el('button','opt');
    b.type = 'button';
    b.setAttribute('data-dim', dim);
    b.setAttribute('data-val', it.id);
    b.setAttribute('aria-pressed','false');
    if(it.title) b.title = it.title;
    var box = el('span','box'); box.innerHTML = ICON.check; b.appendChild(box);
    if(it.swatch){ var sw = el('span','swatch'); sw.style.background = it.swatch; b.appendChild(sw); }
    if(it.dot){ var dt = el('span','dot'); dt.style.background = it.dot; b.appendChild(dt); }
    b.appendChild(el('span','lbl', it.label));
    b.appendChild(el('span','n','0'));
    body.appendChild(b);
  });
  d.appendChild(body);
  return d;
}

function buildRail(){
  var rail = $('#rail');
  rail.innerHTML = '';
  var head = el('div','railhead');
  var cw = el('div','count');
  cw.innerHTML = '<b id="rescount">0</b> of ' + ALL.length;
  head.appendChild(cw);
  var reset = el('button', null, 'Reset'); reset.id = 'resetall'; reset.type = 'button'; reset.hidden = true;
  head.appendChild(reset);
  rail.appendChild(head);

  rail.appendChild(facet('Architecture','arch', M.archs.map(function(a){
    return {id:a.id, label:a.label, title:a.note};
  }), null, 'Which processor or platform the item runs on — hardware and software alike.'));

  rail.appendChild(facet('Development status','st', M.statuses.map(function(s){
    return {id:s.id, label:s.label, title:s.note,
            dot:'var(--'+(s.tone==='good'?'ok':s.tone==='warn'?'warn':s.tone==='info'?'info':'off')+')'};
  })));

  rail.appendChild(facet('Era','era', M.eras.map(function(e,i){
    return {id:e.id, label:e.label+'  ·  '+e.range, title:e.note, swatch:'var(--e'+(i+1)+')'};
  })));

  var catItems = [];
  M.groups.forEach(function(g){
    catItems.push({groupLabel:g.label});
    g.cats.forEach(function(c){ catItems.push({id:c.id, label:c.label}); });
  });
  rail.appendChild(facet('Category','cat', catItems));

  rail.appendChild(facet('Kind','kind', M.kinds.map(function(k){
    return {id:k.id, label:k.label, title:k.note};
  }), null, 'The distinction that prevents most Amiga confusion: an original machine, a replica, an FPGA recreation, an emulator and a curated environment are five different things.'));

  rail.addEventListener('click', function(ev){
    var b = ev.target.closest('.opt'); if(!b) return;
    var dim = b.getAttribute('data-dim'), val = b.getAttribute('data-val');
    if(S.f[dim][val]) delete S.f[dim][val]; else S.f[dim][val] = 1;
    if(S.view === 'overview') setView('catalog'); else render();
  });
  reset.addEventListener('click', resetAll);
}

function resetAll(){
  DIMS.forEach(function(d){ S.f[d] = {}; });
  S.q = ''; $('#search').value = '';
  S.ess = false;
  render();
}

/* How many entries the weight-5 landmark filter keeps. */
var ESS_N = ALL.filter(function(e){ return (e.wt || 0) >= 5; }).length;

/* One notion of "essentials", shared by the catalogue and the lineage map:
   the catalogue drops its non-landmark plates, the map re-forms its clusters
   from what is left, and the rail count and chips report it like any other
   filter. Both toggles read and write this single flag, so the two views can
   never disagree about what is showing. */
function setEss(on){
  if(S.ess === on) return;
  S.ess = on;
  if(GRAPH.tour) tourEnd();
  if(on && S.sel && (BY_ID[S.sel].wt || 0) < 5) closeDetail();
  if(S.view === 'graph') graphRemap();
  render();
}

/* both Essentials toggles reflect the one flag */
function syncEss(){
  ['#gess','#cess'].forEach(function(sel){
    var b = $(sel);
    if(b) b.setAttribute('aria-pressed', S.ess ? 'true' : 'false');
  });
}

function syncRail(){
  var counts = {
    arch: facetCounts('arch'), st: facetCounts('st'),
    era: facetCounts('era'), cat: facetCounts('cat'), kind: facetCounts('kind')
  };
  Array.prototype.forEach.call(document.querySelectorAll('.opt'), function(b){
    var dim = b.getAttribute('data-dim'), val = b.getAttribute('data-val');
    var on = !!S.f[dim][val], n = counts[dim][val] || 0;
    b.setAttribute('aria-pressed', on ? 'true' : 'false');
    b.querySelector('.n').textContent = n;
    b.setAttribute('data-zero', (!on && n === 0) ? '1' : '0');
  });
  Array.prototype.forEach.call(document.querySelectorAll('[data-on-for]'), function(s){
    var dim = s.getAttribute('data-on-for');
    var n = facetOn(dim).length;
    s.textContent = n; s.hidden = n === 0;
    /* mark the whole facet so its heading picks up the accent when it is
       narrowing the results — visible even with the facet collapsed */
    var f = s.closest ? s.closest('.facet') : null;
    if(f) f.setAttribute('data-on', n ? '1' : '0');
  });
  syncEss();
  var r = results();
  $('#rescount').textContent = r.length;
  $('#resetall').hidden = activeCount() === 0;
  $('#searchclear').hidden = !S.q;
  return r;
}

/* ---------------------------------------------------------------- chips */
function buildChips(){
  var c = $('#chips'); c.innerHTML = '';
  function add(label, val, onClear){
    var ch = el('span','chip');
    ch.innerHTML = '<span>'+label+' <b>'+val+'</b></span>';
    var x = el('button', null, '×'); x.type='button';
    x.setAttribute('aria-label','Remove filter '+val);
    x.addEventListener('click', onClear);
    ch.appendChild(x); c.appendChild(ch);
  }
  if(S.q) add('Search', '“'+S.q+'”', function(){ S.q=''; $('#search').value=''; render(); });
  if(S.ess) add('Only', 'Essentials', function(){ setEss(false); });
  var maps = {arch:ARCH_LABEL, st:ST_LABEL, cat:CAT_LABEL, kind:KIND_LABEL};
  DIMS.forEach(function(dim){
    facetOn(dim).forEach(function(v){
      var lbl = dim==='era' ? (ERA_OF[v]?ERA_OF[v].label:v) : (maps[dim][v] || v);
      var name = {arch:'Arch',st:'Status',era:'Era',cat:'Category',kind:'Kind'}[dim];
      add(name, lbl, function(){ delete S.f[dim][v]; render(); });
    });
  });
}

/* ---------------------------------------------------------------- cards */
function statusEl(e){
  var s = el('span','st'); s.setAttribute('data-tone', ST_TONE[e.st] || 'off');
  s.innerHTML = '<i></i>' + (ST_LABEL[e.st] || e.st);
  return s;
}
function card(e){
  var b = el('button','card'); b.type = 'button';
  b.setAttribute('data-id', e.id);
  b.style.setProperty('--era', eraColor(e.era));
  b.style.setProperty('--era-t', eraTint(e.era));
  if(S.sel === e.id) b.setAttribute('aria-current','true');

  b.appendChild(plateEl(e));

  var body = el('div','cardbody');
  var line = el('div','cardline');
  line.appendChild(el('h3', null, e.n));
  line.appendChild(el('span','yr', String(e.y)));
  body.appendChild(line);

  /* one era-coloured spec line, the way a museum label runs
     maker · status · architecture under the object's name */
  var spec = [e.by]
    .concat((e.arch||[]).slice(0,2).map(function(a){ return ARCH_LABEL[a]||a; }))
    .filter(Boolean).join(' · ');
  body.appendChild(el('div','cardspec', spec));
  body.appendChild(el('p', null, e.d));

  var foot = el('div','cardfoot');
  foot.appendChild(statusEl(e));
  if(e.u) foot.appendChild(el('span','haslink','Has official page'));
  if(e.unv){ var u = el('span','unv','?'); u.title = 'Some details unverified'; foot.appendChild(u); }
  body.appendChild(foot);

  b.appendChild(body);
  return b;
}

function renderCatalog(list){
  var host = $('#cat-scroll');
  host.innerHTML = '';
  buildChips();

  if(!list.length){
    var em = el('div','empty');
    em.innerHTML = '<h2>Nothing matches those filters</h2>'+
      '<p>Try removing a filter — combinations like <em>PowerPC</em> plus <em>Origins</em> have no entries by definition.</p>';
    var btn = el('button','btn','Reset filters'); btn.type='button';
    btn.addEventListener('click', resetAll);
    em.appendChild(btn); host.appendChild(em);
    return;
  }

  var sorted = list.slice();
  if(S.sort === 'year')       sorted.sort(function(a,b){ return a.y - b.y || a.n.localeCompare(b.n); });
  if(S.sort === 'year-desc')  sorted.sort(function(a,b){ return b.y - a.y || a.n.localeCompare(b.n); });
  if(S.sort === 'name')       sorted.sort(function(a,b){ return a.n.localeCompare(b.n); });
  if(S.sort === 'weight')     sorted.sort(function(a,b){ return (b.wt||0)-(a.wt||0) || a.y-b.y; });

  var buckets = {};
  sorted.forEach(function(e){ (buckets[e.cat] = buckets[e.cat] || []).push(e); });
  var filtering = activeCount() > 0;
  /* A filter used to force every section open, which meant the chevrons did
     nothing while filtering. Now the filter only changes the DEFAULT: when it
     changes, explicit choices are dropped so results are visible, and after
     that a click on any section is honoured either way. */
  var sig = JSON.stringify([S.f, S.q]);
  if(sig !== S.filterSig){
    S.filterSig = sig;
    S.open.c = {}; S.open.g = {};
    S.openedFirst = filtering ? 1 : 0;
  }
  if(!S.openedFirst){
    /* open the first section that has anything in it, so the catalogue never
       greets you as a wall of shut drawers */
    S.openedFirst = 1;
    for(var gi = 0; gi < M.groups.length; gi++){
      var fc = M.groups[gi].cats.filter(function(c){ return (buckets[c.id]||[]).length; })[0];
      if(fc){ S.open.c[fc.id] = 1; break; }
    }
  }

  M.groups.forEach(function(g){
    var gItems = 0;
    g.cats.forEach(function(c){ gItems += (buckets[c.id]||[]).length; });
    if(!gItems) return;

    /* groups default open; categories default open only while filtering */
    var gOpen = Object.prototype.hasOwnProperty.call(S.open.g, g.id)
      ? !!S.open.g[g.id] : true;
    var gh = el('button','grouphead'); gh.type = 'button';
    gh.setAttribute('aria-expanded', gOpen ? 'true' : 'false');
    gh.innerHTML = ICON.chev;
    gh.appendChild(el('h2', null, g.label));
    gh.appendChild(el('span','n', gItems + (gItems===1?' entry':' entries')));
    gh.addEventListener('click', function(){
      S.open.g[g.id] = !gOpen;
      renderCatalog(results());
    });
    host.appendChild(gh);
    if(!gOpen) return;

    g.cats.forEach(function(c){
      var items = buckets[c.id]; if(!items || !items.length) return;
      var cOpen = Object.prototype.hasOwnProperty.call(S.open.c, c.id)
        ? !!S.open.c[c.id] : filtering;
      var ch = el('button','cathead'); ch.type = 'button';
      ch.setAttribute('aria-expanded', cOpen ? 'true' : 'false');
      ch.innerHTML = ICON.chev;
      ch.appendChild(el('h3', null, c.label));
      ch.appendChild(el('span','n', String(items.length)));
      if(!cOpen){
        var tops = items.slice().sort(function(a,b){ return (b.wt||0)-(a.wt||0); })
                        .slice(0,3).map(function(e){ return e.n; }).join(' · ');
        ch.appendChild(el('span','preview', tops + (items.length > 3 ? ' …' : '')));
      }
      ch.addEventListener('click', function(){
        S.open.c[c.id] = !cOpen;
        renderCatalog(results());
      });
      host.appendChild(ch);
      if(cOpen){
        var grid = el('div','grid');
        items.forEach(function(e){ grid.appendChild(card(e)); });
        host.appendChild(grid);
      }
    });
  });
}

/* ---------------------------------------------------------------- overview */
function renderOverview(){
  var host = $('#pane-overview');
  if(host.getAttribute('data-built')) return;
  host.setAttribute('data-built','1');

  var live  = ALL.filter(function(e){ return e.st==='shipping'||e.st==='active'; }).length;
  var links = ALL.reduce(function(a,e){ return a + (e.rel||[]).length; }, 0);
  var wrap  = el('div','scroll ovscroll');
  var r     = el('div','front');

  /* ---------------- hero ---------------- */
  var hero = el('div','fhero');
  hero.innerHTML =
    '<div class="fmark">'+rainbowMark(92,'rb-hero')+'</div>'+
    '<div class="fheadline">'+
      '<div class="eyebrow">A field guide · snapshot August 2026</div>'+
      '<h1>A computer that outlived<br>its manufacturer by<br>30+ years.</h1>'+
      '<p class="flede">Commodore went bankrupt in April 1994. The Amiga did not. This is a map of what '+
      'happened instead — the accelerators, replica boards and FPGA recreations that kept the hardware '+
      'running, the operating systems still in development, the emulators that quietly became '+
      'infrastructure, and the archives that made all of it usable.</p>'+
    '</div>';
  r.appendChild(hero);

  /* Three hundred and thirty entries is a lot to be handed at once. This says
     what the thing is and how to work it, in about fifteen seconds. */
  var howto = el('div','fhow');
  howto.innerHTML =
    '<div class="fhow-lead">'+
      '<span class="eyebrow">What this is</span>'+
      '<p>A catalogue of <b>'+ALL.length+' things</b> — machines, boards, operating systems, '+
      'shells, emulators, distributions and archives — with what each one is, who makes it, '+
      'whether it still ships, and what it descends from. Nothing is sold here and nothing is '+
      'tracked; it is one file you can keep.</p>'+
    '</div>'+
    '<div class="fhow-steps">'+
      '<div class="fhow-s"><span class="fhow-n mono">1</span>'+
        '<div><h3>Browse the Catalogue</h3>'+
        '<p>Everything, in numbered sections. Open a section to see its plates; click any plate '+
        'for the full entry. The column on the left narrows by processor, era, status or kind '+
        '&mdash; the counts tell you what is left before you click.</p></div></div>'+
      '<div class="fhow-s"><span class="fhow-n mono">2</span>'+
        '<div><h3>Use the Lineage to see how things connect</h3>'+
        '<p>Twelve clusters, each showing how many entries it holds and how they spread across '+
        'the eras. Open one and its members lay out in time order, joined to whatever they '+
        'descend from.</p></div></div>'+
      '<div class="fhow-s"><span class="fhow-n mono">3</span>'+
        '<div><h3>Or follow a route</h3>'+
        '<p>Six guided paths through the collection, each a story told in order &mdash; the 68k '+
        'continuum, the PowerPC branch, the FPGA rebirth. They are the fastest way in if you do '+
        'not know what you are looking for.</p></div></div>'+
    '</div>';
  r.appendChild(howto);

  var rb = el('div','frule'); r.appendChild(rb);

  /* ---------------- headline numbers ---------------- */
  var stats = el('div','fstats');
  [[String(ALL.length),'entries catalogued'],[String(live),'shipping or active'],
   [String(M.groups.reduce(function(a,g){ return a + g.cats.length; },0)),'sections'],
   [String(links),'lineage links'],['1984—2026','years covered']]
  .forEach(function(pr){
    var d = el('div','fstat');
    d.innerHTML = '<div class="v">'+pr[0]+'</div><div class="k">'+pr[1]+'</div>';
    stats.appendChild(d);
  });
  r.appendChild(stats);

  /* ---------------- featured plates ----------------
     One object from each of eight different sections, so the front page is a
     cross-section of the whole collection rather than a top-eight list. */
  r.appendChild(sectionHead('01','Selected objects',
    'Eight of the three hundred and thirty. Any plate opens its full entry.'));
  var picked = [], usedCat = {};
  ['hw-classic','hw-fpga','hw-accel','os','shell','emu','distro','pres'].forEach(function(cid){
    var best = ALL.filter(function(e){ return e.cat===cid; })
                  .sort(function(a,b){ return (b.wt||0)-(a.wt||0) || a.y-b.y; })[0];
    if(best && !usedCat[cid]){ usedCat[cid] = 1; picked.push(best); }
  });
  var fg = el('div','fplates');
  picked.forEach(function(e){
    var b = el('button','fplate'); b.type='button';
    b.style.setProperty('--era', eraColor(e.era));
    b.style.setProperty('--era-t', eraTint(e.era));
    b.appendChild(plateEl(e,'front'));
    var t = el('div','fplate-t');
    t.innerHTML = '<div class="fpline"><span class="fpn">'+esc(e.n)+'</span>'+
      '<span class="yr mono">'+e.y+'</span></div>'+
      '<div class="fpspec">'+esc(CAT_LABEL[e.cat])+'</div>';
    b.appendChild(t);
    b.addEventListener('click', function(){ setView('catalog'); openDetail(e.id); });
    fg.appendChild(b);
  });
  r.appendChild(fg);

  /* ---------------- eras ---------------- */
  r.appendChild(sectionHead('02','Four eras','Choose one and the catalogue filters to it.'));
  var eras = el('div','feras');
  M.eras.forEach(function(x,i){
    var n = ALL.filter(function(e){ return e.era===x.id; }).length;
    var b = el('button','fera'); b.type='button';
    b.style.setProperty('--era','var(--e'+(i+1)+')');
    b.style.setProperty('--era-t','var(--e'+(i+1)+'-t)');
    b.innerHTML =
      '<div class="fera-n mono">'+x.range+'</div>'+
      '<h3>'+esc(x.label)+'</h3>'+
      '<p>'+esc(x.note)+'</p>'+
      '<div class="fera-c"><b class="mono">'+n+'</b> entries</div>';
    b.addEventListener('click', function(){
      DIMS.forEach(function(d){ S.f[d] = {}; });
      S.f.era[x.id] = 1; setView('catalog');
    });
    eras.appendChild(b);
  });
  r.appendChild(eras);

  /* ---------------- guided routes ---------------- */
  if(TOURS.length){
    r.appendChild(sectionHead('03','Guided routes',
      'Six paths through the lineage, each one a story told in order.'));
    var tg = el('div','froutes');
    TOURS.forEach(function(t){
      var b = el('button','froute'); b.type='button';
      b.innerHTML = '<span class="froute-n mono">'+pad2(t.steps.length)+' stops</span>'+
        '<h3>'+esc(t.label)+'</h3>'+
        '<span class="froute-go">Follow the route &rarr;</span>';
      b.addEventListener('click', function(){ setView('graph'); tourStart(t.id); });
      tg.appendChild(b);
    });
    r.appendChild(tg);
  }

  /* ---------------- taxonomy ---------------- */
  r.appendChild(sectionHead('04','How to read this',
    'Six kinds of thing are constantly confused for one another. The Kind filter keeps them apart.'));
  var tax = el('div','ftax');
  ['original','replica','fpga','emulator','compatible','oslike'].forEach(function(id){
    var k = M.kinds.find(function(x){ return x.id===id; });
    if(!k) return;
    var d = el('div','ftaxrow');
    d.innerHTML = '<b>'+esc(k.label)+'</b><span>'+esc(k.note)+'</span>';
    tax.appendChild(d);
  });
  r.appendChild(tax);

  /* ---------------- theses ---------------- */
  r.appendChild(sectionHead('05','Six things this map shows', null));
  var th = el('div','ftheses');
  M.theses.forEach(function(t,i){
    var d = el('div','fthesis');
    d.innerHTML = '<span class="fth-n mono">'+pad2(i+1)+'</span>'+
      '<div><h3>'+esc(t.t)+'</h3><p>'+esc(t.b)+'</p></div>';
    th.appendChild(d);
  });
  r.appendChild(th);

  /* ---------------- cautions ---------------- */
  r.appendChild(sectionHead('06','Cautions','This is a snapshot, not a guarantee.'));
  var cl = el('div','fcautions');
  M.cautions.forEach(function(c){
    var d = el('div','fcaution');
    d.innerHTML = '<b>'+esc(c[0])+'</b><span>'+esc(c[1])+'</span>';
    cl.appendChild(d);
  });
  r.appendChild(cl);

  var f = el('div','ffoot');
  f.innerHTML = '<div class="frule frule-sm"></div>'+
    '<p>Compiled from primary sources — official project and vendor pages, GitHub releases, Aminet, '+
    'OS4Depot, MorphOS Storage, amiga-news.de and community archives — verified August 2026. Entries marked '+
    '<span class="unv">?</span> contain details that could not be confirmed against a primary source. '+
    'Years given are first public release; announcement and retail dates often differ. '+
    'Listing a project is not an endorsement of any unofficial download bundle.</p>'+
    '<p class="fnote">Plate numbers read section · index. Every word, number and link on this site comes '+
    'from <code>amiga-data.js</code>, sitting next to this file — edit it and reload.</p>';
  r.appendChild(f);

  wrap.appendChild(r);
  host.appendChild(wrap);
}

function esc(t){
  return String(t == null ? '' : t)
    .replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}
function sectionHead(num, title, note){
  var d = el('div','fsect');
  d.innerHTML = '<span class="fsect-n mono">'+num+'</span>'+
    '<h2>'+esc(title)+'</h2>'+
    (note ? '<p>'+esc(note)+'</p>' : '');
  return d;
}

/* ---------------------------------------------------------------- drawer */
/* Raised for the remainder of the click that opens the sheet, so the
   document-level dismiss handler can tell "the user clicked away" from "the
   user just picked this entry". Cleared on the next turn of the event loop,
   which is after the click has finished bubbling. */
var OPENING = false;

/* Whether the sheet is actually on screen. S.sel is a selection, which a
   running route and the cluster board also set without opening anything. */
function sheetOpen(){
  var d = $('#drawer');
  return !!d && d.getAttribute('data-open') === '1';
}

function openDetail(id){
  OPENING = true;
  setTimeout(function(){ OPENING = false; }, 0);
  var e = BY_ID[id]; if(!e) return;
  S.sel = id;
  var d = $('#drawer');
  d.style.setProperty('--era', eraColor(e.era));
  d.innerHTML = '';

  d.style.setProperty('--era-t', eraTint(e.era));

  var close = el('button','iconbtn dclose'); close.type='button';
  close.setAttribute('aria-label','Close details'); close.innerHTML = ICON.x;
  d.appendChild(close);

  /* one scroll container for the whole panel — the picture and the title
     scroll away with the text instead of permanently eating the top third */
  var sc = el('div','dscroll');
  d.appendChild(sc);

  /* A picture earns the space at the top of the sheet. A pictogram does not —
     it is an identity mark, so it rides inline with the title and the reader
     gets 216px of text back. */
  if(e.img) sc.appendChild(plateEl(e,'lg'));

  var h = el('div','dhead' + (e.img ? '' : ' dhead-mark'));
  var ht = el('div','dhead-t');
  ht.appendChild(el('div','eyebrow', 'Plate ' + plateNo(e) + ' · ' + CAT_LABEL[e.cat]));
  ht.appendChild(el('h2', null, e.n));
  if(e.by) ht.appendChild(el('div','by', e.by));
  if(e.img){
    h.appendChild(ht);
  } else {
    var row = el('div','dhead-row');
    row.appendChild(thumbEl(e,'lg'));
    row.appendChild(ht);
    h.appendChild(row);
  }
  var meta = el('div','dmeta');
  meta.appendChild(statusEl(e));
  (e.arch||[]).forEach(function(a){ meta.appendChild(el('span','ac', ARCH_LABEL[a]||a)); });
  h.appendChild(meta);
  sc.appendChild(h);
  close.addEventListener('click', closeDetail);

  var b = el('div','dbody');

  if(e.unv){
    var w = el('div','warnbox dsec');
    w.innerHTML = '<b>Partly unverified.</b> Some details in this entry — typically current availability, '+
      'exact version or attribution — could not be confirmed against a primary source in August 2026.';
    b.appendChild(w);
  }

  var s1 = el('div','dsec');
  s1.appendChild(el('span','eyebrow','What it is'));
  s1.appendChild(el('p', null, e.d));
  b.appendChild(s1);

  if(e.w){
    var s2 = el('div','dsec why');
    s2.appendChild(el('span','eyebrow','Why it matters'));
    s2.appendChild(el('p', null, e.w));
    b.appendChild(s2);
  }

  var s3 = el('div','dsec');
  s3.appendChild(el('span','eyebrow','Facts'));
  var facts = el('div','dfacts');
  function fact(k,v){
    if(!v) return;
    var f = el('div','dfact');
    f.appendChild(el('div','k',k));
    var vv = el('div','v'); vv.textContent = v; f.appendChild(vv);
    facts.appendChild(f);
  }
  fact('Year', String(e.y) + '  ·  ' + ERA_OF[e.era].label);
  fact('Kind', KIND_LABEL[e.kind] || e.kind);
  fact('Status', ST_LABEL[e.st] || e.st);
  fact('Architecture', (e.arch||[]).map(function(a){ return ARCH_LABEL[a]||a; }).join(', '));
  fact('Detail', e.ver);
  s3.appendChild(facts);
  b.appendChild(s3);

  function relSection(title, ids, note){
    if(!ids || !ids.length) return;
    var s = el('div','dsec');
    s.appendChild(el('span','eyebrow', title));
    if(note) s.appendChild(el('div','facethelp', note));
    var list = el('div','rellist');
    ids.forEach(function(rid){
      var t = BY_ID[rid]; if(!t) return;
      var rb = el('button','relbtn'); rb.type='button';
      rb.style.setProperty('--era', eraColor(t.era));
      var bar = el('span','bar'); rb.appendChild(bar);
      rb.appendChild(el('span', null, t.n));
      rb.appendChild(el('span','yr', String(t.y)));
      rb.addEventListener('click', function(){ openDetail(rid); });
      list.appendChild(rb);
    });
    s.appendChild(list);
    b.appendChild(s);
  }
  relSection('Descends from', e.rel);
  relSection('Led to', e.children);

  /* links: official when verified, plus generated searches for everything */
  var s6 = el('div','dsec');
  s6.appendChild(el('span','eyebrow','Links'));
  var lr = el('div','linkrow');
  function extlink(label, href, primary){
    var a = el('a','extlink' + (primary ? ' primary' : ''), label);
    a.href = href; a.target = '_blank'; a.rel = 'noopener noreferrer';
    lr.appendChild(a);
  }
  if(e.u) extlink('Official site ↗', e.u, true);
  var q = encodeURIComponent(e.n + ' Amiga');
  extlink('Wikipedia ↗', 'https://en.wikipedia.org/w/index.php?search=' + q);
  extlink('Web search ↗', 'https://duckduckgo.com/?q=' + q);
  if(['soft','dev','shell','tech','distro','os'].includes(e.cat))
    extlink('Aminet ↗', 'https://aminet.net/search?query=' + encodeURIComponent(e.n));
  s6.appendChild(lr);
  if(!e.u) s6.appendChild(el('div','facethelp',
    'No verified official page for this entry — the searches above are the reliable route.'));
  b.appendChild(s6);

  if(e.imgCredit){
    var s7 = el('div','dsec');
    s7.appendChild(el('span','eyebrow','Image'));
    s7.appendChild(el('div','facethelp', e.imgCredit));
    b.appendChild(s7);
  }

  if(e.tags && e.tags.length){
    var s5 = el('div','dsec');
    s5.appendChild(el('span','eyebrow','Tags'));
    var tr = el('div','tagrow');
    e.tags.forEach(function(t){ tr.appendChild(el('span','tag', t)); });
    s5.appendChild(tr);
    b.appendChild(s5);
  }

  sc.appendChild(b);
  d.setAttribute('data-open','1');
  d.setAttribute('aria-hidden','false');
  close.focus();
  markSelection();
  lfSyncDrawer();
  /* the sheet has just taken the right of the stage — bring the selection back
     into the part that is still visible */
  if(GRAPH.ready){
    GRAPH.draw();
    if(S.view === 'graph' && !LF.open){
      var n = GRAPH.byId[id];
      if(n && GRAPH.present[id]) focusPoint(n.x, n.y, Math.max(GRAPH.k, 1.6));
    }
  }
}
function closeDetail(){
  S.sel = null;
  if(typeof LF !== 'undefined' && LF.open){ LF.sel = null; lineageRender(); }
  lfSyncDrawer();
  var d = $('#drawer');
  d.setAttribute('data-open','0');
  d.setAttribute('aria-hidden','true');
  markSelection();
  if(GRAPH.ready) GRAPH.draw();
}
function markSelection(){
  Array.prototype.forEach.call(document.querySelectorAll('.card'), function(c){
    if(c.getAttribute('data-id') === S.sel) c.setAttribute('aria-current','true');
    else c.removeAttribute('aria-current');
  });
}

/* ================================================================ graph
   The constellation view:
   · Group by — re-cluster from six perspectives (domain, era, architecture,
     status, kind, nested matryoshka).
   · Nested is a true drill-down: it opens as AMIGA plus five group shells;
     click a shell to reveal its categories, click a category to reveal its
     entries, click again to close. Searching auto-opens matching shells.
   · Essentials only — hide everything but the weight-5 landmarks.
   Within a mode, search and filters only spotlight and dim — geography is
   stable; drilling and re-grouping are explicit re-maps.                   */

var DOMAINS = RAW.domains || [];
var DOMAIN_OF = {};
DOMAINS.forEach(function(d){ d.cats.forEach(function(c){ DOMAIN_OF[c] = d; }); });

var TOURS = RAW.tours || [];

var GROUP_OF_CAT = {};
M.groups.forEach(function(g){ g.cats.forEach(function(c){ GROUP_OF_CAT[c.id] = g.id; }); });

var GROUPBYS = [
  {id:'domain', label:'Domain'},
  {id:'era',    label:'Era'},
  {id:'arch',   label:'Architecture'},
  {id:'st',     label:'Status'},
  {id:'kind',   label:'Kind'},
  {id:'nested', label:'Nested (matryoshka)'}
];

/* The three zones of the domain view, as drawn in the design: hardware on the
   left, the systems that run on it in the middle, and the practice that keeps
   both alive on the right. Only the domain grouping has zones — the other
   groupings are one flat set, so they get the wreath. */
var ZONES = [
  {id:'z-hw',    label:'Hardware',               groups:['g-hw']},
  {id:'z-sys',   label:'System software',        groups:['g-sys']},
  {id:'z-scene', label:'Practice & stewardship', groups:['g-emu','g-make','g-scene']}
];
var ZONE_OF_GROUP = {};
ZONES.forEach(function(z){ z.groups.forEach(function(g){ ZONE_OF_GROUP[g] = z.id; }); });
function zoneOfHub(h){
  var m = h.members && h.members[0];
  if(!m) return null;
  var g = M.groups.find(function(gr){
    return gr.cats.some(function(c){ return c.id === m.e.cat; });
  });
  return g ? ZONE_OF_GROUP[g.id] : null;
}

var GRAPH = {
  ready:false, nodes:[], byId:{}, hubs:[], superHubs:[], links:[],
  cv:null, cx:null, tx:0, ty:0, k:1, hover:null, hoverHub:null,
  W:0, H:0, dpr:1, raf:0, laidOut:false, anim:null,
  groupBy:'domain', present:{}, level:'clusters', zones:[], fitAxis:'both', kHint:1, grid:false,
  exp:{g:{}, c:{}}, forceKey:'',
  tour:null, tourStep:0
};

function idHash(s){
  var h = 0;
  for(var c=0;c<s.length;c++) h = (h*31 + s.charCodeAt(c)) & 0x7fffffff;
  return h;
}

function graphBuild(){
  GRAPH.nodes = ALL.map(function(e){
    return {id:e.id, e:e, x:0, y:0, r:2.6 + (e.wt||1)*0.95,
            dom:DOMAIN_OF[e.cat] || DOMAINS[DOMAINS.length-1]};
  });
  GRAPH.byId = {};
  GRAPH.nodes.forEach(function(n){ GRAPH.byId[n.id] = n; });
  GRAPH.links = [];
  ALL.forEach(function(e){
    (e.rel||[]).forEach(function(pid){
      if(GRAPH.byId[pid]) GRAPH.links.push({a:GRAPH.byId[pid], b:GRAPH.byId[e.id]});
    });
  });
  GRAPH.ready = true;
}

function essFilter(list){
  return S.ess ? list.filter(function(n){ return (n.e.wt||0) >= 5; }) : list;
}

/* filters and search auto-open the matryoshka shells that contain matches */
function forcedExpansion(){
  var f = {g:{}, c:{}};
  if(activeCount() > 0){
    var match = {}; results().forEach(function(e){ match[e.id] = 1; });
    GRAPH.nodes.forEach(function(n){
      if(!match[n.id]) return;
      f.c[n.e.cat] = 1;
      var g = GROUP_OF_CAT[n.e.cat]; if(g) f.g[g] = 1;
    });
  }
  if(S.sel && BY_ID[S.sel]){
    f.c[BY_ID[S.sel].cat] = 1;
    var g2 = GROUP_OF_CAT[BY_ID[S.sel].cat]; if(g2) f.g[g2] = 1;
  }
  return f;
}
function forcedKey(f){
  return Object.keys(f.g).sort().join(',') + '|' + Object.keys(f.c).sort().join(',');
}

function clustersFor(mode){
  var pool = essFilter(GRAPH.nodes);
  function bucket(defs, keyFn, minSize, otherLabel){
    var map = {}, order = [];
    defs.forEach(function(d){ map[d.id] = {id:d.id, label:d.label, members:[]}; order.push(d.id); });
    var other = {id:'other-bucket', label:otherLabel || 'Other', members:[]};
    pool.forEach(function(n){
      var k = keyFn(n);
      if(map[k]) map[k].members.push(n); else other.members.push(n);
    });
    var out = order.map(function(id){ return map[id]; });
    if(minSize){
      var keep = [], merged = other.members.slice();
      out.forEach(function(c){
        if(c.members.length >= minSize) keep.push(c);
        else merged = merged.concat(c.members);
      });
      out = keep;
      other.members = merged;
    }
    if(other.members.length) out.push(other);
    return out.filter(function(c){ return c.members.length > 0; });
  }
  if(mode === 'era')
    return bucket(M.eras.map(function(e){ return {id:e.id, label:e.label+' '+e.range}; }),
                  function(n){ return n.e.era; });
  if(mode === 'arch')
    return bucket(M.archs.map(function(a){ return {id:a.id, label:a.label}; }),
                  function(n){ return (n.e.arch||[])[0] || 'other'; }, 5, 'Other platforms');
  if(mode === 'st')
    return bucket(M.statuses.map(function(s){ return {id:s.id, label:s.label}; }),
                  function(n){ return n.e.st; });
  if(mode === 'kind')
    return bucket(M.kinds.map(function(k){ return {id:k.id, label:k.label}; }),
                  function(n){ return n.e.kind; }, 8, 'Other kinds');
  return bucket(DOMAINS.map(function(d){ return {id:d.id, label:d.label}; }),
                function(n){ return n.dom.id; });
}

function ringPlan(count){
  var rings = [], left = count, i = 1;
  while(left > 0){
    var r = 17 + i*16.5;
    var cap = Math.max(6, Math.floor(2*Math.PI*r / 15.5));
    var take = Math.min(cap, left);
    rings.push({r:r, n:take});
    left -= take; i++;
  }
  return rings;
}
function placeRings(hub, members, cx0, cy0){
  var idx = 0, baseA = Math.atan2(hub.y - cy0, hub.x - cx0);
  hub.rings.forEach(function(ring, ri){
    var start = baseA + (ri % 2 ? 0.5 : 0) + (idHash(hub.uid || 'h') % 100)/100;
    for(var i=0;i<ring.n;i++){
      var a = start + (i / ring.n) * 2*Math.PI;
      var n = members[idx++];
      n.x = hub.x + Math.cos(a) * ring.r;
      n.y = hub.y + Math.sin(a) * ring.r;
    }
  });
}
/* Past about nine clusters the wreath stops helping: its corners bring
   neighbours closer in a straight line than their spacing along the path, so
   labels collide there however wide the ring gets. Beyond that count a plain
   grid is both safer and easier to read. */
function placeInGrid(items, W, H, maxHubR){
  GRAPH.grid = true;
  var reserve = 0;
  items.forEach(function(h){ reserve = Math.max(reserve, labelReserve(h)); });
  var needW = 2*maxHubR + 40, needH = 2*maxHubR + reserve + 18;
  var cols = Math.max(1, Math.min(items.length, Math.floor((W - 40) / needW)));
  var rows = Math.ceil(items.length / cols);
  while(rows * needH > H - 24 && cols < items.length){
    cols++; rows = Math.ceil(items.length / cols);
  }
  var cw = Math.max(needW, (W - 40) / cols);
  var ch = Math.max(needH, (H - 24) / rows);
  var x0 = (W - cols*cw)/2, y0 = (H - rows*ch)/2;
  items.forEach(function(h, i){
    var r = Math.floor(i/cols), c = i % cols;
    /* centre a short last row so the block does not read as ragged */
    var inRow = Math.min(cols, items.length - r*cols);
    var pad = (cols - inRow) * cw / 2;
    h.x = x0 + pad + cw*(c + 0.5);
    h.y = y0 + ch*(r + 0.5) - reserve*0.28;
    h.R = h.hubR + labelReserve(h) + 10;
  });
}

/* Medallions are placed along the perimeter of a rectangle rather than an
   ellipse. An ellipse crowds its points near the ends of the major axis — at
   twelve clusters they ended up 80px apart there, so labels collided with the
   next medallion's ring. Walking a rectangle's perimeter gives every neighbour
   the SAME spacing, which is the only way to guarantee the labels clear. */
function placeOnRect(items, cx0, cy0, w, h){
  var hw = w/2, hh = h/2, P = 2*w + 2*h, n = items.length;
  items.forEach(function(it, i){
    var t = (i / n) * P, x, y;
    if(t < hw){                     x = t;                    y = -hh; }
    else if(t < hw + h){            x = hw;                   y = -hh + (t - hw); }
    else if(t < hw + h + w){        x = hw - (t - hw - h);     y = hh; }
    else if(t < hw + h + w + h){    x = -hw;                  y = hh - (t - hw - h - w); }
    else {                          x = -hw + (t - hw - h - w - h); y = -hh; }
    it.x = cx0 + x; it.y = cy0 + y;
  });
}

/* Zoned placement. Each zone is a panel; inside it the medallions zigzag
   left-right-right-left down the panel, with an odd one centred at the foot —
   which is what makes the arrangement read as a constellation rather than a
   grid, while still guaranteeing every label its own space. */
/* A medallion's label sits UNDER its disc, drawn at a fixed screen size, so
   the layout has to reserve room for it or the label of one row lands on the
   ring of the next. This is that reserve, in world units: the 24 unit gap to
   the baseline, up to two lines at 17, and a descender. */
var LABEL_GAP = 24, LABEL_LINE = 17, LABEL_TAIL = 8;
function labelLines(h){
  var t = h.label || (h.d && h.d.label) || '';
  return (t.length > 15 && t.includes(' ')) ? 2 : 1;
}
function labelReserve(h){
  /* Labels are drawn at a fixed SCREEN size, so in world units they get bigger
     as the map zooms out. kHint is the zoom the last fit settled on, which is
     what makes the second layout pass converge. */
  return (LABEL_GAP + LABEL_LINE*labelLines(h) + LABEL_TAIL) / (GRAPH.kHint || 1);
}
/* Shrink the discs, never the type: if a row's share of the panel cannot hold
   disc + label + disc, the discs give way. */
function fitDiscs(items, step, stagger){
  var reserve = 0;
  items.forEach(function(h){ reserve = Math.max(reserve, labelReserve(h)); });
  var allowed = (step - 2*stagger - reserve - 14) / 2;
  allowed = Math.max(24, Math.min(52, allowed));
  items.forEach(function(h){
    h.hubR = Math.min(h.hubR, allowed);
    h.R = h.hubR + labelReserve(h) + 10;
  });
  return reserve;
}

function placeInZones(hubs, W, H, maxR){
  var buckets = {};
  ZONES.forEach(function(z){ buckets[z.id] = []; });
  var loose = [];
  hubs.forEach(function(h){
    var z = zoneOfHub(h);
    if(z && buckets[z]) buckets[z].push(h); else loose.push(h);
  });
  if(loose.length) buckets[ZONES[ZONES.length-1].id] =
    buckets[ZONES[ZONES.length-1].id].concat(loose);

  var live = ZONES.filter(function(z){ return buckets[z.id].length; });
  var GAP = 30, PADX = 26, PADY = 34;

  /* On a phone three panels side by side would be 120px wide each, so the
     zones stack instead and the map scrolls: the medallions keep their size,
     which is the whole point of them. */
  if(W < 780){
    var zy = PADY, zw = Math.max(300, W - 2*PADX), outV = [];
    live.forEach(function(z){
      var items = buckets[z.id];
      var cols = items.length > 2 ? 2 : 1;
      var rows = Math.ceil(items.length / cols);
      var step = 2*maxR + 30 + LABEL_GAP + LABEL_LINE*2 + LABEL_TAIL;
      fitDiscs(items, step, 0);
      var top = zy + 40;
      var dx = cols === 1 ? 0 : Math.min(zw/2 - maxR - 10, maxR + 16);
      items.forEach(function(h, i){
        var row = cols === 1 ? i : Math.floor(i/2);
        var side = cols === 1 ? 0 : (i % 2 ? 1 : -1);
        if(cols === 2 && i === items.length-1 && items.length % 2) side = 0;
        h.x = PADX + zw/2 + side*dx;
        h.y = top + step*(row + 0.5);
      });
      var zh = 40 + rows*step + 14;
      outV.push({id:z.id, label:z.label, x:PADX, y:zy, w:zw, h:zh});
      zy += zh + GAP;
    });
    GRAPH.zones = outV;
    GRAPH.fitAxis = 'width';
    return;
  }
  GRAPH.fitAxis = 'both';
  var weights = live.map(function(z){ return buckets[z.id].length > 3 ? 1 : 0.66; });
  var wSum = weights.reduce(function(a,b){ return a+b; }, 0);
  var avail = Math.max(600, W - 2*PADX - GAP*(live.length-1));
  var x = PADX, out = [];

  live.forEach(function(z, zi){
    var items = buckets[z.id];
    var zw = avail * weights[zi] / wSum;
    var cols = items.length > 3 ? 2 : 1;
    var top = PADY + 26, span = Math.max(200, H - top - PADY);
    var rows = cols === 1 ? items.length : Math.ceil(items.length/2);
    var step = span / rows;
    /* the zigzag is charm, not information — it yields first when room is short */
    var stag = Math.min(step*0.10, 26);
    fitDiscs(items, step, stag);
    var zMaxR = 0; items.forEach(function(h){ zMaxR = Math.max(zMaxR, h.hubR); });
    var dx = Math.min(zw/2 - zMaxR - 8, zw*0.27);

    items.forEach(function(h, i){
      var row, side;
      if(cols === 1){ row = i; side = 0; }
      else if(i === items.length-1 && items.length % 2){ row = rows-1; side = 0; }
      else { row = Math.floor(i/2); side = (i % 4 < 2) ? (i%2 ? 1 : -1) : (i%2 ? -1 : 1); }
      h.x = x + zw/2 + side*dx;
      h.y = top + step*(row + 0.5) - side*stag;
    });
    out.push({id:z.id, label:z.label, x:x, y:PADY, w:zw, h:H - 2*PADY});
    x += zw + GAP;
  });
  GRAPH.zones = out;
}

function placeOnEllipse(items, cx0, cy0, rx, ry, pad){
  var total = 0;
  items.forEach(function(it){ total += it.R + pad; });
  var angle = -Math.PI/2;
  items.forEach(function(it){
    var span = 2*Math.PI * (it.R + pad) / total;
    var mid = angle + span/2;
    it.x = cx0 + Math.cos(mid) * rx;
    it.y = cy0 + Math.sin(mid) * ry;
    angle += span;
  });
}

function graphLayout(){
  var W = GRAPH.W, H = GRAPH.H, cx0 = W/2, cy0 = H/2;
  GRAPH.present = {};
  GRAPH.superHubs = [];
  GRAPH.grid = false;

  if(GRAPH.groupBy === 'nested'){
    var forced = forcedExpansion();
    GRAPH.forceKey = forcedKey(forced);
    function gOpen(gid){ return !!(GRAPH.exp.g[gid] || forced.g[gid]); }
    function cOpen(cid){ return !!(GRAPH.exp.c[cid] || forced.c[cid]); }

    var supers = [];
    M.groups.forEach(function(g){
      var subs = [], gCount = 0;
      g.cats.forEach(function(c){
        var cid = c.id;
        var members = essFilter(GRAPH.nodes.filter(function(n){ return n.e.cat === cid; }));
        if(!members.length) return;
        gCount += members.length;
        if(!gOpen(g.id)) return;
        members.sort(function(a,b){ return a.e.y - b.e.y || a.id.localeCompare(b.id); });
        if(cOpen(cid)){
          var rings = ringPlan(members.length);
          subs.push({uid:'c-'+cid, tkind:'c', tid:cid, open:true,
                     d:{label:CAT_LABEL[cid]}, members:members, rings:rings,
                     R:(rings.length?rings[rings.length-1].r:17)+10, hubR:15});
        } else {
          var hr = 14 + Math.min(6, members.length/6);
          subs.push({uid:'c-'+cid, tkind:'c', tid:cid, open:false, collapsed:true,
                     d:{label:CAT_LABEL[cid]}, members:members, rings:[],
                     R:hr + 7, hubR:hr});
        }
      });
      if(!gCount) return;
      if(!gOpen(g.id)){
        var ghr = 28 + Math.min(12, gCount/11);
        supers.push({uid:'g-'+g.id, tkind:'g', tid:g.id, open:false, collapsed:true,
                     label:g.label, subs:[], R:ghr + 8, hubR:ghr, count:gCount});
        return;
      }
      var maxSub = 0, sum = 0;
      subs.forEach(function(s){ maxSub = Math.max(maxSub, s.R); sum += 2*s.R + 22; });
      var orbit = Math.max(maxSub + 52, sum/(2*Math.PI));
      supers.push({uid:'g-'+g.id, tkind:'g', tid:g.id, open:true,
                   label:g.label, subs:subs, R:orbit + maxSub + 8, hubR:27,
                   orbit:orbit, count:gCount});
    });
    var rx = Math.max(300, W/2 - 130), ry = Math.max(230, H/2 - 90);
    placeOnEllipse(supers, cx0, cy0, rx, ry, 60);
    supers.forEach(function(g){
      if(!g.open) return;
      placeOnEllipse(g.subs, g.x, g.y, g.orbit, g.orbit, 22);
      g.subs.forEach(function(s){ if(s.open) placeRings(s, s.members, g.x, g.y); });
    });
    GRAPH.superHubs = supers;
    GRAPH.hubs = [];
    supers.forEach(function(g){ GRAPH.hubs = GRAPH.hubs.concat(g.subs); });
    GRAPH.hubs.forEach(function(h){
      if(h.open) h.members.forEach(function(n){ GRAPH.present[n.id] = 1; });
    });
  } else {
    /* Two levels of detail. CLUSTERS is the default and the answer to "why is
       everything so small": twelve medallions instead of three hundred dots,
       each carrying its count and an era-composition ring, with a label at a
       fixed screen size that zooming can never shrink. EVERY ENTRY is the
       original constellation, kept for people who want the full texture. */
    var medal = GRAPH.level === 'clusters';
    var clusters = clustersFor(GRAPH.groupBy);
    GRAPH.hubs = clusters.map(function(c){
      var members = c.members.slice().sort(function(a,b){
        return a.e.y - b.e.y || a.id.localeCompare(b.id);
      });
      if(medal){
        var hr = 34 + Math.min(16, Math.sqrt(members.length) * 2.6);
        return {uid:'h-'+c.id, open:false, medallion:true, d:{label:c.label},
                members:members, rings:[], R:hr + 54, hubR:hr};
      }
      var rings = ringPlan(members.length);
      return {uid:'h-'+c.id, open:true, d:{label:c.label}, members:members, rings:rings,
              R:(rings.length?rings[rings.length-1].r:17)+12, hubR:23};
    });
    var maxHubR = 0, maxR = 0;
    GRAPH.hubs.forEach(function(h){
      maxHubR = Math.max(maxHubR, h.hubR); maxR = Math.max(maxR, h.R);
    });
    GRAPH.zones = [];
    if(medal && GRAPH.groupBy === 'domain'){
      placeInZones(GRAPH.hubs, W, H, maxHubR);
    } else if(medal && GRAPH.hubs.length > 9){
      placeInGrid(GRAPH.hubs, W, H, maxHubR);
    } else if(medal){
      /* size the wreath so every neighbour clears the one before it — disc,
         label and disc — all the way round, then grow it to fill the viewport */
      var reserveW = 0;
      GRAPH.hubs.forEach(function(h){ reserveW = Math.max(reserveW, labelReserve(h)); });
      var pitch = 2*maxHubR + reserveW + 26;
      var need = pitch * GRAPH.hubs.length;                     /* perimeter needed */
      var mw = Math.max(360, W - 2*maxHubR - 190);   /* room for centred labels */
      var mh = Math.max(240, H - 2*maxHubR - reserveW - 40);
      var grow = need / (2*mw + 2*mh);
      if(grow > 1){ mw *= grow; mh *= grow; }
      placeOnRect(GRAPH.hubs, cx0, cy0, mw, mh);
      GRAPH.hubs.forEach(function(h){ h.R = h.hubR + labelReserve(h) + 10; });
    } else {
      var rx2 = Math.max(300, W/2 - maxR - 40);
      var ry2 = Math.max(240, H/2 - maxR - 34);
      placeOnEllipse(GRAPH.hubs, cx0, cy0, rx2, ry2, 34);
    }
    if(!medal){
      GRAPH.hubs.forEach(function(h){ placeRings(h, h.members, cx0, cy0); });
      GRAPH.hubs.forEach(function(h){
        h.members.forEach(function(n){ GRAPH.present[n.id] = 1; });
      });
    }
  }

  GRAPH.center = {x:cx0, y:cy0, r:GRAPH.groupBy === 'nested' ? 32 : 34};
  GRAPH.laidOut = true;
}

function graphFit(animate){
  if(!GRAPH.laidOut) return;
  var x0=1e9,y0=1e9,x1=-1e9,y1=-1e9;
  var boxes = GRAPH.superHubs.length ? GRAPH.superHubs : GRAPH.hubs;
  if(!boxes.length) boxes = [{x:GRAPH.center.x, y:GRAPH.center.y, R:120}];
  boxes.forEach(function(h){
    x0 = Math.min(x0, h.x - h.R); y0 = Math.min(y0, h.y - h.R);
    x1 = Math.max(x1, h.x + h.R); y1 = Math.max(y1, h.y + h.R);
  });
  GRAPH.zones.forEach(function(z){
    x0 = Math.min(x0, z.x); y0 = Math.min(y0, z.y);
    x1 = Math.max(x1, z.x + z.w); y1 = Math.max(y1, z.y + z.h);
  });
  var bw = x1-x0, bh = y1-y0;
  var bw2 = bw, k, tx, ty;
  if(GRAPH.fitAxis === 'width'){
    /* fit the width and let the map scroll vertically: shrinking a stack of
       twelve medallions to fit a phone screen would collide every label */
    k = Math.min(Math.max(GRAPH.W/(bw2+24), 0.5), 1.2);
    tx = GRAPH.W/2 - (x0+bw2/2)*k;
    ty = 12 - y0*k;
  } else {
    k = Math.min(GRAPH.W/(bw2+40), GRAPH.H/(bh+40));
    k = Math.min(Math.max(k, 0.22), 1.6);
    tx = GRAPH.W/2 - (x0+bw2/2)*k;
    ty = GRAPH.H/2 - (y0+bh/2)*k;
  }
  if(animate) animateView(tx, ty, k); else { GRAPH.tx=tx; GRAPH.ty=ty; GRAPH.k=k; GRAPH.draw(); }
}

function animateView(tx, ty, k){
  var reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  if(reduce){ GRAPH.tx=tx; GRAPH.ty=ty; GRAPH.k=k; GRAPH.draw(); return; }
  var s = {tx:GRAPH.tx, ty:GRAPH.ty, k:GRAPH.k}, t0 = null, DUR = 380;
  if(GRAPH.anim) cancelAnimationFrame(GRAPH.anim);
  function step(ts){
    if(!t0) t0 = ts;
    var p = Math.min(1, (ts - t0)/DUR);
    var e = 1 - Math.pow(1-p, 3);
    GRAPH.tx = s.tx + (tx - s.tx)*e;
    GRAPH.ty = s.ty + (ty - s.ty)*e;
    GRAPH.k  = s.k  + (k  - s.k )*e;
    graphDraw();
    if(p < 1) GRAPH.anim = requestAnimationFrame(step);
    else GRAPH.anim = null;
  }
  GRAPH.anim = requestAnimationFrame(step);
}
/* How much of the canvas the detail sheet is covering on the right. Centring on
   the full canvas width put the selected node underneath it. */
function sheetInset(){
  if(!S.sel) return 0;
  var d = $('#drawer');
  if(!d || d.getAttribute('data-open') !== '1') return 0;
  var w = d.getBoundingClientRect().width || 0;
  return Math.min(w, GRAPH.W * 0.55);
}
function focusPoint(x, y, k){
  var ins = sheetInset();
  animateView((GRAPH.W - ins)/2 - x*k, GRAPH.H/2 - y*k, k);
}

function graphResize(){
  var cv = GRAPH.cv; if(!cv) return;
  var r = cv.parentNode.getBoundingClientRect();
  GRAPH.dpr = Math.min(window.devicePixelRatio || 1, 2);
  GRAPH.W = r.width; GRAPH.H = r.height;
  cv.width = Math.round(r.width * GRAPH.dpr);
  cv.height = Math.round(r.height * GRAPH.dpr);
  relayout(false);
}

function lineageSync(){ if(LF.open) lineageClose(); }
/* Lay out, fit, and if the fit had to zoom out far enough that screen-sized
   labels no longer clear in world units, lay out once more with that zoom as
   the hint. Two passes are enough to converge in every grouping. */
function relayout(animate){
  GRAPH.kHint = 1;
  graphLayout();
  graphFit(animate);
  if(GRAPH.k < 0.92){
    GRAPH.kHint = Math.max(0.34, GRAPH.k);
    graphLayout();
    graphFit(animate);
  }
}
function graphRemap(){
  lineageSync();
  if(!GRAPH.W) return;
  relayout(true);
}

/* toggle a matryoshka shell, then keep the camera on it */
function graphToggle(hub){
  var opening;
  if(hub.tkind === 'g'){
    opening = !GRAPH.exp.g[hub.tid];
    if(opening) GRAPH.exp.g[hub.tid] = 1;
    else {
      delete GRAPH.exp.g[hub.tid];
      M.groups.forEach(function(g){
        if(g.id === hub.tid) g.cats.forEach(function(c){ delete GRAPH.exp.c[c.id]; });
      });
    }
  } else {
    opening = !GRAPH.exp.c[hub.tid];
    if(opening) GRAPH.exp.c[hub.tid] = 1;
    else delete GRAPH.exp.c[hub.tid];
  }
  graphLayout();
  /* the shell expands or closes in place; re-fit so the whole map stays
     visible and the change reads as the matryoshka opening, not a teleport */
  graphFit(true);
}

function cssv(name){
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || '#888';
}

/* A cluster medallion: an era-composition ring, the count set large, and the
   label underneath. Every piece of text here is sized in SCREEN pixels
   (dividing by GRAPH.k inside the scaled transform), so panning and zooming
   move the map without ever shrinking a word below reading size. */
function drawMedallion(cx, h, dim, hover, eraCols, matchText){
  var k = GRAPH.k, R = h.hubR;
  var surfC = cssv('--surf'), inkC = cssv('--ink'), ink3 = cssv('--ink-3');
  var lineC = cssv('--line'), accC = cssv('--acc');
  cx.globalAlpha = dim ? .34 : 1;

  /* the era ring: how this cluster is spread across the four eras */
  var counts = [0,0,0,0], total = h.members.length;
  h.members.forEach(function(n){
    var i = M.eras.findIndex(function(x){ return x.id === n.e.era; });
    if(i >= 0) counts[i]++;
  });
  var ang = -Math.PI/2, gap = 0.055;
  cx.lineWidth = 4/k; cx.lineCap = 'butt';
  for(var i=0;i<4;i++){
    if(!counts[i]) continue;
    var sweep = 2*Math.PI * counts[i]/total;
    cx.strokeStyle = eraCols[i];
    cx.beginPath();
    cx.arc(h.x, h.y, R + 8/k, ang + gap/2, ang + sweep - gap/2);
    cx.stroke();
    ang += sweep;
  }

  cx.beginPath(); cx.arc(h.x, h.y, R, 0, 6.2832);
  cx.fillStyle = surfC; cx.fill();
  cx.lineWidth = (hover ? 2.2 : 1.2)/k;
  cx.strokeStyle = hover ? accC : lineC;
  cx.stroke();

  var live = h.members.filter(function(n){
    return n.e.st === 'shipping' || n.e.st === 'active';
  }).length;

  cx.textAlign = 'center';
  var numPx = Math.max(19, Math.min(30, R * 0.60));
  var subPx = Math.max(8.5, Math.min(10, R * 0.20));
  cx.fillStyle = inkC;
  cx.font = (numPx/k) + "px 'Instrument Serif', Georgia, serif";
  cx.fillText(matchText || String(total), h.x, h.y + (numPx*0.17)/k);
  cx.fillStyle = ink3;
  cx.font = '500 ' + (subPx/k) + "px 'JetBrains Mono', monospace";
  var sub = live ? live + ' LIVE' : 'ALL HISTORIC';
  if(cx.measureText(sub).width > (R*1.45)) sub = live ? live + ' LIVE' : '0 LIVE';
  cx.fillText(sub, h.x, h.y + (numPx*0.17 + subPx + 4)/k);

  /* label under the medallion, on the same reserve the layout allowed for */
  cx.fillStyle = hover ? accC : inkC;
  cx.font = (16/k) + "px 'Instrument Serif', Georgia, serif";
  var label = h.label || h.d.label, lines = [label];
  if(labelLines(h) > 1){
    var w2 = label.split(' '), half = Math.ceil(w2.length/2);
    lines = [w2.slice(0,half).join(' '), w2.slice(half).join(' ')];
  }
  var ly = h.y + R + LABEL_GAP/k;
  /* labels are centred, so one on the edge of the wreath would run off the
     canvas — nudge it back in rather than clipping a word */
  var vw2 = GRAPH.W/k, vx2 = -GRAPH.tx/k, pad2 = 8/k;
  lines.forEach(function(t, li){
    var half = cx.measureText(t).width/2;
    var lx2 = Math.max(vx2 + half + pad2, Math.min(h.x, vx2 + vw2 - half - pad2));
    cx.fillText(t, lx2, ly + li * LABEL_LINE/k);
  });
  cx.textAlign = 'left';
  cx.globalAlpha = 1;
}

function drawHubDisc(cx, h, dim, surfC, inkC, ink3, lineC, hover, countText){
  cx.globalAlpha = dim ? .42 : 1;
  /* dashed halo on collapsed shells: same language as "there is more here" */
  if(h.collapsed){
    cx.setLineDash([3/GRAPH.k, 3/GRAPH.k]);
    cx.strokeStyle = hover ? inkC : ink3;
    cx.lineWidth = 1.1/GRAPH.k;
    cx.beginPath(); cx.arc(h.x, h.y, h.hubR + 4.5, 0, 6.2832); cx.stroke();
    cx.setLineDash([]);
  }
  cx.beginPath(); cx.arc(h.x, h.y, h.hubR, 0, 6.2832);
  cx.fillStyle = surfC; cx.fill();
  cx.lineWidth = 1.2/GRAPH.k;
  cx.strokeStyle = hover ? inkC : lineC;
  cx.stroke();
  cx.fillStyle = inkC; cx.textAlign = 'center';
  var label = h.label || h.d.label;
  var words = label.split(' ');
  var fs2 = h.hubR >= 26 ? 8.5 : (h.hubR <= 16 ? 6.6 : 7.8);
  cx.font = '700 ' + fs2 + 'px ' + "'Archivo', sans-serif";
  if(words.length > 2){
    var half = Math.ceil(words.length/2);
    cx.fillText(words.slice(0,half).join(' '), h.x, h.y - 3.5);
    cx.fillText(words.slice(half).join(' '), h.x, h.y + 4);
  } else if(words.length === 2){
    cx.fillText(words[0], h.x, h.y - 3.5);
    cx.fillText(words[1], h.x, h.y + 4);
  } else {
    cx.fillText(label, h.x, h.y + 1);
  }
  cx.fillStyle = ink3;
  cx.font = '600 ' + (h.hubR <= 16 ? 6 : 6.8) + 'px ' + "'JetBrains Mono', monospace";
  cx.fillText(countText, h.x, h.y + (h.hubR <= 16 ? 10 : 12));
  cx.textAlign = 'left';
  cx.globalAlpha = 1;
}

function matchN2(hubs, match){
  var n = 0;
  hubs.forEach(function(h){ h.members.forEach(function(m){ if(match[m.id]) n++; }); });
  return n;
}
/* The card that appears over a medallion: what the cluster is, how it sits in
   time, and the five landmarks inside it — enough to decide whether to open it
   without opening it. It never takes the pointer, so there is no hover trap. */
function peekHTML(h){
  var ms = h.members.map(function(n){ return n.e; });
  var live = ms.filter(function(e){ return e.st==='shipping'||e.st==='active'; }).length;
  var ys = ms.map(function(e){ return e.y; });
  var span = ys.length ? Math.min.apply(null, ys) + '—' + Math.max.apply(null, ys) : '';
  var top = ms.slice().sort(function(a,b){ return (b.wt||0)-(a.wt||0) || a.y-b.y; }).slice(0,5)
    .sort(function(a,b){ return a.y - b.y; });
  return '<div class="peek-rule"></div>'+
    '<div class="peek-b">'+
      '<div class="peek-t">'+esc(h.label || h.d.label)+'</div>'+
      '<div class="peek-m mono">'+ms.length+' entries · '+
        (live ? live + ' still live' : 'all historic')+
        (span ? ' · '+span : '')+'</div>'+
      '<div class="peek-l">'+top.map(function(e){
        return '<div class="peek-r"><i style="background:'+eraColor(e.era)+'"></i>'+
          '<span>'+esc(e.n)+'</span><b class="mono">'+e.y+'</b></div>';
      }).join('')+'</div>'+
      '<div class="peek-f"><span class="peek-go">Click to open</span>'+
        (ms.length > 5 ? '<span class="peek-more">'+(ms.length-5)+' more inside</span>' : '')+
      '</div>'+
    '</div>';
}

function graphDraw(){
  var cx = GRAPH.cx; if(!cx || !GRAPH.laidOut) return;

  /* in nested mode a change in search/filter state re-opens shells */
  if(GRAPH.groupBy === 'nested'){
    var fk = forcedKey(forcedExpansion());
    if(fk !== GRAPH.forceKey) graphLayout();
  }

  var W = GRAPH.W, H = GRAPH.H, dpr = GRAPH.dpr;
  cx.setTransform(dpr,0,0,dpr,0,0);
  cx.clearRect(0,0,W,H);

  var eraCols = M.eras.map(function(_,i){ return cssv('--e'+(i+1)); });
  var inkC = cssv('--ink'), ink3 = cssv('--ink-3'), ink4 = cssv('--ink-4');
  var accC = cssv('--acc'), surfC = cssv('--bg-deep'), lineC = cssv('--line');

  var anyFilter = dimmingCount() > 0;
  var match = {};
  results().forEach(function(e){ match[e.id] = 1; });
  var matchN = 0;
  Object.keys(GRAPH.present).forEach(function(id){ if(match[id]) matchN++; });

  var selNode = (S.sel && GRAPH.present[S.sel]) ? GRAPH.byId[S.sel] : null;
  var linked = {};
  if(selNode){
    linked[selNode.id] = 1;
    (selNode.e.rel||[]).forEach(function(i){ linked[i]=1; });
    (selNode.e.children||[]).forEach(function(i){ linked[i]=1; });
  }
  function emph(n){
    if(selNode) return linked[n.id] ? 2 : 0;
    if(anyFilter) return match[n.id] ? 2 : 0;
    return 1;
  }

  cx.save();
  cx.translate(GRAPH.tx, GRAPH.ty); cx.scale(GRAPH.k, GRAPH.k);

  /* zone panels */
  if(GRAPH.zones.length){
    cx.lineWidth = 1/GRAPH.k;
    GRAPH.zones.forEach(function(z){
      cx.fillStyle = cssv('--surf'); cx.globalAlpha = .5;
      cx.fillRect(z.x, z.y, z.w, z.h);
      cx.globalAlpha = 1;
      cx.strokeStyle = cssv('--line-soft');
      cx.strokeRect(z.x + .5/GRAPH.k, z.y + .5/GRAPH.k, z.w, z.h);
      cx.fillStyle = ink3;
      cx.font = '500 ' + (12/GRAPH.k) + "px 'JetBrains Mono', monospace";
      cx.fillText(z.label.toUpperCase(), z.x + 14/GRAPH.k, z.y + 22/GRAPH.k);
    });
  }

  /* cluster threads: one line per pair of clusters that actually descend from
     each other, weighted by how many links there are */
  if(GRAPH.hubs.length && GRAPH.hubs[0].medallion){
    var of = {};
    GRAPH.hubs.forEach(function(h){ h.members.forEach(function(n){ of[n.id] = h; }); });
    var pair = {};
    ALL.forEach(function(e){
      var a = of[e.id]; if(!a) return;
      (e.rel||[]).forEach(function(pid){
        var b2 = of[pid]; if(!b2 || b2 === a) return;
        var key = a.uid < b2.uid ? a.uid+'|'+b2.uid : b2.uid+'|'+a.uid;
        (pair[key] = pair[key] || {a:a, b:b2, n:0}).n++;
      });
    });
    Object.keys(pair).forEach(function(key){
      var t = pair[key];
      var hot = GRAPH.hoverHub === t.a || GRAPH.hoverHub === t.b;
      cx.strokeStyle = hot ? accC : lineC;
      cx.globalAlpha = hot ? .8 : Math.min(.62, .24 + t.n*0.04);
      cx.lineWidth = (hot ? 1.6 : Math.min(2.4, 0.8 + t.n*0.1))/GRAPH.k;
      var mx = (t.a.x + t.b.x)/2, my = (t.a.y + t.b.y)/2;
      var ddx = t.b.x - t.a.x, ddy = t.b.y - t.a.y;
      cx.beginPath(); cx.moveTo(t.a.x, t.a.y);
      cx.quadraticCurveTo(mx - ddy*0.075, my + ddx*0.075, t.b.x, t.b.y);
      cx.stroke();
    });
    cx.globalAlpha = 1;
  }

  /* spokes */
  cx.strokeStyle = lineC; cx.lineWidth = 1.2/GRAPH.k; cx.globalAlpha = .5;
  if(GRAPH.zones.length || GRAPH.grid){
    /* zoned or gridded view: the panels and the real descent threads carry the
       structure, so a hub-and-spoke star on top of them is just noise */
  } else if(GRAPH.superHubs.length){
    GRAPH.superHubs.forEach(function(g){
      cx.beginPath(); cx.moveTo(GRAPH.center.x, GRAPH.center.y); cx.lineTo(g.x, g.y); cx.stroke();
      if(g.open){
        cx.globalAlpha = .38;
        g.subs.forEach(function(s){
          cx.beginPath(); cx.moveTo(g.x, g.y); cx.lineTo(s.x, s.y); cx.stroke();
        });
        cx.globalAlpha = .5;
      }
    });
  } else {
    GRAPH.hubs.forEach(function(h){
      cx.beginPath(); cx.moveTo(GRAPH.center.x, GRAPH.center.y); cx.lineTo(h.x, h.y); cx.stroke();
    });
  }
  cx.globalAlpha = 1;

  /* lineage links among present nodes */
  var essView = S.ess;
  GRAPH.links.forEach(function(l){
    if(!GRAPH.present[l.a.id] || !GRAPH.present[l.b.id]) return;
    var sameHub = l.a.e.cat === l.b.e.cat;
    var isSel = selNode && linked[l.a.id] && linked[l.b.id] &&
                (l.a.id === selNode.id || l.b.id === selNode.id);
    var isHov = !selNode && GRAPH.hover &&
                (l.a === GRAPH.hover || l.b === GRAPH.hover);
    if(isSel || isHov){
      cx.strokeStyle = accC; cx.globalAlpha = .92; cx.lineWidth = 1.7/GRAPH.k + .3;
    } else if(selNode || anyFilter){
      cx.strokeStyle = ink4; cx.globalAlpha = .07; cx.lineWidth = 1/GRAPH.k;
    } else {
      cx.strokeStyle = ink4;
      cx.globalAlpha = essView ? .42 : (sameHub ? .26 : .1);
      cx.lineWidth = 1/GRAPH.k;
    }
    cx.beginPath();
    var mx = (l.a.x + l.b.x)/2, my = (l.a.y + l.b.y)/2;
    var dx = l.b.x - l.a.x, dy = l.b.y - l.a.y;
    var bend = sameHub ? 0 : 0.14;
    cx.moveTo(l.a.x, l.a.y);
    cx.quadraticCurveTo(mx - dy*bend, my + dx*bend, l.b.x, l.b.y);
    cx.stroke();
  });
  cx.globalAlpha = 1;

  /* centre — only where there is a star to centre */
  if(!GRAPH.zones.length && !GRAPH.grid){
    cx.beginPath(); cx.arc(GRAPH.center.x, GRAPH.center.y, GRAPH.center.r, 0, 6.2832);
    cx.fillStyle = inkC; cx.fill();
    cx.fillStyle = surfC; cx.textAlign = 'center';
    cx.font = '18px ' + "'Instrument Serif', Georgia, serif";
    cx.fillText('Amiga', GRAPH.center.x, GRAPH.center.y + 3);
    cx.font = '500 7.5px ' + "'JetBrains Mono', monospace";
    cx.fillText('1984–2026', GRAPH.center.x, GRAPH.center.y + 14);
    cx.textAlign = 'left';
  }

  /* dots */
  GRAPH.hubs.forEach(function(h){
    if(!h.open) return;
    h.members.forEach(function(n){
      var em = emph(n);
      var eraI = M.eras.findIndex(function(x){ return x.id === n.e.era; });
      var col = eraCols[eraI < 0 ? 0 : eraI];
      var isSel = S.sel === n.id;
      var r = n.r * (isSel ? 1.7 : 1) * (essView ? 1.25 : 1);
      cx.globalAlpha = em === 0 ? .2 : 1;
      cx.beginPath(); cx.arc(n.x, n.y, r, 0, 6.2832);
      cx.fillStyle = col; cx.fill();
      if(isSel || GRAPH.hover === n || (selNode && linked[n.id])){
        cx.lineWidth = 1.6/GRAPH.k;
        cx.strokeStyle = isSel ? accC : inkC;
        cx.stroke();
      }
      cx.globalAlpha = 1;
    });
  });

  /* hubs */
  GRAPH.superHubs.forEach(function(g){
    drawHubDisc(cx, g, false, surfC, inkC, ink3, lineC, GRAPH.hoverHub === g,
                g.count + (g.collapsed ? ' · open' : ' entries'));
  });
  GRAPH.hubs.forEach(function(h){
    var mMatch = 0;
    if(anyFilter) h.members.forEach(function(n){ if(match[n.id]) mMatch++; });
    var dimHub = (selNode || anyFilter) &&
                 !(anyFilter && mMatch > 0) &&
                 !(selNode && h.members.some(function(n){ return linked[n.id]; }));
    if(h.medallion){
      drawMedallion(cx, h, dimHub, GRAPH.hoverHub === h, eraCols,
                    anyFilter ? (mMatch + '/' + h.members.length) : null);
      return;
    }
    drawHubDisc(cx, h, dimHub, surfC, inkC, ink3, lineC, GRAPH.hoverHub === h,
                anyFilter ? (mMatch + '/' + h.members.length)
                          : (h.members.length + (h.hubR <= 16 ? (h.collapsed ? ' ·open' : '') : ' entries')));
  });

  /* labels */
  var presentN = Object.keys(GRAPH.present).length;
  var few = essView || presentN <= 150;
  var fs = Math.max(9, Math.min(12.5, 11/Math.sqrt(GRAPH.k)));
  cx.font = '700 ' + fs + "px 'Archivo', 'Helvetica Neue', Helvetica, sans-serif";
  var placed = [];
  GRAPH.hubs.concat(GRAPH.superHubs).forEach(function(h){
    placed.push({x:h.x - h.hubR, y:h.y - h.hubR, w:h.hubR*2, h:h.hubR*2});
  });
  var vw = GRAPH.W / GRAPH.k, vx0 = -GRAPH.tx / GRAPH.k;

  function labelable(n){
    if(S.sel === n.id || GRAPH.hover === n) return true;
    if(selNode) return !!linked[n.id];
    if(anyFilter) return match[n.id] && ((n.e.wt||0) >= 4 || matchN <= 60 || GRAPH.k > 1.6);
    if(few) return true;
    if(GRAPH.k > 2.4) return true;
    if(GRAPH.k > 1.45) return (n.e.wt||0) >= 4;
    return (n.e.wt||0) >= 5;
  }
  var cands = [];
  GRAPH.hubs.forEach(function(h){
    if(!h.open) return;
    h.members.forEach(function(n){ if(labelable(n)) cands.push(n); });
  });
  cands.sort(function(a,b){
    var pa = (S.sel===a.id?100:0) + (GRAPH.hover===a?90:0) + (a.e.wt||0);
    var pb = (S.sel===b.id?100:0) + (GRAPH.hover===b?90:0) + (b.e.wt||0);
    return pb - pa;
  });
  cands.forEach(function(n){
    var isSel = S.sel === n.id, isHov = GRAPH.hover === n;
    var r = n.r * (isSel ? 1.7 : 1);
    var w = cx.measureText(n.e.n).width;
    var gap = r + 4.5/GRAPH.k;
    var flip = (n.x + gap + w) > vx0 + vw - 6/GRAPH.k;
    var lx = flip ? n.x - gap - w : n.x + gap;
    var ly = n.y + fs*0.35;
    var box = {x:lx-1, y:ly-fs, w:w+2, h:fs+3};
    if(!(isSel || isHov)){
      for(var i=0;i<placed.length;i++){
        var p = placed[i];
        if(box.x < p.x+p.w && box.x+box.w > p.x && box.y < p.y+p.h && box.y+box.h > p.y) return;
      }
    }
    placed.push(box);
    cx.globalAlpha = 1;
    cx.strokeStyle = surfC; cx.lineWidth = 3.4/GRAPH.k; cx.lineJoin = 'round';
    cx.strokeText(n.e.n, lx, ly);
    cx.fillStyle = isSel ? accC : inkC;
    cx.fillText(n.e.n, lx, ly);
    cx.globalAlpha = 1;
  });

  cx.restore();

  var gc = $('#gcount');
  if(gc){
    if(GRAPH.groupBy === 'nested' && presentN === 0 && !anyFilter){
      gc.textContent = ALL.length + ' entries in 5 shells — click a shell to drill in';
    } else {
      var clusterTotal = GRAPH.hubs.reduce(function(a,h){ return a + h.members.length; }, 0);
      if(GRAPH.level === 'clusters' && GRAPH.groupBy !== 'nested'){
        gc.textContent = anyFilter
          ? matchN2(GRAPH.hubs, match) + ' of ' + clusterTotal + ' match — click a cluster to read it'
          : clusterTotal + ' entries in ' + GRAPH.hubs.length +
            ' clusters — click one to open it';
      } else {
        gc.textContent = anyFilter
          ? matchN + ' of ' + presentN + ' match — rest dimmed'
          : (essView ? presentN + ' essentials of ' + ALL.length
                     : presentN + ' entries in ' + GRAPH.hubs.length + ' clusters');
      }
    }
  }
}
if(typeof window !== 'undefined'){ window.__GRAPH = GRAPH; window.__lo = function(h){ lineageOpen(h); }; window.__od = function(i){ openDetail(i); }; }  /* handles for tests */
GRAPH.draw = function(){
  if(GRAPH.raf) return;
  GRAPH.raf = requestAnimationFrame(function(){ GRAPH.raf = 0; graphDraw(); });
};

/* ---- tours ---- */
function tourStart(id){
  /* a route walks single entries, so it needs the full constellation */
  if(GRAPH.level !== 'all' && GRAPH.groupBy !== 'nested'){
    GRAPH.level = 'all';
    var lc = $('#glvl-c'), la = $('#glvl-a');
    if(lc){ lc.setAttribute('aria-pressed','false'); la.setAttribute('aria-pressed','true'); }
    lineageSync();
    graphLayout();
  }
  var t = TOURS.find(function(x){ return x.id === id; });
  if(!t) return;
  /* a route walks single entries, so it needs the full constellation back --
     resetAll() clears the Essentials flag along with the facets, and the map
     has to be re-laid-out from the restored node set. */
  var wasEss = S.ess;
  if(activeCount()){ resetAll(); }
  if(wasEss) graphLayout();
  GRAPH.tour = t; GRAPH.tourStep = 0;
  Array.prototype.forEach.call(document.querySelectorAll('.gchip[data-tour]'), function(b){
    b.setAttribute('aria-pressed', b.getAttribute('data-tour') === id ? 'true' : 'false');
  });
  var rb = $('#groutesbtn');
  if(rb){ rb.textContent = t.label; rb.setAttribute('aria-pressed','true'); }
  tourGo(0);
}
function tourGo(i){
  var t = GRAPH.tour; if(!t) return;
  GRAPH.tourStep = Math.max(0, Math.min(t.steps.length-1, i));
  var st = t.steps[GRAPH.tourStep];
  var n = GRAPH.byId[st.id];
  S.sel = st.id;
  markSelection();
  /* the matryoshka opens itself to show the current stop */
  if(GRAPH.groupBy === 'nested' && n){
    var cid = n.e.cat, gid = GROUP_OF_CAT[cid];
    if(!GRAPH.exp.c[cid] || !GRAPH.exp.g[gid]){
      GRAPH.exp.g[gid] = 1; GRAPH.exp.c[cid] = 1;
      graphLayout();
    }
  }
  var bar = $('#tourbar');
  bar.hidden = false;
  $('#tourtitle').textContent = t.label;
  $('#tourstep').textContent = (GRAPH.tourStep+1) + ' / ' + t.steps.length;
  $('#tourname').textContent = n ? n.e.n : st.id;
  $('#tournote').textContent = st.note;
  $('#tourprev').disabled = GRAPH.tourStep === 0;
  $('#tournext').textContent = (GRAPH.tourStep === t.steps.length-1) ? 'Finish' : 'Next →';
  if(n) focusPoint(n.x, n.y, Math.max(GRAPH.k, 1.9));
  GRAPH.draw();
}
function tourNext(){
  if(!GRAPH.tour) return;
  if(GRAPH.tourStep >= GRAPH.tour.steps.length - 1) tourEnd();
  else tourGo(GRAPH.tourStep + 1);
}
function tourEnd(){
  GRAPH.tour = null;
  $('#tourbar').hidden = true;
  Array.prototype.forEach.call(document.querySelectorAll('.gchip[data-tour]'), function(b){
    b.setAttribute('aria-pressed','false');
  });
  var rb = $('#groutesbtn');
  if(rb){ rb.textContent = 'Routes'; rb.setAttribute('aria-pressed','false'); }
  S.sel = null; markSelection();
  graphFit(true);
}

/* ================================================== the readable lineage
   Opening a cluster on the constellation does NOT zoom into it. Zooming can
   only ever make 330 labels smaller, which was the whole problem. Instead the
   cluster changes representation: it becomes four era columns running left to
   right, every member gets a full-width card at a readable size, and the
   descent threads run through the gutters so lineage reads as direction
   rather than as a hairball. */
var LF = { open:null, sel:null, redraw:null, cards:null };

function lineageOpen(hub){
  LF.open = hub;
  LF.sel = S.sel;
  lineageRender();
  var back = document.querySelector('#lfocus .lfback');
  if(back) back.focus();
}
/* The sheet overlays the right of the stage. When it is open the board reflows
   into the strip that is actually visible, so a selected card can never end up
   underneath it. */
function lfSyncDrawer(){
  var pane = $('#pane-graph');
  if(pane) pane.setAttribute('data-sheet', S.sel ? '1' : '0');
  var cat = $('#pane-catalog');
  if(cat) cat.setAttribute('data-sheet', S.sel ? '1' : '0');
  if(LF.open) requestAnimationFrame(function(){ lineageReflow(); });
  else if(S.sel) requestAnimationFrame(function(){ catScrollTo(S.sel); });
}

/* The catalogue reflows the same way, so bring the chosen plate to the middle
   of the strip the sheet leaves free. */
function catScrollTo(id){
  var sc = $('#cat-scroll');
  if(!sc || S.view !== 'catalog') return;
  var c = sc.querySelector('.card[data-id="' + id + '"]');
  if(!c) return;
  var sr = sc.getBoundingClientRect(), cr = c.getBoundingClientRect();
  var target = sc.scrollTop + (cr.top - sr.top) - (sr.height - cr.height)/2;
  sc.scrollTop = Math.max(0, Math.min(target, sc.scrollHeight - sc.clientHeight));
}
function lineageClose(){
  var wasOpen = !!LF.open;
  var pane = $('#pane-graph');
  if(pane) pane.setAttribute('data-lf','0');
  if(pane) pane.scrollTop = 0;
  LF.open = null;
  var host = $('#lfocus');
  if(host){ host.hidden = true; host.innerHTML = ''; }
  if(wasOpen){ var cv = $('#gcanvas'); if(cv) cv.focus(); }
}
function lineageRender(){
  var hub = LF.open, host = $('#lfocus');
  if(!hub){ lineageClose(); return; }
  var label = hub.label || (hub.d && hub.d.label) || 'Cluster';
  var members = (hub.members || []).map(function(n){ return n.e || n; });
  host.hidden = false;
  host.innerHTML = '';
  var pane = $('#pane-graph');
  if(pane){ pane.setAttribute('data-lf','1'); pane.setAttribute('data-sheet', S.sel ? '1' : '0'); }

  /* ---- breadcrumb + cluster brief ---- */
  var live = members.filter(function(e){ return e.st==='shipping'||e.st==='active'; }).length;
  var years = members.map(function(e){ return e.y; });
  var span  = years.length ? Math.min.apply(null, years) + '—' + Math.max.apply(null, years) : '';
  var archN = {};
  members.forEach(function(e){ (e.arch||[]).forEach(function(a){ archN[a] = (archN[a]||0)+1; }); });
  var archTop = Object.keys(archN).sort(function(a,b){ return archN[b]-archN[a]; }).slice(0,3)
    .map(function(a){ return (ARCH_LABEL[a]||a) + ' ' + archN[a]; }).join(' · ');

  var bar = el('div','lfbar');
  var back = el('button','lfback'); back.type = 'button';
  back.innerHTML = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.9" '+
    'stroke-linecap="round"><path d="M10 3 5 8l5 5"/></svg><span>All clusters</span>';
  back.addEventListener('click', lineageClose);
  bar.appendChild(back);

  var crumb = el('div','lfcrumb');
  var grpLabel = members.length ? (CAT_GROUP[members[0].cat] || '') : '';
  crumb.innerHTML = (grpLabel ? '<span class="lfgrp">'+esc(grpLabel)+'</span><span class="lfsep">&rsaquo;</span>' : '')+
    '<h2>'+esc(label)+'</h2>';
  bar.appendChild(crumb);

  var brief = el('div','lfbrief');
  brief.innerHTML =
    '<span><b class="mono">'+members.length+'</b> entries</span>'+
    '<span><b class="mono">'+live+'</b> still live</span>'+
    (span ? '<span class="mono">'+span+'</span>' : '')+
    (archTop ? '<span class="lfarch mono">'+esc(archTop)+'</span>' : '');
  bar.appendChild(brief);
  host.appendChild(bar);

  /* ---- the board ---- */
  var board = el('div','lfboard');
  var inner = el('div','lfinner');
  var byEra = {};
  M.eras.forEach(function(x){ byEra[x.id] = []; });
  members.slice().sort(function(a,b){ return a.y - b.y || a.n.localeCompare(b.n); })
    .forEach(function(e){ (byEra[e.era] = byEra[e.era] || []).push(e); });

  /* the ancestry of the selected entry, within this cluster */
  var here = {}; members.forEach(function(e){ here[e.id] = 1; });
  var chain = {};
  if(LF.sel && here[LF.sel]){
    var front = [LF.sel];
    while(front.length){
      var nxt = [];
      front.forEach(function(id){
        chain[id] = 1;
        (BY_ID[id] && BY_ID[id].rel || []).forEach(function(r){
          if(here[r] && !chain[r]) nxt.push(r);
        });
      });
      front = nxt;
    }
  }

  var cardOf = {};
  /* an era with nothing in it gets no column: a cluster that only spans two
     eras should not spend half its width on empty air */
  var liveEras = M.eras.filter(function(x){ return (byEra[x.id]||[]).length; });
  inner.setAttribute('data-cols', String(Math.max(1, liveEras.length)));
  liveEras.forEach(function(x){
    var i = M.eras.findIndex(function(y){ return y.id === x.id; });
    var col = el('div','lfcol');
    col.style.setProperty('--era','var(--e'+(i+1)+')');
    col.style.setProperty('--era-t','var(--e'+(i+1)+'-t)');
    var head = el('div','lfhead');
    head.innerHTML = '<div class="lfrule"></div>'+
      '<div class="lfheadrow"><span class="lflab">'+esc(x.label)+'</span>'+
      '<span class="lfyrs mono">'+esc(x.range)+'</span>'+
      '<span class="lfn">'+(byEra[x.id]||[]).length+'</span></div>';
    col.appendChild(head);
    var stack = el('div','lfstack');
    (byEra[x.id]||[]).forEach(function(e){
      var c = el('button','lfcard'); c.type='button';
      c.setAttribute('data-id', e.id);
      if(e.id === LF.sel) c.setAttribute('aria-current','true');
      else if(chain[e.id]) c.setAttribute('data-chain','1');
      var liveDot = (e.st==='shipping'||e.st==='active');
      c.innerHTML =
        '<span class="lfspine"></span>'+
        '<span class="lfbody">'+
          '<span class="lfline"><span class="lfname">'+esc(e.n)+'</span>'+
          '<span class="lfyear mono">'+e.y+'</span></span>'+
          '<span class="lfmeta"><span class="lfby">'+esc(e.by||'—')+'</span>'+
          '<i class="lfdot'+(liveDot?' on':'')+'"></i>'+
          '<span class="lfst">'+esc(ST_LABEL[e.st]||e.st)+'</span></span>'+
        '</span>';
      c.addEventListener('click', function(){
        LF.sel = e.id;
        openDetail(e.id);
        lineageRender();
      });
      stack.appendChild(c);
      cardOf[e.id] = c;
    });
    col.appendChild(stack);
    inner.appendChild(col);
  });

  var svg = document.createElementNS('http://www.w3.org/2000/svg','svg');
  svg.setAttribute('class','lfthreads');
  inner.appendChild(svg);
  board.appendChild(inner);

  var legend = el('div','lflegend');
  legend.innerHTML =
    '<span>Each card is joined to the ones it <b>descends from</b>; the columns run in time order.</span>'+
    (LF.sel && here[LF.sel]
      ? '<span class="lfhot">Red traces the ancestry of '+esc(BY_ID[LF.sel].n)+'.</span>'
      : '<span>Select an entry to trace its ancestry.</span>');
  board.appendChild(legend);

  host.appendChild(board);
  LF.redraw = function(){ lineageThreads(inner, svg, cardOf, chain, members); };
  LF.cards = cardOf;
  requestAnimationFrame(function(){
    LF.redraw();
    if(LF.sel) lineageScrollTo(LF.sel);
  });
}

/* Redraw the threads after the board changes width (the sheet opening or
   closing reflows the columns, and the threads are drawn from real boxes). */
function lineageReflow(){
  if(LF.redraw) LF.redraw();
  if(LF.sel) lineageScrollTo(LF.sel);
}

/* Put the selected card in the middle of what the reader can actually see. */
function lineageScrollTo(id){
  var c = LF.cards && LF.cards[id];
  var pane = $('#pane-graph');
  if(!c || !pane) return;
  var pr = pane.getBoundingClientRect(), cr = c.getBoundingClientRect();
  var target = pane.scrollTop + (cr.top - pr.top) - (pr.height - cr.height)/2;
  var max = pane.scrollHeight - pane.clientHeight;
  pane.scrollTop = Math.max(0, Math.min(target, max));
}

/* Threads are drawn after layout from the cards' real boxes, so they follow
   the columns however they wrap or scroll. */
function lineageThreads(inner, svg, cardOf, chain, members){
  var W = inner.scrollWidth, H = inner.scrollHeight;
  svg.setAttribute('width', W); svg.setAttribute('height', H);
  svg.setAttribute('viewBox','0 0 '+W+' '+H);
  var hot = [], cold = [];
  var base = inner.getBoundingClientRect();
  function box(el2){
    var r = el2.getBoundingClientRect();
    return {x:r.left - base.left, y:r.top - base.top, w:r.width, h:r.height};
  }
  members.forEach(function(e){
    var kid = cardOf[e.id]; if(!kid) return;
    (e.rel||[]).forEach(function(pid){
      var par = cardOf[pid]; if(!par) return;
      var a = box(par), b = box(kid);
      var isHot = !!(chain[e.id] && chain[pid]);
      var d;
      if(b.x > a.x + 4){
        var ax = a.x + a.w, ay = a.y + a.h/2, bx = b.x, by = b.y + b.h/2;
        var mx = (ax + bx) / 2;
        d = 'M'+ax+' '+ay+' C'+mx+' '+ay+' '+mx+' '+by+' '+bx+' '+by;
      } else {
        /* same column: a bracket down the left margin */
        var sx = Math.min(a.x, b.x) - 11;
        d = 'M'+a.x+' '+(a.y + a.h/2)+' H'+sx+' V'+(b.y + b.h/2)+' H'+b.x;
      }
      (isHot ? hot : cold).push('<path d="'+d+'" fill="none" stroke="'+
        (isHot ? 'var(--acc)' : 'var(--line)')+'" stroke-width="'+(isHot ? 2 : 1.2)+'"/>');
    });
  });
  svg.innerHTML = cold.join('') + hot.join('');
}

function graphInit(){
  var cv = $('#gcanvas');
  GRAPH.cv = cv; GRAPH.cx = cv.getContext('2d');
  graphBuild();

  var gb = $('#groupby');
  GROUPBYS.forEach(function(g){
    var o = document.createElement('option');
    o.value = g.id; o.textContent = g.label;
    gb.appendChild(o);
  });
  gb.value = GRAPH.groupBy;
  function syncNestedCtl(){
    var nested = GRAPH.groupBy === 'nested';
    $('#gopenall').hidden = !nested;
    $('#gcloseall').hidden = !nested;
    var lv = $('#glvl-c'); if(lv) lv.parentNode.hidden = nested;
  }
  gb.addEventListener('change', function(){
    GRAPH.groupBy = this.value;
    if(GRAPH.tour) tourEnd();
    syncNestedCtl();
    graphRemap();
  });

  function syncLevel(){
    $('#glvl-c').setAttribute('aria-pressed', GRAPH.level === 'clusters' ? 'true' : 'false');
    $('#glvl-a').setAttribute('aria-pressed', GRAPH.level === 'all' ? 'true' : 'false');
    $('#glvl-c').parentNode.hidden = GRAPH.groupBy === 'nested';
  }
  function setLevel(v){
    if(GRAPH.level === v) return;
    GRAPH.level = v;
    if(GRAPH.tour && v === 'clusters') tourEnd();
    syncLevel();
    graphRemap();
  }
  $('#glvl-c').addEventListener('click', function(){ setLevel('clusters'); });
  $('#glvl-a').addEventListener('click', function(){ setLevel('all'); });
  syncLevel();

  var eb = $('#gess');
  eb.textContent = 'Essentials only · ' + ESS_N;
  eb.addEventListener('click', function(){ setEss(!S.ess); });

  $('#gopenall').addEventListener('click', function(){
    M.groups.forEach(function(g){
      GRAPH.exp.g[g.id] = 1;
      g.cats.forEach(function(c){ GRAPH.exp.c[c.id] = 1; });
    });
    graphRemap();
  });
  $('#gcloseall').addEventListener('click', function(){
    GRAPH.exp = {g:{}, c:{}};
    if(S.sel) closeDetail();
    graphRemap();
  });
  syncNestedCtl();

  var host = $('#gtours');
  TOURS.forEach(function(t){
    var b = el('button','gchip', t.label);
    b.type = 'button';
    b.setAttribute('data-tour', t.id);
    b.setAttribute('aria-pressed','false');
    b.addEventListener('click', function(){
      $('#groutes').open = false;
      if(GRAPH.tour && GRAPH.tour.id === t.id) tourEnd();
      else tourStart(t.id);
    });
    host.appendChild(b);
  });
  /* click away to dismiss the routes list */
  document.addEventListener('click', function(ev){
    var d = $('#groutes');
    if(d && d.open && !d.contains(ev.target)) d.open = false;
  });
  /* The control bar scrolls sideways on narrow screens, which clips anything
     absolutely positioned inside it — the panel was opening behind the bar's
     own edge. Pinning it to the viewport on open puts it back in sight. */
  var rd = $('#groutes');
  rd.addEventListener('toggle', function(){
    var panel = rd.querySelector('.groutes-p');
    if(!rd.open){ panel.removeAttribute('style'); return; }
    var r = rd.querySelector('summary').getBoundingClientRect();
    panel.style.position = 'fixed';
    panel.style.top = Math.round(r.bottom + 6) + 'px';
    panel.style.left = Math.round(Math.min(r.left, window.innerWidth - 262)) + 'px';
  });
  $('#tourprev').addEventListener('click', function(){ tourGo(GRAPH.tourStep - 1); });
  $('#tournext').addEventListener('click', tourNext);
  $('#tourexit').addEventListener('click', tourEnd);
  $('#tourinfo').addEventListener('click', function(){ if(S.sel) openDetail(S.sel); });

  /* The era key belongs beside the controls, not in a band of its own at the
     bottom: it was duplicating the rail's Era facet and stealing 50px of map. */
  var leg = $('#glegend');
  leg.innerHTML = '<span class="legttl">Eras</span>' + M.eras.map(function(e,i){
    return '<div class="row" title="'+esc(e.range)+'">'+
           '<span class="sw" style="background:var(--e'+(i+1)+')"></span>'+esc(e.label)+'</div>';
  }).join('');

  /* interaction */
  /* The map is a canvas, so nothing in it is reachable by tab on its own.
     Making it one focus stop with arrow keys to walk the clusters and Enter to
     open one means the whole lineage is usable without a pointer. */
  cv.setAttribute('tabindex','0');
  cv.setAttribute('role','application');
  cv.setAttribute('aria-label','Lineage map. Arrow keys move between clusters, Enter opens one.');
  var kbIdx = -1;
  cv.addEventListener('keydown', function(ev){
    var list = GRAPH.hubs.concat(GRAPH.superHubs);
    if(!list.length) return;
    var nav = {ArrowRight:1, ArrowDown:1, ArrowLeft:-1, ArrowUp:-1, Tab:0};
    if(ev.key in nav && ev.key !== 'Tab'){
      ev.preventDefault();
      kbIdx = (kbIdx + nav[ev.key] + list.length) % list.length;
      GRAPH.hoverHub = list[kbIdx]; GRAPH.hover = null;
      var h = GRAPH.hoverHub;
      var tp = $('#gtip');
      tp.setAttribute('data-on','1');
      if(h.medallion){
        tp.className = 'peek'; tp.innerHTML = peekHTML(h);
        var ph = tp.offsetHeight || 250;
        var ax = h.x*GRAPH.k + GRAPH.tx, ay = h.y*GRAPH.k + GRAPH.ty;
        var lft = ax + h.hubR*GRAPH.k + 16;
        if(lft + 268 > GRAPH.W - 8) lft = ax - h.hubR*GRAPH.k - 16 - 268;
        tp.style.left = Math.max(8, lft) + 'px';
        tp.style.top = Math.max(8, Math.min(ay - ph/2, GRAPH.H - ph - 8)) + 'px';
      } else {
        tp.className = '';
        tp.innerHTML = '<div class="n">'+esc(h.label || h.d.label)+'</div>'+
          '<div class="m">'+(h.members ? h.members.length : h.count)+' entries</div>';
        tp.style.left = Math.min(h.x*GRAPH.k + GRAPH.tx + 14, GRAPH.W - 260) + 'px';
        tp.style.top = Math.max(8, h.y*GRAPH.k + GRAPH.ty - 10) + 'px';
      }
      GRAPH.draw();
      return;
    }
    if((ev.key === 'Enter' || ev.key === ' ') && GRAPH.hoverHub){
      ev.preventDefault();
      if(GRAPH.hoverHub.tkind === 'g') graphToggle(GRAPH.hoverHub);
      else lineageOpen(GRAPH.hoverHub);
    }
  });
  cv.addEventListener('blur', function(){
    if(kbIdx >= 0){ kbIdx = -1; GRAPH.hoverHub = null; $('#gtip').setAttribute('data-on','0'); GRAPH.draw(); }
  });

  var dragging = false, lastX = 0, lastY = 0, moved = 0;
  function toWorld(px, py){ return {x:(px - GRAPH.tx)/GRAPH.k, y:(py - GRAPH.ty)/GRAPH.k}; }

  function pick(px, py){
    var w = toWorld(px, py);
    var anyFilter = dimmingCount() > 0;
    var match = null;
    if(anyFilter){ match = {}; results().forEach(function(e){ match[e.id]=1; }); }
    var best = null, bd = 13/GRAPH.k;
    GRAPH.hubs.forEach(function(h){
      if(!h.open) return;
      h.members.forEach(function(n){
        if(match && !match[n.id] && S.sel !== n.id) return;
        var dx = n.x - w.x, dy = n.y - w.y, d = Math.sqrt(dx*dx + dy*dy);
        if(d < Math.max(n.r + 5/GRAPH.k, bd) && (!best || d < best.d)) best = {n:n, d:d};
      });
    });
    if(best) return {type:'node', n:best.n};
    for(var i=0;i<GRAPH.hubs.length;i++){
      var h = GRAPH.hubs[i];
      var dx = h.x - w.x, dy = h.y - w.y;
      if(Math.sqrt(dx*dx + dy*dy) < h.hubR + 5/GRAPH.k) return {type:'hub', h:h};
    }
    for(var j=0;j<GRAPH.superHubs.length;j++){
      var g = GRAPH.superHubs[j];
      var dx2 = g.x - w.x, dy2 = g.y - w.y;
      if(Math.sqrt(dx2*dx2 + dy2*dy2) < g.hubR + 5/GRAPH.k) return {type:'hub', h:g};
    }
    return null;
  }

  cv.addEventListener('mousedown', function(ev){
    dragging = true; moved = 0; lastX = ev.clientX; lastY = ev.clientY; cv.classList.add('drag');
  });
  window.addEventListener('mouseup', function(){ dragging = false; cv.classList.remove('drag'); });
  cv.addEventListener('mousemove', function(ev){
    var r = cv.getBoundingClientRect();
    var px = ev.clientX - r.left, py = ev.clientY - r.top;
    if(dragging){
      GRAPH.tx += ev.clientX - lastX; GRAPH.ty += ev.clientY - lastY;
      moved += Math.abs(ev.clientX-lastX) + Math.abs(ev.clientY-lastY);
      lastX = ev.clientX; lastY = ev.clientY;
      GRAPH.draw(); return;
    }
    var hit = pick(px, py);
    var tip = $('#gtip');
    var hn = hit && hit.type==='node' ? hit.n : null;
    var hh = hit && hit.type==='hub' ? hit.h : null;
    if(hn !== GRAPH.hover || hh !== GRAPH.hoverHub){
      GRAPH.hover = hn; GRAPH.hoverHub = hh; GRAPH.draw();
    }
    if(hn){
      tip.setAttribute('data-on','1');
      tip.innerHTML = '<div class="n">'+esc(hn.e.n)+'</div><div class="m">'+hn.e.y+' · '+
        esc(ST_LABEL[hn.e.st]||'')+' · '+
        esc((hn.e.arch||[]).map(function(a){return ARCH_LABEL[a]||a;}).join('/'))+'</div>';
      tip.style.left = Math.min(px + 14, GRAPH.W - 250) + 'px';
      tip.style.top  = Math.max(py - 10, 4) + 'px';
      cv.style.cursor = 'pointer';
    } else if(hh){
      tip.setAttribute('data-on','1');
      var cnt = hh.members && hh.members.length ? hh.members.length : hh.count;
      if(hh.medallion){
        tip.className = 'peek';
        tip.innerHTML = peekHTML(hh);
        var pw = 268, ph = tip.offsetHeight || 250;
        var atX = hh.x*GRAPH.k + GRAPH.tx, atY = hh.y*GRAPH.k + GRAPH.ty;
        var r2 = hh.hubR*GRAPH.k;
        var left = atX + r2 + 16;
        if(left + pw > GRAPH.W - 8) left = atX - r2 - 16 - pw;
        tip.style.left = Math.max(8, left) + 'px';
        tip.style.top  = Math.max(8, Math.min(atY - ph/2, GRAPH.H - ph - 8)) + 'px';
      } else {
        tip.className = '';
        var act = (hh.tkind === 'g')
          ? (hh.collapsed ? 'click to open the shell' : 'click to close the shell')
          : 'click to read this cluster';
        tip.innerHTML = '<div class="n">'+esc(hh.label || hh.d.label)+'</div><div class="m">'+
          cnt+' entries · '+act+'</div>';
        tip.style.left = Math.min(px + 14, GRAPH.W - 250) + 'px';
        tip.style.top  = Math.max(py - 10, 4) + 'px';
      }
      cv.style.cursor = 'pointer';
    } else {
      tip.setAttribute('data-on','0');
      cv.style.cursor = dragging ? 'grabbing' : 'grab';
    }
  });
  cv.addEventListener('mouseleave', function(){
    $('#gtip').setAttribute('data-on','0');
    if(GRAPH.hover || GRAPH.hoverHub){ GRAPH.hover = null; GRAPH.hoverHub = null; GRAPH.draw(); }
  });
  cv.addEventListener('click', function(ev){
    if(moved > 5) return;
    var r = cv.getBoundingClientRect();
    var hit = pick(ev.clientX - r.left, ev.clientY - r.top);
    if(hit && hit.type === 'node'){
      /* openDetail re-centres on the part of the canvas the sheet leaves free */
      openDetail(hit.n.id);
    } else if(hit && hit.type === 'hub'){
      /* A group shell still opens in place — that is the matryoshka. But a
         CLUSTER opens the readable board: zooming a cluster could only ever
         make its labels smaller, so opening one changes representation
         instead of scale. */
      if(hit.h.tkind === 'g'){
        graphToggle(hit.h);
      } else {
        lineageOpen(hit.h);
      }
    } else {
      closeDetail();
    }
  });
  cv.addEventListener('wheel', function(ev){
    ev.preventDefault();
    var r = cv.getBoundingClientRect();
    var px = ev.clientX - r.left, py = ev.clientY - r.top;
    var f = Math.exp(-ev.deltaY * 0.0016);
    var nk = Math.max(0.22, Math.min(9, GRAPH.k * f));
    var w = toWorld(px, py);
    GRAPH.k = nk;
    GRAPH.tx = px - w.x * GRAPH.k;
    GRAPH.ty = py - w.y * GRAPH.k;
    GRAPH.draw();
  }, {passive:false});

  var tPrev = null;
  cv.addEventListener('touchstart', function(ev){
    if(ev.touches.length === 1) tPrev = {x:ev.touches[0].clientX, y:ev.touches[0].clientY};
    else if(ev.touches.length === 2){
      var a = ev.touches[0], b = ev.touches[1];
      tPrev = {d:Math.hypot(a.clientX-b.clientX, a.clientY-b.clientY)};
    }
  }, {passive:true});
  cv.addEventListener('touchmove', function(ev){
    if(!tPrev) return;
    ev.preventDefault();
    var r = cv.getBoundingClientRect();
    if(ev.touches.length === 1 && tPrev.x != null){
      GRAPH.tx += ev.touches[0].clientX - tPrev.x;
      GRAPH.ty += ev.touches[0].clientY - tPrev.y;
      tPrev = {x:ev.touches[0].clientX, y:ev.touches[0].clientY};
      GRAPH.draw();
    } else if(ev.touches.length === 2 && tPrev.d != null){
      var a = ev.touches[0], b = ev.touches[1];
      var d = Math.hypot(a.clientX-b.clientX, a.clientY-b.clientY);
      var px = (a.clientX+b.clientX)/2 - r.left, py = (a.clientY+b.clientY)/2 - r.top;
      var w = {x:(px - GRAPH.tx)/GRAPH.k, y:(py - GRAPH.ty)/GRAPH.k};
      GRAPH.k = Math.max(0.22, Math.min(9, GRAPH.k * (d/tPrev.d)));
      GRAPH.tx = px - w.x*GRAPH.k; GRAPH.ty = py - w.y*GRAPH.k;
      tPrev.d = d; GRAPH.draw();
    }
  }, {passive:false});
  cv.addEventListener('touchend', function(){ tPrev = null; }, {passive:true});

  $('#gzin').addEventListener('click', function(){ zoomBy(1.35); });
  $('#gzout').addEventListener('click', function(){ zoomBy(1/1.35); });
  $('#gfit').addEventListener('click', function(){ graphFit(true); });
  function zoomBy(f){
    var px = GRAPH.W/2, py = GRAPH.H/2;
    var w = {x:(px-GRAPH.tx)/GRAPH.k, y:(py-GRAPH.ty)/GRAPH.k};
    GRAPH.k = Math.max(0.22, Math.min(9, GRAPH.k*f));
    GRAPH.tx = px - w.x*GRAPH.k; GRAPH.ty = py - w.y*GRAPH.k;
    GRAPH.draw();
  }
}

/* ---------------------------------------------------------------- views */
function setView(v){
  if(v !== 'graph' && typeof LF !== 'undefined' && LF.open) lineageClose();
  S.view = v;
  /* the rail filters the collection; the front page is not the collection */
  var app = document.getElementById('app');
  if(app) app.setAttribute('data-view', v);
  if(v === 'overview' && S.railOpen){
    S.railOpen = false;
    $('#rail').setAttribute('data-open','0');
    $('#scrim').setAttribute('data-on','0');
  }
  ['overview','catalog','graph'].forEach(function(name){
    $('#pane-'+name).hidden = (name !== v);
    var btn = $('#v-'+name);
    btn.setAttribute('aria-pressed', name === v ? 'true' : 'false');
  });
  if(v === 'overview') renderOverview();
  if(v === 'graph'){
    if(!GRAPH.cv) graphInit();
    requestAnimationFrame(graphResize);
  }
  render();
}

function render(){
  var list = syncRail();
  if(S.view === 'catalog') renderCatalog(list);
  if(S.view === 'graph' && GRAPH.ready) GRAPH.draw();
}

/* ---------------------------------------------------------------- boot */
function boot(){
  buildShell();
  buildRail();

  /* theme */
  var stored = null;
  try{ stored = localStorage.getItem('amiga-theme'); }catch(err){}
  if(stored) document.documentElement.setAttribute('data-theme', stored);
  $('#themebtn').addEventListener('click', function(){
    var cur = document.documentElement.getAttribute('data-theme');
    var isDark = cur ? cur === 'dark'
      : !window.matchMedia('(prefers-color-scheme: light)').matches;
    var next = isDark ? 'light' : 'dark';
    document.documentElement.setAttribute('data-theme', next);
    try{ localStorage.setItem('amiga-theme', next); }catch(err){}
    if(GRAPH.ready) GRAPH.draw();
  });

  /* search */
  var si = $('#search'), t = 0;
  si.addEventListener('input', function(){
    clearTimeout(t);
    t = setTimeout(function(){
      S.q = si.value.trim();
      if(S.q && S.view === 'overview') setView('catalog'); else render();
    }, 110);
  });
  $('#searchclear').addEventListener('click', function(){
    si.value = ''; S.q = ''; si.focus(); render();
  });

  $('#v-overview').addEventListener('click', function(){ setView('overview'); });
  $('#v-catalog').addEventListener('click', function(){ setView('catalog'); });
  $('#v-graph').addEventListener('click', function(){ setView('graph'); });

  $('#sort').addEventListener('change', function(){ S.sort = this.value; render(); });
  $('#expandall').addEventListener('click', function(){
    M.groups.forEach(function(g){ S.open.g[g.id] = true;
      g.cats.forEach(function(c){ S.open.c[c.id] = true; }); });
    render();
  });
  $('#collapseall').addEventListener('click', function(){
    M.groups.forEach(function(g){ S.open.g[g.id] = true;
      g.cats.forEach(function(c){ S.open.c[c.id] = false; }); });
    render();
  });

  var cess = $('#cess');
  cess.textContent = 'Essentials only · ' + ESS_N;
  cess.addEventListener('click', function(){ setEss(!S.ess); });

  $('#cat-scroll').addEventListener('click', function(ev){
    var c = ev.target.closest('.card'); if(!c) return;
    openDetail(c.getAttribute('data-id'));
  });

  $('#railtoggle').addEventListener('click', function(){
    S.railOpen = !S.railOpen;
    $('#rail').setAttribute('data-open', S.railOpen ? '1' : '0');
    $('#scrim').setAttribute('data-on', S.railOpen ? '1' : '0');
  });
  $('#scrim').addEventListener('click', function(){
    S.railOpen = false;
    $('#rail').setAttribute('data-open','0');
    $('#scrim').setAttribute('data-on','0');
  });

  /* Any click outside the sheet dismisses it, with two guards.
     · OPENING — a card, a star on the map or a row on the cluster board all
       sit outside the sheet, and their handlers run while the same click is
       still bubbling towards this one. openDetail() raises OPENING for the
       rest of that event so picking an entry cannot immediately drop it.
     · a running route owns the selection. tourGo() sets S.sel to mark the
       current stop, so S.sel is NOT the same thing as "the sheet is open" —
       hence sheetOpen(), which reads the sheet itself. During a route the
       sheet is dismissed by Escape or the route's own controls, otherwise
       walking to the next stop would fight this handler. */
  document.addEventListener('click', function(ev){
    if(OPENING || GRAPH.tour || !sheetOpen()) return;
    var t = ev.target;
    if(t && t.closest && t.closest('.drawer')) return;
    closeDetail();
  });

  document.addEventListener('keydown', function(ev){
    if(ev.key === 'Escape'){
      if(S.sel) closeDetail();
      else if(LF.open) lineageClose();
      else if(GRAPH.tour) tourEnd();
      else if(S.railOpen){ S.railOpen = false; $('#rail').setAttribute('data-open','0'); $('#scrim').setAttribute('data-on','0'); }
    }
    if(ev.key === '/' && document.activeElement !== si){ ev.preventDefault(); si.focus(); si.select(); }
    if(GRAPH.tour && document.activeElement !== si){
      if(ev.key === 'ArrowRight'){ ev.preventDefault(); tourNext(); }
      if(ev.key === 'ArrowLeft'){ ev.preventDefault(); tourGo(GRAPH.tourStep - 1); }
    }
    if(ev.key === '1' && !ev.metaKey && !ev.ctrlKey && document.activeElement !== si) setView('overview');
    if(ev.key === '2' && !ev.metaKey && !ev.ctrlKey && document.activeElement !== si) setView('catalog');
    if(ev.key === '3' && !ev.metaKey && !ev.ctrlKey && document.activeElement !== si) setView('graph');
  });

  var rt = 0;
  window.addEventListener('resize', function(){
    clearTimeout(rt);
    rt = setTimeout(function(){ if(S.view === 'graph') graphResize(); }, 120);
  });

  setView('overview');
  console.log('[amiga] ' + ALL.length + ' entries, ' +
    ALL.reduce(function(a,e){ return a + (e.rel||[]).length; }, 0) + ' lineage links.');
}

if(document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot);
else boot();

})();
