/**
 * Record the project site's animated clips from the REAL interface.
 *
 *   node tools/record.mjs              every clip, into docs/assets/video/
 *   node tools/record.mjs anonymous    only clips whose name matches
 *   node tools/record.mjs --gif        also write a GIF beside each clip
 *
 * Same principle as tools/screenshots.mjs — the same Svelte components, the same
 * stylesheet, only the Wails bridge replaced — but a clip needs the TRANSITION
 * rather than the end state, so three things differ.
 *
 * 1. No `--virtual-time-budget`. Virtual time runs the page's timers as fast as
 *    they will go, which is exactly right for a still and destroys a recording:
 *    a two-second animation would be over before the first frame arrived.
 *    Recording is therefore real time, and slower.
 *
 * 2. The steps are held until the camera is rolling. The scene director parks
 *    them behind `__SNMPLENS_PLAY__` under `?record=1`, and this waits for
 *    `__SNMPLENS_ARMED__` rather than for a fixed delay — so a slow machine
 *    records the same clip as a fast one instead of one that starts halfway
 *    through.
 *
 * 3. Frames come from the DevTools protocol (`Page.startScreencast`), not from
 *    one Chrome launch per frame. Node 22 has a global WebSocket, so that costs
 *    no dependency.
 *
 * `Page.screencastFrame` fires when the page CHANGES, so frames arrive at an
 * irregular rate — a still second produces none at all. They are resampled onto
 * a fixed grid here rather than handed to ffmpeg's concat demuxer: a numbered
 * sequence is one flag on two platforms, where concat is a generated file full
 * of paths that Windows quotes differently.
 */
