// ==UserScript==
// @name         Kobo full-size covers
// @namespace    org.nextcolor.waifuwebshelf
// @version      1.0
// @description  Download a Kobo cover at full size from any kobo.com page.
// @match        https://www.kobo.com/*
// @grant        GM.xmlHttpRequest
// @connect      cdn.kobo.com
// ==/UserScript==

// Hovering a cover shows a button that downloads it at full size. Covers are
// served as book-images/ID/WIDTH/HEIGHT/QUALITY/GREYSCALE/NAME.EXT. WIDTH
// upscales past the source, so 0/0 asks for the stored size, and quality
// 100 keeps JPEG re-encoding loss down. See also scripts/kobo-covers.sh.
//
// The CDN sends no CORS headers and Greasemonkey 4 has no download API, so
// the image is fetched with GM.xmlHttpRequest and saved from a blob link.
// Tampermonkey and Violentmonkey support the same call.
(function () {
  'use strict';

  const coverRe = /cdn\.kobo\.com\/book-images\/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(\/[^?#]*)?/i;

  function coverOf(img) {
    const m = coverRe.exec(img.currentSrc || img.src || '');
    if (!m) return null;
    const last = (m[2] || '').split('/').pop().replace(/\.[^.]*$/, '');
    const name = last.replace(/[^A-Za-z0-9._-]+/g, '-').replace(/^[-.]+|-+$/g, '') || m[1];
    return {
      url: `https://cdn.kobo.com/book-images/${m[1]}/0/0/100/False/${name}.jpg`,
      name: `${name}.jpg`,
    };
  }

  const btn = document.createElement('button');
  btn.type = 'button';
  btn.textContent = 'Download full size';
  Object.assign(btn.style, {
    position: 'fixed',
    zIndex: 2147483647,
    display: 'none',
    padding: '4px 8px',
    font: '13px/1.3 system-ui, sans-serif',
    color: '#fff',
    background: 'rgba(0, 0, 0, 0.75)',
    border: '0',
    borderRadius: '4px',
    cursor: 'pointer',
  });
  document.body.appendChild(btn);

  let current = null;

  function hide() {
    btn.style.display = 'none';
    current = null;
  }

  document.addEventListener('mouseover', (e) => {
    if (e.target === btn) return;
    const img = e.target.closest ? e.target.closest('img') : null;
    const cover = img && coverOf(img);
    if (!cover) {
      hide();
      return;
    }
    current = cover;
    const r = img.getBoundingClientRect();
    btn.style.left = `${Math.max(r.left + 6, 0)}px`;
    btn.style.top = `${Math.max(r.top + 6, 0)}px`;
    btn.textContent = 'Download full size';
    btn.style.display = 'block';
  }, true);

  window.addEventListener('scroll', hide, { passive: true });

  btn.addEventListener('click', (e) => {
    e.preventDefault();
    e.stopPropagation();
    if (!current) return;
    const { url, name } = current;
    btn.textContent = 'Downloading…';
    const failed = () => { btn.textContent = 'Could not download'; };
    GM.xmlHttpRequest({
      method: 'GET',
      url,
      responseType: 'blob',
      onload: (res) => {
        if (res.status !== 200 || !res.response) {
          failed();
          return;
        }
        const href = URL.createObjectURL(res.response);
        const a = document.createElement('a');
        a.href = href;
        a.download = name;
        document.body.appendChild(a);
        a.click();
        a.remove();
        setTimeout(() => URL.revokeObjectURL(href), 60000);
        btn.textContent = 'Saved';
      },
      onerror: failed,
    });
  });
})();
