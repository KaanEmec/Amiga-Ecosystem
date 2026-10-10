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
        '<span class="eyebrow">Object details</span>'+
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
     A deliberate front-page cross-section: classic hardware, modern
     accelerators, graphics, interfaces, distributions, emulation and
     preservation. Keep the order editorial rather than data-driven. */
  r.appendChild(sectionHead('01','Selected objects',
    'Twelve of the three hundred and thirty-three. Any plate opens its full entry.'));
  var picked = [
    'a1200', 'v4sa', 'pistorm32', 'picasso', 'workbench',
    'amithlon', 'pimiga', 'uae', 'whdload', 'amos', 'amigaforever', 'a6000'
  ].map(function(id){ return BY_ID[id]; }).filter(Boolean);
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
    b.addEventListener('click', function(){ openDetail(e.id, b); });
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
