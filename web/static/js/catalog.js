/* ==========================================================================
   The Amiga Constellation — catalog.js
   shell, facet rail, chips, cards, catalogue and overview
   Load order (classic scripts sharing one global scope, no modules):
   data.js -> core.js -> catalog.js -> detail.js -> lineage.js -> app.js
   ========================================================================== */
'use strict';

/* ---------------------------------------------------------------- shell */
function buildShell(){
  document.body.innerHTML =
  '<div id="app">'+
    '<a class="skip" href="#stage">Skip to the collection</a>'+
    '<header class="topbar">'+
      '<div class="brand">'+
        rainbowMark(26,'rb-top')+
        '<span class="wm"><b>Amiga</b> <i>Constellation</i></span>'+
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
        '<a class="toplink" href="/suggestions" title="Suggest an entry or a correction">'+ICON.suggest+'<span>Suggest</span></a>'+
        '<button class="iconbtn" id="themebtn" type="button" aria-label="Screen Mode"></button>'+
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
            '<span class="glbl">Arrange</span>'+
            '<div class="garrange" id="garrange" role="group" aria-label="Arrange clusters by"></div>'+
            '<div class="gdetail" role="group" aria-label="Level of detail">'+
              '<button id="glvl-c" type="button" aria-pressed="true">Clusters</button>'+
              '<button id="glvl-a" type="button" aria-pressed="false">Every entry</button>'+
            '</div>'+
            '<button class="gchip gess" id="gess" type="button" aria-pressed="false">Essentials</button>'+
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
      '</main>'+
    '</div>'+
  '</div>'+
  '<dialog class="detail-dialog" id="detail-dialog" aria-labelledby="detail-title">'+
    '<div class="detail-panel">'+
      '<div class="detail-toolbar">'+
        '<button class="btn ghost" id="detail-back" type="button" disabled>← Back</button>'+
        '<span class="eyebrow" id="detail-kicker">Object details</span>'+
        '<button class="iconbtn" id="detail-close" type="button" aria-label="Close details" autofocus>'+ICON.x+'</button>'+
      '</div>'+
      '<div class="dscroll" id="detail-content"></div>'+
    '</div>'+
    '<div class="detail-flight-layer" aria-hidden="true" inert></div>'+
  '</dialog>';
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
    ch.innerHTML = '<span>'+esc(label)+' <b>'+esc(val)+'</b></span>';
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
    if(!S.catalogInitialized && !filtering){
      /* The first unfiltered catalogue visit starts as a compact index:
         Hardware is the only open top-level drawer, and its categories stay
         folded until the visitor chooses one. Later visits preserve choices. */
      M.groups.forEach(function(g){
        S.open.g[g.id] = g.id === 'g-hw';
        g.cats.forEach(function(c){ S.open.c[c.id] = false; });
      });
      S.openedFirst = 1;
    }else{
      S.openedFirst = filtering ? 1 : 0;
    }
  }
  S.catalogInitialized = true;
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

  var links  = ALL.reduce(function(a,e){ return a + (e.rel||[]).length; }, 0);
  var first  = M.eras[0], last = M.eras[M.eras.length-1];
  var span   = first.from+'–'+last.to;
  var wrap   = el('div','scroll ovscroll');
  var r      = el('div','front');
  function section(num, title, note, body){
    var s = el('section','fsec');
    s.appendChild(sectionHead(num, title, note));
    s.appendChild(body);
    r.appendChild(s);
  }

  /* ---------------- hero: the words on the left, the map on the right ---------------- */
  var hero = el('div','fhero');
  var hl = el('div','fhero-l');
  hl.innerHTML =
    '<div class="eyebrow">A field guide · snapshot August 2026</div>'+
    '<h1>A computer that outlived<br>its manufacturer by<br>30+ years.</h1>'+
    '<p class="flede">Commodore went bankrupt in April 1994. The Amiga did not. This is a map of what '+
    'happened instead — the accelerators, replica boards and FPGA recreations that kept the hardware '+
    'running, the operating systems still in development, the emulators that quietly became '+
    'infrastructure, and the archives that made all of it usable.</p>'+
    '<div class="fctas">'+
      '<button class="btn" type="button" id="fcta-map">'+
        '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">'+
        '<circle cx="12" cy="12" r="3"/><circle cx="4" cy="6" r="2"/><circle cx="20" cy="6" r="2"/>'+
        '<circle cx="6" cy="19" r="2"/><circle cx="19" cy="18" r="2"/>'+
        '<path d="M6 7l4 3M18 7l-4 3M7 18l3-4M17 17l-3-3"/></svg>Open the lineage map</button>'+
      '<button class="btn ghost" type="button" id="fcta-cat">Browse the catalogue</button>'+
    '</div>'+
    '<div class="fstatline mono">'+
      [[ALL.length,'entries'],[links,'lineage links'],[DOMAINS.length,'clusters'],
       [TOURS.length,'guided routes'],[span,'']].map(function(p){
        return '<span><b>'+p[0]+'</b>'+(p[1] ? ' '+p[1] : '')+'</span>';
      }).join('')+
    '</div>';
  hl.querySelector('#fcta-map').addEventListener('click', function(){ setView('graph'); });
  hl.querySelector('#fcta-cat').addEventListener('click', function(){ setView('catalog'); });
  hero.appendChild(hl);

  /* The panel is a drawing of the map, not a live one, and it is dark in both
     themes; it opens the Lineage view at cluster level. */
  var map = el('button','fmap'); map.type = 'button';
  map.setAttribute('aria-label', 'Open the lineage map: '+ALL.length+
    ' entries arranged as a constellation around '+DOMAINS.length+' clusters');
  map.innerHTML =
    '<span class="fmap-art">'+heroMapSVG()+
      '<span class="fmap-chip l mono">Lineage · three of '+DOMAINS.length+' clusters</span>'+
      '<span class="fmap-chip r mono">Open the map →</span>'+
    '</span>'+
    '<span class="fmap-cap">'+
      '<span class="fmap-cap-b">'+
        '<span class="fmap-cap-t">'+ALL.length+' entries, '+links+' links, '+DOMAINS.length+' clusters.</span>'+
        '<span class="fmap-cap-h">Drag, zoom, open any dot. Arrange by domain, era, architecture, status or kind.</span>'+
      '</span>'+
      '<span class="fmap-leg mono">'+M.eras.map(function(x){
        return '<span><i style="background:'+eraColor(x.id)+'"></i>'+esc(x.label)+'</span>';
      }).join('')+'</span>'+
    '</span>';
  map.addEventListener('click', function(){ setView('graph'); pressGraphButton('#glvl-c'); });
  var hr = el('div','fhero-r'); hr.appendChild(map);
  hero.appendChild(hr);
  r.appendChild(hero);

  /* ---------------- first releases by year, and the four eras ---------------- */
  var years = el('div','fyears');
  var byYear = {};
  ALL.forEach(function(e){ byYear[e.y] = (byYear[e.y]||0) + 1; });
  var eraOf = function(y){
    var x = M.eras.find(function(z){ return y >= z.from && y <= z.to; });
    return x ? x.id : last.id;
  };
  var bars = '', axis = [];
  for(var y = first.from; y <= last.to; y++){
    var n = byYear[y] || 0;
    bars += '<div class="fbar" data-era="'+eraOf(y)+'" title="'+y+' · '+n+(n===1?' entry':' entries')+
      '" style="height:'+Math.max(3, n*6)+'px;background:'+eraColor(eraOf(y))+'"></div>';
    if(y === first.from || y === last.to || (y % 5 === 0 && y - first.from > 1 && last.to - y > 1))
      axis.push('<span>'+y+'</span>');
  }
  years.innerHTML =
    '<div class="fyears-head"><h2>First releases, year by year</h2>'+
    '<p>One bar per year. Pick an era below to light up its years; click again to clear.</p></div>'+
    '<div class="fbars">'+bars+'</div>'+
    '<div class="faxis mono">'+axis.join('')+'</div>';
  var eras = el('div','feras');
  M.eras.forEach(function(x){
    var n = ALL.filter(function(e){ return e.era===x.id; }).length;
    var d = el('div','fera');
    d.setAttribute('data-era', x.id);
    d.style.setProperty('--era', eraColor(x.id));
    d.style.setProperty('--era-t', eraTint(x.id));
    var pick = el('button','fera-pick'); pick.type='button';
    pick.setAttribute('aria-pressed','false');
    pick.innerHTML =
      '<span class="fera-n mono">'+x.range+'</span>'+
      '<h3>'+esc(x.label)+'</h3>'+
      '<p>'+esc(x.note)+'</p>'+
      '<span class="fera-c"><b class="mono">'+n+'</b> entries</span>';
    pick.addEventListener('click', function(){
      var on = years.getAttribute('data-hl') === x.id;
      if(on) years.removeAttribute('data-hl'); else years.setAttribute('data-hl', x.id);
      eras.querySelectorAll('.fera-pick').forEach(function(b){
        b.setAttribute('aria-pressed', !on && b === pick ? 'true' : 'false');
      });
    });
    var go = el('button','fera-go mono', 'Browse '+n+' entries →'); go.type='button';
    go.addEventListener('click', function(){
      DIMS.forEach(function(d){ S.f[d] = {}; });
      S.f.era[x.id] = 1; setView('catalog');
    });
    d.appendChild(pick); d.appendChild(go);
    eras.appendChild(d);
  });
  years.appendChild(eras);
  r.appendChild(years);

  /* ---------------- three ways in ---------------- */
  var ways = el('div','fways');
  var sections = M.groups.reduce(function(a,g){ return a + g.cats.length; }, 0);
  var withImg  = ALL.filter(function(e){ return e.img; }).length;
  /* screenshots live outside the versioned path: they change without the JS
     hash changing, and the plain path is ETag-revalidated */
  [
    {shot:'lineage-dark', pos:'20% 0', era:'e3',
     alt:'The Lineage view: twelve cluster rings arranged under Hardware, System software and Practice and stewardship',
     eyebrow:'Lineage · clusters', title:'See how things connect',
     body:'Twelve clusters, each ring coloured by the eras it spans. Open one and its members lay out in time order, joined to whatever they descend from.',
     go:function(){ setView('graph'); }},
    {shot:'catalog-dark', pos:'0 0', era:'e2',
     alt:'The Catalogue view: numbered sections with a filter rail for architecture, status and era',
     eyebrow:'Catalogue · '+sections+' sections', title:'Browse everything, in plates',
     body:'Numbered sections and plates. The rail narrows by processor, era, status or kind, and its counts never lead to an empty result.',
     go:function(){ setView('catalog'); }},
    {shot:'detail-light', pos:'50% 12%', era:'e4',
     alt:'An entry dialog for the Vampire V4 Standalone with photo, facts and the three suggestion actions',
     eyebrow:'Entry · '+withImg+' with photographs', title:'Open any plate for the full story',
     body:'What it is, who makes it, whether it still ships, why it matters, and what it descends from. Spotted a mistake? Suggest a change from right there.',
     go:function(b){ if(BY_ID.v4sa) openDetail('v4sa', b); else setView('catalog'); }}
  ].forEach(function(w){
    var b = el('button','fway fcard'); b.type='button';
    b.style.setProperty('--era', eraColor(w.era));
    b.innerHTML =
      '<img class="fshot" src="/static/img/'+w.shot+'.jpg" alt="'+esc(w.alt)+'" '+
        'loading="lazy" decoding="async" style="object-position:'+w.pos+'">'+
      '<span class="fway-t"><span class="eyebrow">'+esc(w.eyebrow)+'</span>'+
      '<h3 class="flift">'+esc(w.title)+'</h3><p>'+esc(w.body)+'</p></span>';
    b.addEventListener('click', function(){ w.go(b); });
    ways.appendChild(b);
  });
  section('01','Three ways in','Every screen below is live on the site. Click one to go there.', ways);

  /* ---------------- the clusters, as era-composition rings ---------------- */
  var cl = el('div','fclusters');
  DOMAINS.forEach(function(d){
    var members = ALL.filter(function(e){ return d.cats.indexOf(e.cat) >= 0; });
    var live = members.filter(isLive).length;
    var mix = M.eras.map(function(x){ return members.filter(function(e){ return e.era===x.id; }).length; });
    var total = members.length || 1, acc = 0, stops = [];
    mix.forEach(function(n, i){
      if(!n) return;
      var from = acc/total*100, to = (acc+n)/total*100, cut = Math.max(from, to-1.6);
      stops.push(eraColor(M.eras[i].id)+' '+from.toFixed(1)+'% '+cut.toFixed(1)+'%');
      stops.push('transparent '+cut.toFixed(1)+'% '+to.toFixed(1)+'%');
      acc += n;
    });
    var b = el('button','fcl fcard'); b.type='button';
    b.innerHTML =
      '<span class="fcl-ring" style="background:conic-gradient(from -90deg, '+stops.join(', ')+')">'+
        '<span class="fcl-core"><span class="fcl-n">'+members.length+'</span>'+
        '<span class="fcl-live mono">'+live+' live</span></span></span>'+
      '<span class="fcl-l flift">'+esc(d.label)+'</span>'+
      '<span class="fcl-g mono">'+esc(CAT_GROUP[d.cats[0]] || '')+'</span>';
    b.addEventListener('click', function(){ openCluster(d.id); });
    cl.appendChild(b);
  });
  section('02','The clusters',
    'Each ring is coloured by the eras its entries come from. Click one to open it on the map.', cl);

  /* ---------------- selected objects ----------------
     A deliberate front-page cross-section: classic hardware, modern
     accelerators, graphics, interfaces, distributions, emulation and
     preservation. Keep the order editorial rather than data-driven. */
  var picked = [
    'a1200', 'v4sa', 'pistorm32', 'picasso', 'workbench',
    'amithlon', 'pimiga', 'uae', 'whdload', 'amos', 'amigaforever', 'a6000'
  ].map(function(id){ return BY_ID[id]; }).filter(function(e){ return e && e.img; }).slice(0,8);
  var fg = el('div','fplates');
  picked.forEach(function(e){
    var b = el('button','fplate fcard'); b.type='button';
    b.style.setProperty('--era', eraColor(e.era));
    var box = el('span','fplate-img');
    if(e.imgBg) box.style.background = e.imgBg;
    if(e.imgFit === 'contain') box.style.padding = '18px';
    var im = document.createElement('img');
    im.alt = e.n; im.loading = 'lazy'; im.decoding = 'async';
    im.style.objectFit = e.imgFit; im.style.objectPosition = e.imgPos;
    im.src = e.img;
    box.appendChild(im);
    b.appendChild(box);
    var t = el('span','fplate-t');
    t.innerHTML = '<span class="fpline"><span class="fpn flift">'+esc(e.n)+'</span>'+
      '<span class="yr mono">'+e.y+'</span></span>'+
      '<span class="fpspec">'+esc(CAT_LABEL[e.cat])+'</span>';
    b.appendChild(t);
    b.addEventListener('click', function(){ openDetail(e.id, b); });
    fg.appendChild(b);
  });
  section('03','Selected objects',
    picked.length+' of the '+ALL.length+'. Any plate opens its full entry.', fg);

  /* ---------------- guided routes ---------------- */
  if(TOURS.length){
    var tg = el('div','froutes');
    TOURS.forEach(function(t){
      var stops = t.steps.map(function(s){ return BY_ID[s.id]; }).filter(Boolean);
      if(!stops.length) return;
      var a = stops[0], z = stops[stops.length-1];
      var b = el('button','froute fcard'); b.type='button';
      b.innerHTML = '<span class="froute-n mono">'+pad2(t.steps.length)+' stops · '+a.y+'–'+z.y+'</span>'+
        '<h3 class="flift">'+esc(t.label)+'</h3>'+
        '<span class="ftrack">'+stops.map(function(e, i){
          return '<span class="fstop"><i class="fdot" style="background:'+eraColor(e.era)+'"></i>'+
            (i < stops.length-1 ? '<i class="fseg"></i>' : '')+'</span>';
        }).join('')+'</span>'+
        '<span class="froute-ends">'+esc(a.n)+' <span>→</span> '+esc(z.n)+'</span>'+
        '<span class="froute-go mono">Follow the route →</span>';
      b.addEventListener('click', function(){ setView('graph'); tourStart(t.id); });
      tg.appendChild(b);
    });
    section('04','Guided routes',
      'Each one walks the map stop by stop. The fastest way in if you do not know what you are looking for.', tg);
  }

  /* ---------------- taxonomy ---------------- */
  var tax = el('div','ftax');
  ['original','replica','fpga','emulator','compatible','oslike'].forEach(function(id){
    var k = M.kinds.find(function(x){ return x.id===id; });
    if(!k) return;
    var d = el('div','ftaxrow');
    d.innerHTML = '<b>'+esc(k.label)+'</b><span>'+esc(k.note)+'</span>';
    tax.appendChild(d);
  });
  section('05','How to read this',
    'Six kinds of thing are constantly confused for one another. The Kind filter keeps them apart.', tax);

  /* ---------------- theses ---------------- */
  var th = el('div','ftheses');
  M.theses.forEach(function(t,i){
    var d = el('div','fthesis');
    d.innerHTML = '<span class="fth-n mono">'+pad2(i+1)+'</span>'+
      '<div><h3>'+esc(t.t)+'</h3><p>'+esc(t.b)+'</p></div>';
    th.appendChild(d);
  });
  section('06','Six things this map shows', null, th);

  /* ---------------- cautions ---------------- */
  var cs = el('div','fcautions');
  M.cautions.forEach(function(c){
    var d = el('div','fcaution');
    d.innerHTML = '<b>'+esc(c[0])+'</b><span>'+esc(c[1])+'</span>';
    cs.appendChild(d);
  });
  section('07','Cautions','This is a snapshot, not a guarantee.', cs);

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

