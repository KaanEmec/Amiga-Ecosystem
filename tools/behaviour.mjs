import { chromium } from 'playwright';
const b = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium' });
const p = await b.newPage({ viewport:{width:1440,height:900}, colorScheme:'light' });
const errs=[]; p.on('pageerror', e=>errs.push('PAGEERROR '+e.message));
await p.goto('file://' + process.cwd() + '/Amiga-Constellation.html');
await p.waitForTimeout(1100);
await p.click('#v-catalog'); await p.waitForTimeout(500);

// --- collapse / expand WHILE a filter is active ---
await p.evaluate(()=>{ const o=[...document.querySelectorAll('.opt')].find(x=>x.getAttribute('data-val')==='fpga'); o.click(); });
await p.waitForTimeout(600);
const n0 = await p.evaluate(()=>document.querySelectorAll('.card').length);
const open0 = await p.evaluate(()=>[...document.querySelectorAll('.cathead')].map(c=>c.getAttribute('aria-expanded')));
await p.evaluate(()=>{ document.querySelectorAll('.cathead')[0].click(); });
await p.waitForTimeout(500);
const open1 = await p.evaluate(()=>[...document.querySelectorAll('.cathead')].map(c=>c.getAttribute('aria-expanded')));
const n1 = await p.evaluate(()=>document.querySelectorAll('.card').length);
console.log('filtered: cats open before', JSON.stringify(open0), 'cards', n0);
console.log('after collapsing the first', JSON.stringify(open1), 'cards', n1);
await p.evaluate(()=>{ document.querySelectorAll('.cathead')[0].click(); });
await p.waitForTimeout(450);
console.log('re-expanded ->', await p.evaluate(()=>document.querySelectorAll('.cathead')[0].getAttribute('aria-expanded')),
            'cards', await p.evaluate(()=>document.querySelectorAll('.card').length));
// group collapse too
await p.evaluate(()=>{ document.querySelector('.grouphead').click(); });
await p.waitForTimeout(400);
console.log('group collapsed ->', await p.evaluate(()=>document.querySelector('.grouphead').getAttribute('aria-expanded')));
await p.evaluate(()=>{ document.querySelector('.grouphead').click(); });
await p.waitForTimeout(400);

// --- click outside closes the sheet ---
await p.evaluate(()=>{ const c=document.querySelector('.card'); if(c)c.click(); });
await p.waitForTimeout(500);
console.log('sheet open:', await p.evaluate(()=>document.querySelector('#drawer').getAttribute('data-open')));
await p.mouse.click(700, 830);          // empty catalogue space
await p.waitForTimeout(400);
console.log('after outside click:', await p.evaluate(()=>document.querySelector('#drawer').getAttribute('data-open')));
// clicking another card should SWAP, not close
await p.evaluate(()=>{ document.querySelectorAll('.card')[0].click(); });
await p.waitForTimeout(400);
const a1 = await p.evaluate(()=>document.querySelector('.dhead h2').textContent);
await p.evaluate(()=>{ document.querySelectorAll('.card')[1].click(); });
await p.waitForTimeout(400);
const a2 = await p.evaluate(()=>({open:document.querySelector('#drawer').getAttribute('data-open'), t:document.querySelector('.dhead h2') && document.querySelector('.dhead h2').textContent}));
console.log('swap:', a1, '->', JSON.stringify(a2));
// clicking inside the sheet must not close it
await p.evaluate(()=>{ document.querySelector('.dsec p').click(); });
await p.waitForTimeout(300);
console.log('click inside sheet keeps it open:', await p.evaluate(()=>document.querySelector('#drawer').getAttribute('data-open')));
console.log(errs.length?errs.join('\n'):'no errors');
await b.close();
