// Uploads the media form via XMLHttpRequest instead of a plain synchronous
// POST so we can show a real progress bar (xhr.upload.onprogress) while a
// large file streams to the server. On completion we follow the same
// Post/Redirect/Get flow the rest of the app uses: the server still answers
// with a 303 redirect (including the flash message in the query string),
// and xhr.responseURL gives us that final URL to navigate to.
function triHumanBytes(n) {
  if (n < 1024) return n + ' o';
  var units = ['Ko', 'Mo', 'Go', 'To'];
  var div = 1024, exp = 0;
  for (var n2 = n / 1024; n2 >= 1024 && exp < units.length - 1; n2 /= 1024) {
    div *= 1024;
    exp += 1;
  }
  return (n / div).toFixed(1) + ' ' + units[exp];
}

function triUploadWithProgress(form) {
  var fileInput = form.querySelector('input[type=file]');
  if (!fileInput || !fileInput.files.length) {
    return true; // let native "required" validation handle the empty case
  }

  var bar = form.querySelector('.tri-upload-progress');
  var fill = form.querySelector('.tri-upload-progress-fill');
  var label = form.querySelector('.tri-upload-progress-label');
  var submitBtn = form.querySelector('button[type=submit]');

  bar.classList.add('show');
  fill.style.width = '0%';
  label.textContent = 'Envoi en cours…';
  if (submitBtn) submitBtn.disabled = true;

  var xhr = new XMLHttpRequest();
  xhr.upload.addEventListener('progress', function (e) {
    if (!e.lengthComputable) return;
    var pct = Math.round((e.loaded / e.total) * 100);
    fill.style.width = pct + '%';
    label.textContent = pct + '% (' + triHumanBytes(e.loaded) + ' / ' + triHumanBytes(e.total) + ')';
  });
  xhr.addEventListener('load', function () {
    if (xhr.status >= 200 && xhr.status < 400) {
      window.location = xhr.responseURL || window.location.href;
    } else {
      if (submitBtn) submitBtn.disabled = false;
      bar.classList.remove('show');
      label.textContent = '';
      alert('Échec du téléversement (code ' + xhr.status + ').');
    }
  });
  xhr.addEventListener('error', function () {
    if (submitBtn) submitBtn.disabled = false;
    bar.classList.remove('show');
    label.textContent = '';
    alert('Échec du téléversement : connexion interrompue.');
  });

  xhr.open('POST', form.action, true);
  xhr.send(new FormData(form));
  return false;
}
