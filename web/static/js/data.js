/* ==========================================================================
   The Amiga Constellation — data.js
   Boot loader. Fetches the live catalogue from /api/catalog (same shape as
   data/amiga-data.json), exposes it as window.AMIGA_DATA, then loads the
   application scripts one after another, in order. app.js boots on load.
   Load order (classic scripts sharing one global scope, no modules):
   data.js -> core.js -> catalog.js -> detail.js -> lineage.js -> app.js -> actions.js
   ========================================================================== */
(function(){
'use strict';

var SCRIPTS = ['core.js', 'catalog.js', 'detail.js', 'lineage.js', 'app.js', 'actions.js'];

/* resolve siblings relative to this file, so the loader is not tied to /static/js/;
   the page links it as /static/v-<version>/js/data.js, so the scripts share that version */
var current = document.currentScript;
var BASE = current && current.src ? current.src.replace(/[^\/]*$/, '') : '/static/js/';

function fail(message){
  console.error('[amiga] ' + message);
  var box = document.createElement('div');
  box.setAttribute('role', 'alert');
  box.style.cssText = 'padding:40px;max-width:640px;margin:0 auto;font-family:sans-serif;' +
    'color:#14161A;background:#F2F3F5';
  var h = document.createElement('h1'); h.textContent = 'Amiga Constellation';
  var p = document.createElement('p');
  p.textContent = 'The catalogue could not be loaded (' + message + '). Please reload the page in a moment.';
  box.appendChild(h); box.appendChild(p);
  document.body.appendChild(box);
}

function loadScripts(i){
  if(i >= SCRIPTS.length) return;
  var s = document.createElement('script');
  s.src = BASE + SCRIPTS[i];
  s.async = false;
  s.onload = function(){ loadScripts(i + 1); };
  s.onerror = function(){ fail('could not load ' + SCRIPTS[i]); };
  document.head.appendChild(s);
}

fetch('/api/catalog', {headers: {'Accept': 'application/json'}})
  .then(function(r){
    if(!r.ok) throw new Error('HTTP ' + r.status);
    return r.json();
  })
  .then(function(doc){
    if(!doc || !Array.isArray(doc.entries)) throw new Error('unexpected catalogue shape');
    window.AMIGA_DATA = doc;
    window.AMIGA_DATA_SOURCE = 'api';
    loadScripts(0);
  })
  .catch(function(err){ fail(err && err.message ? err.message : String(err)); });
})();
