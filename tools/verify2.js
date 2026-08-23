function lin(c){ c/=255; return c<=0.03928 ? c/12.92 : Math.pow((c+0.055)/1.055,2.4); }
function L(h){ const n=parseInt(h.slice(1),16);
  return 0.2126*lin((n>>16)&255)+0.7152*lin((n>>8)&255)+0.0722*lin(n&255); }
function CR(a,b){ const la=L(a),lb=L(b); return (Math.max(la,lb)+0.05)/(Math.min(la,lb)+0.05); }
const f=n=>n.toFixed(2).padStart(5);
console.log('=== accent used as a FILL with white text on it ===');
['#EE4433','#DD3322','#CC2211','#BB2211','#AA1100','#992200'].forEach(c=>
  console.log('  white on '+c+' = '+f(CR('#FFFFFF',c))+(CR('#FFFFFF',c)>=4.5?'  AA':'  fail')));
console.log('\n=== accent used as TEXT / marks on dark grounds ===');
['#FF5544','#FF6655','#FF8877'].forEach(c=>
  console.log('  '+c+' on #111122 = '+f(CR(c,'#111122'))+'   on #222233 = '+f(CR(c,'#222233'))));
console.log('\n=== light ink-4 candidates on the rail ground #DDDDEE ===');
['#556677','#445566','#334455'].forEach(c=>
  console.log('  '+c+' on #DDDDEE = '+f(CR(c,'#DDDDEE'))+'   on #FFFFFF = '+f(CR(c,'#FFFFFF'))));
console.log('\n=== borders (definition, not text — 3:1 is the useful bar) ===');
[['dark','#445566','#222233'],['dark','#556677','#222233'],
 ['light','#BBBBCC','#FFFFFF'],['light','#AABBCC','#FFFFFF']].forEach(([t,c,g])=>
  console.log('  '+t+' line '+c+' on '+g+' = '+f(CR(c,g))));
console.log('\n=== warn/ok/info as small status text, final check ===');
[['dark','#FFCC44','#222233'],['dark','#88DD55','#222233'],['dark','#CCAAFF','#222233'],
 ['light','#775500','#FFFFFF'],['light','#226611','#FFFFFF'],['light','#5522AA','#FFFFFF']]
 .forEach(([t,c,g])=>console.log('  '+t+' '+c+' on '+g+' = '+f(CR(c,g))));
