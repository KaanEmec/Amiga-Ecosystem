import { chromium } from 'playwright';
const b = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium' });
for (const w of [1440, 980, 420]) {
  const p = await b.newPage({ viewport:{width:w,height:820}, colorScheme:'light' });
  const errs=[]; p.on('pageerror', e=>errs.push('PAGEERROR '+e.message));
  await p.goto('file://' + process.cwd() + '/Amiga-Constellation.html');
  await p.waitForTimeout(1100);
  await p.click('#v-graph'); await p.waitForTimeout(1200);
  await p.evaluate(()=>{ document.querySelector('#groutes').scrollIntoView({inline:'end'}); });
  await p.click('#groutesbtn'); await p.waitForTimeout(400);
  const vis = await p.evaluate(()=>{
    const el = document.querySelector('.groutes-p');
    const r = el.getBoundingClientRect();
    return {top:Math.round(r.top), left:Math.round(r.left), w:Math.round(r.width), h:Math.round(r.height),
            inViewport: r.top>=0 && r.left>=0 && r.bottom<=innerHeight && r.right<=innerWidth,
            visible: !!(r.width && r.height) && getComputedStyle(el).visibility!=='hidden'};
  });
  console.log(w, JSON.stringify(vis));
  await p.screenshot({ path:`/tmp/r-${w}.png` });
  // and picking one works
  await p.evaluate(()=>{ document.querySelector('.groutes-p .gchip').click(); });
  await p.waitForTimeout(700);
  console.log('   route started:', await p.evaluate(()=>({open:document.querySelector('#groutes').open, btn:document.querySelector('#groutesbtn').textContent, bar:!document.querySelector('#tourbar').hidden})));
  if (errs.length) console.log(errs.join('\n'));
  await p.close();
}
await b.close();