import { spawn, spawnSync } from 'node:child_process';
import { createServer } from 'node:http';
import {
  readFileSync, writeFileSync, existsSync, mkdirSync, statSync,
  mkdtempSync, rmSync,
} from 'node:fs';
import { join, dirname, extname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { tmpdir } from 'node:os';

const here = dirname(fileURLToPath(import.meta.url));
const repo = join(here, '..');
const frontend = join(repo, 'frontend');
const dist = join(frontend, 'screenshots', 'dist');

const args = process.argv.slice(2);
const outIdx = args.indexOf('--out');
const outDir = outIdx >= 0 ? args[outIdx + 1] : join(repo, 'docs', 'assets', 'video');
const outValueIdx = outIdx >= 0 ? outIdx + 1 : -1;
const filter = args.filter((a, i) => !a.startsWith('--') && i !== outValueIdx)[0] || '';
const wantGif = args.includes('--gif');
const skipBuild = args.includes('--no-build');

/* --- the frame grid ------------------------------------------------------ */

// 20 fps. Above this the file grows for motion nobody is watching frame by
// frame; below it, a panel sliding in reads as a series of jumps.
const FPS = 20;
// The clips are displayed about 900 px wide on the site and are captured at 2x,
// so 1600 is a full retina pixel per displayed pixel with nothing spare.
const OUT_WIDTH = 1600;

/* --- Chrome -------------------------------------------------------------- */

const CHROME_CANDIDATES = [
  'C:/Program Files/Google/Chrome/Application/chrome.exe',
  'C:/Program Files (x86)/Google/Chrome/Application/chrome.exe',
  join(process.env.LOCALAPPDATA || '', 'Google/Chrome/Application/chrome.exe'),
  '/usr/bin/google-chrome',
  '/usr/bin/chromium',
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
];

function findChrome() {
  for (const p of CHROME_CANDIDATES) {
    try {
      if (p && existsSync(p)) return p;
    } catch { /* keep looking */ }
  }
  return null;
}

function haveFfmpeg() {
  const r = spawnSync('ffmpeg', ['-version'], { stdio: 'ignore', shell: process.platform === 'win32' });
  return r.status === 0;
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

/* --- the clip catalogue -------------------------------------------------- */

const { buildScenes } = await import(
  new URL('../frontend/screenshots/scenes.js', import.meta.url).href
);
const { CLIPS } = await import(
  new URL('../frontend/screenshots/clips.js', import.meta.url).href
);

const seedsPath = join(frontend, 'screenshots', 'bridge', 'seeds.json');
if (!existsSync(seedsPath)) {
  console.error('No fixtures yet. Run: node tools/genbridge.mjs tools/screenshot-spec.json');
  process.exit(1);
}
const scenes = buildScenes(JSON.parse(readFileSync(seedsPath, 'utf8')));
const byName = new Map(scenes.map((s) => [s.name, s]));

const clips = CLIPS.filter((c) => !filter || c.name.includes(filter));
if (!clips.length) {
  console.error(`No clip matches ${JSON.stringify(filter)}.`);
  process.exit(1);
}
for (const c of clips) {
  if (!byName.has(c.scene)) {
    console.error(`Clip ${c.name} names scene ${c.scene}, which does not exist.`);
    process.exit(1);
  }
}

/* --- build --------------------------------------------------------------- */

if (!skipBuild) {
  console.log('Building the screenshot bundle…');
  const viteBin = join(frontend, 'node_modules', 'vite', 'bin', 'vite.js');
  if (!existsSync(viteBin)) {
    console.error('Vite is not installed. Run: cd frontend && npm install');
    process.exit(1);
  }
  const build = spawnSync(
    process.execPath,
    [viteBin, 'build', '--config', 'vite.screenshots.config.js', '--logLevel', 'error'],
    { cwd: frontend, stdio: 'inherit' },
  );
  if (build.status !== 0) {
    console.error('The bundle did not build; nothing was recorded.');
    process.exit(1);
  }
} else if (!existsSync(join(dist, 'index.html'))) {
  console.error('--no-build was given but there is no bundle to record.');
  process.exit(1);
}

/* --- serve --------------------------------------------------------------- */

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.woff2': 'font/woff2',
  '.png': 'image/png',
  '.svg': 'image/svg+xml',
};

const server = createServer((req, res) => {
  const path = decodeURIComponent(req.url.split('?')[0]);
  const file = join(dist, path === '/' ? 'index.html' : path);
  if (!file.startsWith(dist) || !existsSync(file) || statSync(file).isDirectory()) {
    res.writeHead(404).end('not found');
    return;
  }
  res.writeHead(200, { 'Content-Type': MIME[extname(file)] || 'application/octet-stream' });
  res.end(readFileSync(file));
});

const port = await new Promise((resolve) => {
  server.listen(0, '127.0.0.1', () => resolve(server.address().port));
});

/* --- the DevTools protocol, minimally ------------------------------------ */

/**
 * Enough of CDP to drive one page: numbered requests, promises keyed by id, and
 * event listeners. Deliberately not a dependency — this is the whole client.
 */
class CDP {
  constructor(url) {
    this.url = url;
    this.next = 1;
    this.pending = new Map();
    this.handlers = new Map();
  }

  open() {
    return new Promise((resolve, reject) => {
      const ws = new WebSocket(this.url);
      this.ws = ws;
      ws.addEventListener('open', () => resolve(this));
      ws.addEventListener('error', () => reject(new Error(`cannot reach ${this.url}`)));
      ws.addEventListener('message', (ev) => {
        let msg;
        try {
          msg = JSON.parse(ev.data);
        } catch {
          return;
        }
        if (msg.id !== undefined) {
          const p = this.pending.get(msg.id);
          if (!p) return;
          this.pending.delete(msg.id);
          if (msg.error) p.reject(new Error(`${p.method}: ${msg.error.message}`));
          else p.resolve(msg.result);
          return;
        }
        const fns = this.handlers.get(msg.method);
        if (fns) for (const fn of fns) fn(msg.params);
      });
    });
  }

  send(method, params = {}) {
    const id = this.next++;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject, method });
      this.ws.send(JSON.stringify({ id, method, params }));
    });
  }

  on(event, fn) {
    if (!this.handlers.has(event)) this.handlers.set(event, []);
    this.handlers.get(event).push(fn);
  }

  close() {
    try {
      this.ws.close();
    } catch { /* already gone */ }
  }

  /** Evaluate an expression and return its value, or undefined on any failure. */
  async value(expression) {
    try {
      const r = await this.send('Runtime.evaluate', { expression, returnByValue: true });
      return r?.result?.value;
    } catch {
      return undefined;
    }
  }

  /** Poll an expression until it is truthy, or give up. */
  async until(expression, timeoutMs, label) {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      if (await this.value(expression)) return true;
      await sleep(100);
    }
    throw new Error(`timed out waiting for ${label || expression}`);
  }
}

/**
 * Chrome writes its chosen port into DevToolsActivePort once it is listening.
 * Asking for port 0 and reading the answer is the only race-free way to get one
 * — picking a free port ourselves leaves a window in which something else takes
 * it, and a fixed port fails the second time two of these run at once.
 */
