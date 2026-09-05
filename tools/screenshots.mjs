/**
 * Regenerate every screenshot on the project site.
 *
 *   node tools/screenshots.mjs            all scenes, into docs/assets/img/
 *   node tools/screenshots.mjs monitor    only scenes whose name matches
 *   node tools/screenshots.mjs --out tmp  somewhere else
 *
 * The pictures are of the REAL interface: the same Svelte components, the same
 * stylesheet, the same translations, with only the Wails bridge replaced by
 * fixtures. A mock-up drawn to look like the application drifts from it the
 * first time anything changes; this cannot show a layout the application does
 * not actually produce.
 *
 * Every step is scripted because a screenshot nobody can regenerate is a
 * screenshot that quietly goes stale — which is exactly what happened to the
 * ones this replaces.
 */
import { spawn, spawnSync } from 'node:child_process';
import { createServer } from 'node:http';
import { readFileSync, existsSync, mkdirSync, statSync } from 'node:fs';
import { join, dirname, extname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';

const here = dirname(fileURLToPath(import.meta.url));
const repo = join(here, '..');
const frontend = join(repo, 'frontend');
const dist = join(frontend, 'screenshots', 'dist');

const args = process.argv.slice(2);
const outIdx = args.indexOf('--out');
const outDir = outIdx >= 0 ? args[outIdx + 1] : join(repo, 'docs', 'assets', 'img');
// `outIdx + 1` is 0 when --out is absent, which excluded the FIRST positional
// argument — so every invocation captured every scene and the filter silently
// did nothing. Guard on the flag being present at all.
const outValueIdx = outIdx >= 0 ? outIdx + 1 : -1;
const filter = args.filter((a, i) => !a.startsWith('--') && i !== outValueIdx)[0] || '';

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

/* --- the scene catalogue ------------------------------------------------- */

const { buildScenes } = await import(
  new URL('../frontend/screenshots/scenes.js', import.meta.url).href
);
const seedsPath = join(frontend, 'screenshots', 'bridge', 'seeds.json');
if (!existsSync(seedsPath)) {
  console.error('No fixtures yet. Run: node tools/genbridge.mjs tools/screenshot-spec.json');
  process.exit(1);
}
const scenes = buildScenes(JSON.parse(readFileSync(seedsPath, 'utf8')))
  .filter((s) => !filter || s.name.includes(filter));

if (!scenes.length) {
  console.error(`No scene matches ${JSON.stringify(filter)}.`);
  process.exit(1);
}

/* --- build --------------------------------------------------------------- */

console.log('Building the screenshot bundle…');

// Vite's JS entry point, run by this same Node — not `npx`. Since Node 20,
// spawning a .cmd without `shell: true` fails with EINVAL, and `shell: true`
// means quoting arguments correctly on two platforms for no benefit.
const viteBin = join(frontend, 'node_modules', 'vite', 'bin', 'vite.js');
if (!existsSync(viteBin)) {
  console.error(`Vite is not installed. Run: cd frontend && npm install`);
  process.exit(1);
}
const build = spawnSync(
  process.execPath,
  [viteBin, 'build', '--config', 'vite.screenshots.config.js', '--logLevel', 'error'],
  { cwd: frontend, stdio: 'inherit' },
);
if (build.status !== 0) {
  console.error('The bundle did not build; nothing was captured.');
  process.exit(1);
}

/* --- serve --------------------------------------------------------------- */

// Over HTTP, never file://. main.js awaits waitLocale() with no .catch, and the
// locale chunk is a module fetch that file:// blocks — the page would be
// permanently blank with nothing in the console to say why.
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
  // The path guard first, then ONE read. Checking existence and then reading is
  // two answers to a question that can change between them; readFileSync throws
  // ENOENT for a missing file and EISDIR for a directory, which is both cases
  // the stat was there for.
  if (!file.startsWith(dist)) {
    res.writeHead(404).end('not found');
    return;
  }
  let body;
  try {
    body = readFileSync(file);
  } catch {
    res.writeHead(404).end('not found');
    return;
  }
  res.writeHead(200, { 'Content-Type': MIME[extname(file)] || 'application/octet-stream' });
  res.end(body);
});

const port = await new Promise((resolve) => {
  server.listen(0, '127.0.0.1', () => resolve(server.address().port));
});

/* --- capture ------------------------------------------------------------- */

const chrome = findChrome();
if (!chrome) {
  console.error('Chrome was not found. Looked in:\n  ' + CHROME_CANDIDATES.join('\n  '));
  server.close();
  process.exit(1);
}

mkdirSync(outDir, { recursive: true });
const profile = mkdtempSync(join(tmpdir(), 'snmplens-shot-'));

/**
 * Kill a process AND its children.
 *
 * `child.kill()` signals the chrome.exe we spawned; Chrome's renderer, GPU and
 * utility processes are its children and survive it, which is how a run that
 * looked finished left fourteen of them behind.
 */
function killTree(child) {
  if (process.platform === 'win32') {
    try {
      spawnSync('taskkill', ['/PID', String(child.pid), '/T', '/F'], { stdio: 'ignore' });
      return;
    } catch { /* fall through to the signal */ }
  }
  try {
    child.kill('SIGKILL');
  } catch { /* already gone */ }
}

