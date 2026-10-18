(() => {
 const form = document.querySelector('.item-form');
 if (!form) return;
 const field = name => form.elements.namedItem(name);
 const show = (selector, visible) => form.querySelectorAll(selector).forEach(el => {
  el.hidden = !visible;
  el.querySelectorAll('input, select, textarea, button').forEach(input => input.disabled = !visible);
 });
 const update = () => {
  const book = field('type').value === 'book';
  show('[data-book]', book); show('[data-merch]', !book);
  show('[data-new-series]', field('series').value === 'new');
  for (const opt of field('line').options) {
   opt.hidden = !!opt.dataset.series && opt.dataset.series !== field('series').value;
   opt.disabled = opt.hidden;
  }
  if (field('line').selectedOptions[0]?.hidden) field('line').value = '';
  if (field('series').value === 'new') field('line').value = 'new';
  show('[data-new-format]', book && field('line').value === 'new');
 };
 field('type').addEventListener('change', update);
 field('series').addEventListener('change', update);
 field('line').addEventListener('change', () => {
  update();
  const opt = field('line').selectedOptions[0];
  if (!opt?.dataset.currency) return;
  // On a trip the currency is the trip's, so only a list price in that currency fits.
  if (field('currency').type === 'hidden') {
   field('msrp').value = opt.dataset.currency === field('currency').value ? opt.dataset.msrp || '' : '';
   return;
  }
  field('currency').value = opt.dataset.currency;
  field('msrp').value = opt.dataset.msrp || '';
 });
 update();
 // Choosing a format fills in its prices, so the no-script button is not needed.
 form.querySelectorAll('[data-defaults]').forEach(el => { el.hidden = true; });
 form.querySelectorAll('.field-error').forEach(error => {
  const input = field(error.id.replace(/-error$/, ''));
  if (input) input.setAttribute('aria-invalid', 'true');
 });
 const invalid = form.querySelector('[aria-invalid="true"]');
 if (invalid) invalid.focus();
})();
