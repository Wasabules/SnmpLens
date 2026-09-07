// Keep docs/sitemap.xml's <lastmod> dates true, and make a lie fail CI.
//
// The sitemap states the rule in its own comment — "bump lastmod in the same
// commit that edits a page; a date that never moves is worse than no date at
// all, because it is believed once and then discounted" — and nothing enforced
// it. changelog.html drifted a day behind within one release, and no tool
// referenced the file at all: `grep sitemap tools/*.mjs` returned nothing.
//
// The date is derived, never invented:
//
//   - a page with UNCOMMITTED changes is being edited now, so it gets today;
//   - otherwise it gets the author date of the last commit that touched it.
//
// Deliberately NOT `new Date()` for every entry on every run. Stamping today on
// a page nobody touched is the over-claiming direction, which is the one a
// crawler actually learns to distrust, and it is precisely the failure the
// file's own comment warns about.
//
//   node tools/sitemap-lastmod.mjs            rewrite the dates
//   node tools/sitemap-lastmod.mjs --check    fail if any is wrong (CI)
import { execFileSync } from 'node:child_process';
import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const sitemapPath = join(root, 'docs', 'sitemap.xml');
const check = process.argv.includes('--check');

const git = (...args) =>
  execFileSync('git', args, { cwd: root, encoding: 'utf8' }).trim();

// The URL in the sitemap maps to a file on disk. "/" is index.html; everything
// else is the path as written. Derived from the sitemap rather than from a list
// here, so a page added to one and not the other is caught rather than skipped.
function fileFor(loc) {
  const path = loc.replace('https://snmplens.com/', '');
  return join('docs', path === '' ? 'index.html' : path);
}

function lastmodFor(file) {
  // Staged or unstaged, either counts: the page is being changed in the commit
  // this run belongs to.
  const dirty = git('status', '--porcelain', '--', file) !== '';
  if (dirty) return new Date().toISOString().slice(0, 10);
  const committed = git('log', '-1', '--format=%cs', '--', file);
  if (!committed) throw new Error(`${file} is not in git and has no changes; cannot date it`);
  return committed;
}

const xml = readFileSync(sitemapPath, 'utf8');
const entries = [...xml.matchAll(/<loc>([^<]+)<\/loc>\s*<lastmod>(\d{4}-\d{2}-\d{2})<\/lastmod>/g)];
if (entries.length === 0) {
  console.error('sitemap-lastmod: no <loc>/<lastmod> pairs found; the sitemap shape changed');
  process.exit(1);
}

let out = xml;
const wrong = [];
for (const [block, loc, was] of entries) {
  const file = fileFor(loc);
  const now = lastmodFor(file);
  if (now === was) continue;
  wrong.push(`${loc}  ${was} -> ${now}`);
  out = out.replace(block, block.replace(`<lastmod>${was}</lastmod>`, `<lastmod>${now}</lastmod>`));
}

if (check) {
  if (wrong.length) {
    console.error('sitemap-lastmod: these dates do not match git:\n  ' + wrong.join('\n  '));
    console.error('\nRun: node tools/sitemap-lastmod.mjs');
    process.exit(1);
  }
  console.log(`sitemap-lastmod: ${entries.length} dates all match git.`);
  process.exit(0);
}

if (wrong.length === 0) {
  console.log(`sitemap-lastmod: ${entries.length} dates already correct; nothing written.`);
  process.exit(0);
}
writeFileSync(sitemapPath, out);
console.log(`sitemap-lastmod: updated ${wrong.length} of ${entries.length}:\n  ` + wrong.join('\n  '));
