// The trip form works without this. Scripting shows the new-store fields only when
// needed, adds receipt rows, and suggests the total from the receipt and the store's tax.
(() => {
 const form = document.querySelector('.trip-form');
 if (!form) return;
 const field = name => form.elements.namedItem(name);
 const store = field('store');
 const newStore = form.querySelector('[data-new-store]');
 const body = form.querySelector('[data-receipt]');
 const addLine = form.querySelector('[data-add-line]');
 const suggest = form.querySelector('[data-suggest]');

 const decimals = cur => ['JPY', 'KRW'].includes((cur || '').trim().toUpperCase()) ? 0 : 2;
 const cents = (text, cur) => {
  const n = parseFloat((text || '').replace(/[$,\s]/g, ''));
  return Number.isFinite(n) ? Math.round(n * 10 ** decimals(cur)) : null;
 };
 const show = (amount, cur) => (amount / 10 ** decimals(cur)).toFixed(decimals(cur));

 const taxBP = () => {
  if (store.value === 'new') {
   const t = parseFloat(field('store_tax').value);
   return Number.isFinite(t) ? Math.round(t * 100) : null;
  }
  const t = store.selectedOptions[0]?.dataset.tax;
  return t ? Number(t) : null;
 };

 const update = () => {
  const isNew = store.value === 'new';
  newStore.hidden = !isNew;
  newStore.querySelectorAll('input').forEach(i => i.disabled = !isNew);
  const cur = field('currency').value;
  let subtotal = 0, any = false;
  body.querySelectorAll('tr').forEach(row => {
   const price = cents(row.querySelector('[name=line_price]').value, cur);
   const count = parseInt(row.querySelector('[name=line_count]').value || '1', 10);
   if (price !== null && count > 0) { subtotal += price * count; any = true; }
  });
  const bp = taxBP();
  if (!any) { suggest.hidden = true; return; }
  const total = bp === null ? subtotal : Math.round(subtotal * (10000 + bp) / 10000);
  suggest.hidden = false;
  suggest.dataset.total = show(total, cur);
  suggest.querySelector('[data-suggest-text]').textContent = bp === null
   ? `Receipt adds up to ${show(subtotal, cur)}.`
   : `Receipt adds up to ${show(subtotal, cur)}, ${show(total, cur)} with ${bp / 100}% tax.`;
 };

 store.addEventListener('change', () => {
  const cur = store.value === 'new' ? field('store_currency').value : store.selectedOptions[0]?.dataset.currency;
  if (cur) field('currency').value = cur;
  update();
 });
 field('store_currency').addEventListener('input', () => {
  if (store.value === 'new') field('currency').value = field('store_currency').value;
 });
 form.addEventListener('input', update);
 suggest.querySelector('[data-suggest-use]').addEventListener('click', () => { field('paid').value = suggest.dataset.total; });
 addLine.hidden = false;
 addLine.addEventListener('click', () => {
  const row = body.querySelector('tr:last-child').cloneNode(true);
  row.querySelectorAll('input').forEach(i => i.value = '');
  body.append(row);
  row.querySelector('[name=line_label]').focus();
 });
 update();
})();
