/**
 * Every asset the site references must be in the repository.
 *
 *   node tools/check-assets.mjs
 *
 * GitHub Pages serves the REPOSITORY, not the working tree, and there is no
 * Pages build step: whatever the HTML names has to be committed or it is a 404
 * on the live site. Nothing else notices — the pages render perfectly on the
 * machine that made them, because the files are right there on disk.
 *
 * This exists because that happened. docs/assets/img/.gitignore excludes the
 * 3200 px captures with `*.png` and a list of exceptions; four PNGs generated
 * later — two favicons, the touch icon and the documentation social card — fell
 * through it. They were referenced from the head of all eight pages and from
 * documentation.html's og:image and JSON-LD, and they were never committed. The
 * defect was live before anyone looked.
 *
 * Checks both directions:
 *   - referenced but not tracked   -> a 404 on the deployed site
 *   - referenced but not on disk   -> a typo in a path
 *
 * Deliberately not a link checker. It answers one question — does the
 * repository contain what the site asks for — which is the question a local
 * preview can never answer, because a local preview reads the disk.
 */
import { execFileSync } from 'node:child_process';
import { readFileSync, readdirSync, existsSync } from 'node:fs';
import { join, dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(join(here, '..'));
const docs = join(repo, 'docs');

/** Local asset references, from the attributes that actually fetch something. */
function referencesIn(html) {
  const out = new Set();

  for (const m of html.matchAll(/(?:src|href|poster)="([^"]+)"/g)) add(out, m[1]);
  // content="…" is only a URL on the social-card meta tags; anything else there
  // is prose and would produce noise.
  for (const m of html.matchAll(/(?:property|name)="(?:og:image|twitter:image)"\s+content="([^"]+)"/g)) {
    add(out, m[1]);
  }
  for (const m of html.matchAll(/srcset="([^"]+)"/g)) {
    for (const part of m[1].split(',')) add(out, part.trim().split(/\s+/)[0]);
  }
  return out;
}

function add(set, raw) {
  if (!raw) return;
  let u = raw.trim();
  // Only our own files. An absolute URL counts when it is this site's.
  u = u.replace(/^https:\/\/snmplens\.com\//, '');
  if (/^(https?:|mailto:|data:|#|\/\/)/.test(u)) return;
  u = u.split('#')[0].split('?')[0].replace(/^\//, '');
  // A directory reference is not a file. `href="./"` is the brand link on every
  // page, and asking whether the repository contains "./" has no answer.
  if (!u || u.endsWith('/') || u.endsWith('.html') || !u.includes('/')) return;
  set.add(u);
}

const tracked = new Set(
  execFileSync('git', ['ls-files'], { cwd: docs, encoding: 'utf8' })
    .split('\n').map((l) => l.trim()).filter(Boolean),
);

const pages = readdirSync(docs).filter((f) => f.endsWith('.html'));
const refs = new Map(); // path -> the pages that name it

for (const page of pages) {
  for (const r of referencesIn(readFileSync(join(docs, page), 'utf8'))) {
    if (!refs.has(r)) refs.set(r, []);
    refs.get(r).push(page);
  }
}

const untracked = [];
const absent = [];
for (const [r, from] of refs) {
  const onDisk = existsSync(join(docs, r));
  if (!onDisk) absent.push([r, from]);
  else if (!tracked.has(r)) untracked.push([r, from]);
}

console.log(`${refs.size} assets referenced by ${pages.length} pages.`);

if (absent.length) {
  console.error(`\n${absent.length} referenced but NOT ON DISK — a wrong path:`);
  for (const [r, from] of absent) console.error(`  ${r}\n      named by ${from.join(', ')}`);
}

if (untracked.length) {
  console.error(`\n${untracked.length} referenced but NOT COMMITTED — 404 on the live site:`);
  for (const [r, from] of untracked) console.error(`  ${r}\n      named by ${from.join(', ')}`);
  console.error('\nPages serves the repository. Commit them, or add a negation to');
  console.error('docs/assets/img/.gitignore if an ignore rule is swallowing them.');
}

if (absent.length || untracked.length) process.exit(1);
console.log('Every one of them is in the repository.');
