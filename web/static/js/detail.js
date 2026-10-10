/* ==========================================================================
   The Amiga Constellation — detail.js
   the object detail dialog
   Load order (classic scripts sharing one global scope, no modules):
   data.js -> core.js -> catalog.js -> detail.js -> lineage.js -> app.js
   ========================================================================== */
'use strict';

/* --------------------------------------------------------- object overlay
   Reading details owns no map state. The background DOM, selection, camera,
   and scroll containers are left in place for the entire modal session. */
var DETAIL = {id:null, source:null, at:null, opener:null, history:[],
  closing:false, motion:0, animations:[], clones:[], resizePending:false, imageSizes:{}};

/* Dialog motion in ms. Every open and close "blooms" from the opener. */
var MOTION = {navigate:150, bloomOpen:600, bloomClose:350};

function detailOpen(){ return $('#detail-dialog').open; }
function reducedMotion(){ return window.matchMedia('(prefers-reduced-motion: reduce)').matches; }

function cancelDetailMotion(){
  DETAIL.motion++;
  DETAIL.animations.forEach(function(a){a.cancel();});
  DETAIL.clones.forEach(function(c){c.remove();});
  DETAIL.animations = []; DETAIL.clones = [];
  var panel = document.querySelector('#detail-dialog .detail-panel');
  if(panel) panel.style.transformOrigin = '';
}

function detailAnimate(node, frames, duration, delay){
  var a = node.animate(frames, {duration:duration, delay:delay || 0, easing:'cubic-bezier(.2,.75,.2,1)', fill:'both'});
  DETAIL.animations.push(a);
}

function detailContentCopy(){
  if(reducedMotion()) return null;
  var sc = $('#detail-content'), r = sc.getBoundingClientRect();
  var copy = el('div','detail-outgoing'), content = sc.cloneNode(true);
  content.removeAttribute('id');
  content.querySelectorAll('[id]').forEach(function(n){n.removeAttribute('id');});
  Object.assign(copy.style,{position:'fixed',left:r.left+'px',top:r.top+'px',width:r.width+'px',height:r.height+'px',overflow:'hidden'});
  Object.assign(content.style,{overflow:'visible',scrollbarGutter:'auto',height:'auto',width:sc.clientWidth+'px',transform:'translateY(-'+sc.scrollTop+'px)'});
  copy.appendChild(content);
  return copy;
}

/* Viewport point the bloom grows from: the clicked star, else the centre of
   the source element, else the centre of the screen. */
function detailBloomPoint(){
  if(DETAIL.at) return DETAIL.at;
  var r = DETAIL.source && DETAIL.source.isConnected && DETAIL.source.getBoundingClientRect();
  if(r && r.width && r.height) return {x:r.left + r.width/2, y:r.top + r.height/2};
  return {x:innerWidth/2, y:innerHeight/2};
}

function detailBloom(panel, closing){
  var pt = detailBloomPoint(), r = panel.getBoundingClientRect();
  var at = (pt.x - r.left)+'px '+(pt.y - r.top)+'px';
  var shut = {clipPath:'circle(0px at '+at+')', transform:'scale(.6)', opacity:0, filter:'blur(6px)'};
  var open = {clipPath:'circle(150% at '+at+')', transform:'none', opacity:1, filter:'blur(0px)'};
  panel.style.transformOrigin = at;
  if(closing){ detailAnimate(panel, [open, shut], MOTION.bloomClose); return; }
  detailAnimate(panel, [shut, {opacity:1, offset:.4}, open], MOTION.bloomOpen);
  var ring = el('div','detail-bloom-ring');
  ring.style.left = pt.x+'px'; ring.style.top = pt.y+'px';
  $('#detail-dialog .detail-flight-layer').appendChild(ring); DETAIL.clones.push(ring);
  detailAnimate(ring, [{transform:'translate(-50%,-50%) scale(1)', opacity:.9},
    {transform:'translate(-50%,-50%) scale(6)', opacity:0}], 450);
  var parts = Array.prototype.slice.call($('#detail-content').querySelectorAll('.detail-media, .dhead'))
    .concat(Array.prototype.slice.call($('#detail-content .dbody').children, 0, 3));
  parts.forEach(function(n, i){
    var delay = 180 + i*60;
    detailAnimate(n, [{opacity:0, transform:'translateY(12px)'}, {opacity:1, transform:'none'}],
      Math.max(MOTION.bloomOpen - delay, 120), delay);
  });
}

