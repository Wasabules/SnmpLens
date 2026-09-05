/**
 * Derive the responsive WebP the site actually serves from the captured PNGs.
 *
 *   node tools/webp.mjs             every PNG in docs/assets/img/
 *   node tools/webp.mjs monitor     only those whose name matches
 *
 * The captures are 3200 px wide because they are taken at 2x, and each is around
 * half a megabyte of PNG. The site displays them at roughly 900 px. Serving the
 * originals means a landing page that leads with pictures also takes eight
 * megabytes to read — and once every screenshot exists in both themes, sixteen.
 *
 * So each PNG becomes two WebP: 1200 px for an ordinary display and 2000 px for
 * a dense one, chosen by `srcset`. Measured on these images that is about a
 * seven-fold saving with no visible loss, which is what makes "more pictures"
 * affordable rather than a promise to be paid for by the reader.
 *
 * The PNGs stay. `og:image` is fetched by crawlers and chat clients that do not
 * all decode WebP, and the originals are what the next resize is derived from.
 *
 * `-preset text` is not a stylistic choice: libwebp's presets change how it
 * spends its bit budget, and a screenshot is a picture of TEXT — the photo
 * presets smear the 1 px stems of a 12 px monospace font, which is most of what
 * these images contain.
 */
import { spawnSync } from 'node:child_process';
import { readdirSync, existsSync, statSync, openSync, readSync, closeSync } from 'node:fs';
import { join, dirname, basename } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const repo = join(here, '..');

const args = process.argv.slice(2);
const dirIdx = args.indexOf('--dir');
const imgDir = dirIdx >= 0 ? args[dirIdx + 1] : join(repo, 'docs', 'assets', 'img');
const dirValueIdx = dirIdx >= 0 ? dirIdx + 1 : -1;
const filter = args.filter((a, i) => !a.startsWith('--') && i !== dirValueIdx)[0] || '';

/** The two rungs `srcset` picks between. */
export const WIDTHS = [1200, 2000];

const QUALITY = 82;

function ffmpeg(argv) {
  return spawnSync('ffmpeg', argv, {
    stdio: ['ignore', 'ignore', 'pipe'],
    shell: process.platform === 'win32',
  });
}

function pngWidth(file) {
  // The IHDR width sits at byte 16 of every PNG. Cheaper and more reliable than
  // asking ffprobe, which may not be installed even where ffmpeg is.
  const fd = openSync(file, 'r');
  try {
    const buf = Buffer.alloc(24);
    readSync(fd, buf, 0, 24, 0);
    return buf.readUInt32BE(16);
  } finally {
    closeSync(fd);
  }
}

export function derive(imgDir, filter = '') {
  if (!existsSync(imgDir)) return { written: 0, skipped: 0, failed: 0, bytes: 0, saved: 0 };

  const sources = readdirSync(imgDir)
    .filter((f) => f.endsWith('.png'))
    .filter((f) => !filter || f.includes(filter))
    .sort();

  let written = 0; let skipped = 0; let failed = 0; let bytes = 0; let saved = 0;

  for (const file of sources) {
    const src = join(imgDir, file);
    const stem = basename(file, '.png');
    const srcW = pngWidth(src);
    const srcBytes = statSync(src).size;
    let derivedBytes = 0;
    let any = false;

    for (const w of WIDTHS) {
      // Never upscale. A 1600-wide source has no 2000 rung, and inventing one
      // makes a bigger file that is no sharper.
      if (srcW < w) { skipped++; continue; }
      const out = join(imgDir, `${stem}-${w}.webp`);
      const r = ffmpeg([
        '-y', '-i', src,
        '-vf', `scale=${w}:-2:flags=lanczos`,
        '-c:v', 'libwebp', '-lossless', '0',
        '-quality', String(QUALITY), '-preset', 'text',
        '-compression_level', '6',
        out,
      ]);
      if (r.status !== 0) {
        const err = (r.stderr || '').toString().trim().split('\n').slice(-3).join(' ');
        console.log(`  ${stem}-${w}.webp  FAILED — ${err}`);
        failed++;
        continue;
      }
      derivedBytes += statSync(out).size;
      written++;
      any = true;
    }

    if (any) {
      bytes += derivedBytes;
      saved += srcBytes - derivedBytes;
      console.log(
        `  ${stem.padEnd(26)} ${String(Math.round(srcBytes / 1024)).padStart(4)} KB png`
        + ` -> ${String(Math.round(derivedBytes / 1024)).padStart(4)} KB webp`,
      );
    }
  }

  return { written, skipped, failed, bytes, saved };
}

/**
 * The social card: 1200x630, PNG, cropped rather than letterboxed.
 *
 * `og:image` cannot be one of the screenshots. They are 3200x1600-plus at a
 * 1.6 ratio, and every card renderer crops that to about 1.91:1 — from the
 * middle, which on these images is the middle of a table. Cropping it here, from
 * the TOP, keeps the header, the workspace tabs and the first rows: the part
 * that says what the application is.
 *
 * PNG rather than WebP because this one is fetched by crawlers and chat clients
 * rather than by browsers, and Twitter's card renderer still does not take WebP.
 */
export function socialCard(imgDir) {
  const src = join(imgDir, 'operations-dark.png');
  if (!existsSync(src)) return null;
  const out = join(imgDir, 'og-card.png');
  // 3200x1680 is 1.905:1 — the card ratio — so the scale that follows does not
  // squash anything.
  const r = ffmpeg([
    '-y', '-i', src,
    '-vf', 'crop=3200:1680:0:0,scale=1200:630:flags=lanczos',
    '-q:v', '2', out,
  ]);
  return r.status === 0 ? out : null;
}

// Only when run directly, so screenshots.mjs can import `derive` instead of
// spawning another Node.
if (import.meta.url === `file://${process.argv[1].replace(/\\/g, '/')}`
    || process.argv[1].endsWith('webp.mjs')) {
  const check = ffmpeg(['-version']);
  if (check.status !== 0) {
    console.error('ffmpeg is not on PATH; nothing was derived.');
    process.exit(1);
  }
  console.log(`Deriving WebP from ${imgDir}`);
  const r = derive(imgDir, filter);
  if (socialCard(imgDir)) console.log('  og-card.png                1200x630');
  console.log(
    `\n${r.written} written, ${r.skipped} skipped (source too small), ${r.failed} failed`
    + ` — ${Math.round(r.bytes / 1024)} KB of WebP against ${Math.round((r.bytes + r.saved) / 1024)} KB of PNG.`,
  );
  process.exit(r.failed ? 1 : 0);
}