function capture(scene) {
  const out = join(outDir, `${scene.name}.png`);
  const url = `http://127.0.0.1:${port}/index.html?scene=${encodeURIComponent(scene.name)}`;

  // Remove the previous capture FIRST. What is waited on below is the file
  // appearing at a stable size, and last run's file is already there at a
  // perfectly stable size — so without this, a re-run of a scene that has been
  // captured before finishes on its first poll and kills Chrome before it has
  // written anything. It reported success, every time, and changed nothing.
  try {
    rmSync(out, { force: true });
  } catch { /* if it cannot be removed, the size check below still guards */ }

  return new Promise((resolve) => {
    const child = spawn(chrome, [
      '--headless=new',
      '--disable-gpu',
      // Without these Chrome opens a window, asks to be the default browser,
      // and never exits.
      '--no-first-run',
      '--no-default-browser-check',
      `--user-data-dir=${join(profile, scene.name)}`,
      // No scrollbar gutter down the right-hand edge of every image.
      '--hide-scrollbars',
      `--window-size=${scene.width},${scene.height}`,
      // 2x, so the images stay sharp on the displays people actually read them
      // on. A 1600x1000 scene becomes a 3200x2000 file.
      '--force-device-scale-factor=2',
      // Virtual time runs the page's timers as fast as they will go and stops
      // when the budget is spent, so the wait is deterministic rather than a
      // sleep long enough to "probably" be sufficient.
      '--virtual-time-budget=12000',
      `--screenshot=${out}`,
      url,
    ], { stdio: 'ignore' });

    // Headless Chrome writes the screenshot and then, often enough to matter,
    // does not exit — measured on this machine, roughly one run in three left a
    // process tree alive with the PNG already on disk, and waiting on `exit`
    // alone hung the whole capture there for as long as anyone let it.
    //
    // So the file is what is waited on, not the process: it is the thing we
    // actually want, and it exists whether Chrome chooses to leave or not. The
    // size has to be stable across two polls because a half-written PNG exists
    // too, and reading its header would report nonsense dimensions.
    let settled = false;
    let lastSize = -1;
    const deadline = Date.now() + 90000;

    const finish = () => {
      if (settled) return;
      settled = true;
      clearInterval(poll);
      killTree(child);
      done();
    };

    const poll = setInterval(() => {
      if (settled) return;
      if (existsSync(out)) {
        const size = statSync(out).size;
        if (size > 0 && size === lastSize) { finish(); return; }
        lastSize = size;
      }
      if (Date.now() > deadline) finish();
    }, 250);

    child.on('exit', finish);

    function done() {
      if (!existsSync(out)) {
        console.log(`  ${scene.name}: FAILED — no file was written`);
        resolve(false);
        return;
      }
      const buf = readFileSync(out);
      // The PNG header carries the real dimensions; trust it over the flags.
      const w = buf.readUInt32BE(16);
      const h = buf.readUInt32BE(20);
      const kb = Math.round(buf.length / 1024);
      const expected = scene.width * 2;
      const ok = w === expected;
      console.log(
        `  ${scene.name.padEnd(20)} ${w}x${h}  ${String(kb).padStart(5)} KB` +
        (ok ? '' : `  <- expected ${expected} wide; --force-device-scale-factor did not apply`),
      );
      resolve(true);
    }
  });
}

console.log(`\nCapturing ${scenes.length} scene(s) into ${outDir}`);
let failures = 0;
for (const scene of scenes) {
  const ok = await capture(scene);
  if (!ok) failures++;
}

server.close();
try {
  rmSync(profile, { recursive: true, force: true });
} catch { /* a locked profile directory is not worth failing over */ }

console.log(`\n${scenes.length - failures} captured, ${failures} failed.`);

/* --- derive what the site actually serves -------------------------------- */

// The PNGs are 3200 px wide and around half a megabyte each; the site displays
// them at about 900. Deriving the WebP here rather than in a separate command is
// deliberate — a capture that leaves the derived files stale puts an old picture
// on the page while the new one sits beside it, unused and looking correct.
if (!failures) {
  console.log('\nDeriving the responsive WebP the pages reference…');
  const { derive, socialCard, icons } = await import(new URL('./webp.mjs', import.meta.url).href);
  const d = derive(outDir, filter);
  // Only on a full run: the card is cut from operations-dark, and re-cutting it
  // while capturing an unrelated scene would silently pin it to an older one.
  if (!filter && socialCard(outDir)) console.log('  og-card.png (1200x630) for og:image');
  if (!filter) {
    const made = icons(outDir);
    if (made.length) console.log(`  ${made.length} icons from SnmpLens.png`);
  }
  if (d.failed) {
    console.log(`\n${d.failed} WebP failed — is ffmpeg on PATH?`);
    failures += d.failed;
  } else {
    console.log(
      `\n${d.written} written — ${Math.round(d.bytes / 1024)} KB of WebP`
      + ` against ${Math.round((d.bytes + d.saved) / 1024)} KB of PNG.`,
    );
  }
}

if (!failures) {
  console.log('\nThe site references these by name from docs/assets/img/; nothing else');
  console.log('needs updating. Look at them before committing — this checks that a file');
  console.log('was written and that it is the right size, not that it shows the right thing.');
}
process.exit(failures ? 1 : 0);
