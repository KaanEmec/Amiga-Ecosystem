import { chromium } from 'playwright';
import fs from 'fs';
const root = process.cwd();
const b = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium' });
async function src(){
  const p = await b.newPage(); const msgs = [];
  p.on('console', m => msgs.push(m.text()));
  await p.goto('file://' + root + '/Amiga-Constellation.html');
  await p.waitForTimeout(900);
  const r = await p.evaluate(()=>window.AMIGA_DATA_SOURCE);
  await p.close(); return r;
}
console.log('with sidecar :', await src());
fs.renameSync(root+'/amiga-data.js', root+'/_side.bak');
console.log('without      :', await src());
fs.renameSync(root+'/_side.bak', root+'/amiga-data.js');
await b.close();
