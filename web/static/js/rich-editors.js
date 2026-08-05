// Wires a WYSIWYG editor onto RichText_HTML fields (Quill) and a
// live-preview Markdown editor onto RichText_MD fields (EasyMDE). Both
// libraries hide the original <textarea> and manage their own UI, so each
// editor is kept in sync with its textarea on every change (not just on
// submit) so native `required` validation keeps working.
//
// Exposed as window.triInitRichEditors(root) so it can be re-run against a
// subtree (e.g. a form injected into the content-row edit modal after the
// page has already loaded), not just the whole document on first paint.
window.triInitRichEditors = function (root) {
  root = root || document;

  root.querySelectorAll('textarea.tri-richtext-html').forEach(function (ta) {
    if (ta.dataset.triRichInit) return;
    ta.dataset.triRichInit = '1';
    var mount = document.createElement('div');
    mount.className = 'tri-quill-mount';
    ta.style.display = 'none';
    ta.parentNode.insertBefore(mount, ta);

    var quill = new Quill(mount, {
      theme: 'snow',
      placeholder: ta.getAttribute('placeholder') || '',
      modules: {
        toolbar: [
          ['bold', 'italic', 'underline', 'strike'],
          [{ header: [2, 3, false] }],
          [{ list: 'ordered' }, { list: 'bullet' }],
          ['link', 'blockquote', 'code-block'],
          ['clean'],
        ],
      },
    });
    if (ta.value) {
      quill.root.innerHTML = ta.value;
    }
    quill.on('text-change', function () {
      ta.value = quill.root.innerHTML;
    });
  });

  root.querySelectorAll('textarea.tri-richtext-md').forEach(function (ta) {
    if (ta.dataset.triRichInit) return;
    ta.dataset.triRichInit = '1';
    var mde = new EasyMDE({
      element: ta,
      spellChecker: false,
      status: false,
      placeholder: ta.getAttribute('placeholder') || '',
    });
    mde.codemirror.on('change', function () {
      ta.value = mde.value();
    });
  });
};

document.addEventListener('DOMContentLoaded', function () {
  window.triInitRichEditors(document);
});