function detailTransition(kind, done, outgoing){
  cancelDetailMotion();
  if(reducedMotion() || !$('#detail-dialog').animate){ if(done) done(); return; }
  var token = DETAIL.motion, closing = kind === 'close';
  if(kind === 'navigate'){
    if(outgoing){
      $('#detail-dialog .detail-flight-layer').appendChild(outgoing); DETAIL.clones.push(outgoing);
      detailAnimate(outgoing,[{opacity:1},{opacity:0}],MOTION.navigate);
    }
    detailAnimate($('#detail-content'), [{opacity:0},{opacity:1}],MOTION.navigate);
  }else detailBloom($('#detail-dialog .detail-panel'), closing);
  $('#detail-dialog').classList.toggle('detail-closing',closing);
  Promise.all(DETAIL.animations.map(function(a){return a.finished.catch(function(){});})).then(function(){
    if(token !== DETAIL.motion) return;
    cancelDetailMotion();
    if(done) done();
  });
}

/* The panel gets a frame to lay out before the bloom measures it. A later
   load cancels the pending motion instead of animating an obsolete panel. */
function scheduleDetailTransition(kind, outgoing){
  var token = DETAIL.motion;
  requestAnimationFrame(function(){
    if(token === DETAIL.motion && detailOpen() && !DETAIL.closing) detailTransition(kind,null,outgoing);
  });
}

function openDetail(id, source, at){
  if(!BY_ID[id]) return;
  var d = $('#detail-dialog'), fresh = !d.open || DETAIL.closing, previousOpener = DETAIL.opener, outgoing;
  cancelDetailMotion(); DETAIL.closing = false; d.classList.remove('detail-closing');
  if(fresh){
    DETAIL.history = [];
    DETAIL.source = source instanceof Element ? source : null;
    DETAIL.at = at && isFinite(at.x) && isFinite(at.y) ? {x:at.x, y:at.y} : null;
    DETAIL.opener = DETAIL.source || (d.open ? previousOpener : document.activeElement);
    /* A tour may still be easing into its stop when Details is clicked. */
    if(GRAPH.anim){ cancelAnimationFrame(GRAPH.anim); GRAPH.anim = null; }
  }else if(DETAIL.id !== id){
    outgoing = detailContentCopy();
    DETAIL.history.push({id:DETAIL.id, scroll:$('#detail-content').scrollTop});
  }else return;
  DETAIL.id = id;
  renderDetail(id);
  if(!d.open) d.showModal();
  $('#detail-content').scrollTop = 0;
  (fresh ? $('#detail-close') : $('#detail-title')).focus({preventScroll:true});
  scheduleDetailTransition(fresh ? 'open' : 'navigate',outgoing);
}

function detailBack(){
  if(!DETAIL.history.length || DETAIL.closing) return;
  cancelDetailMotion();
  var outgoing = detailContentCopy();
  var previous = DETAIL.history.pop(); DETAIL.id = previous.id;
  renderDetail(previous.id);
  $('#detail-title').focus({preventScroll:true});
  $('#detail-content').scrollTop = previous.scroll;
  scheduleDetailTransition('navigate',outgoing);
}

