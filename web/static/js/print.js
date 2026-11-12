// Print view helper: lets the page be printed (saved as PDF) when opened
// directly, and prints automatically when requested with ?autoprint=1.
(function () {
  'use strict';
  var button = document.getElementById('print-now');
  if (button) {
    button.addEventListener('click', function () { window.print(); });
  }
  if (new URLSearchParams(window.location.search).get('autoprint') === '1') {
    window.addEventListener('load', function () { window.print(); });
  }
})();
