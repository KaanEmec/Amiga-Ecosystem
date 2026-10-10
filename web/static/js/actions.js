/* ==========================================================================
   The Amiga Constellation — actions.js
   Loaded last by data.js. Adds "suggest" links to the entry dialog through
   the AMIGA_DETAIL_ACTIONS hook in detail.js, and opens an entry from a
   #entry=<id> link (used by the suggestion pages).
   ========================================================================== */
(function(){
'use strict';

var ACTIONS = [
  ['Suggest a change', 'change'],
  ['Suggest an image', 'image'],
  ['Suggest removal', 'remove']
];

window.AMIGA_DETAIL_ACTIONS = function(entry, container){
  if(!entry || !entry.id || !container) return;
  var label = document.createElement('span');
  label.className = 'eyebrow sg-dlg-label';
  label.textContent = 'Improve this entry';
  var row = document.createElement('div');
  row.className = 'linkrow sg-dlg-actions';
  ACTIONS.forEach(function(a){
    var link = document.createElement('a');
    link.className = 'extlink';
    link.textContent = a[0];
    link.href = '/suggestions/new?type=' + a[1] + '&entry=' + encodeURIComponent(entry.id);
    row.appendChild(link);
  });
  container.appendChild(label);
  container.appendChild(row);
};

function openFromHash(){
  var m = /^#entry=([a-z0-9-]{1,64})$/.exec(window.location.hash);
  if(!m || typeof openDetail !== 'function' || typeof BY_ID === 'undefined' || !BY_ID[m[1]]) return;
  openDetail(m[1]);
}
window.addEventListener('hashchange', openFromHash);
openFromHash();
})();
