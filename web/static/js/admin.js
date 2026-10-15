/* Owner panel: small progressive enhancements. Every form works without it. */
(function () {
  'use strict';

  /* Related-entries picker: reveal a filter box that hides non-matching
     unchecked options. Checked options always stay visible. */
  function initPicker(root) {
    var input = root.querySelector('.admin-picker-filter');
    var labels = Array.prototype.slice.call(root.querySelectorAll('.admin-picker-list label'));
    if (!input || !labels.length) return;
    input.hidden = false;
    input.addEventListener('input', function () {
      var q = input.value.trim().toLowerCase();
      labels.forEach(function (label) {
        var box = label.querySelector('input');
        var match = !q || label.textContent.toLowerCase().indexOf(q) !== -1;
        label.hidden = !(match || (box && box.checked));
      });
    });
  }

  /* Ask before leaving a form with unsaved edits. */
  function guardUnsaved(form) {
    var dirty = false;
    form.addEventListener('input', function () { dirty = true; });
    form.addEventListener('submit', function () { dirty = false; });
    window.addEventListener('beforeunload', function (e) {
      if (dirty) { e.preventDefault(); e.returnValue = ''; }
    });
  }

  /* "Select all" for the bulk forms: toggles every ids checkbox that
     belongs to the form named by data-select-all (form attribute included). */
  function initSelectAll(all) {
    var formID = all.getAttribute('data-select-all');
    function boxes() {
      return Array.prototype.filter.call(document.querySelectorAll('input[type=checkbox][name=ids]'), function (b) {
        return b.form && b.form.id === formID;
      });
    }
    if (!boxes().length) return;
    all.hidden = false;
    var label = all.closest('label');
    if (label) label.hidden = false;
    all.addEventListener('change', function () {
      boxes().forEach(function (b) { b.checked = all.checked; });
    });
  }

  function init() {
    Array.prototype.forEach.call(document.querySelectorAll('[data-select-all]'), initSelectAll);
    Array.prototype.forEach.call(document.querySelectorAll('[data-picker]'), initPicker);
    Array.prototype.forEach.call(document.querySelectorAll('form.admin-entry-form'), guardUnsaved);
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
  else init();
})();
