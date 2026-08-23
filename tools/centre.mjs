import { chromium } from 'playwright';
const b = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium' });
const p = await b.newPage({ viewport:{width:1440,height:900}, colorScheme:'light' });
const errs=[]; p.on('pageerror', e=>errs.push('PAGEERROR '+e.message));
await p.goto('file://' + process.cwd() + '/Amiga-Constellation.html');
await p.waitForTimeout(1200);

// --- graph mode: a node on the far right must end up left of the sheet ---
await p.click('#v-graph'); await p.waitForTimeout1200?.() ; await p.waitForTimeout(1200);
await p.click('#glvl-a'); await p.waitForTimeout(1400);
const before = await p.evaluate(()=>{
  const G=window.__GRAPH;
  // pick the node furthest right on screen
  let best=null, bx=-1e9;
  Object.keys(G.present).forEach(id=>{ const n=G.byId[id]; const sx=n.x*G.k+G.tx; if(sx>bx){bx=sx;best=n;} });
  return {id:best.id, screenX: Math.round(bx)};
});
console.log('rightmost node', JSON.stringify(before));
await p.evaluate((id)=>{ window.__od(id); }, before.id);
await p.waitForTimeout(900);
console.log('after opening it:', await p.evaluate((id)=>{
  const G=window.__GRAPH, n=G.byId[id];
  const sx=n.x*G.k+G.tx, sy=n.y*G.k+G.ty;
  const d=document.querySelector('#drawer').getBoundingClientRect();
  const cv=document.querySelector('#gcanvas').getBoundingClientRect();
  return {nodeX: Math.round(cv.left+sx), sheetLeft: Math.round(d.left), clear: (cv.left+sx) < d.left - 20,
          nodeY: Math.round(cv.top+sy)};
}, before.id));
await p.screenshot({ path:'/tmp/c-graph.png' });

// --- catalogue: a plate in the right column must reflow into view ---
await p.click('#v-catalog'); await p.waitForTimeout(600);
await p.evaluate(()=>{ document.querySelectorAll('.cathead').forEach(c=>{ if(c.getAttribute('aria-expanded')!=='true') c.click(); }); });
await p.waitForTimeout(700);
const target = await p.evaluate(()=>{
  const cs=[...document.querySelectorAll('.card')];
  // a card near the right edge, well down the list
  const c = cs.filter(x=>x.getBoundingClientRect().left > 700)[6] || cs[10];
  return c.getAttribute('data-id');
});
await p.evaluate((id)=>{ document.querySelector(`.card[data-id="${id}"]`).click(); }, target);
await p.waitForTimeout(800);
console.log('catalogue plate:', await p.evaluate((id)=>{
  const c=document.querySelector(`.card[data-id="${id}"]`);
  const d=document.querySelector('#drawer').getBoundingClientRect();
  const r=c.getBoundingClientRect();
  return {left:Math.round(r.left), right:Math.round(r.right), top:Math.round(r.top),
          sheetLeft:Math.round(d.left), clear:r.right <= d.left+1, inView:r.top>100 && r.bottom<895};
}, target));
await p.screenshot({ path:'/tmp/c-cat.png' });
console.log(errs.length?errs.join('\n'):'no errors');
await b.close();
