/* ==========================================================================
   The Amiga Constellation — app.js
   views, render, theme, search and keyboard wiring, boot
   Load order (classic scripts sharing one global scope, no modules):
   data.js -> core.js -> catalog.js -> detail.js -> lineage.js -> app.js
   ========================================================================== */
'use strict';

/* ---------------------------------------------------------------- views */
function setView(v){
  if(detailOpen()) return;
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

  /* Screen Mode (theme.js already applied the stored theme in <head>);
     the canvas paints with resolved colours and fonts, so it redraws */
  function redrawGraph(){ if(GRAPH.ready) GRAPH.draw(); }
  AmigaTheme.mount($('#themebtn'), {onChange: function(){
    redrawGraph();
    if(document.fonts && document.fonts.ready) document.fonts.ready.then(redrawGraph);
  }});

  /* search */
  var si = $('#search'), t = 0;
  si.addEventListener('input', function(){
    clearTimeout(t);
    t = setTimeout(function(){
      if(detailOpen()) return; // do not let a pending query rebuild the inert page
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
    openDetail(c.getAttribute('data-id'), c);
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

  $('#detail-close').addEventListener('click',closeDetail);
  $('#detail-back').addEventListener('click',detailBack);
  var dialog = $('#detail-dialog'), backdropDown = false;
  dialog.addEventListener('cancel',function(ev){ev.preventDefault(); closeDetail();});
  dialog.addEventListener('keydown',function(ev){
    if(ev.key !== 'Tab') return;
    var stops = Array.from(dialog.querySelectorAll('.detail-panel button:not([disabled]), .detail-panel a[href], .detail-panel [tabindex="0"]'))
      .filter(function(n){return n.getClientRects().length;});
    var first = stops[0], last = stops[stops.length-1], active = document.activeElement;
    if(!first){ev.preventDefault(); return;}
    if(ev.shiftKey && (active === first || stops.indexOf(active) === -1)){
      ev.preventDefault(); last.focus();
    }else if(!ev.shiftKey && active === last){
      ev.preventDefault(); first.focus();
    }
  });
  dialog.addEventListener('pointerdown',function(ev){backdropDown = ev.target === dialog;});
  dialog.addEventListener('pointercancel',function(){backdropDown = false;});
  dialog.addEventListener('click',function(ev){
    if(backdropDown && ev.target === dialog) closeDetail();
    backdropDown = false;
  });

  document.addEventListener('keydown', function(ev){
    if(detailOpen()) return; // modal owns keyboard handling while reading
    if(ev.key === 'Escape'){
      if(LF.open) lineageClose();
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
    var preserveCamera = detailOpen();
    if(detailOpen()){
      cancelDetailMotion(); DETAIL.resizePending = true;
      if(DETAIL.closing) finishDetailClose();
    }
    rt = setTimeout(function(){
      if(detailOpen()) { DETAIL.resizePending = true; return; }
      if(S.view === 'graph') graphResize(preserveCamera);
      if(LF.open && LF.redraw) LF.redraw();
    }, 120);
  });
  window.matchMedia('(prefers-reduced-motion: reduce)').addEventListener('change',function(){
    cancelDetailMotion(); if(DETAIL.closing) finishDetailClose();
  });

  setView('overview');
  console.log('[amiga] ' + ALL.length + ' entries, ' +
    ALL.reduce(function(a,e){ return a + (e.rel||[]).length; }, 0) + ' lineage links.');
  /* Phase 2 extension point: later scripts can wait for the UI to be built. */
  document.dispatchEvent(new CustomEvent('amiga:ready'));
}

if(document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot);
else boot();
