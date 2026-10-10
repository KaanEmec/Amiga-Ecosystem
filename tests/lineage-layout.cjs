// Dependency-free geometry/state checks; no browser globals or app test API needed.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const root = path.resolve(__dirname, '..');
// The UI ships as classic scripts sharing one global scope; concatenate them in
// the order data.js loads them (data.js itself only fetches /api/catalog).
const SCRIPTS = ['core.js', 'catalog.js', 'detail.js', 'lineage.js', 'app.js'];
let source = SCRIPTS.map(name => fs.readFileSync(path.join(root, 'web/static/js', name), 'utf8')).join('\n');
assert.equal(source.split("if(document.readyState === 'loading')").length, 2, 'boot hook anchor');
source = source.replace("if(document.readyState === 'loading')", `
globalThis.layoutTest = {GRAPH, S, graphBuild, graphLayout, graphFit, relayout, zonesFor, clustersFor, clampClusterPan};
if(document.readyState === 'loading')`);
const context = {
  window: {AMIGA_DATA: JSON.parse(fs.readFileSync(path.join(root, 'data/amiga-data.json'), 'utf8'))},
  document: {readyState:'loading', addEventListener(){}},
  requestAnimationFrame(){return 1;}, cancelAnimationFrame(){}, console:{log(){},warn:console.warn}
};
vm.runInNewContext(source, context);
const {GRAPH:g, S:s, graphBuild, relayout, graphFit, zonesFor, clampClusterPan} = context.layoutTest;
graphBuild();
let cases = 0;
for(const width of [390, 700, 1000, 1600]){
  for(const mode of ['domain','era','arch','st','kind']){
    for(const essentials of [false,true]){
      g.W=width; g.H=600; g.groupBy=mode; g.level='clusters'; s.ess=essentials; s.q='';
      relayout(false);
      const ids = g.hubs.flatMap(h=>h.members.map(n=>n.id));
      const expected = g.nodes.filter(n=>!essentials || n.e.wt>=5).map(n=>n.id);
      assert.equal(new Set(ids).size,ids.length,`${mode}: duplicate members`);
      assert.deepEqual([...ids].sort(),[...expected].sort(),`${mode}: membership`);
      assert(g.zones.length>0);
      const descriptors = zonesFor(mode);
      for(const h of g.hubs){
        const descriptor = descriptors.find(z=>z.buckets.includes(h.bucketId));
        assert(descriptor,`${mode}: unmapped ${h.bucketId}`);
        const zone = g.zones.find(z=>z.id===descriptor.id);
        assert(h.x-h.hubR>=zone.x && h.x+h.hubR<=zone.x+zone.w,`${mode}: horizontal panel bounds`);
        assert(h.y-h.hubR>=zone.y && h.y+h.hubR<=zone.y+zone.h,`${mode}: vertical panel bounds`);
        assert(Number.isFinite(h.x+h.y+h.hubR));
      }
      const before = JSON.stringify(g.hubs.map(h=>[h.uid,h.x,h.y,h.hubR]));
      for(const extreme of [-1e6,1e6]){
        g.tx=extreme; g.ty=extreme; clampClusterPan();
        const top=Math.min(...g.zones.map(z=>z.y))*g.k+g.ty;
        const bottom=Math.max(...g.zones.map(z=>z.y+z.h))*g.k+g.ty;
        assert(top<g.H && bottom>0,`${mode}: panning lost map`);
      }
      s.q='amiga'; s.f.st.active=true;
      relayout(false);
      assert.equal(JSON.stringify(g.hubs.map(h=>[h.uid,h.x,h.y,h.hubR])),before,`${mode}: filters moved clusters`);
      s.f.st={}; s.q='';
      g.level='all'; relayout(false);
      assert.equal(g.zones.length,0);
      assert.equal(g.fitAxis,'both');
      assert.equal(Object.keys(g.present).length,expected.length);
      const target = graphFit(false,true);
      assert(Number.isFinite(target.k+target.tx+target.ty));
      cases++;
    }
  }
}
assert(!/matryoshka|superHubs|gopenall|gcloseall|groupBy === 'nested'/.test(source));
console.log(`Lineage layout: ${cases} combinations passed (five arrangements, four widths, Essentials on/off).`);
