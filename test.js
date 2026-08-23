const { chromium } = require('playwright');
const path = require('path');

(async () => {
  const browser = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium' });
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 });
  const page = await ctx.newPage();

  const errors = [], warnings = [], requests = [];
  page.on('console', m => {
    if (m.type() === 'error') errors.push(m.text());
    if (m.type() === 'warning') warnings.push(m.text());
  });
  page.on('pageerror', e => errors.push('PAGEERROR: ' + e.message));
  page.on('request', r => { const u = r.url(); if (!u.startsWith('file:')) requests.push(u); });

  const file = 'file://' + path.resolve(__dirname, 'Amiga-Constellation.html');
  await page.goto(file, { waitUntil: 'load' });
  await page.waitForTimeout(1200);

  const shot = async (name) => {
    await page.screenshot({ path: `shots/${name}.png` });
    console.log('  shot: ' + name);
  };

  // ---- overview
  const stats = await page.$$eval('.stat .v', ns => ns.map(n => n.textContent));
  console.log('overview stats:', stats.join(' | '));
  await shot('1-overview');

  // ---- catalog
  await page.click('#v-catalog');
  await page.waitForTimeout(400);
  const total = await page.textContent('#rescount');
  const cards = await page.$$eval('.card', n => n.length);
  const sections = await page.$$eval('.secthead h2', n => n.map(x => x.textContent));
  console.log('catalog: rescount=' + total + '  cards=' + cards);
  console.log('sections (' + sections.length + '):', sections.join(', '));
  await shot('2-catalog');

  // ---- filter: FPGA + active/shipping
  await page.click('.opt[data-dim="arch"][data-val="fpga"]');
  await page.waitForTimeout(300);
  const fpgaCount = await page.textContent('#rescount');
  console.log('filter FPGA -> ' + fpgaCount);
  await page.click('.opt[data-dim="st"][data-val="shipping"]');
  await page.waitForTimeout(300);
  console.log('filter FPGA+shipping -> ' + (await page.textContent('#rescount')));
  const chips = await page.$$eval('.chip', n => n.map(x => x.textContent.replace(/×$/,'')));
  console.log('chips:', chips.join(' / '));
  await shot('3-filtered');

  // ---- detail drawer
  await page.click('.card');
  await page.waitForTimeout(450);
  const dTitle = await page.textContent('.dhead h2');
  const dSecs = await page.$$eval('.dsec .eyebrow', n => n.map(x => x.textContent));
  console.log('drawer: "' + dTitle + '"  sections: ' + dSecs.join(', '));
  await shot('4-detail');

  // ---- reset, then search
  await page.click('.dclose');
  await page.click('#resetall');
  await page.waitForTimeout(250);
  await page.fill('#search', 'vampire');
  await page.waitForTimeout(400);
  console.log('search "vampire" -> ' + (await page.textContent('#rescount')));
  await page.fill('#search', 'rtg retargetable');
  await page.waitForTimeout(400);
  console.log('search "rtg retargetable" -> ' + (await page.textContent('#rescount')));
  await page.fill('#search', 'zzzznope');
  await page.waitForTimeout(400);
  const emptyVisible = await page.$('.empty') !== null;
  console.log('empty state shows: ' + emptyVisible);
  await shot('5-empty');
  await page.fill('#search', '');
  await page.waitForTimeout(300);

  // ---- graph
  await page.click('#v-graph');
  await page.waitForTimeout(1400);
  const canvasInk = await page.evaluate(() => {
    const cv = document.querySelector('#gcanvas');
    const cx = cv.getContext('2d');
    const d = cx.getImageData(0, 0, cv.width, cv.height).data;
    let n = 0; for (let i = 3; i < d.length; i += 4) if (d[i] > 8) n++;
    return { painted: n, total: d.length / 4, w: cv.width, h: cv.height };
  });
  console.log('graph canvas: ' + canvasInk.w + 'x' + canvasInk.h +
              '  painted px=' + canvasInk.painted +
              ' (' + (100 * canvasInk.painted / canvasInk.total).toFixed(1) + '%)');
  await shot('6-graph');

  // hover + click a node to verify picking works
  await page.mouse.move(720, 450);
  await page.waitForTimeout(300);
  await page.mouse.wheel(0, -420);
  await page.waitForTimeout(500);
  await shot('7-graph-zoom');

  // ---- light theme
  await page.click('#v-catalog');
  await page.click('#themebtn');
  await page.waitForTimeout(400);
  await shot('8-light');

  // ---- narrow viewport
  await page.setViewportSize({ width: 430, height: 860 });
  await page.waitForTimeout(400);
  await shot('9-mobile');

  console.log('\nexternal requests: ' + (requests.length ? requests.join(', ') : 'none'));
  console.log('console errors: ' + (errors.length ? '\n  ' + errors.join('\n  ') : 'none'));
  console.log('console warnings: ' + (warnings.length ? '\n  ' + warnings.join('\n  ') : 'none'));

  await browser.close();
  if (errors.length) process.exitCode = 1;
})();
