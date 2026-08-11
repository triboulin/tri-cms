// Media picker: replaces a bare <select> of media IDs (indistinguishable
// filenames, no way to tell files apart without opening each one) with a
// modal showing every media as a thumbnail, for both single (media-select)
// and multiple (media-multiselect) content fields. See content_form.html for
// the markup this wires up.
//
// Exposed as window.triInitMediaPickers(root) so it can be re-run against a
// subtree (e.g. a form injected into the content-row edit modal after the
// page has already loaded), not just the whole document on first paint.
(function () {
  function openModal(modal) {
    modal.hidden = false;
    var filter = modal.querySelector('.tri-media-picker-filter');
    if (filter) {
      filter.value = '';
      filterItems(modal, '');
      filter.focus();
    }
  }

  function closeModal(modal) {
    modal.hidden = true;
  }

  function filterItems(modal, query) {
    query = query.trim().toLowerCase();
    modal.querySelectorAll('.tri-media-picker-item').forEach(function (item) {
      var label = (item.getAttribute('data-label') || '').toLowerCase();
      item.style.display = !query || label.indexOf(query) !== -1 ? '' : 'none';
    });
  }

  function cssEscape(value) {
    if (window.CSS && CSS.escape) return CSS.escape(value);
    return String(value).replace(/["\\]/g, '\\$&');
  }

  function buildChip(key, value, label, previewURL, isImage, isVideo, multi) {
    var chip = document.createElement('div');
    chip.className = 'tri-media-picker-chip';
    chip.setAttribute('data-value', value);

    var thumb = document.createElement('span');
    thumb.className = 'tri-media-picker-chip-thumb';
    if (isImage) {
      var img = document.createElement('img');
      img.src = previewURL;
      img.alt = label;
      thumb.appendChild(img);
    } else {
      var icon = document.createElement('span');
      icon.className = 'material-icons';
      icon.setAttribute('aria-hidden', 'true');
      icon.textContent = isVideo ? 'videocam' : 'insert_drive_file';
      thumb.appendChild(icon);
    }
    chip.appendChild(thumb);

    var labelSpan = document.createElement('span');
    labelSpan.className = 'tri-media-picker-chip-label';
    labelSpan.textContent = label;
    chip.appendChild(labelSpan);

    if (multi) {
      var hidden = document.createElement('input');
      hidden.type = 'hidden';
      hidden.name = key;
      hidden.value = value;
      chip.appendChild(hidden);
    }

    var removeBtn = document.createElement('button');
    removeBtn.type = 'button';
    removeBtn.className = 'tri-media-picker-remove';
    removeBtn.setAttribute('aria-label', 'Retirer');
    removeBtn.textContent = '\u00d7';
    chip.appendChild(removeBtn);

    return chip;
  }

  window.triInitMediaPickers = function (root) {
    root = root || document;
    root.querySelectorAll('.tri-media-picker').forEach(function (picker) {
      if (picker.dataset.triPickerInit) return;
      picker.dataset.triPickerInit = '1';

      var key = picker.getAttribute('data-key');
      var multi = picker.getAttribute('data-multi') === 'true';
      var uploadUrl = picker.getAttribute('data-upload-url');
      var preview = picker.querySelector('.tri-media-picker-preview');
      var openBtn = picker.querySelector('.tri-media-picker-open');
      var modal = picker.querySelector('.tri-media-picker-modal');
      if (!modal) return;
      var closeBtn = modal.querySelector('.tri-media-picker-close');
      var filterInput = modal.querySelector('.tri-media-picker-filter');
      var valueInput = picker.querySelector('.tri-media-picker-value');
      var grid = modal.querySelector('.tri-media-picker-grid');
      var uploadBtn = modal.querySelector('.tri-media-picker-upload-btn');
      var uploadInput = modal.querySelector('.tri-media-picker-upload-input');
      var uploadError = modal.querySelector('.tri-media-picker-upload-error');

      if (openBtn) openBtn.addEventListener('click', function () { openModal(modal); });
      if (closeBtn) closeBtn.addEventListener('click', function () { closeModal(modal); });
      modal.addEventListener('click', function (e) {
        if (e.target === modal) closeModal(modal);
      });
      if (filterInput) {
        filterInput.addEventListener('input', function () { filterItems(modal, filterInput.value); });
      }

      // Picking an item: delegated on the grid (not bound per-item at init)
      // so a thumbnail added later by the upload flow below is clickable
      // without any extra wiring.
      function selectItem(item) {
        var value = item.getAttribute('data-value');
        var label = item.getAttribute('data-label') || '';
        var previewURL = item.getAttribute('data-preview') || '';
        var isImage = item.getAttribute('data-is-image') === 'true';
        var isVideo = item.getAttribute('data-is-video') === 'true';

        if (!multi) {
          modal.querySelectorAll('.tri-media-picker-item.selected').forEach(function (el) {
            el.classList.remove('selected');
          });
          item.classList.add('selected');
          if (valueInput) valueInput.value = value;
          preview.innerHTML = '';
          preview.appendChild(buildChip(key, value, label, previewURL, isImage, isVideo, false));
          closeModal(modal);
          return;
        }

        var existing = preview.querySelector('.tri-media-picker-chip[data-value="' + cssEscape(value) + '"]');
        if (existing) {
          existing.remove();
          item.classList.remove('selected');
        } else {
          item.classList.add('selected');
          preview.appendChild(buildChip(key, value, label, previewURL, isImage, isVideo, true));
        }
      }

      if (grid) {
        grid.addEventListener('click', function (e) {
          var item = e.target.closest('.tri-media-picker-item');
          if (item) selectItem(item);
        });
      }

      preview.addEventListener('click', function (e) {
        var btn = e.target.closest('.tri-media-picker-remove');
        if (!btn) return;
        var chip = btn.closest('.tri-media-picker-chip');
        if (!chip) return;
        var value = chip.getAttribute('data-value');
        chip.remove();
        if (!multi && valueInput) valueInput.value = '';
        var modalItem = modal.querySelector('.tri-media-picker-item[data-value="' + cssEscape(value) + '"]');
        if (modalItem) modalItem.classList.remove('selected');
      });

      // Upload a file without leaving the surrounding form: POST to
      // data-upload-url (htmxUploadMediaForPicker), which renders exactly
      // one partial:media_picker_item fragment back -- insert it at the
      // front of the grid and select it immediately, same as clicking an
      // existing thumbnail would.
      if (uploadBtn && uploadInput && uploadUrl) {
        uploadBtn.addEventListener('click', function () { uploadInput.click(); });
        uploadInput.addEventListener('change', function () {
          var file = uploadInput.files[0];
          if (!file) return;
          if (uploadError) { uploadError.hidden = true; uploadError.textContent = ''; }
          uploadBtn.disabled = true;
          var originalLabel = uploadBtn.innerHTML;
          uploadBtn.innerHTML = '<span class="material-icons" aria-hidden="true">hourglass_top</span> Envoi…';

          var formData = new FormData();
          formData.append('file', file);
          fetch(uploadUrl, { method: 'POST', body: formData })
            .then(function (resp) {
              if (!resp.ok) return resp.text().then(function (msg) { throw new Error(msg || 'Échec du téléversement.'); });
              return resp.text();
            })
            .then(function (html) {
              var empty = modal.querySelector('.tri-media-picker-empty');
              if (empty) empty.remove();
              var tmp = document.createElement('div');
              tmp.innerHTML = html.trim();
              var newItem = tmp.firstElementChild;
              if (grid && newItem) {
                grid.insertBefore(newItem, grid.firstChild);
                filterItems(modal, filterInput ? filterInput.value : '');
                selectItem(newItem);
              }
            })
            .catch(function (err) {
              if (uploadError) {
                uploadError.textContent = err.message || 'Échec du téléversement.';
                uploadError.hidden = false;
              }
            })
            .finally(function () {
              uploadBtn.disabled = false;
              uploadBtn.innerHTML = originalLabel;
              uploadInput.value = '';
            });
        });
      }
    });
  };

  document.addEventListener('DOMContentLoaded', function () {
    window.triInitMediaPickers(document);
  });

  document.addEventListener('keydown', function (e) {
    if (e.key !== 'Escape') return;
    document.querySelectorAll('.tri-media-picker-modal:not([hidden])').forEach(closeModal);
  });
})();
