/* ==========================================================================
   The Amiga Constellation — lineage.js
   lineage map: domains, zones, layout, drawing, tours, the
   lineage board and graphInit
   Load order (classic scripts sharing one global scope, no modules):
   data.js -> core.js -> catalog.js -> detail.js -> lineage.js -> app.js
   ========================================================================== */
'use strict';

/* ================================================================ graph
   Five perspectives share the same clusters and detail interactions.
   Search and facets spotlight matches without changing geography;
   arrangement and Essentials changes explicitly rebuild the map. */

var DOMAINS = RAW.domains || [];
var DOMAIN_OF = {};
DOMAINS.forEach(function(d){ d.cats.forEach(function(c){ DOMAIN_OF[c] = d; }); });

var TOURS = RAW.tours || [];

var GROUPBYS = [
  {id:'domain', label:'Domain'},
  {id:'era',    label:'Era'},
  {id:'arch',   label:'Architecture'},
  {id:'st',     label:'Status'},
  {id:'kind',   label:'Kind'}
];

/* Regions group cluster identities, never individual members. In particular,
   multi-platform entries keep their existing primary-architecture assignment. */
var ZONES = {
  domain:[
    {id:'z-hw',label:'Hardware',buckets:['d-classic','d-ng','d-fpga','d-accel','d-exp']},
    {id:'z-sys',label:'System software',buckets:['d-os','d-shell','d-distro','d-tech']},
    {id:'z-scene',label:'Practice & stewardship',buckets:['d-emu','d-make','d-scene']}
  ],
  arch:[
    {id:'z-native',label:'68k & PowerPC',buckets:['68k','ppc']},
    {id:'z-fpga',label:'FPGA',buckets:['fpga']},
    {id:'z-host',label:'Host & other platforms',buckets:['x86','arm','riscv','web','other','other-bucket']}
  ],
  st:[
    {id:'z-current',label:'Current',buckets:['shipping','active','limited']},
    {id:'z-unreleased',label:'Unreleased',buckets:['planned','prototype']},
    {id:'z-historic',label:'Historic',buckets:['historic','other-bucket']}
  ],
  kind:[
    {id:'z-machines',label:'Machines & components',buckets:['original','replica','fpga','appliance','compatible','component','prototype']},
    {id:'z-software',label:'Software & environments',buckets:['os','amigalike','oslike','emulator','shell','toolkit','launcher','tool','app']},
    {id:'z-people',label:'People, preservation & ideas',buckets:['archive','org','media','event','concept','other','other-bucket']}
  ]
};
function zonesFor(mode){
  if(mode === 'era') return M.eras.map(function(e){
    return {id:'z-'+e.id,label:e.label,subtitle:e.range,buckets:[e.id]};
  });
  return ZONES[mode] || ZONES.domain;
}

