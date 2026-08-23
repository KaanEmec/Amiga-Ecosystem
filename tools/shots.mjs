import { chromium } from 'playwright';
const url = 'file://' + process.cwd() + '/Amiga-Constellation.html';
const b = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium' });
const errs = [];
for (const scheme of ['light','dark']) {
  const p = await b.newPage({ viewport:{width:1440,height:900}, colorScheme: scheme });
  p.on('pageerror', e => errs.push(scheme+' PAGEERROR: '+e.message));
  await p.goto(url); await p.waitForTimeout(1400);
  await p.screenshot({ path:`/tmp/f-${scheme}-1-overview.png` });
  await p.evaluate(()=>document.querySelector('.ovscroll').scrollTop = 2450);
  await p.waitForTimeout(400);
  await p.screenshot({ path:`/tmp/f-${scheme}-2-overview-low.png` });
  await p.click('#v-catalog'); await p.waitForTimeout(500);
  await p.evaluate(()=>{const c=document.querySelectorAll('.cathead'); if(c[1])c[1].click();});
  await p.waitForTimeout(500);
  await p.screenshot({ path:`/tmp/f-${scheme}-3-catalog.png` });
  await p.click('#v-graph'); await p.waitForTimeout(1300);
  await p.screenshot({ path:`/tmp/f-${scheme}-4-lineage.png` });
  await p.evaluate(()=>{ const G=window.__GRAPH; const h=G.hubs.find(x=>(x.d&&x.d.label)==='FPGA, replicas & appliances'||(x.d&&x.d.label)==='FPGA & replicas'); if(h) window.__lo(h); });
  await p.waitForTimeout(700);
  await p.screenshot({ path:`/tmp/f-${scheme}-5-board.png` });
  await p.close();
}
console.log(errs.length ? errs.join('\n') : 'no page errors');
await b.close();