async function debuggerUrl(profileDir, timeoutMs = 20000) {
  const portFile = join(profileDir, 'DevToolsActivePort');
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (existsSync(portFile)) {
      const lines = readFileSync(portFile, 'utf8').split('\n');
      const p = parseInt(lines[0], 10);
      if (p > 0) {
        try {
          const list = await fetch(`http://127.0.0.1:${p}/json/list`).then((r) => r.json());
          const page = list.find((t) => t.type === 'page' && t.webSocketDebuggerUrl);
          if (page) return page.webSocketDebuggerUrl;
        } catch { /* Chrome is listening but not yet serving; try again */ }
      }
    }
    await sleep(120);
  }
  throw new Error('Chrome never reported a debugging port');
}

/* --- recording ----------------------------------------------------------- */

const chrome = findChrome();
if (!chrome) {
  console.error('Chrome was not found. Looked in:\n  ' + CHROME_CANDIDATES.join('\n  '));
  server.close();
  process.exit(1);
}
if (!haveFfmpeg()) {
  console.error('ffmpeg is not on PATH; the frames cannot be encoded.');
  server.close();
  process.exit(1);
}

mkdirSync(outDir, { recursive: true });
const work = mkdtempSync(join(tmpdir(), 'snmplens-clip-'));

/**
 * Record one clip and return its frames, each with the moment it was rendered.
 */
async function shoot(clip, scene) {
  const profile = join(work, `profile-${clip.name}`);
  mkdirSync(profile, { recursive: true });

  const child = spawn(chrome, [
    '--headless=new',
    '--disable-gpu',
    '--no-first-run',
    '--no-default-browser-check',
    `--user-data-dir=${profile}`,
    '--remote-debugging-port=0',
    '--hide-scrollbars',
    `--window-size=${scene.width},${scene.height}`,
    // 2x, so the clip is a full pixel per displayed pixel after the downscale.
    '--force-device-scale-factor=2',
    // Deliberately NOT --virtual-time-budget: it would run the animation to its
    // end before the first frame was captured.
    'about:blank',
  ], { stdio: 'ignore' });

  const frames = [];
  let cdp = null;
  try {
    cdp = await new CDP(await debuggerUrl(profile)).open();
    await cdp.send('Page.enable');
    await cdp.send('Runtime.enable');

    const url = `http://127.0.0.1:${port}/index.html`
      + `?scene=${encodeURIComponent(scene.name)}&record=1`;
    await cdp.send('Page.navigate', { url });

    // The director sets this once the workspace has been chosen and its loads
    // have settled — which is the first frame worth having.
    await cdp.until('window.__SNMPLENS_ARMED__ === true', 30000, 'the scene to arm');
    if (clip.settle) await sleep(clip.settle);

    cdp.on('Page.screencastFrame', (p) => {
      frames.push({
        // metadata.timestamp is when the frame was RENDERED, in epoch seconds.
        // Receipt time here would fold this process's own scheduling into the
        // clip's timing, which shows up as a stutter that was never on screen.
        t: (p.metadata && p.metadata.timestamp ? p.metadata.timestamp * 1000 : Date.now()),
        buf: Buffer.from(p.data, 'base64'),
      });
      // Unacknowledged frames stop the stream after one. This is not optional.
      cdp.send('Page.screencastFrameAck', { sessionId: p.sessionId }).catch(() => {});
    });

    await cdp.send('Page.startScreencast', {
      format: 'jpeg',
      // The clip is re-encoded to a lossy codec anyway, so the only thing a
      // higher number buys is a slower capture. PNG frames at this size stall
      // the stream outright.
      quality: 92,
      maxWidth: scene.width * 2,
      maxHeight: scene.height * 2,
      everyNthFrame: 1,
    });

    // A beat of the opening state, so the clip does not begin mid-gesture.
    await sleep(clip.lead ?? 700);
    await cdp.value(`window.__SNMPLENS_PLAY__(${clip.pace ?? 900})`);
    await cdp.until('window.__SNMPLENS_READY__ === true',
      clip.timeout ?? 60000, 'the steps to finish');
    // And a beat of the result, so it can be read before the loop restarts.
    await sleep(clip.tail ?? 1800);

    await cdp.send('Page.stopScreencast');
  } finally {
    if (cdp) cdp.close();
    child.kill();
  }
  return frames;
}

/**
 * Resample irregular frames onto a fixed grid.
 *
 * The screencast fires on CHANGE, so a second in which nothing moves produces no
 * frames at all — feeding the sequence straight to ffmpeg would play those
 * seconds back instantly. Each slot takes the most recent frame at or before it,
 * which is what was actually on screen at that moment.
 */
