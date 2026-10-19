// Sends covers one per request with progress, so a whole series stays under
// the upload limit and no request runs long. Without this script the forms
// send everything at once.
(() => {
 const status = document.querySelector('[data-covers-status]');
 if (!status) return;

 const show = (text, failed) => {
  status.hidden = false;
  status.className = failed ? 'error' : 'flash';
  status.textContent = text;
 };

 const post = async (url, body) => {
  try {
   const res = await fetch(url, {method: 'POST', body, headers: {Accept: 'application/json'}});
   return await res.json();
  } catch {
   return {saved: 0, errors: ['Could not reach the site. Try again.']};
  }
 };

 const busy = (form, on) => form.querySelectorAll('button').forEach(b => { b.disabled = on; });

 // Leaving stops the saving, so the browser asks first. It shows its own wording.
 const stay = e => { e.preventDefault(); e.returnValue = ''; };

 // run sends each job in turn and reloads the page when all of them worked.
 const run = async (form, jobs) => {
  let saved = 0;
  const errors = [];
  addEventListener('beforeunload', stay);
  for (const [i, job] of jobs.entries()) {
   show(`Saving cover ${i + 1} of ${jobs.length}… Do not close this tab.`);
   const res = await post(job.url, job.body);
   saved += res.saved || 0;
   errors.push(...(res.errors || []));
  }
  removeEventListener('beforeunload', stay);
  if (!errors.length) {
   location.href = location.pathname + '?ok=covers-saved';
   return;
  }
  show(`Saved ${saved} of ${jobs.length} covers. ${errors.join(' ')}`, true);
  busy(form, false);
 };

 const upload = document.querySelector('form[data-covers="upload"]');
 upload?.addEventListener('submit', e => {
  e.preventDefault();
  const files = [...upload.elements.cover.files];
  const number = upload.elements.volume_number.value.trim();
  if (number && files.length > 1) {
   show('Leave the volume number empty to add several covers.', true);
   return;
  }
  busy(upload, true);
  run(upload, files.map(file => {
   const body = new FormData();
   body.append('volume_number', number);
   body.append('cover', file);
   return {url: upload.action, body};
  }));
 });

 const find = document.querySelector('form[data-covers="find"]');
 find?.addEventListener('submit', async e => {
  e.preventDefault();
  busy(find, true);
  show('Looking for covers…');
  const link = find.elements.link.value.trim();
  const res = await post(find.action, new FormData(find));
  if (!res.covers) {
   show((res.errors || ['Reload the page and try again.']).join(' '), true);
   busy(find, false);
   return;
  }
  run(find, res.covers.map(c => {
   const body = new FormData();
   body.append('link', link);
   body.append('volume', c.volume);
   body.append('url', c.url);
   return {url: find.dataset.get, body};
  }));
 });
})();
