// Site-wide small behaviors, loaded on every page from partial:head:
//  - breadcrumb dropdowns: open on click/keyboard AND on hover. Hover uses a
//    short delayed close (not bare CSS :hover) so moving the pointer from
//    the label to the dropdown across the small gap between them doesn't
//    slam the menu shut before it arrives -- a real bug that a plain
//    ":hover { display:block }" rule used to cause (see app.css). Click
//    still works standalone for touch/keyboard, where hover doesn't apply.
//  - a single styled confirmation modal replacing every native confirm()/
//    prompt() call, including the "retype the exact name" project-delete
//    flow.
//  - a generic "disable + spinner" state on form submit buttons.
//  - a small reusable client-side table filter (opt-in via data attributes).
//  - collapsible "create" panels: a button on a section title toggles a
//    hidden form instead of that form permanently occupying its own row.
//  - one-click file selection: a button opens a hidden native file input
//    and the form submits itself the instant a file is chosen -- no
//    separate "confirm"/"upload" click.

(function () {
  'use strict';

  // ---- Breadcrumb dropdowns: click/keyboard, not just :hover ------------

  // Touch devices synthesize a mouseenter right before the click on a first
  // tap, so attaching real hover listeners there made the first tap open
  // the menu and the immediately-following click close it again -- a second
  // tap was needed to actually get in. Only wire up hover on pointers that
  // truly support it (a mouse), so touch relies on click alone.
  var supportsHover = window.matchMedia && window.matchMedia('(hover: hover) and (pointer: fine)').matches;

  function closeAllCrumbs(except) {
    document.querySelectorAll('.tri-crumb.open').forEach(function (c) {
      if (c === except) return;
      c.classList.remove('open');
      var caret = c.querySelector('.tri-crumb-caret');
      if (caret) caret.setAttribute('aria-expanded', 'false');
    });
  }

  document.querySelectorAll('.tri-crumb').forEach(function (crumb) {
    var hoverEl = crumb; // .tri-crumb and .tri-crumb-hover are the same element
    var caret = crumb.querySelector('.tri-crumb-caret');
    if (caret) {
      caret.setAttribute('tabindex', '0');
      caret.setAttribute('role', 'button');
      caret.setAttribute('aria-haspopup', 'true');
      caret.setAttribute('aria-expanded', 'false');
    }

    function toggle() {
      var willOpen = !crumb.classList.contains('open');
      closeAllCrumbs(willOpen ? crumb : null);
      crumb.classList.toggle('open', willOpen);
      if (caret) caret.setAttribute('aria-expanded', willOpen ? 'true' : 'false');
    }

    hoverEl.addEventListener('click', function (e) {
      // Let real navigation links behave normally; only intercept clicks on
      // the label/caret area itself.
      if (e.target.closest('a')) return;
      e.stopPropagation();
      toggle();
    });
    if (caret) {
      caret.addEventListener('keydown', function (e) {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          toggle();
        }
      });
    }

    // Forgiving hover: open immediately on enter, but close on leave only
    // after a short grace period, cancelled if the pointer re-enters (either
    // back onto the label or onto the dropdown itself, since the dropdown is
    // a DOM child of this same element) before it fires.
    if (supportsHover) {
      var closeTimer = null;
      function clearCloseTimer() {
        if (closeTimer) {
          clearTimeout(closeTimer);
          closeTimer = null;
        }
      }
      hoverEl.addEventListener('mouseenter', function () {
        clearCloseTimer();
        closeAllCrumbs(crumb);
        crumb.classList.add('open');
        if (caret) caret.setAttribute('aria-expanded', 'true');
      });
      hoverEl.addEventListener('mouseleave', function () {
        clearCloseTimer();
        closeTimer = setTimeout(function () {
          crumb.classList.remove('open');
          if (caret) caret.setAttribute('aria-expanded', 'false');
          closeTimer = null;
        }, 300);
      });
    }
  });

  document.addEventListener('click', function () {
    closeAllCrumbs();
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') {
      closeAllCrumbs();
      document.querySelectorAll('details.tri-popover[open]').forEach(function (d) {
        d.removeAttribute('open');
      });
    }
  });

  // ---- Confirmation modal, replacing native confirm()/prompt() ----------

  var modalRoot = null;
  function ensureModal() {
    if (modalRoot) return modalRoot;
    modalRoot = document.createElement('div');
    modalRoot.className = 'tri-modal-overlay';
    modalRoot.innerHTML =
      '<div class="tri-modal" role="alertdialog" aria-modal="true" aria-labelledby="tri-modal-title">' +
      '  <h3 id="tri-modal-title"><span class="material-icons" aria-hidden="true">warning</span> Confirmation</h3>' +
      '  <p class="tri-modal-message"></p>' +
      '  <div class="tri-modal-name-wrap" style="display:none;">' +
      '    <label for="tri-modal-name-input">Tapez le nom exact pour confirmer</label>' +
      '    <input type="text" id="tri-modal-name-input" autocomplete="off">' +
      '    <p class="tri-modal-name-error" style="display:none;">Le nom ne correspond pas.</p>' +
      '  </div>' +
      '  <div class="tri-modal-actions">' +
      '    <button type="button" class="tri-btn tri-btn-ghost" data-action="cancel">Annuler</button>' +
      '    <button type="button" class="tri-btn tri-btn-danger" data-action="confirm">Confirmer</button>' +
      '  </div>' +
      '</div>';
    document.body.appendChild(modalRoot);
    return modalRoot;
  }

  // Shows the modal and calls onConfirm() if the user validates. When
  // expectedName is set, the confirm button stays disabled until the typed
  // value matches it exactly (the project-delete "retype to confirm" flow).
  function showConfirmModal(message, expectedName, onConfirm) {
    var root = ensureModal();
    root.classList.add('show');
    root.querySelector('.tri-modal-message').textContent = message;
    var nameWrap = root.querySelector('.tri-modal-name-wrap');
    var nameInput = root.querySelector('#tri-modal-name-input');
    var nameError = root.querySelector('.tri-modal-name-error');
    var confirmBtn = root.querySelector('[data-action="confirm"]');
    var cancelBtn = root.querySelector('[data-action="cancel"]');
    nameError.style.display = 'none';
    nameInput.value = '';

    if (expectedName) {
      nameWrap.style.display = 'block';
      confirmBtn.disabled = true;
      nameInput.oninput = function () {
        confirmBtn.disabled = nameInput.value !== expectedName;
      };
      setTimeout(function () { nameInput.focus(); }, 0);
    } else {
      nameWrap.style.display = 'none';
      confirmBtn.disabled = false;
      setTimeout(function () { confirmBtn.focus(); }, 0);
    }

    function close() {
      root.classList.remove('show');
      confirmBtn.onclick = null;
      cancelBtn.onclick = null;
      document.removeEventListener('keydown', onKeydown);
    }
    function onKeydown(e) {
      if (e.key === 'Escape') close();
    }
    document.addEventListener('keydown', onKeydown);

    cancelBtn.onclick = close;
    root.onclick = function (e) {
      if (e.target === root) close();
    };
    confirmBtn.onclick = function () {
      if (expectedName && nameInput.value !== expectedName) {
        nameError.style.display = 'block';
        return;
      }
      close();
      onConfirm();
    };
  }

  document.addEventListener('submit', function (e) {
    var form = e.target;
    if (!(form instanceof HTMLFormElement)) return;
    if (form.dataset.confirmed === 'true') return; // programmatic re-submit after confirmation

    var message = form.dataset.confirm;
    var expectedName = form.dataset.confirmName;
    if (!message && !expectedName) return;

    e.preventDefault();
    showConfirmModal(
      message || 'Cette action est irréversible. Tapez le nom exact pour confirmer.',
      expectedName || null,
      function () {
        if (expectedName) {
          var hidden = form.querySelector('input[name="confirm_name"]');
          if (hidden) hidden.value = expectedName;
        }
        form.dataset.confirmed = 'true';
        form.requestSubmit ? form.requestSubmit() : form.submit();
      }
    );
  });

  // ---- Generic "disable + spinner" on submit -----------------------------

  document.addEventListener(
    'submit',
    function (e) {
      var form = e.target;
      if (!(form instanceof HTMLFormElement)) return;
      // Forms gated by the confirm modal only actually submit once already
      // confirmed; skip the first (intercepted) dispatch.
      if ((form.dataset.confirm || form.dataset.confirmName) && form.dataset.confirmed !== 'true') return;
      var btn = form.querySelector('button[type="submit"]');
      if (!btn || btn.disabled) return;
      btn.disabled = true;
      btn.classList.add('tri-btn-loading');
    },
    true
  );

  // ---- Simple client-side table filter (opt-in) --------------------------
  // <input data-table-filter="#some-table"> filters that table's <tbody> rows
  // by plain substring match against each row's text content.

  document.querySelectorAll('[data-table-filter]').forEach(function (input) {
    var table = document.querySelector(input.dataset.tableFilter);
    if (!table) return;
    var rows = Array.prototype.slice.call(table.querySelectorAll('tbody tr'));
    input.addEventListener('input', function () {
      var q = input.value.trim().toLowerCase();
      var visible = 0;
      rows.forEach(function (row) {
        var match = !q || row.textContent.toLowerCase().indexOf(q) !== -1;
        row.style.display = match ? '' : 'none';
        if (match) visible += 1;
      });
      var scope = input.closest('.tri-content') || document;
      var empty = scope.querySelector('.tri-filter-empty');
      if (empty) empty.style.display = visible === 0 ? '' : 'none';
    });
  });

  // ---- Collapsible "create" panels ---------------------------------------
  // A "Nouveau X"/"Ajouter" button at the end of a section title
  // (data-toggle-target="panel-id") shows/hides the matching panel, which
  // starts `hidden` in the markup -- instead of that create form always
  // taking up a full row of its own above/below the list it adds to.
  // A link to #panel-id from elsewhere (e.g. Collections' "Nouveau schéma"
  // jumping into Conception) still works: the panel opens itself on load
  // when the URL hash names it, same as it would have been visible before.

  document.querySelectorAll('[data-toggle-target]').forEach(function (btn) {
    var target = document.getElementById(btn.getAttribute('data-toggle-target'));
    if (!target) return;
    btn.addEventListener('click', function () {
      target.hidden = !target.hidden;
      if (!target.hidden) {
        target.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        var firstField = target.querySelector('input, select, textarea');
        if (firstField) firstField.focus();
      }
    });
  });

  if (location.hash) {
    var hashTarget = document.getElementById(location.hash.slice(1));
    if (hashTarget && hashTarget.hidden) {
      hashTarget.hidden = false;
      hashTarget.scrollIntoView({ block: 'start' });
    }
  }

  // ---- One-click file selection ------------------------------------------
  // A button with data-file-trigger="<input id>" opens that hidden native
  // file input on click; the moment a file is chosen, its form submits
  // itself -- picking a file *is* the action, there's no separate button to
  // confirm it with. Whatever the form's own submit handling already does
  // (a plain POST/redirect, or a custom handler like triUploadWithProgress)
  // still runs exactly the same way, since this only ever triggers a real
  // submit event.

  document.querySelectorAll('[data-file-trigger]').forEach(function (btn) {
    var input = document.getElementById(btn.getAttribute('data-file-trigger'));
    if (!input) return;
    btn.addEventListener('click', function () { input.click(); });
    input.addEventListener('change', function () {
      if (!input.files || !input.files.length) return;
      var form = input.closest('form');
      if (form) form.requestSubmit ? form.requestSubmit() : form.submit();
    });
  });
})();
