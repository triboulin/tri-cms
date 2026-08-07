// Short interactive API documentation on the global Administration > API
// page: clicking a row in the quick-reference tables preloads the try-it
// console (method/path/body), and "Envoyer" fires a real request -- using
// the current browser session by default (the same authenticate()
// middleware accepts either a session cookie or a Bearer token), or the
// token typed into the Jeton field to simulate an external API client.
// Endpoints are documented with a literal "{project_id}" placeholder in
// their path (the console is no longer scoped to one project, since tokens
// are global now): the "ID de projet" field's value is substituted in for
// every occurrence right before the request is sent, so the reference
// tables can stay project-agnostic while still being directly runnable.
(function () {
  var methodSel = document.getElementById('api-console-method');
  var pathInput = document.getElementById('api-console-path');
  var projectIdInput = document.getElementById('api-console-project-id');
  var tokenInput = document.getElementById('api-console-token');
  var bodyInput = document.getElementById('api-console-body');
  var sendBtn = document.getElementById('api-console-send');
  var responseBox = document.getElementById('api-console-response');
  if (!sendBtn || !responseBox) return;

  var statusEl = responseBox.querySelector('.tri-api-console-status');
  var bodyEl = responseBox.querySelector('.tri-api-console-body');

  document.querySelectorAll('.tri-api-doc-row').forEach(function (row) {
    row.addEventListener('click', function () {
      methodSel.value = row.getAttribute('data-method') || 'GET';
      pathInput.value = row.getAttribute('data-path') || '';
      bodyInput.value = row.getAttribute('data-body') || '';
      pathInput.focus();
      pathInput.scrollIntoView({ behavior: 'smooth', block: 'center' });
    });
  });

  sendBtn.addEventListener('click', function () {
    var method = methodSel.value;
    var path = pathInput.value.trim();
    if (!path) return;
    var projectId = projectIdInput ? projectIdInput.value.trim() : '';
    if (projectId) path = path.split('{project_id}').join(projectId);

    var headers = { Accept: 'application/json' };
    var token = tokenInput.value.trim();
    if (token) headers.Authorization = 'Bearer ' + token;

    var opts = { method: method, headers: headers, credentials: 'same-origin' };
    if (method !== 'GET' && method !== 'DELETE' && bodyInput.value.trim()) {
      headers['Content-Type'] = 'application/json';
      opts.body = bodyInput.value;
    }

    sendBtn.disabled = true;
    sendBtn.classList.add('tri-btn-loading');
    responseBox.hidden = false;
    statusEl.textContent = 'Envoi…';
    statusEl.className = 'tri-api-console-status';
    bodyEl.textContent = '';

    fetch(path, opts)
      .then(function (resp) {
        statusEl.textContent = resp.status + ' ' + resp.statusText;
        statusEl.className = 'tri-api-console-status ' + (resp.ok ? 'tri-api-console-status-ok' : 'tri-api-console-status-error');
        return resp.text();
      })
      .then(function (text) {
        try {
          bodyEl.textContent = JSON.stringify(JSON.parse(text), null, 2);
        } catch (e) {
          bodyEl.textContent = text;
        }
      })
      .catch(function (err) {
        statusEl.textContent = 'Erreur réseau';
        statusEl.className = 'tri-api-console-status tri-api-console-status-error';
        bodyEl.textContent = String(err);
      })
      .then(function () {
        sendBtn.disabled = false;
        sendBtn.classList.remove('tri-btn-loading');
      });
  });
})();
