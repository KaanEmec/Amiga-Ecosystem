import { chromium } from 'playwright';
const b = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium' });
function fmt(x){ return x.toFixed(2); }
for (const scheme of ['light','dark']) {
  const p = await b.newPage({ viewport:{width:1440,height:900}, colorScheme: scheme });
  await p.goto('file://' + process.cwd() + '/Amiga-Constellation.html');
  await p.waitForTimeout(1200);
  const run = async (label) => {
    const bad = await p.evaluate(() => {
      const lum = c => { const m = c.match(/\d+(\.\d+)?/g).map(Number);
        const f = v => { v/=255; return v<=0.03928? v/12.92 : Math.pow((v+0.055)/1.055,2.4); };
        return 0.2126*f(m[0]) + 0.7152*f(m[1]) + 0.0722*f(m[2]); };
      const cr = (a,b) => { const x=lum(a), y=lum(b); return (Math.max(x,y)+0.05)/(Math.min(x,y)+0.05); };
      const bgOf = el => { let n = el;
        while (n && n !== document.documentElement) {
          const c = getComputedStyle(n).backgroundColor;
          if (c && c !== 'rgba(0, 0, 0, 0)' && !/, 0\)$/.test(c)) return c;
          n = n.parentElement; }
        return getComputedStyle(document.body).backgroundColor; };
      const out = [];
      document.querySelectorAll('*').forEach(el => {
        if (!el.offsetParent && el.tagName !== 'BODY') return;
        const txt = Array.from(el.childNodes).filter(n=>n.nodeType===3).map(n=>n.textContent.trim()).join('');
        if (!txt) return;
        const st = getComputedStyle(el);
        if (st.visibility === 'hidden' || st.opacity === '0') return;
        const r = cr(st.color, bgOf(el));
        const px = parseFloat(st.fontSize);
        const large = px >= 24 || (px >= 18.66 && parseInt(st.fontWeight) >= 700);
        const need = large ? 3 : 4.5;
        if (r < need) out.push({t:txt.slice(0,40), c:st.color, bg:bgOf(el), r:+r.toFixed(2), px, sel:el.className||el.tagName});
      });
      return out;
    });
    console.log(scheme, label, bad.length ? JSON.stringify(bad, null, 1) : 'all pass');
  };
  await run('overview');
  await p.click('#v-catalog'); await p.waitForTimeout(500);
  await p.evaluate(()=>{const c=document.querySelectorAll('.cathead'); if(c[0])c[0].click();});
  await p.waitForTimeout(400);
  await p.evaluate(()=>{const c=document.querySelector('.card'); if(c)c.click();});
  await p.waitForTimeout(500);
  await run('catalog+drawer');
  await p.click('#v-graph'); await p.waitForTimeout(1000);
  await p.evaluate(()=>{ const G=window.__GRAPH; const h=G.hubs.find(x=>(x.d&&x.d.label)==='Operating systems'); if(h) window.__lo(h); });
  await p.waitForTimeout(600);
  await run('lineage board');
  await p.close();
}
await b.close();
