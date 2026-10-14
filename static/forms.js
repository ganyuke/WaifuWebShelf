// Native forms work without this; scripting moves keyboard focus to the error.
(() => {
 const invalid = document.querySelector('[aria-invalid="true"]');
 if (!invalid) return;
 for (let parent = invalid.parentElement; parent; parent = parent.parentElement) {
  if (parent.tagName === 'DETAILS') parent.open = true;
 }
 invalid.focus();
})();
