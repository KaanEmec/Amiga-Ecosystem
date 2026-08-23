import { chromium } from 'playwright';
const b = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium' });
const p = await b.newPage({ viewport:{width:1440,height:900}, colorScheme:'light' });
const errs=[]; p.on('pageerror', e=>errs.push('PAGEERROR '+e.message));
await p.goto('file://' + process.cwd() + '/Amiga-Constellation.html');
await p.waitForTimeout(1100);
// skip link
await p.keyboard.press('Tab');
console.log('first tab stop:', await p.evaluate(()=>document.activeElement.className || document.activeElement.tagName));
await p.click('#v-graph'); await p.waitForTimeout(1300);
await p.focus('#gcanvas');
await p.keyboard.press('ArrowRight');
await p.waitForTimeout(300);
console.log('after ArrowRight, hovered cluster:', await p.evaluate(()=>{
  const h = window.__GRAPH.hoverHub; return h ? (h.label || (h.d&&h.d.label)) : null; }));
console.log('peek visible:', await p.evaluate(()=>document.querySelector('#gtip').getAttribute('data-on')));
await p.screenshot({ path:'/tmp/k-peek.png' });
await p.keyboard.press('ArrowRight'); await p.keyboard.press('ArrowRight');
await p.keyboard.press('Enter'); await p.waitForTimeout(600);
console.log('board opened:', await p.evaluate(()=>!document.querySelector('#lfocus').hidden));
console.log('focus after open:', await p.evaluate(()=>document.activeElement.className));
await p.keyboard.press('Escape'); await p.waitForTimeout(400);
console.log('board closed:', await p.evaluate(()=>document.querySelector('#lfocus').hidden));
console.log('focus returned to:', await p.evaluate(()=>document.activeElement.id));
console.log(errs.length?errs.join('\n'):'no errors');
await b.close();
