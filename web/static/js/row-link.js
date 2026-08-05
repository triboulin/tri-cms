// Generic "whole row is a link" behavior: a <tr data-href="..."> navigates
// there with a normal full-page load on click (or Enter/Space when
// focused) -- for list rows whose only action is "open this", so there's no
// need for a lingering button at the end of the line.
(function () {
  document.querySelectorAll('tr[data-href]').forEach(function (row) {
    function go() {
      window.location.href = row.getAttribute('data-href');
    }
    row.addEventListener('click', go);
    row.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        go();
      }
    });
  });
})();
