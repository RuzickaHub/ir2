// static/main.js
// Minimal placeholder JS to call /api for listing images
const API = '/api';

async function loadImages() {
  const res = await fetch(API);
  const imgs = await res.json();
  const grid = document.getElementById('grid');
  grid.innerHTML = '';
  imgs.forEach(i => {
    const d = document.createElement('div');
    d.className = 'tile';
    d.innerHTML = `<img src="${i.url}" alt="${i.name}" loading="lazy"><div class="meta">${i.width}×${i.height}</div>`;
    grid.appendChild(d);
  });
}

document.addEventListener('DOMContentLoaded', ()=> {
  loadImages();
  const input = document.getElementById('upload');
  if (input) {
    input.addEventListener('change', async (e) => {
      const files = Array.from(e.target.files);
      for (const f of files) {
        const fd = new FormData();
        fd.append('file', f);
        const resp = await fetch(API, { method: 'POST', body: fd });
        const j = await resp.json();
        console.log('upload', j);
      }
      await loadImages();
    });
  }
});
