/* ==========================================================================
   The Amiga Constellation — suggest.js
   Server-rendered suggestion pages (list, detail, form): theme toggle, +1
   votes via fetch, Turnstile widgets, and on the form: per-type fields,
   searchable entry and lineage pickers, and "did you mean" hints.
   Standalone classic script; it does not use the catalogue scripts.
   ========================================================================== */
(function(){
'use strict';

function $(s, root){ return (root || document).querySelector(s); }
function $$(s, root){ return Array.prototype.slice.call((root || document).querySelectorAll(s)); }
function el(tag, cls, txt){
  var e = document.createElement(tag);
  if(cls) e.className = cls;
  if(txt != null) e.textContent = txt;
  return e;
}
function debounce(fn, ms){
  var t = 0;
  return function(){ var a = arguments; clearTimeout(t); t = setTimeout(function(){ fn.apply(null, a); }, ms); };
}
function api(method, url, opts){
  opts = opts || {};
  var headers = {'Accept': 'application/json', 'X-Requested-With': 'fetch'};
  Object.keys(opts.headers || {}).forEach(function(k){ headers[k] = opts.headers[k]; });
  return fetch(url, {method: method, headers: headers, credentials: 'same-origin', body: opts.body})
    .then(function(r){
      return r.json().catch(function(){ return {}; }).then(function(j){ return {ok: r.ok, status: r.status, json: j}; });
    });
}

/* ------------------------------------------------------------- theme */
var root = document.documentElement;
try{ var stored = localStorage.getItem('amiga-theme'); if(stored) root.setAttribute('data-theme', stored); }catch(err){}

/* --------------------------------------------------------- turnstile */
function whenTurnstile(cb){
  var tries = 0;
  (function poll(){
    if(window.turnstile && window.turnstile.render) return cb(window.turnstile);
    if(++tries < 200) setTimeout(poll, 100);
  })();
}

/* The vote widget runs invisibly and only on demand; each token is single
   use, so the widget is reset after every vote. */
var VOTE_TS = {id: null, waiting: null};
function setupVoteWidget(){
  var box = $('.sg-vote-ts');
  if(!box) return;
  whenTurnstile(function(ts){
    VOTE_TS.id = ts.render(box, {
      sitekey: box.getAttribute('data-sitekey'),
      action: box.getAttribute('data-action') || 'vote',
      execution: 'execute',
      appearance: 'interaction-only',
      callback: function(token){ if(VOTE_TS.waiting){ var w = VOTE_TS.waiting; VOTE_TS.waiting = null; w.resolve(token); } },
      'error-callback': function(){ if(VOTE_TS.waiting){ var w = VOTE_TS.waiting; VOTE_TS.waiting = null; w.reject(new Error('The anti-spam check failed. Please try again.')); } }
    });
  });
}
function voteToken(){
  if(!$('.sg-vote-ts')) return Promise.resolve('');
  return new Promise(function(resolve, reject){
    if(VOTE_TS.id === null) return reject(new Error('The anti-spam check is still loading. Please try again.'));
    VOTE_TS.waiting = {resolve: resolve, reject: reject};
    try{ window.turnstile.reset(VOTE_TS.id); window.turnstile.execute(VOTE_TS.id); }
    catch(err){ VOTE_TS.waiting = null; reject(err); }
  });
}

/* ------------------------------------------------------------- votes */
function voteMessage(btn, text){
  var box = $('.sg-vote-msg');
  if(!box) return;
  box.textContent = text || '';
  box.hidden = !text;
}
function setupVotes(){
  $$('.sg-vote').forEach(function(btn){
    btn.addEventListener('click', function(){
      if(btn.disabled || btn.getAttribute('aria-busy') === 'true') return;
      var on = btn.getAttribute('aria-pressed') !== 'true';
      var id = btn.getAttribute('data-id');
      btn.setAttribute('aria-busy', 'true');
      voteMessage(btn, '');
      voteToken().then(function(token){
        return api(on ? 'PUT' : 'DELETE', '/api/suggestions/' + encodeURIComponent(id) + '/vote',
          {headers: token ? {'X-Turnstile-Token': token} : {}});
      }).then(function(res){
        if(!res.ok){
          if(res.status === 409) btn.disabled = true;
          throw new Error(res.json.error || ('Request failed (' + res.status + ')'));
        }
        /* reflect the new state on every button for this suggestion */
        $$('.sg-vote[data-id="' + id + '"]').forEach(function(b){
          b.setAttribute('aria-pressed', res.json.voted ? 'true' : 'false');
          var c = $('.sg-count', b); if(c) c.textContent = res.json.votes;
          b.setAttribute('aria-label', '+1 suggestion ' + id + ' (' + res.json.votes + ' so far)');
        });
      }).catch(function(err){
        voteMessage(btn, err && err.message ? err.message : 'Could not record your +1.');
      }).then(function(){ btn.removeAttribute('aria-busy'); });
    });
  });
}

/* --------------------------------------------------- list: filters */
function setupFilters(){
  var f = $('.sg-filters');
  if(!f) return;
  $$('select', f).forEach(function(s){ s.addEventListener('change', function(){ f.submit(); }); });
}

/* ---------------------------------------------------- form: pickers */
function search(q){
  return fetch('/api/entries/search?q=' + encodeURIComponent(q), {headers: {'Accept': 'application/json'}, credentials: 'same-origin'})
    .then(function(r){ return r.ok ? r.json() : {entries: [], suggestions: []}; })
    .catch(function(){ return {entries: [], suggestions: []}; });
}

var pickerSeq = 0;
/* combobox over /api/entries/search; onPick(entry) */
function picker(input, onPick){
  var list = el('ul', 'sg-results');
  var lid = 'sg-results-' + (++pickerSeq);
  list.id = lid; list.setAttribute('role', 'listbox'); list.hidden = true;
  input.parentNode.insertBefore(list, input.nextSibling);
  input.setAttribute('role', 'combobox');
  input.setAttribute('aria-autocomplete', 'list');
  input.setAttribute('aria-controls', lid);
  input.setAttribute('aria-expanded', 'false');
  var items = [], active = -1;

  function close(){ list.hidden = true; input.setAttribute('aria-expanded', 'false'); input.removeAttribute('aria-activedescendant'); active = -1; }
  function highlight(i){
    active = i;
    items.forEach(function(it, j){ it.li.setAttribute('aria-selected', j === i ? 'true' : 'false'); });
    if(i >= 0) input.setAttribute('aria-activedescendant', items[i].li.id);
  }
  function pick(i){ var e = items[i] && items[i].entry; close(); if(e) onPick(e); }
  var run = debounce(function(){
    var q = input.value.trim();
    if(q.length < 2){ close(); return; }
    search(q).then(function(res){
      if(input.value.trim() !== q) return;
      list.innerHTML = ''; items = [];
      (res.entries || []).forEach(function(e, i){
        var li = el('li', 'sg-result');
        li.id = lid + '-' + i; li.setAttribute('role', 'option');
        li.appendChild(el('span', 'sg-rn', e.name));
        li.appendChild(el('span', 'sg-ry mono', (e.year || '') + ' · ' + e.id));
        li.addEventListener('mousedown', function(ev){ ev.preventDefault(); pick(i); });
        list.appendChild(li); items.push({li: li, entry: e});
      });
      if(!items.length){ list.appendChild(el('li', 'sg-result sg-none', 'No matching entry')); }
      list.hidden = false; input.setAttribute('aria-expanded', 'true'); highlight(items.length ? 0 : -1);
    });
  }, 200);
  input.addEventListener('input', run);
  input.addEventListener('blur', function(){ setTimeout(close, 120); });
  input.addEventListener('keydown', function(ev){
    if(list.hidden) return;
    if(ev.key === 'ArrowDown'){ ev.preventDefault(); highlight(Math.min(items.length - 1, active + 1)); }
    else if(ev.key === 'ArrowUp'){ ev.preventDefault(); highlight(Math.max(0, active - 1)); }
    else if(ev.key === 'Enter'){ if(active >= 0){ ev.preventDefault(); pick(active); } }
    else if(ev.key === 'Escape'){ close(); }
  });
}

/* ------------------------------------------------------- form: hints */
function renderHints(res, entryId){
  var aside = $('.sg-hints');
  if(!aside) return;
  var ents = $('.sg-hint-entries', aside), sugs = $('.sg-hint-sugs', aside);
  ents.innerHTML = ''; sugs.innerHTML = '';
  var entries = entryId ? [] : (res.entries || []).slice(0, 5);
  var pending = (res.suggestions || []).filter(function(s){ return !entryId || s.entry_id === entryId; }).slice(0, 5);
  if(entries.length){
    ents.appendChild(el('p', 'sg-hint-h', 'Already in the catalogue'));
    var ul = el('ul');
    entries.forEach(function(e){
      var li = el('li'), a = el('a', null, e.name);
      a.href = '/#entry=' + encodeURIComponent(e.id); a.target = '_blank'; a.rel = 'noopener';
      li.appendChild(a); li.appendChild(el('span', 'mono', ' ' + (e.year || '')));
      ul.appendChild(li);
    });
    ents.appendChild(ul);
  }
  if(pending.length){
    sugs.appendChild(el('p', 'sg-hint-h', 'Pending suggestions'));
    var ul2 = el('ul');
    pending.forEach(function(s){
      var li = el('li'), a = el('a', null, '#' + s.id + ' ' + s.title);
      a.href = '/suggestions/' + s.id; a.target = '_blank'; a.rel = 'noopener';
      li.appendChild(a); li.appendChild(el('span', 'mono', ' +' + s.votes));
      ul2.appendChild(li);
    });
    sugs.appendChild(ul2);
    sugs.appendChild(el('p', 'sg-help', 'A +1 on an existing suggestion helps more than a duplicate.'));
  }
  aside.hidden = !(entries.length || pending.length);
}

/* -------------------------------------------------------------- form */
function setupForm(){
  var form = $('.sg-form');
  if(!form) return;
  var errors = $('#sg-errors');
  if(errors) errors.focus();

  /* per-type sections */
  function currentType(){ var r = $('input[name="type"]:checked', form); return r ? r.value : 'new'; }
  function applyType(){
    var t = currentType();
    form.setAttribute('data-type', t);
    $$('[data-for]', form).forEach(function(n){
      n.hidden = n.getAttribute('data-for').split(' ').indexOf(t) < 0;
    });
  }
  $$('input[name="type"]', form).forEach(function(r){
    r.addEventListener('change', function(){
      applyType();
      var id = entryHidden && entryHidden.value;
      if(currentType() === 'change' && id && !$('input[name="f_name"]', form).value.trim()){
        location.href = '/suggestions/new?type=change&entry=' + encodeURIComponent(id);
      }
      refreshHints();
    });
  });
  applyType();

  /* entry picker: the visible box searches, a hidden input carries the id */
  var entryBox = $('#sg-entry'), entryHidden = null;
  if(entryBox){
    entryHidden = el('input'); entryHidden.type = 'hidden'; entryHidden.name = 'entry'; entryHidden.value = entryBox.value;
    entryBox.removeAttribute('name');
    entryBox.parentNode.appendChild(entryHidden);
    if(entryBox.getAttribute('data-label')) entryBox.value = entryBox.getAttribute('data-label');
    entryBox.placeholder = 'Search the catalogue by name';
    picker(entryBox, function(e){
      var changed = entryHidden.value !== e.id;
      entryHidden.value = e.id; entryBox.value = e.name;
      if(changed && currentType() === 'change'){
        location.href = '/suggestions/new?type=change&entry=' + encodeURIComponent(e.id);
        return;
      }
      refreshHints();
    });
    entryBox.addEventListener('input', function(){ if(!entryBox.value.trim()) entryHidden.value = ''; });
  }

  /* lineage picker: adds ticked chips */
  var relBox = $('#sg-related'), chips = $('.sg-chips', form);
  if(relBox && chips){
    relBox.removeAttribute('name');
    relBox.placeholder = 'Search for an earlier entry';
    picker(relBox, function(e){
      relBox.value = '';
      if($('input[name="f_related"][value="' + e.id + '"]', chips)) return;
      var lab = el('label', 'chip sg-chip'), cb = el('input');
      cb.type = 'checkbox'; cb.name = 'f_related'; cb.value = e.id; cb.checked = true;
      lab.appendChild(cb); lab.appendChild(document.createTextNode(' ' + e.name));
      chips.appendChild(lab);
    });
  }

  /* did-you-mean */
  var nameBox = $('input[name="f_name"]', form);
  var refreshHints = debounce(function(){
    var t = currentType();
    if(t === 'new'){
      var q = nameBox ? nameBox.value.trim() : '';
      if(q.length < 3){ renderHints({}, null); return; }
      search(q).then(function(res){ renderHints(res, null); });
    }else if(entryHidden && entryHidden.value){
      search(entryBox.value || entryHidden.value).then(function(res){ renderHints(res, entryHidden.value); });
    }else{
      renderHints({}, null);
    }
  }, 350);
  if(nameBox) nameBox.addEventListener('input', refreshHints);
  refreshHints();

  /* Turnstile: rendered explicitly; reset when the token expires or fails */
  var box = $('.sg-ts', form);
  if(box){
    whenTurnstile(function(ts){
      var wid = ts.render(box, {
        sitekey: box.getAttribute('data-sitekey'),
        action: box.getAttribute('data-action') || 'suggest',
        'expired-callback': function(){ ts.reset(wid); },
        'error-callback': function(){ setTimeout(function(){ ts.reset(wid); }, 1500); }
      });
    });
  }
}

function boot(){
  var tb = $('#sg-theme');
  if(tb) tb.addEventListener('click', function(){
    var cur = root.getAttribute('data-theme');
    var isDark = cur ? cur === 'dark' : !window.matchMedia('(prefers-color-scheme: light)').matches;
    var next = isDark ? 'light' : 'dark';
    root.setAttribute('data-theme', next);
    try{ localStorage.setItem('amiga-theme', next); }catch(err){}
  });
  setupVoteWidget();
  setupVotes();
  setupFilters();
  setupForm();
}
if(document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot);
else boot();
})();
