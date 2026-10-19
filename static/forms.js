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
