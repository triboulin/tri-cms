// Content grid row-click editing: clicking (or Enter/Space on) a row opens
// the existing edit form in a modal instead of navigating to a separate
// page, and the modal also carries the delete action -- replacing the
// per-row action icons the grid used to have at the end of every line.
(function () {
  var modal = document.getElementById('content-edit-modal');
  if (!modal) return;

  var body = modal.querySelector('.tri-content-modal-body');
  var closeBtn = modal.querySelector('.tri-content-modal-close');
  var deleteForm = modal.querySelector('.tri-content-modal-delete-form');
  var loadToken = 0;

  function openModal() {
    modal.hidden = false;
  }

  function closeModal() {
    modal.hidden = true;
    body.innerHTML = '';
  }

  function extractCard(html) {
    var doc = new DOMParser().parseFromString(html, 'text/html');
    return doc.querySelector('.tri-card');
  }

  function loadRow(row) {
    var editURL = row.getAttribute('data-edit-url');
    var deleteURL = row.getAttribute('data-delete-url');
    if (!editURL) return;

    var myToken = ++loadToken;
    body.innerHTML = '<p class="tri-empty">Chargement…</p>';
    if (deleteForm && deleteURL) deleteForm.setAttribute('action', deleteURL);
    openModal();

    fetch(editURL, { credentials: 'same-origin' })
      .then(function (resp) {
        if (!resp.ok) throw new Error('http ' + resp.status);
        return resp.text();
      })
      .then(function (html) {
        if (myToken !== loadToken) return; // a newer row was opened meanwhile
        var card = extractCard(html);
        body.innerHTML = '';
        if (!card) {
          body.innerHTML = '<p class="tri-empty">Impossible de charger ce contenu.</p>';
          return;
        }
        body.appendChild(card);

        // The fetched form's "Annuler" link would navigate away; inside the
        // modal it should just close it instead.
        var cancelLink = card.querySelector('a.tri-btn-ghost');
        if (cancelLink) {
          cancelLink.addEventListener('click', function (e) {
            e.preventDefault();
            closeModal();
          });
        }

        if (window.triInitRichEditors) window.triInitRichEditors(card);
        if (window.triInitMediaPickers) window.triInitMediaPickers(card);
      })
      .catch(function () {
        if (myToken !== loadToken) return;
        body.innerHTML = '<p class="tri-empty">Erreur de chargement. <a href="' + editURL + '">Ouvrir la page complète</a>.</p>';
      });
  }

  document.querySelectorAll('tr[data-edit-url]').forEach(function (row) {
    row.addEventListener('click', function () { loadRow(row); });
    row.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        loadRow(row);
      }
    });
  });

  if (closeBtn) closeBtn.addEventListener('click', closeModal);
  modal.addEventListener('click', function (e) {
    if (e.target === modal) closeModal();
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && !modal.hidden) closeModal();
  });
})();