function resample(frames, fps) {
  if (!frames.length) return [];
  const t0 = frames[0].t;
  const span = frames[frames.length - 1].t - t0;
  const slots = Math.max(1, Math.round((span / 1000) * fps));
  const out = [];
  let i = 0;
  for (let n = 0; n <= slots; n++) {
    const at = t0 + (n / fps) * 1000;
    while (i + 1 < frames.length && frames[i + 1].t <= at) i++;
    out.push(frames[i].buf);
  }
  return out;
}

function ffmpeg(argv, label) {
  const r = spawnSync('ffmpeg', argv, {
    stdio: ['ignore', 'ignore', 'pipe'],
    shell: process.platform === 'win32',
  });
  if (r.status !== 0) {
    const err = (r.stderr || '').toString().trim().split('\n').slice(-6).join('\n');
    throw new Error(`ffmpeg failed on ${label}:\n${err}`);
  }
}

const kb = (p) => Math.round(statSync(p).size / 1024);

console.log(`\nRecording ${clips.length} clip(s) into ${outDir}`);
let failures = 0;

for (const clip of clips) {
  const scene = byName.get(clip.scene);
  const dir = join(work, clip.name);
  mkdirSync(dir, { recursive: true });

  try {
    const raw = await shoot(clip, scene);
    const grid = resample(raw, FPS);
    if (grid.length < FPS) {
      throw new Error(`only ${grid.length} frames — nothing moved, or the steps did not run`);
    }
    grid.forEach((buf, n) => {
      writeFileSync(join(dir, `f-${String(n + 1).padStart(5, '0')}.jpg`), buf);
    });

    const pattern = join(dir, 'f-%05d.jpg');
    const scale = `scale=${OUT_WIDTH}:-2:flags=lanczos`;
    const mp4 = join(outDir, `${clip.name}.mp4`);
    const webm = join(outDir, `${clip.name}.webm`);
    const poster = join(outDir, `${clip.name}.jpg`);

    // H.264 for the browsers that will not decode VP9, VP9 for the size. The
    // page offers both and lets the browser choose.
    ffmpeg(['-y', '-framerate', String(FPS), '-i', pattern, '-vf', scale,
      '-c:v', 'libx264', '-preset', 'veryslow', '-crf', '24',
      // yuv420p, or Safari and a good many hardware decoders show nothing at
      // all. -2 on the height above keeps both dimensions even, which that
      // pixel format requires.
      '-pix_fmt', 'yuv420p', '-movflags', '+faststart', mp4], clip.name);

    ffmpeg(['-y', '-framerate', String(FPS), '-i', pattern, '-vf', scale,
      '-c:v', 'libvpx-vp9', '-crf', '34', '-b:v', '0', '-row-mt', '1',
      '-pix_fmt', 'yuv420p', webm], clip.name);

    // The poster is the LAST frame, not the first: it is what the clip is for,
    // and it is what stays on screen for anyone whose browser blocks autoplay.
    ffmpeg(['-y', '-i', join(dir, `f-${String(grid.length).padStart(5, '0')}.jpg`),
      '-vf', scale, '-q:v', '3', poster], clip.name);

    let gifNote = '';
    if (wantGif) {
      const gif = join(outDir, `${clip.name}.gif`);
      // Halved rate and width: a GIF has no interframe compression worth the
      // name, so this is the difference between 2 MB and 20.
      ffmpeg(['-y', '-framerate', String(FPS), '-i', pattern,
        '-vf', 'fps=12,scale=1000:-1:flags=lanczos,split[a][b];'
             + '[a]palettegen=max_colors=128[p];'
             + '[b][p]paletteuse=dither=bayer:bayer_scale=3', gif], clip.name);
      gifNote = `  gif ${kb(gif)} KB`;
    }

    const secs = (grid.length / FPS).toFixed(1);
    console.log(
      `  ${clip.name.padEnd(22)} ${secs}s  ${grid.length} frames  `
      + `mp4 ${String(kb(mp4)).padStart(4)} KB  webm ${String(kb(webm)).padStart(4)} KB${gifNote}`,
    );
  } catch (e) {
    console.log(`  ${clip.name.padEnd(22)} FAILED — ${e.message}`);
    failures++;
  }
}

server.close();
try {
  rmSync(work, { recursive: true, force: true });
} catch { /* a locked profile directory is not worth failing over */ }

console.log(`\n${clips.length - failures} recorded, ${failures} failed.`);
if (!failures) {
  console.log('\nWatch them before committing — this checks that frames arrived and');
  console.log('that ffmpeg accepted them, not that the clip shows the right thing.');
}
process.exit(failures ? 1 : 0);