var GRAPH = {
  ready:false, nodes:[], byId:{}, hubs:[], links:[],
  cv:null, cx:null, tx:0, ty:0, k:1, hover:null, hoverHub:null,
  W:0, H:0, dpr:1, raf:0, laidOut:false, anim:null,
  groupBy:'domain', present:{}, level:'clusters', zones:[], fitAxis:'both', kHint:1,
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

function clustersFor(mode){
  var pool = essFilter(GRAPH.nodes);
  function bucket(defs, keyFn, otherLabel){
    var map = {}, order = [];
    defs.forEach(function(d){ map[d.id] = {id:d.id, label:d.label, members:[]}; order.push(d.id); });
    var other = {id:'other-bucket', label:otherLabel || 'Other', members:[]};
    pool.forEach(function(n){
      var k = keyFn(n);
      if(map[k]) map[k].members.push(n); else other.members.push(n);
    });
    var out = order.map(function(id){ return map[id]; });
    if(other.members.length) out.push(other);
    return out.filter(function(c){ return c.members.length > 0; });
  }
  if(mode === 'era')
    return bucket(M.eras.map(function(e){ return {id:e.id, label:e.label+' '+e.range}; }),
                  function(n){ return n.e.era; });
  if(mode === 'arch')
    return bucket(M.archs.map(function(a){ return {id:a.id, label:a.label}; }),
                  function(n){ return (n.e.arch||[])[0] || 'other'; }, 'Other platforms');
  if(mode === 'st')
    return bucket(M.statuses.map(function(s){ return {id:s.id, label:s.label}; }),
                  function(n){ return n.e.st; });
  if(mode === 'kind')
    return bucket(M.kinds.map(function(k){ return {id:k.id, label:k.label}; }),
                  function(n){ return n.e.kind; }, 'Other kinds');
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
  var zones = zonesFor(GRAPH.groupBy), buckets = {};
  zones.forEach(function(z){ buckets[z.id] = []; });
  var loose = [];
  hubs.forEach(function(h){
    var z = zones.find(function(z){return z.buckets.indexOf(h.bucketId) !== -1;});
    if(z) buckets[z.id].push(h); else loose.push(h);
  });
  if(loose.length){
    var fallback = {id:'z-other',label:'Other',buckets:[]};
    zones = zones.concat([fallback]); buckets[fallback.id] = loose;
  }
  var live = zones.filter(function(z){ return buckets[z.id].length; });
  var GAP = 30, PADX = 26, PADY = 34;
  if(!live.length){GRAPH.zones=[]; return;}

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
      outV.push({id:z.id, label:z.label, subtitle:z.subtitle, x:PADX, y:zy, w:zw, h:zh});
      zy += zh + GAP;
    });
    GRAPH.zones = outV;
    GRAPH.fitAxis = 'width';
    return;
  }
  /* Dense Kind panels may extend below the viewport instead of shrinking
     labels into one another. The map remains pannable at a width-fit scale. */
  var minStep = 68 + labelReserve({label:'Two line label here'}) + 22;
  var rowCount = Math.max.apply(null,live.map(function(z){
    var n = buckets[z.id].length; return n > 3 ? Math.ceil(n/2) : n;
  }));
  if(rowCount > 3) H = Math.max(H, rowCount*minStep + 2*PADY + 40);
  GRAPH.fitAxis = H > GRAPH.H + 1 ? 'width' : 'both';
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
    out.push({id:z.id, label:z.label, subtitle:z.subtitle, x:x, y:PADY, w:zw, h:H - 2*PADY});
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
  GRAPH.present = {}; GRAPH.hover = null; GRAPH.hoverHub = null;
  GRAPH.zones = []; GRAPH.fitAxis = 'both';

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
      return {uid:'h-'+c.id, bucketId:c.id, open:false, medallion:true, d:{label:c.label},
              members:members, rings:[], R:hr + 54, hubR:hr};
    }
    var rings = ringPlan(members.length);
    return {uid:'h-'+c.id, bucketId:c.id, open:true, d:{label:c.label}, members:members, rings:rings,
            R:(rings.length?rings[rings.length-1].r:17)+12, hubR:23};
  });
  var maxHubR = 0, maxR = 0;
  GRAPH.hubs.forEach(function(h){
    maxHubR = Math.max(maxHubR, h.hubR); maxR = Math.max(maxR, h.R);
  });
  if(medal){
    placeInZones(GRAPH.hubs, W, H, maxHubR);
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
  GRAPH.center = {x:cx0, y:cy0, r:34};
  GRAPH.laidOut = true;
}

