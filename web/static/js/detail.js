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
  var parts = Array.prototype.slice.call($('#detail-content').querySelectorAll('.detail-media, .dhead, .dlineage'))
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

/* Without a photograph the plate column still stands: the entry's pictogram
   on its era tint, numbered like a catalogue card, with room to add one. */
function detailPlate(e){
  var plate = el('div','detail-media detail-plate');
  plate.style.setProperty('--era', eraColor(e.era)); plate.style.setProperty('--era-t', eraTint(e.era));
  plate.appendChild(el('span','plate-n', plateNo(e)));
  var pict = el('span','plate-pict'); pict.innerHTML = pictSvg(e); plate.appendChild(pict);
  var cap = el('span','detail-plate-cap','No image yet. ');
  var ask = el('a', null, 'Suggest one');
  ask.href = '/suggestions/new?type=image&entry=' + encodeURIComponent(e.id);
  cap.appendChild(ask); plate.appendChild(cap);
  return plate;
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
      cancelDetailMotion(); media.replaceWith(detailPlate(e)); hero.classList.add('detail-no-image');
      if(DETAIL.closing) finishDetailClose();
    });
    image.src = e.img; media.appendChild(image); hero.appendChild(media);
  }
  $('#detail-kicker').textContent = 'Plate ' + plateNo(e) + ' · ' + CAT_LABEL[e.cat];
  if(!e.img) hero.appendChild(detailPlate(e));
  var h = el('div','dhead');
  var ht = el('div','dhead-t');
  var title = el('h2', null, e.n); title.id = 'detail-title'; title.tabIndex = -1;
  ht.appendChild(title);
  if(e.by) ht.appendChild(el('div','by', e.by));
  h.appendChild(ht);
  var meta = el('div','dmeta');
  meta.appendChild(statusEl(e));
  (e.arch||[]).forEach(function(a){ meta.appendChild(el('span','ac', ARCH_LABEL[a]||a)); });
  h.appendChild(meta);
  h.appendChild(el('p','detail-intro',e.d));

  /* links sit with the title: the official page is the one red control in the
     dialog; the generated searches stand beside it as plain links */
  var lr = el('div','dlinks');
  function extlink(label, href, primary){
    var a = el('a', primary ? 'extlink primary' : 'dlink', label);
    a.href = href; a.target = '_blank'; a.rel = 'noopener noreferrer';
    lr.appendChild(a);
  }
  if(e.u) extlink('Official site ↗', e.u, true);
  else lr.appendChild(el('span','dlink-none','No verified official page'));
  var q = encodeURIComponent(e.n + ' Amiga');
  extlink('Wikipedia', 'https://en.wikipedia.org/w/index.php?search=' + q);
  extlink('Web search', 'https://www.google.com/search?q=' + q);
  if(['soft','dev','shell','tech','distro','os'].includes(e.cat))
    extlink('Aminet', 'https://aminet.net/search?query=' + encodeURIComponent(e.n));
  h.appendChild(lr);
  /* extension point: actions.js adds the "Suggest …" links under the links */
  if(window.AMIGA_DETAIL_ACTIONS) window.AMIGA_DETAIL_ACTIONS(e, h);
  hero.appendChild(h); sc.appendChild(hero);

  /* lineage strip: time runs left to right through a spine that stands for
     this entry, so every arrow points the way the lineage flows */
  var rel = (e.rel||[]).map(function(id){return BY_ID[id];}).filter(Boolean);
  var kids = (e.children||[]).map(function(id){return BY_ID[id];}).filter(Boolean);
  if(rel.length || kids.length){
    var lineage = el('section','dlineage'); lineage.setAttribute('aria-label','Lineage');
    function relCol(title, items, side){
      var col = el('div','dlin-col ' + side);
      col.appendChild(el('span','eyebrow', title));
      if(!items.length){
        col.appendChild(el('div','dlin-none', side === 'from' ? 'No recorded ancestor' : 'No recorded descendant'));
        return col;
      }
      var list = el('div','rellist');
      items.sort(function(a,b){ return a.y - b.y || a.n.localeCompare(b.n); }).forEach(function(t){
        var rb = el('button','relbtn'); rb.type = 'button';
        rb.style.setProperty('--era', eraColor(t.era));
        rb.setAttribute('aria-label', title + ' ' + t.n + ', ' + t.y);
        rb.innerHTML = ICON.arrow;
        rb.appendChild(el('span','bar'));
        rb.appendChild(el('span','n', t.n));
        rb.appendChild(el('span','yr', String(t.y)));
        rb.addEventListener('click', function(){ openDetail(t.id); });
        list.appendChild(rb);
      });
      col.appendChild(list);
      return col;
    }
    lineage.appendChild(relCol('Descends from', rel, 'from'));
    var spine = el('div','dlin-spine'); spine.setAttribute('aria-hidden','true');
    spine.appendChild(el('span','dlin-year', String(e.y)));
    spine.appendChild(el('span','dlin-node'));
    lineage.appendChild(spine);
    lineage.appendChild(relCol('Led to', kids, 'to'));
    sc.appendChild(lineage);
  }

  var b = el('div','dbody');

  if(e.unv){
    var w = el('div','warnbox');
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

  /* status and architecture already stand as chips under the title */
  var s3 = el('div','dsec dsec-facts');
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
  fact('Detail', e.ver);
  s3.appendChild(facts);
  b.appendChild(s3);

  if((e.tags && e.tags.length) || e.imgCredit){
    var foot = el('div','dfoot');
    if(e.tags && e.tags.length){
      var tr = el('div','tagrow');
      e.tags.forEach(function(t){ tr.appendChild(el('span','tag', t)); });
      foot.appendChild(tr);
    }
    if(e.imgCredit) foot.appendChild(el('div','credit', e.imgCredit));
    b.appendChild(foot);
  }

  sc.appendChild(b);
  /* extension point: actions.js counts the view */
  if(window.AMIGA_DETAIL_VIEW) window.AMIGA_DETAIL_VIEW(e);
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
