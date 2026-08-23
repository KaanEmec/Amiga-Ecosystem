import { chromium } from 'playwright';
const b = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium' });
const p = await b.newPage({ viewport:{width:1440,height:900}, colorScheme:'light' });
const errs=[]; p.on('pageerror', e=>errs.push('PAGEERROR '+e.message));
await p.goto('file://' + process.cwd() + '/Amiga-Constellation.html');
await p.waitForTimeout(1100);
await p.click('#v-graph'); await p.waitForTimeout(1300);
for (const mode of ['era','arch','st','kind','nested']) {
  await p.selectOption('#groupby', mode);
  await p.waitForTimeout(1100);
  await p.screenshot({ path:`/tmp/mode-${mode}.png` });
  const info = await p.evaluate(()=>{
    const G = window.__GRAPH;
    // report the tightest vertical clearance between any two medallions
    let worst = 1e9, pair = '';
    const hs = G.hubs.filter(h=>h.medallion);
    for (let i=0;i<hs.length;i++) for (let j=i+1;j<hs.length;j++){
      const a=hs[i], c=hs[j];
      if (Math.abs(a.x-c.x) > (a.hubR + c.hubR + 60)) continue;   // not vertically stacked
      const up = a.y < c.y ? a : c, lo = a.y < c.y ? c : a;
      const gap = (lo.y - lo.hubR - 8) - (up.y + up.hubR + 24 + 17*2);
      if (gap < worst){ worst = gap; pair = (up.d&&up.d.label)+' / '+(lo.d&&lo.d.label); }
    }
    return {k: +G.k.toFixed(2), hubs: hs.length, worstGap: worst===1e9?'n/a':Math.round(worst), pair};
  });
  console.log(mode, JSON.stringify(info));
}
console.log(errs.length?errs.join('\n'):'no errors');
await b.close();