function graphFit(animate, measureOnly){
  if(!GRAPH.laidOut) return;
  var x0=1e9,y0=1e9,x1=-1e9,y1=-1e9;
  var boxes = GRAPH.hubs;
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
  if(measureOnly) return {tx:tx,ty:ty,k:k};
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
function focusPoint(x, y, k){
  animateView(GRAPH.W/2 - x*k, GRAPH.H/2 - y*k, k);
}
function stopGraphMotion(){
  if(GRAPH.anim){cancelAnimationFrame(GRAPH.anim); GRAPH.anim=null;}
}
function clampClusterPan(){
  if(GRAPH.level !== 'clusters' || !GRAPH.zones.length) return;
  function clamp(value,lo,hi,size,startPad,endPad){
    if((hi-lo)*GRAPH.k <= size-startPad-endPad) return (size-(lo+hi)*GRAPH.k)/2;
    return Math.max(size-endPad-hi*GRAPH.k,Math.min(startPad-lo*GRAPH.k,value));
  }
  var zones = GRAPH.zones;
  GRAPH.tx = clamp(GRAPH.tx,Math.min.apply(null,zones.map(function(z){return z.x;})),
    Math.max.apply(null,zones.map(function(z){return z.x+z.w;})),GRAPH.W,12,12);
  GRAPH.ty = clamp(GRAPH.ty,Math.min.apply(null,zones.map(function(z){return z.y;})),
    Math.max.apply(null,zones.map(function(z){return z.y+z.h;})),GRAPH.H,12,64);
}

function graphResize(preserveCamera){
  var cv = GRAPH.cv; if(!cv) return;
  var oldWidth = GRAPH.W, oldHeight = GRAPH.H;
  var r = cv.parentNode.getBoundingClientRect();
  GRAPH.dpr = Math.min(window.devicePixelRatio || 1, 2);
  GRAPH.W = r.width; GRAPH.H = r.height;
  cv.width = Math.round(r.width * GRAPH.dpr);
  cv.height = Math.round(r.height * GRAPH.dpr);
  if(preserveCamera === true && oldWidth && oldHeight){
    GRAPH.tx += (GRAPH.W-oldWidth)/2; GRAPH.ty += (GRAPH.H-oldHeight)/2;
    GRAPH.draw();
  } else relayout(false);
}

function lineageSync(){ if(LF.open) lineageClose(false); }
/* Lay out, fit, and if the fit had to zoom out far enough that screen-sized
   labels no longer clear in world units, lay out once more with that zoom as
   the hint. Two passes are enough to converge in every grouping. */
function relayout(animate){
  GRAPH.kHint = 1;
  graphLayout();
  var target = graphFit(false,true);
  if(target && target.k < 0.92){
    GRAPH.kHint = Math.max(0.34,target.k);
    graphLayout();
  }
  graphFit(animate);
}
function graphRemap(){
  GRAPH.hover = null; GRAPH.hoverHub = null;
  $('#gtip').setAttribute('data-on','0');
  lineageSync();
  if(!GRAPH.W) return;
  relayout(true);
}

function cssv(name){
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || '#888';
}
/* canvas font stacks, read once per frame from the theme tokens */
var CF = {disp:'serif', mono:'monospace', sans:'sans-serif'};

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
  cx.font = (numPx/k) + 'px ' + CF.disp;
  cx.fillText(matchText || String(total), h.x, h.y + (numPx*0.17)/k);
  cx.fillStyle = ink3;
  cx.font = '500 ' + (subPx/k) + 'px ' + CF.mono;
  var sub = live ? live + ' LIVE' : 'ALL HISTORIC';
  if(cx.measureText(sub).width > (R*1.45)) sub = live ? live + ' LIVE' : '0 LIVE';
  cx.fillText(sub, h.x, h.y + (numPx*0.17 + subPx + 4)/k);

  /* label under the medallion, on the same reserve the layout allowed for */
  cx.fillStyle = hover ? accC : inkC;
  cx.font = (16/k) + 'px ' + CF.disp;
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
  cx.beginPath(); cx.arc(h.x, h.y, h.hubR, 0, 6.2832);
  cx.fillStyle = surfC; cx.fill();
  cx.lineWidth = 1.2/GRAPH.k;
  cx.strokeStyle = hover ? inkC : lineC;
  cx.stroke();
  cx.fillStyle = inkC; cx.textAlign = 'center';
  var label = h.label || h.d.label;
  var words = label.split(' ');
  var fs2 = h.hubR >= 26 ? 8.5 : (h.hubR <= 16 ? 6.6 : 7.8);
  cx.font = '700 ' + fs2 + 'px ' + CF.sans;
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
  cx.font = '600 ' + (h.hubR <= 16 ? 6 : 6.8) + 'px ' + CF.mono;
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

  var W = GRAPH.W, H = GRAPH.H, dpr = GRAPH.dpr;
  cx.setTransform(dpr,0,0,dpr,0,0);
  cx.clearRect(0,0,W,H);

  var eraCols = M.eras.map(function(_,i){ return cssv('--e'+(i+1)); });
  var inkC = cssv('--ink'), ink3 = cssv('--ink-3'), ink4 = cssv('--ink-4');
  var accC = cssv('--acc'), surfC = cssv('--bg-deep'), lineC = cssv('--line');
  CF.disp = cssv('--f-canvas-disp'); CF.mono = cssv('--f-canvas-mono'); CF.sans = cssv('--f-canvas-sans');

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
      cx.font = '500 ' + (12/GRAPH.k) + 'px ' + CF.mono;
      var panelTitle = z.label.toUpperCase();
      var words = panelTitle.split(' '), lines = [], line = '';
      words.forEach(function(word){
        var next = line ? line+' '+word : word;
        if(line && cx.measureText(next).width > z.w - 28/GRAPH.k){lines.push(line); line=word;} else line=next;
      });
      if(line) lines.push(line);
      lines.forEach(function(t,i){cx.fillText(t,z.x+14/GRAPH.k,z.y+(22+i*16)/GRAPH.k);});
      if(z.subtitle) cx.fillText(z.subtitle,z.x+14/GRAPH.k,z.y+(22+lines.length*16)/GRAPH.k);
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
  if(!GRAPH.zones.length){
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
  if(!GRAPH.zones.length){
    cx.beginPath(); cx.arc(GRAPH.center.x, GRAPH.center.y, GRAPH.center.r, 0, 6.2832);
    cx.fillStyle = inkC; cx.fill();
    cx.fillStyle = surfC; cx.textAlign = 'center';
    cx.font = '18px ' + CF.disp;
    cx.fillText('Amiga', GRAPH.center.x, GRAPH.center.y + 3);
    cx.font = '500 7.5px ' + CF.mono;
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
                          : h.members.length + ' entries');
  });

  /* labels */
  var presentN = Object.keys(GRAPH.present).length;
  var few = essView || presentN <= 150;
  var fs = Math.max(9, Math.min(12.5, 11/Math.sqrt(GRAPH.k)));
  cx.font = '700 ' + fs + 'px ' + CF.sans;
  var placed = [];
  GRAPH.hubs.forEach(function(h){
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
    var clusterTotal = GRAPH.hubs.reduce(function(a,h){ return a + h.members.length; }, 0);
      if(GRAPH.level === 'clusters'){
        gc.textContent = anyFilter
          ? matchN2(GRAPH.hubs, match) + ' of ' + clusterTotal + ' match — click a cluster to read it'
          : clusterTotal + ' entries in ' + GRAPH.hubs.length +
            ' clusters — click one to open it' + (GRAPH.fitAxis === 'width' ? ' · scroll to explore' : '');
      } else {
        gc.textContent = anyFilter
          ? matchN + ' of ' + presentN + ' match — rest dimmed'
          : (essView ? presentN + ' essentials of ' + ALL.length
                     : presentN + ' entries in ' + GRAPH.hubs.length + ' clusters');
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
  lineageSync();
  /* a route walks single entries, so it needs the full constellation */
  if(GRAPH.level !== 'all'){
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
function lineageClose(restoreFocus){
  var wasOpen = !!LF.open;
  var pane = $('#pane-graph');
  if(pane) pane.setAttribute('data-lf','0');
  if(pane) pane.scrollTop = 0;
  LF.open = null;
  var host = $('#lfocus');
  if(host){ host.hidden = true; host.innerHTML = ''; }
  if(wasOpen && restoreFocus !== false){ var cv = $('#gcanvas'); if(cv) cv.focus(); }
}
function lineageRender(){
  var hub = LF.open, host = $('#lfocus');
  if(!hub){ lineageClose(); return; }
  var label = hub.label || (hub.d && hub.d.label) || 'Cluster';
  var members = (hub.members || []).map(function(n){ return n.e || n; });
  host.hidden = false;
  host.innerHTML = '';
  var pane = $('#pane-graph');
  if(pane) pane.setAttribute('data-lf','1');

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
  var grpLabel = GRAPH.groupBy === 'domain' && members.length ? (CAT_GROUP[members[0].cat] || '') :
    (GROUPBYS.find(function(g){return g.id === GRAPH.groupBy;}) || {}).label;
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
        openDetail(e.id, c);
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
      : '<span>Open an entry to explore its history and related objects.</span>');
  board.appendChild(legend);

  host.appendChild(board);
  LF.redraw = function(){ lineageThreads(inner, svg, cardOf, chain, members); };
  LF.cards = cardOf;
  requestAnimationFrame(function(){
    LF.redraw();
    if(LF.sel) lineageScrollTo(LF.sel);
  });
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

  var gb = $('#garrange');
  function setGrouping(mode){
    if(GRAPH.groupBy === mode) return;
    GRAPH.groupBy = mode;
    gb.querySelectorAll('button').forEach(function(b){
      b.setAttribute('aria-pressed',b.getAttribute('data-groupby') === mode ? 'true' : 'false');
    });
    if(GRAPH.tour) tourEnd();
    graphRemap();
  }
  GROUPBYS.forEach(function(g){
    var b = el('button',null,g.label); b.type='button';
    b.setAttribute('data-groupby',g.id);
    b.setAttribute('aria-pressed',g.id === GRAPH.groupBy ? 'true' : 'false');
    b.addEventListener('click',function(){setGrouping(g.id);});
    b.addEventListener('keydown',function(ev){
      var step = {ArrowRight:1,ArrowLeft:-1};
      if(!(ev.key in step) && ev.key !== 'Home' && ev.key !== 'End') return;
      ev.preventDefault();
      var i = GROUPBYS.findIndex(function(x){return x.id === g.id;});
      i = ev.key === 'Home' ? 0 : ev.key === 'End' ? GROUPBYS.length-1 : (i+step[ev.key]+GROUPBYS.length)%GROUPBYS.length;
      var next = gb.querySelector('[data-groupby="'+GROUPBYS[i].id+'"]');
      next.focus({preventScroll:true}); next.scrollIntoView({block:'nearest',inline:'nearest'});
      setGrouping(GROUPBYS[i].id);
    });
    gb.appendChild(b);
  });

  function syncLevel(){
    $('#glvl-c').setAttribute('aria-pressed', GRAPH.level === 'clusters' ? 'true' : 'false');
    $('#glvl-a').setAttribute('aria-pressed', GRAPH.level === 'all' ? 'true' : 'false');
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
  $('#tourinfo').addEventListener('click', function(){ if(S.sel) openDetail(S.sel, this); });

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
  cv.setAttribute('title','Scroll or drag to explore clusters. Use the zoom buttons or Ctrl+scroll to zoom.');
  var kbIdx = -1;
  cv.addEventListener('keydown', function(ev){
    if(GRAPH.tour) return; // route arrows belong to the route, not cluster focus
    var list = GRAPH.hubs;
    if(!list.length) return;
    var nav = {ArrowRight:1, ArrowDown:1, ArrowLeft:-1, ArrowUp:-1, Tab:0};
    if(ev.key in nav && ev.key !== 'Tab'){
      ev.preventDefault();
      stopGraphMotion();
      kbIdx = list.indexOf(GRAPH.hoverHub);
      kbIdx = (kbIdx + nav[ev.key] + list.length) % list.length;
      GRAPH.hoverHub = list[kbIdx]; GRAPH.hover = null;
      var h = GRAPH.hoverHub;
      /* Keep keyboard-selected clusters in view when panels extend vertically. */
      var screenY = h.y*GRAPH.k + GRAPH.ty, margin = h.hubR*GRAPH.k + labelReserve(h)*GRAPH.k + 20;
      if(screenY < margin) GRAPH.ty += margin-screenY;
      else if(screenY > GRAPH.H-margin) GRAPH.ty -= screenY-(GRAPH.H-margin);
      clampClusterPan();
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
      lineageOpen(GRAPH.hoverHub);
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
    return null;
  }

  cv.addEventListener('mousedown', function(ev){
    stopGraphMotion();
    dragging = true; moved = 0; lastX = ev.clientX; lastY = ev.clientY; cv.classList.add('drag');
  });
  window.addEventListener('mouseup', function(){ dragging = false; cv.classList.remove('drag'); });
  cv.addEventListener('mousemove', function(ev){
    var r = cv.getBoundingClientRect();
    var px = ev.clientX - r.left, py = ev.clientY - r.top;
    if(dragging){
      GRAPH.tx += ev.clientX - lastX; GRAPH.ty += ev.clientY - lastY;
      clampClusterPan();
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
        var act = 'click to read this cluster';
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
      openDetail(hit.n.id, cv, {x:ev.clientX, y:ev.clientY});
    } else if(hit && hit.type === 'hub'){
      lineageOpen(hit.h);
    } else {
      closeDetail();
    }
  });
  cv.addEventListener('wheel', function(ev){
    ev.preventDefault();
    stopGraphMotion();
    GRAPH.hover = null; GRAPH.hoverHub = null; $('#gtip').setAttribute('data-on','0');
    if(GRAPH.level === 'clusters' && !ev.ctrlKey && !ev.metaKey){
      var unit = ev.deltaMode === 1 ? 16 : ev.deltaMode === 2 ? GRAPH.H : 1;
      GRAPH.tx -= ev.deltaX*unit; GRAPH.ty -= ev.deltaY*unit;
      clampClusterPan();
      GRAPH.draw(); return;
    }
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
    stopGraphMotion();
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
      clampClusterPan();
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
