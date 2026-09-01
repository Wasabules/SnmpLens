// The MIB editor store, against a stubbed bridge.
//
// The thing being pinned here is ordering. Analysis is asynchronous and the
// user keeps typing while it runs, so two answers can be in flight at once and
// arrive in the wrong order. Diagnostics carry line numbers; showing the ones
// computed two keystrokes ago points at lines that have since moved, which is
// worse than showing none.
import * as esbuild from 'esbuild';
import { writeFileSync, mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const calls = [];
let pending = [];
let drafts = {};

globalThis.__stub = {
  MibEditorRead: async (name) => ({
    name, content: 'A DEFINITIONS ::= BEGIN\nEND\n', sha256: 'h', diagnostics: [],
  }),
  MibEditorReadDraft: async (name) => drafts[name] || '',
  MibEditorSaveDraft: async (name, content) => { drafts[name] = content; },
  MibEditorDiscardDraft: async (name) => { delete drafts[name]; },
  // Each analysis parks itself; the test decides when and in what order each
  // one answers.
  MibEditorAnalyse: (content) => {
    calls.push(content);
    return new Promise((resolve) => {
      pending.push({ content, resolve });
    });
  },
};

const stub = `const s = globalThis.__stub;
export const MibEditorRead = (...a) => s.MibEditorRead(...a);
export const MibEditorReadDraft = (...a) => s.MibEditorReadDraft(...a);
export const MibEditorSaveDraft = (...a) => s.MibEditorSaveDraft(...a);
export const MibEditorDiscardDraft = (...a) => s.MibEditorDiscardDraft(...a);
export const MibEditorAnalyse = (...a) => s.MibEditorAnalyse(...a);`;

const dir = mkdtempSync(join(tmpdir(), 'snmplens-mibed-'));
writeFileSync(join(dir, 'stub.js'), stub);

const entry = new URL('../src/stores/mibEditorStore.js', import.meta.url).pathname.replace(/^[/]([A-Za-z]:)/, '$1');
const out = join(dir, 'bundle.mjs');
await esbuild.build({
  entryPoints: [entry],
  bundle: true, format: 'esm', outfile: out, platform: 'node', logLevel: 'silent',
  plugins: [{
    name: 'alias',
    setup(b) {
      b.onResolve({ filter: /wailsjs[/]go[/]main[/]App$/ }, () => ({ path: join(dir, 'stub.js') }));
    },
  }],
});

const { mibEditorStore } = await import(pathToFileURL(out).href);
const { get } = await import('svelte/store');

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};
const tick = () => new Promise((r) => setTimeout(r, 0));

await mibEditorStore.open('A-MIB');
pending = [];
calls.length = 0;

// Two edits, two analyses in flight. The FIRST one answers last.
mibEditorStore.setBuffer('first');
mibEditorStore.refresh();
await tick();
mibEditorStore.setBuffer('second');
mibEditorStore.refresh();
await tick();

check('both analyses were dispatched', pending.length === 2, `pending=${pending.length}`);

const [older, newer] = pending;
newer.resolve({ diagnostics: [{ line: 2, message: 'from the newer buffer' }], missing: [] });
await tick();
older.resolve({ diagnostics: [{ line: 999, message: 'from the older buffer' }], missing: [] });
await tick();

const shown = get(mibEditorStore).diagnostics;
check('the stale answer does not overwrite the fresh one',
  shown.length === 1 && shown[0].message === 'from the newer buffer',
  shown.map((d) => d.message).join(' | ') || 'none');

// An answer computed for a buffer the user has since changed must not be shown
// either: its line numbers describe text that no longer exists.
pending = [];
mibEditorStore.setBuffer('third');
mibEditorStore.refresh();
await tick();
const inflight = pending[0];
mibEditorStore.setBuffer('fourth');          // the user types again
inflight.resolve({ diagnostics: [{ line: 5, message: 'about the third buffer' }], missing: [] });
await tick();

const after = get(mibEditorStore).diagnostics;
check('an answer about a superseded buffer is discarded',
  !after.some((d) => d.message === 'about the third buffer'),
  after.map((d) => d.message).join(' | ') || 'none');

// And the ordinary case still works.
pending = [];
mibEditorStore.setBuffer('fifth');
mibEditorStore.refresh();
await tick();
pending[0].resolve({ diagnostics: [{ line: 1, message: 'current' }], missing: [] });
await tick();
check('a current answer is applied',
  get(mibEditorStore).diagnostics.some((d) => d.message === 'current'));
check('checking is cleared when the answer lands',
  get(mibEditorStore).checking === false);


// --- drafts ---
//
// Abandoning changes has to reach the disk. It did not: saying yes to
// "discard your changes?" left the draft in place, so reopening the file
// recovered exactly the edits that had just been refused — and asked again.
drafts = { 'B-MIB': 'work in progress' };
await mibEditorStore.discardDraft('B-MIB');
check('discarding removes the stored buffer', drafts['B-MIB'] === undefined);

// Reverting is a decision, so the draft goes immediately rather than in a
// second and a bit — long enough to leave the file and have it survive.
drafts = {};
await mibEditorStore.open('C-MIB');
mibEditorStore.setBuffer('edited');
await tick();
drafts['C-MIB'] = 'edited';           // as the debounced writer would have
mibEditorStore.revert();
await tick();
check('reverting removes the draft at once', drafts['C-MIB'] === undefined);
check('reverting restores the saved content',
  get(mibEditorStore).buffer === 'A DEFINITIONS ::= BEGIN\nEND\n');

// A pending draft write belongs to the file it was scheduled for. Reading the
// current file instead wrote one file's buffer under another file's name.
drafts = {};
await mibEditorStore.open('D-MIB');
mibEditorStore.setBuffer('belongs to D');
await mibEditorStore.open('E-MIB');    // switch before the debounce fires
await new Promise((r) => setTimeout(r, 1400));
check('a pending draft is not written under the newly opened file',
  drafts['E-MIB'] === undefined, JSON.stringify(drafts));

process.exit(failures ? 1 : 0);