function renderDetail(id){
  var e = BY_ID[id], d = $('#detail-dialog'), sc = $('#detail-content');
  d.style.setProperty('--era', eraColor(e.era)); d.style.setProperty('--era-t', eraTint(e.era));
  sc.innerHTML = '';
  $('#detail-back').disabled = !DETAIL.history.length;
  $('#detail-back').setAttribute('aria-label',DETAIL.history.length ? 'Back to '+BY_ID[DETAIL.history[DETAIL.history.length-1].id].n : 'Back');
  var hero = el('div','detail-hero' + (e.img ? '' : ' detail-no-image'));
  if(e.img){
    var media = el('div','detail-media');
    var image = document.createElement('img'); image.className = 'detail-image';
    image.alt = e.n; image.decoding = 'async';
    var size = DETAIL.imageSizes[e.img];
    if(!size){
      var loaded = Array.from(document.querySelectorAll('#app .plate-img')).find(function(im){
        return im.getAttribute('src') === e.img && im.complete && im.naturalWidth;
      });
      if(loaded) size = DETAIL.imageSizes[e.img] = [loaded.naturalWidth, loaded.naturalHeight];
    }
    if(size){image.width=size[0]; image.height=size[1];}
    if(e.imgBg) media.style.backgroundColor = e.imgBg;
    image.addEventListener('load',function(){
      DETAIL.imageSizes[e.img] = [image.naturalWidth,image.naturalHeight];
      if(!size && DETAIL.id === id && DETAIL.animations.length && !DETAIL.closing) cancelDetailMotion();
    });
    image.addEventListener('error',function(){
      if(!image.isConnected) return;
      cancelDetailMotion(); media.remove(); hero.classList.add('detail-no-image');
      h.prepend(thumbEl(e,'lg'));
      if(DETAIL.closing) finishDetailClose();
    });
    image.src = e.img; media.appendChild(image); hero.appendChild(media);
  }
  var h = el('div','dhead');
  if(!e.img) h.appendChild(thumbEl(e,'lg'));
  var ht = el('div','dhead-t');
  ht.appendChild(el('div','eyebrow', 'Plate ' + plateNo(e) + ' · ' + CAT_LABEL[e.cat]));
  var title = el('h2', null, e.n); title.id = 'detail-title'; title.tabIndex = -1;
  ht.appendChild(title);
  if(e.by) ht.appendChild(el('div','by', e.by));
  /* extension point: actions.js adds the "Suggest …" buttons under the title */
  if(window.AMIGA_DETAIL_ACTIONS) window.AMIGA_DETAIL_ACTIONS(e, ht);
  h.appendChild(ht);
  var meta = el('div','dmeta');
  meta.appendChild(statusEl(e));
  (e.arch||[]).forEach(function(a){ meta.appendChild(el('span','ac', ARCH_LABEL[a]||a)); });
  h.appendChild(meta);
  var intro = el('p','detail-intro',e.d); h.appendChild(intro);
  hero.appendChild(h); sc.appendChild(hero);

  var b = el('div','dbody');

  if(e.unv){
    var w = el('div','warnbox dsec');
    w.innerHTML = '<b>Partly unverified.</b> Some details in this entry — typically current availability, '+
      'exact version or attribution — could not be confirmed against a primary source in August 2026.';
    b.appendChild(w);
  }

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
  extlink('Web search ↗', 'https://www.google.com/search?q=' + q);
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
}
function closeDetail(){
  if(!detailOpen() || DETAIL.closing) return;
  DETAIL.closing = true;
  detailTransition('close',finishDetailClose);
}
function finishDetailClose(){
  cancelDetailMotion();
  var d = $('#detail-dialog'); d.close(); d.classList.remove('detail-closing');
  var opener = DETAIL.opener;
  if(!opener || !opener.isConnected || !opener.getClientRects().length)
    opener = S.view === 'graph' && !LF.open ? $('#gcanvas') : $('#stage');
  opener.focus({preventScroll:true});
  DETAIL.id = null; DETAIL.source = null; DETAIL.at = null; DETAIL.opener = null;
  DETAIL.history = []; DETAIL.closing = false;
  $('#detail-content').innerHTML = '';
  if(DETAIL.resizePending){
    DETAIL.resizePending=false;
    if(S.view === 'graph') graphResize(true);
    if(LF.open && LF.redraw) LF.redraw();
  }
}
function markSelection(){
  Array.prototype.forEach.call(document.querySelectorAll('.card'), function(c){
    if(c.getAttribute('data-id') === S.sel) c.setAttribute('aria-current','true');
    else c.removeAttribute('aria-current');
  });
}
