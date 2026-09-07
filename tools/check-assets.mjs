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

// SECURITY.md and the published policy must keep pointing at each other.
//
// They are two files saying the same thing to two audiences: GitHub reads
// SECURITY.md to fill in "Report a vulnerability", and docs/security.html is
// what a reader or a procurement questionnaire is sent to. The failure is not
// that either goes missing — it is that one is edited and the other is not,
// and a researcher then follows a link to a policy that no longer says what
// the repository says. Cheap to check, and impossible to notice by hand.
// The phrase is matched across whitespace: both files wrap their prose, and
// the first run of this check failed on 'private vulnerability' ending a line
// in SECURITY.md. A check that a line break can defeat is worse than none.
const policyUrl = 'https://snmplens.com/security.html';
const problems = [];

const securityMd = join(repo, 'SECURITY.md');
if (!existsSync(securityMd)) {
  problems.push("SECURITY.md is missing - GitHub's Report a vulnerability entry point is empty.");
} else {
  const md = readFileSync(securityMd, 'utf8');
  if (!md.includes(policyUrl)) {
    problems.push(`SECURITY.md no longer links ${policyUrl}, so the short form and the full policy have drifted apart.`);
  }
  // Both must name the same reporting channel. There is no email address on
  // purpose: the channel is GitHub's private reporting, and inventing a second
  // one is how a report ends up somewhere nobody reads.
  if (!/private\s+vulnerability\s+reporting/i.test(md)) {
    problems.push('SECURITY.md no longer names the private vulnerability reporting channel.');
  }
}

const policyPage = join(docs, 'security.html');
if (!existsSync(policyPage)) {
  problems.push('docs/security.html is missing, but SECURITY.md sends readers to it.');
} else if (!/private\s+vulnerability\s+reporting/i.test(readFileSync(policyPage, 'utf8'))) {
  problems.push('docs/security.html no longer names the private vulnerability reporting channel.');
}

if (problems.length) {
  console.error(''); console.error('Security policy:');
  for (const line of problems) console.error(`  ${line}`);
  process.exit(1);
}
console.log('SECURITY.md and docs/security.html agree on where to report.');
