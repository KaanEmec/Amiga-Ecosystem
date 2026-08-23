function lin(c){ c/=255; return c<=0.03928 ? c/12.92 : Math.pow((c+0.055)/1.055,2.4); }
function L(h){ const n=parseInt(h.slice(1),16);
  return 0.2126*lin((n>>16)&255)+0.7152*lin((n>>8)&255)+0.0722*lin(n&255); }
function CR(a,b){ const la=L(a),lb=L(b); return (Math.max(la,lb)+0.05)/(Math.min(la,lb)+0.05); }
const f=n=>n.toFixed(2).padStart(5);
// every value below is 12-bit legal (each channel a multiple of 0x11)
const D = {bg:'#111122', bgDeep:'#000011', surf:'#222233', surf2:'#333344', surf3:'#555566',
  ink:'#FFFFFF', ink2:'#DDEEFF', ink3:'#BBCCDD', ink4:'#99AABB',
  acc:'#FF5544', accSoft:'#FF8877', ok:'#88DD55', warn:'#FFCC44', info:'#CCAAFF', off:'#AABBCC',
  e1:'#FF6655', e2:'#FFAA33', e3:'#33CCDD', e4:'#99EE55'};
const Lt = {bg:'#EEEEFF', bgDeep:'#DDDDEE', surf:'#FFFFFF', surf2:'#EEEEFF', surf3:'#CCCCDD',
  ink:'#000011', ink2:'#223344', ink3:'#334455', ink4:'#556677',
  acc:'#CC2200', accSoft:'#AA3322', ok:'#226611', warn:'#775500', info:'#5522AA', off:'#445566',
  e1:'#CC2200', e2:'#994400', e3:'#005577', e4:'#336600'};
function rep(name,p,tiny){
  console.log('\n=== '+name+' ===   (target: AA 4.5 for text, AAA 7 preferred for small text)');
  const gr=[['bg',p.bg],['surf',p.surf],['surf2',p.surf2],['bgDeep',p.bgDeep]];
  let worst=99, worstK='';
  tiny.forEach(k=>{
    const cells = gr.map(([gn,g])=>{ const r=CR(p[k],g);
      if(r<worst){worst=r;worstK=k+' on '+gn;}
      return gn+' '+f(r)+(r>=7?' AAA':r>=4.5?' AA ':r>=3?' A- ':' XX ');}).join('|');
    console.log('  '+k.padEnd(8)+p[k]+' '+cells);
  });
  console.log('  --> worst text ratio: '+worst.toFixed(2)+'  ('+worstK+')');
}
const textTokens=['ink','ink2','ink3','ink4','accSoft','ok','warn','info','off'];
rep('DARK (new)',D,textTokens);
rep('LIGHT (new)',Lt,textTokens);
console.log('\n=== era colours as canvas text/marks ===');
['e1','e2','e3','e4'].forEach(k=>console.log('  '+k+' dark '+D[k]+' on bg '+f(CR(D[k],D.bg))+
  '   light '+Lt[k]+' on bg '+f(CR(Lt[k],Lt.bg))));
console.log('\n=== ramp separation (distinct steps still readable as hierarchy?) ===');
[['dark',D],['light',Lt]].forEach(([n,p])=>{
  const r=['ink','ink2','ink3','ink4'].map(k=>CR(p[k],p.surf));
  console.log('  '+n+': '+r.map(x=>x.toFixed(1)).join(' / ')+
    '   steps: '+[r[0]/r[1],r[1]/r[2],r[2]/r[3]].map(x=>x.toFixed(2)+'x').join(' '));
});
console.log('\n=== accent on accent-filled buttons (white text on --acc) ===');
console.log('  dark  #FFFFFF on '+D.acc+' = '+f(CR('#FFFFFF',D.acc)));
console.log('  light #FFFFFF on '+Lt.acc+' = '+f(CR('#FFFFFF',Lt.acc)));
