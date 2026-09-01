// The editor's undo history.
//
// This is code that can destroy work, so the cases below are the ones that
// destroy it: an undo that restores the wrong text, a run that groups edits it
// should not, and a stack that grows without bound on a 185 KB file.
import { createHistory, diff, shouldCoalesce, COALESCE_MS, MAX_DEPTH } from '../src/mibeditor/history.js';

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

const sel = (start, end = start) => ({ start, end });

// A clock the test drives, so grouping is tested rather than raced.
let now = 0;
const clock = () => now;

// --- diff ---
check('diff finds an insertion', JSON.stringify(diff('ab', 'axb')) === JSON.stringify({ at: 1, removed: '', inserted: 'x' }));
check('diff finds a deletion', JSON.stringify(diff('axb', 'ab')) === JSON.stringify({ at: 1, removed: 'x', inserted: '' }));
check('diff finds a replacement', JSON.stringify(diff('axb', 'ayb')) === JSON.stringify({ at: 1, removed: 'x', inserted: 'y' }));
check('diff of identical text is empty', diff('a', 'a').removed === '' && diff('a', 'a').inserted === '');
check('diff trims a shared suffix', diff('aaa', 'aa').at === 2);

// --- typing groups into words ---
{
  now = 0;
  const h = createHistory('', clock);
  let text = '';
  const type = (ch) => {
    const before = text;
    text += ch;
    h.record(before, text, sel(before.length), sel(text.length));
    now += 50;
  };
  for (const ch of 'hello world') type(ch);

  check('typing does not make one step per character', h.depth < 5, `${h.depth} steps for 11 characters`);

  const first = h.undo(text);
  check('one undo takes back a word, not a letter',
    first && first.text === 'hello ', `got ${JSON.stringify(first && first.text)}`);
  const second = h.undo(first.text);
  check('the next undo takes back the previous word',
    second && second.text === '', `got ${JSON.stringify(second && second.text)}`);
  check('there is nothing left to undo', h.canUndo === false);
}

// --- a pause ends the run, as it does in every editor ---
{
  now = 0;
  const h = createHistory('', clock);
  h.record('', 'abc', sel(0), sel(3));
  now += COALESCE_MS + 100;
  h.record('abc', 'abcdef', sel(3), sel(6));
  check('a pause starts a new step', h.depth === 2, `${h.depth}`);
  check('undo after a pause takes back only the later text',
    h.undo('abcdef').text === 'abc');
}

// --- the caret comes back with the text ---
{
  now = 0;
  const h = createHistory('hello', clock);
  h.record('hello', 'hello world', sel(5), sel(11));
  const back = h.undo('hello world');
  check('undo restores the text', back.text === 'hello');
  check('undo restores the caret', back.selection.start === 5,
    `caret at ${back.selection.start}`);
}

// --- deliberate actions are one step each ---
{
  now = 0;
  const h = createHistory('a', clock);
  h.record('a', 'ab', sel(1), sel(2));           // typing
  h.record('ab', 'ab' + 'X'.repeat(50), sel(2), sel(52), true); // a snippet
  h.record('ab' + 'X'.repeat(50), 'ab' + 'X'.repeat(50) + 'c', sel(52), sel(53));

  check('an atomic edit is its own step', h.depth === 3, `${h.depth}`);
  let t = 'ab' + 'X'.repeat(50) + 'c';
  t = h.undo(t).text;
  check('undoing after a snippet takes back the typing first', t === 'ab' + 'X'.repeat(50));
  t = h.undo(t).text;
  check('the next undo takes back the whole snippet in one step', t === 'ab');
}

// --- redo, and the branch that gets thrown away ---
{
  now = 0;
  const h = createHistory('', clock);
  h.record('', 'one', sel(0), sel(3));
  now += COALESCE_MS + 1;
  h.record('one', 'one two', sel(3), sel(7));

  let t = 'one two';
  t = h.undo(t).text;
  check('undo steps back', t === 'one');
  check('redo becomes available', h.canRedo === true);
  t = h.redo(t).text;
  check('redo steps forward', t === 'one two');

  t = h.undo(t).text;
  h.record(t, t + ' three', sel(3), sel(9));
  check('a new edit discards what had been undone', h.canRedo === false);
}

// --- backspacing groups too ---
{
  now = 0;
  const h = createHistory('hello', clock);
  let text = 'hello';
  for (let i = 0; i < 5; i++) {
    const before = text;
    text = text.slice(0, -1);
    h.record(before, text, sel(before.length), sel(text.length));
    now += 40;
  }
  check('a run of backspaces is one step', h.depth === 1, `${h.depth}`);
  check('undoing it restores the whole word', h.undo('').text === 'hello');
}

// --- grouping must not merge unrelated edits ---
{
  now = 0;
  const h = createHistory('abcdef', clock);
  h.record('abcdef', 'abXcdef', sel(2), sel(3));
  h.record('abXcdef', 'abXcdEef', sel(6), sel(7)); // typed somewhere else
  check('edits at unrelated positions are separate steps', h.depth === 2, `${h.depth}`);
}
check('typing then deleting are separate steps',
  shouldCoalesce({ at: 0, removed: '', inserted: 'a', time: 0 }, { at: 1, removed: 'a', inserted: '' }, 10, false) === false);

// --- memory: entries are diffs, and the stack is bounded ---
{
  now = 0;
  const big = 'x'.repeat(185000);
  const h = createHistory(big, clock);
  let text = big;
  for (let i = 0; i < MAX_DEPTH + 50; i++) {
    const before = text;
    text += 'y';
    h.record(before, text, sel(before.length), sel(text.length), true); // atomic: no grouping
    now += COALESCE_MS + 1;
  }
  check('the stack is bounded', h.depth <= MAX_DEPTH, `${h.depth}`);
  // The document must still be recoverable to the oldest kept step.
  let t = text;
  let steps = 0;
  while (h.canUndo) { t = h.undo(t).text; steps++; }
  check('every kept step still unwinds correctly', t === big + 'y'.repeat(50),
    `${steps} steps, length ${t.length}`);
}

// Speed: recording must not be something typing waits for.
{
  now = 0;
  const big = 'x'.repeat(185000);
  const h = createHistory(big, clock);
  let text = big;
  const started = process.hrtime.bigint();
  for (let i = 0; i < 500; i++) {
    const before = text;
    text = text.slice(0, 1000) + 'y' + text.slice(1000);
    h.record(before, text, sel(1000), sel(1001));
    now += 10;
  }
  const ms = Number(process.hrtime.bigint() - started) / 1e6;
  check('recording 500 keystrokes on a 185 KB file is instant', ms < 200, `${ms.toFixed(0)} ms`);
}

process.exit(failures ? 1 : 0);
