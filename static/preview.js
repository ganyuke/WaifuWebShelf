// Hovering a small picture shows a card with a bigger picture and its
// details, fetched from the same path under /preview. Mouse only.
(() => {
 if (!matchMedia('(hover: hover) and (pointer: fine)').matches) return;
 const cards = new Map();
 const card = document.createElement('div');
 card.className = 'pv';
 card.hidden = true;
 card.setAttribute('role', 'tooltip');
 document.body.append(card);
 let timer = null;
 let current = null;

 const load = path => {
  if (!cards.has(path)) {
   cards.set(path, fetch('/preview' + path).then(r => (r.ok ? r.text() : '')).catch(() => ''));
  }
  return cards.get(path);
 };

 // Beside the picture, flipped to the other side or nudged up when it would leave the window.
 const place = el => {
  const r = el.getBoundingClientRect();
  const w = card.offsetWidth;
  const h = card.offsetHeight;
  let x = r.right + 12;
  if (x + w > innerWidth - 8) x = r.left - 12 - w;
  x = Math.max(8, x);
  const y = Math.max(8, Math.min(r.top, innerHeight - 8 - h));
  card.style.left = x + scrollX + 'px';
  card.style.top = y + scrollY + 'px';
 };

 const hide = () => {
  clearTimeout(timer);
  current = null;
  card.hidden = true;
 };

 const show = async el => {
  const html = await load(el.dataset.preview);
  if (current !== el || !html) return;
  card.innerHTML = html;
  card.hidden = false;
  place(el);
  const img = card.querySelector('img');
  if (img && !img.complete) img.addEventListener('load', () => { if (current === el) place(el); }, { once: true });
 };

 // The card replaces the browser's own tooltip, so keep the text for screen readers only.
 document.querySelectorAll('[data-preview][title]').forEach(el => {
  if (!el.hasAttribute('aria-label')) el.setAttribute('aria-label', el.title);
  el.removeAttribute('title');
 });

 document.addEventListener('pointerover', e => {
  if (e.pointerType !== 'mouse') return;
  const el = e.target.closest('[data-preview]');
  if (!el || el === current) return;
  hide();
  current = el;
  timer = setTimeout(() => show(el), 300);
 });
 document.addEventListener('pointerout', e => {
  if (!current || current.contains(e.relatedTarget)) return;
  if (e.target.closest('[data-preview]') === current) hide();
 });
 document.addEventListener('pointerdown', hide);
 document.addEventListener('keydown', e => { if (e.key === 'Escape') hide(); });
})();
