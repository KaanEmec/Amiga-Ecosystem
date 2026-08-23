import { chromium } from 'playwright';
const b = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium' });
for (const scheme of ['light','dark']) {
  const p = await b.newPage({ viewport:{width:1440,height:900}, colorScheme:scheme });
  await p.goto('file://' + process.cwd() + '/Amiga-Constellation.html');
  await p.waitForTimeout(1100);
  await p.click('#v-catalog'); await p.waitForTimeout(500);
  await p.fill('#search','AmigaOS 4.1'); await p.waitForTimeout(700);
  await p.evaluate(()=>{const c=document.querySelector('.card'); if(c)c.click();});
  await p.waitForTimeout(600);
  await p.screenshot({ path:`/tmp/d-${scheme}-top.png` });
  const info = await p.evaluate(()=>{
    const sc=document.querySelector('.dscroll');
    return {scrollable: sc.scrollHeight > sc.clientHeight, h: sc.scrollHeight, c: sc.clientHeight};
  });
  console.log(scheme, JSON.stringify(info));
  await p.evaluate(()=>{document.querySelector('.dscroll').scrollTop = 520;});
  await p.waitForTimeout(350);
  await p.screenshot({ path:`/tmp/d-${scheme}-scrolled.png` });
  await p.close();
}
await b.close();
