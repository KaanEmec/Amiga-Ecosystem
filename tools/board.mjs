import { chromium } from 'playwright';
const b = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium' });
const p = await b.newPage({ viewport:{width:1440,height:900}, colorScheme:'light' });
const errs=[]; p.on('pageerror', e=>errs.push('PAGEERROR '+e.message));
await p.goto('file://' + process.cwd() + '/Amiga-Constellation.html');
await p.waitForTimeout(1200);
await p.click('#v-graph'); await p.waitForTimeout(1300);
await p.evaluate(()=>{ const G=window.__GRAPH; const h=G.hubs.find(x=>(x.d&&x.d.label)==='Operating systems'); if(h) window.__lo(h); });
await p.waitForTimeout(700);
await p.screenshot({ path:'/tmp/b-open.png' });
console.log('pane scrolls:', await p.evaluate(()=>{const q=document.querySelector('#pane-graph');return {h:q.scrollHeight,c:q.clientHeight,lf:q.getAttribute('data-lf')};}));
await p.evaluate(()=>{ document.querySelector('#pane-graph').scrollTop = 300; });
await p.waitForTimeout(300);
await p.screenshot({ path:'/tmp/b-scrolled.png' });
// select a card in the LAST column and check it is visible
await p.evaluate(()=>{ document.querySelector('#pane-graph').scrollTop = 0; });
await p.waitForTimeout(200);
await p.evaluate(()=>{ const cs=[...document.querySelectorAll('.lfcard')]; const c=cs.find(x=>/MorphOS 3.20/.test(x.textContent)); if(c) c.click(); });
await p.waitForTimeout(800);
await p.screenshot({ path:'/tmp/b-sel.png' });
console.log('selected card visible:', await p.evaluate(()=>{
  const c=[...document.querySelectorAll('.lfcard')].find(x=>/MorphOS 3.20/.test(x.textContent));
  const d=document.querySelector('#drawer').getBoundingClientRect();
  const r=c.getBoundingClientRect();
  return {left:Math.round(r.left), right:Math.round(r.right), top:Math.round(r.top),
          sheetLeft:Math.round(d.left), underSheet: r.right > d.left, inView: r.top>60 && r.bottom<900};
}));
console.log(errs.length?errs.join('\n'):'no errors');
await b.close();
