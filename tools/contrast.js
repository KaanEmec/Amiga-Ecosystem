function lin(c){ c/=255; return c<=0.03928 ? c/12.92 : Math.pow((c+0.055)/1.055,2.4); }
function L(hex){ const n=parseInt(hex.slice(1),16);
  return 0.2126*lin((n>>16)&255)+0.7152*lin((n>>8)&255)+0.0722*lin(n&255); }
function CR(a,b){ const la=L(a),lb=L(b); return ((Math.max(la,lb)+0.05)/(Math.min(la,lb)+0.05)); }
const f=n=>n.toFixed(2);

const dark = {bg:'#111122', bgDeep:'#000011', surf:'#222233', surf2:'#333344', surf3:'#444455',
  ink:'#EEEEFF', ink2:'#AABBCC', ink3:'#7788AA', ink4:'#556677',
  acc:'#EE4433', accSoft:'#DD5544', ok:'#66CC44', warn:'#FFBB33', info:'#AA88EE', off:'#778899',
  e1:'#EE4433', e2:'#FF9922', e3:'#22BBCC', e4:'#88DD44'};
const light = {bg:'#EEEEFF', bgDeep:'#DDDDEE', surf:'#FFFFFF', surf2:'#EEEEFF', surf3:'#CCCCDD',
  ink:'#111122', ink2:'#334455', ink3:'#556677', ink4:'#8899AA',
  acc:'#CC3322', accSoft:'#BB4433', ok:'#338822', warn:'#AA7700', info:'#7744CC', off:'#667788',
  e1:'#CC3322', e2:'#CC7700', e3:'#0088AA', e4:'#448811'};

function report(name, p){
  console.log('\n=== '+name+' ===');
  const grounds = [['bg',p.bg],['surf',p.surf],['bgDeep',p.bgDeep],['surf2',p.surf2]];
  ['ink','ink2','ink3','ink4','accSoft','ok','warn','info','off','e2','e4','e3'].forEach(k=>{
    const row = grounds.map(([gn,g])=>{
      const r = CR(p[k],g);
      const flag = r>=7?'AAA':r>=4.5?'AA ':r>=3?'A- ':'FAIL';
      return gn+' '+f(r)+' '+flag;
    }).join(' | ');
    console.log('  '+k.padEnd(9)+p[k]+'  '+row);
  });
}
report('DARK (current)', dark);
report('LIGHT (current)', light);

// 12-bit legal candidates, brightest-first, for dark theme text on #111122/#222233
console.log('\n=== candidate 12-bit greys vs dark grounds (bg #111122 / surf #222233) ===');
['#FFFFFF','#EEEEFF','#DDEEFF','#DDDDEE','#CCDDEE','#CCCCDD','#BBCCDD','#AABBCC','#99AABB','#8899AA','#7788AA']
 .forEach(h=>console.log('  '+h+'  bg '+f(CR(h,'#111122')).padStart(5)+'   surf '+f(CR(h,'#222233')).padStart(5)));
console.log('\n=== candidate 12-bit greys vs light grounds (surf #FFFFFF / bg #EEEEFF) ===');
['#111122','#223344','#334455','#445566','#556677','#667788','#778899','#8899AA']
 .forEach(h=>console.log('  '+h+'  surf '+f(CR(h,'#FFFFFF')).padStart(5)+'   bg '+f(CR(h,'#EEEEFF')).padStart(5)));
