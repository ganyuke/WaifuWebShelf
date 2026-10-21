// Native forms work without this; scripting moves keyboard focus to the error.
(() => {
 const invalid = document.querySelector('[aria-invalid="true"]');
 if (!invalid) return;
 for (let parent = invalid.parentElement; parent; parent = parent.parentElement) {
  if (parent.tagName === 'DETAILS') parent.open = true;
 }
 invalid.focus();
})();

// Photos over the server's 25 MB limit are refused before sending, so the
// rest of the form isn't lost.
document.addEventListener('change', e => {
 const input = e.target;
 if (!(input instanceof HTMLInputElement) || input.type !== 'file') return;
 const big = [...input.files].some(f => f.size > 25 << 20);
 input.setCustomValidity(big ? 'Use a photo under 25 MB.' : '');
 if (big) input.reportValidity();
});

// A form is sent once. Clicking again while the page loads does nothing.
// Forms that send with fetch (covers.js) cancel the native submit and handle
// this themselves.
document.addEventListener('submit', e => {
 const form = e.target;
 if (e.defaultPrevented || form.method !== 'post') return;
 if (form.dataset.sending) {
  e.preventDefault();
  return;
 }
 form.dataset.sending = '1';
});

// Going back to a page restores it from the browser's cache with the mark
// still on, so clear it.
window.addEventListener('pageshow', e => {
 if (!e.persisted) return;
 for (const form of document.querySelectorAll('form[data-sending]')) delete form.dataset.sending;
});
