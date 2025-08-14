const canvas = document.getElementById('grid');
const ctx = canvas.getContext('2d');
const statusEl = document.getElementById('status');

let lastWidth = 0;
let lastHeight = 0;

function resizeCanvas(width, height) {
  const size = Math.min(window.innerWidth, window.innerHeight - 60);
  canvas.width = size;
  canvas.height = size;
}

function drawGrid(data) {
  if (!data) return;
  if (data.width !== lastWidth || data.height !== lastHeight) {
    lastWidth = data.width; lastHeight = data.height;
    resizeCanvas(lastWidth, lastHeight);
  }

  const cols = data.width;
  const rows = data.height;
  const cellSize = Math.floor(Math.min(canvas.width / cols, canvas.height / rows));
  ctx.clearRect(0, 0, canvas.width, canvas.height);

  // grid lines
  ctx.strokeStyle = '#2a2f3a';
  ctx.lineWidth = 1;
  for (let x = 0; x <= cols; x++) {
    ctx.beginPath();
    ctx.moveTo(x * cellSize + 0.5, 0.5);
    ctx.lineTo(x * cellSize + 0.5, rows * cellSize + 0.5);
    ctx.stroke();
  }
  for (let y = 0; y <= rows; y++) {
    ctx.beginPath();
    ctx.moveTo(0.5, y * cellSize + 0.5);
    ctx.lineTo(cols * cellSize + 0.5, y * cellSize + 0.5);
    ctx.stroke();
  }

  // occupants
  for (const cell of data.cells) {
    if (cell.occupant != null) {
      const x = cell.x;
      const y = rows - 1 - cell.y; // y-up display like server file output
      const px = x * cellSize;
      const py = y * cellSize;
      ctx.fillStyle = '#19c37d';
      ctx.fillRect(px + 2, py + 2, cellSize - 4, cellSize - 4);

      // label
      ctx.fillStyle = '#0f1115';
      ctx.font = `${Math.max(10, Math.floor(cellSize * 0.35))}px monospace`;
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      const label = `E${cell.occupant}`;
      ctx.fillText(label, px + cellSize / 2, py + cellSize / 2);
    }
  }
}

function connect() {
  const src = new EventSource('/events');
  src.onopen = () => statusEl.textContent = 'Connected';
  src.onerror = () => statusEl.textContent = 'Disconnected (retrying…)';
  src.onmessage = (ev) => {
    try {
      const data = JSON.parse(ev.data);
      drawGrid(data);
    } catch (e) {
      console.error('Bad event payload', e);
    }
  };
}

window.addEventListener('resize', () => resizeCanvas(lastWidth, lastHeight));
connect();
