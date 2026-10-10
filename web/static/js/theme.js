/* ==========================================================================
   The Amiga Constellation — theme.js
   Screen Mode. A blocking classic script in <head> of the SPA shell and the
   suggestion layout: it applies the stored theme before first paint, then
   AmigaTheme.mount(button) turns an .iconbtn into the picker (role=menu
   popover, one menuitemradio per mode). Nothing stored = follow the OS.
   ========================================================================== */
(function(){
'use strict';

var KEY = 'amiga-theme', root = document.documentElement;
var MODES = [
  {id:'light', name:'Museum Mode Light', tag:'Paper and ink', sw:['#F2F3F5','#14161A','#B01B1B']},
  {id:'dark', name:'Museum Mode Dark', tag:'After hours', sw:['#14161A','#FFFFFF','#FF6E5C']},
  {id:'workbench', name:'Workbench Mode', tag:'Kickstart 1.3, interlaced', sw:['#0055aa','#ffffff','#ff8800']}
];
var SVG = '<svg viewBox="0 0 16 16" aria-hidden="true" focusable="false" ';
var GLYPH = {
  light: SVG+'fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><circle cx="8" cy="8" r="3.1"/><path d="M8 1v1.6M8 13.4V15M1 8h1.6M13.4 8H15M3.1 3.1l1.1 1.1M11.8 11.8l1.1 1.1M12.9 3.1l-1.1 1.1M4.2 11.8l-1.1 1.1"/></svg>',
  dark: SVG+'fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"><path d="M13.6 10.2A6 6 0 0 1 5.8 2.4a6 6 0 1 0 7.8 7.8z"/></svg>',
  /* a 16x16 Workbench disk, one SVG unit per CSS pixel */
  workbench: SVG+'class="px" shape-rendering="crispEdges"><path fill="#0055aa" d="M1 1h12v1h1v1h1v12H1z"/>'+
    '<path fill="#fff" d="M4 1h7v5H4zM3 8h10v6H3z"/><path fill="#0055aa" d="M8 2h2v3H8z"/>'+
    '<path fill="#ff8800" d="M4 10h8v1H4zM4 12h6v1H4z"/></svg>'
};
var mq = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null;
var mounts = [], uid = 0;

function find(id){ for(var i = 0; i < MODES.length; i++) if(MODES[i].id === id) return MODES[i]; return null; }
function stored(){ try{ var v = localStorage.getItem(KEY); return find(v) ? v : null; }catch(err){ return null; } }
function get(){ return stored() || root.getAttribute('data-theme') || (mq && mq.matches ? 'dark' : 'light'); }

var first = stored();
if(first) root.setAttribute('data-theme', first);

function refresh(changed){
  var cur = get(), m = find(cur);
  mounts.forEach(function(p){
    p.btn.innerHTML = GLYPH[cur];
    p.btn.setAttribute('aria-label', 'Screen Mode: ' + m.name);
    p.btn.title = 'Screen Mode: ' + m.name;
    p.items.forEach(function(it){ it.setAttribute('aria-checked', it.getAttribute('data-mode') === cur ? 'true' : 'false'); });
    if(changed && p.opts.onChange) p.opts.onChange(cur);
  });
}

function set(name){
  if(!find(name)) return;
  root.setAttribute('data-theme', name);
  try{ localStorage.setItem(KEY, name); }catch(err){}
  refresh(true);
}

if(mq) (mq.addEventListener ? mq.addEventListener('change', onOS) : mq.addListener(onOS));
function onOS(){ if(!stored()) refresh(true); }

function mount(btn, opts){
  var n = ++uid, menuId = 'scrmode-' + n, headId = menuId + '-h';
  var pop = document.createElement('div');
  pop.className = 'scrmode';
  pop.hidden = true;
  pop.innerHTML = '<div class="scrmode-h" id="'+headId+'">Screen Mode</div>' +
    '<div class="scrmode-list" role="menu" id="'+menuId+'" aria-labelledby="'+headId+'">' +
    MODES.map(function(m){
      return '<button type="button" class="scrmode-i" role="menuitemradio" tabindex="-1" data-mode="'+m.id+'">' +
        '<span class="scrmode-sw" aria-hidden="true">' + m.sw.map(function(c){ return '<i style="background:'+c+'"></i>'; }).join('') + '</span>' +
        '<span class="scrmode-t"><b>'+m.name+'</b><small>'+m.tag+'</small></span></button>';
    }).join('') + '</div>';
  document.body.appendChild(pop);
  var p = {btn: btn, opts: opts || {}, pop: pop, items: [].slice.call(pop.querySelectorAll('.scrmode-i'))};
  mounts.push(p);
  btn.setAttribute('aria-haspopup', 'menu');
  btn.setAttribute('aria-expanded', 'false');
  btn.setAttribute('aria-controls', menuId);
  refresh(false);

  function place(){
    var r = btn.getBoundingClientRect(), w = pop.offsetWidth;
    var left = Math.max(8, Math.min(r.right - w, window.innerWidth - w - 8));
    pop.style.top = Math.round(r.bottom + 8) + 'px';
    pop.style.left = Math.round(left) + 'px';
  }
  function open(focusLast){
    pop.hidden = false; place();
    btn.setAttribute('aria-expanded', 'true');
    var i = p.items.filter(function(it){ return it.getAttribute('aria-checked') === 'true'; })[0];
    (focusLast ? p.items[p.items.length - 1] : i || p.items[0]).focus();
    document.addEventListener('pointerdown', outside, true);
    window.addEventListener('resize', place);
  }
  function close(refocus){
    if(pop.hidden) return;
    pop.hidden = true;
    btn.setAttribute('aria-expanded', 'false');
    document.removeEventListener('pointerdown', outside, true);
    window.removeEventListener('resize', place);
    if(refocus) btn.focus();
  }
  function outside(ev){ if(!pop.contains(ev.target) && !btn.contains(ev.target)) close(false); }

  btn.addEventListener('click', function(){ if(pop.hidden) open(false); else close(true); });
  btn.addEventListener('keydown', function(ev){
    if(ev.key === 'ArrowDown' || ev.key === 'ArrowUp'){ ev.preventDefault(); open(ev.key === 'ArrowUp'); }
  });
  pop.addEventListener('click', function(ev){
    var it = ev.target.closest('.scrmode-i'); if(!it) return;
    set(it.getAttribute('data-mode')); close(true);
  });
  pop.addEventListener('keydown', function(ev){
    var i = p.items.indexOf(document.activeElement), k = ev.key, len = p.items.length;
    if(k === 'Escape'){ ev.preventDefault(); ev.stopPropagation(); close(true); return; }
    if(k === 'Tab'){ close(true); return; }
    var to = k === 'ArrowDown' ? (i + 1) % len : k === 'ArrowUp' ? (i - 1 + len) % len :
      k === 'Home' ? 0 : k === 'End' ? len - 1 : -1;
    if(to >= 0){ ev.preventDefault(); p.items[to].focus(); }
  });
  return p;
}

window.AmigaTheme = {get: get, set: set, mount: mount};
})();