/* The graph toolbar owns the level and arrangement switches; the front page
   presses the same buttons, so the map ends up as a visitor would leave it. */
function pressGraphButton(sel){
  var b = $(sel);
  if(b && b.getAttribute('aria-pressed') !== 'true') b.click();
}
function openCluster(id){
  setView('graph');
  pressGraphButton('#glvl-c');
  pressGraphButton('#garrange [data-groupby="domain"]');
  /* on a first visit the map is laid out on the next frame; do it now so the
     hubs exist (the queued frame relays out harmlessly, without closing this) */
  if(!GRAPH.laidOut) graphResize();
  var hub = (GRAPH.hubs||[]).find(function(h){ return h.bucketId === id; });
  if(hub) lineageOpen(hub);
}
/* A drawing of three clusters, hand-placed: the hero's picture of the map.
   Colours are the dark theme's, as the panel is dark in both themes. */
function heroMapSVG(){
  return '<svg class="fmap-svg" viewBox="0 0 600 440" aria-hidden="true">'+
    '<g fill="#4A5058" opacity=".55">'+
      '<circle cx="40" cy="60" r="2"/><circle cx="70" cy="300" r="2"/><circle cx="120" cy="400" r="2"/><circle cx="560" cy="40" r="2"/><circle cx="580" cy="250" r="2"/><circle cx="530" cy="380" r="2"/><circle cx="330" cy="40" r="2"/><circle cx="260" cy="30" r="2"/><circle cx="40" cy="220" r="2"/><circle cx="590" cy="170" r="2"/><circle cx="480" cy="420" r="2"/><circle cx="200" cy="420" r="2"/><circle cx="560" cy="330" r="2"/><circle cx="30" cy="380" r="2"/>'+
    '</g>'+
    '<g stroke="#33383F" stroke-width="1.5" fill="none">'+
      '<path d="M160 150L440 130"/><path d="M160 150L300 330"/><path d="M440 130L300 330"/>'+
    '</g>'+
    '<g stroke="#4A5058" stroke-width="1" fill="none" opacity=".8">'+
      '<path d="M232 150L382 172"/><path d="M182 218L242 288"/><path d="M462 198L322 262"/><path d="M372 330L489 165"/>'+
    '</g>'+
    '<g stroke="#33383F" stroke-width="1" fill="none">'+
      '<path d="M160 150L232 150M160 150L209 185M160 150L182 218M160 150L141 207M160 150L102 192M160 150L100 150M160 150L102 108M160 150L141 93M160 150L182 82M160 150L209 115"/>'+
      '<path d="M440 130L512 130M440 130L489 165M440 130L462 198M440 130L421 187M440 130L382 172M440 130L380 130M440 130L382 88M440 130L421 73M440 130L462 62M440 130L489 95"/>'+
      '<path d="M300 330L372 330M300 330L349 365M300 330L322 398M300 330L281 387M300 330L242 372M300 330L240 330M300 330L242 288M300 330L281 273M300 330L322 262M300 330L349 295"/>'+
    '</g>'+
    '<g>'+
      '<circle cx="232" cy="150" r="5" fill="#E8AF52"/><circle cx="209" cy="185" r="4" fill="#E8AF52"/><circle cx="182" cy="218" r="5" fill="#E8AF52"/><circle cx="141" cy="207" r="4" fill="#E8AF52"/><circle cx="102" cy="192" r="5" fill="#E8AF52"/><circle cx="100" cy="150" r="4" fill="#E8AF52"/><circle cx="102" cy="108" r="5" fill="#5CC98F"/><circle cx="141" cy="93" r="4" fill="#E8AF52"/><circle cx="182" cy="82" r="5" fill="#E8AF52"/><circle cx="209" cy="115" r="4" fill="#5CC98F"/>'+
      '<circle cx="512" cy="130" r="5" fill="#CCA2E6"/><circle cx="489" cy="165" r="4" fill="#CCA2E6"/><circle cx="462" cy="198" r="5" fill="#79BEF0"/><circle cx="421" cy="187" r="4" fill="#CCA2E6"/><circle cx="382" cy="172" r="5" fill="#79BEF0"/><circle cx="380" cy="130" r="4" fill="#CCA2E6"/><circle cx="382" cy="88" r="5" fill="#CCA2E6"/><circle cx="421" cy="73" r="4" fill="#79BEF0"/><circle cx="462" cy="62" r="5" fill="#CCA2E6"/><circle cx="489" cy="95" r="4" fill="#CCA2E6"/>'+
      '<circle cx="372" cy="330" r="5" fill="#5CC98F"/><circle cx="349" cy="365" r="4" fill="#79BEF0"/><circle cx="322" cy="398" r="5" fill="#CCA2E6"/><circle cx="281" cy="387" r="4" fill="#79BEF0"/><circle cx="242" cy="372" r="5" fill="#5CC98F"/><circle cx="240" cy="330" r="4" fill="#5CC98F"/><circle cx="242" cy="288" r="5" fill="#CCA2E6"/><circle cx="281" cy="273" r="4" fill="#79BEF0"/><circle cx="322" cy="262" r="5" fill="#CCA2E6"/><circle cx="349" cy="295" r="4" fill="#5CC98F"/>'+
    '</g>'+
    '<g fill="#E4E7EB" font-family="\'Public Sans\', Helvetica, Arial, sans-serif" font-size="11" font-weight="600">'+
      '<text x="243" y="154">Amiga 1200</text>'+
      '<text x="499" y="91">MiSTer FPGA</text>'+
      '<text x="230" y="334" text-anchor="end">WinUAE</text>'+
    '</g>'+
    '<g>'+
      '<circle cx="160" cy="150" r="36" fill="#1E2126" stroke="#E8AF52" stroke-width="2.5"/>'+
      '<circle cx="160" cy="150" r="31" fill="none" stroke="#33383F" stroke-width="1"/>'+
      '<text x="160" y="146" text-anchor="middle" fill="#FFFFFF" font-family="\'Instrument Serif\', Georgia, serif" font-size="12.5">Classic</text>'+
      '<text x="160" y="159" text-anchor="middle" fill="#FFFFFF" font-family="\'Instrument Serif\', Georgia, serif" font-size="12.5">machines</text>'+
      '<text x="160" y="170" text-anchor="middle" fill="#B9BFC7" font-family="\'JetBrains Mono\', Menlo, monospace" font-size="7.5" letter-spacing=".08em">21 ENTRIES</text>'+
      '<circle cx="440" cy="130" r="36" fill="#1E2126" stroke="#CCA2E6" stroke-width="2.5"/>'+
      '<circle cx="440" cy="130" r="31" fill="none" stroke="#33383F" stroke-width="1"/>'+
      '<text x="440" y="126" text-anchor="middle" fill="#FFFFFF" font-family="\'Instrument Serif\', Georgia, serif" font-size="12.5">FPGA &amp;</text>'+
      '<text x="440" y="139" text-anchor="middle" fill="#FFFFFF" font-family="\'Instrument Serif\', Georgia, serif" font-size="12.5">replicas</text>'+
      '<text x="440" y="150" text-anchor="middle" fill="#B9BFC7" font-family="\'JetBrains Mono\', Menlo, monospace" font-size="7.5" letter-spacing=".08em">26 ENTRIES</text>'+
      '<circle cx="300" cy="330" r="36" fill="#1E2126" stroke="#5CC98F" stroke-width="2.5"/>'+
      '<circle cx="300" cy="330" r="31" fill="none" stroke="#33383F" stroke-width="1"/>'+
      '<text x="300" y="333" text-anchor="middle" fill="#FFFFFF" font-family="\'Instrument Serif\', Georgia, serif" font-size="12.5">Emulators</text>'+
      '<text x="300" y="345" text-anchor="middle" fill="#B9BFC7" font-family="\'JetBrains Mono\', Menlo, monospace" font-size="7.5" letter-spacing=".08em">18 ENTRIES</text>'+
    '</g>'+
  '</svg>';
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
