import { writable, get } from 'svelte/store';
import {
  MibEditorRead,
  MibEditorAnalyse,
  MibEditorSaveDraft,
  MibEditorReadDraft,
  MibEditorDiscardDraft,
} from '../../wailsjs/go/main/App';

// The editor's state lives here, not in the panel component.
//
// The tab shell mounts panels with {#if activeTab === …}, which DESTROYS the
// component on every switch. With the buffer in component state, typing an
// edit and pressing Ctrl+1 lost it silently — no prompt, no trace. That is the
// one failure a text editor may not have, so the buffer outlives the panel.
//
// The buffer is also mirrored to a draft file on the Go side, which covers what
// a store cannot: closing the window, and the machine going down. Drafts are
// written OUTSIDE the MIB directory — a half-finished MIB in there would be
// loaded by the rest of the app.

const EMPTY = {
  source: null,       // the file as it was read; null when nothing is open
  buffer: '',         // what the user is editing
  diagnostics: [],
  missingImports: [],
  checking: false,
};

function createMibEditorStore() {
  const { subscribe, update, set } = writable({ ...EMPTY });

  let checkTimer;
  let draftTimer;

  function isDirty(state) {
    return state.source !== null && state.buffer !== state.source.content;
  }

  /** Whether there is unsaved work, for the tab badge and the leave guard. */
  function dirty() {
    return isDirty(get({ subscribe }));
  }

  async function open(name) {
    clearTimeout(draftTimer); // whatever was pending belongs to the file being left
    const source = await MibEditorRead(name);
    // A draft for this file means a previous session left work behind.
    let draft = '';
    try {
      draft = await MibEditorReadDraft(name);
    } catch (e) {
      draft = '';
    }
    const recovered = draft && draft !== source.content;

    set({
      ...EMPTY,
      source,
      buffer: recovered ? draft : source.content,
      diagnostics: source.diagnostics || [],
    });
    if (recovered) refresh();
    return { source, recovered };
  }

  function openSource(source) {
    set({ ...EMPTY, source, buffer: source.content, diagnostics: source.diagnostics || [] });
  }

  function setBuffer(buffer) {
    update((s) => ({ ...s, buffer, checking: true }));
    scheduleRefresh();
    scheduleDraft();
  }

  function scheduleRefresh() {
    clearTimeout(checkTimer);
    checkTimer = setTimeout(refresh, 350);
  }

  // Validation and the import check both run in Go and touch nothing: no file,
  // no gosmi state. That is what makes them safe on every keystroke.
  // ONE bridge call, one parse. This used to be two calls that each parsed the
  // file, on every pause in typing, over a MIB that can be 185 KB.
  // Analyses can overlap — the user keeps typing while one runs — and nothing
  // makes them answer in the order they were asked. An answer is applied only
  // if the text it describes is still the text on screen: diagnostics carry
  // line numbers, and showing the ones computed two keystrokes ago points at
  // lines that have since moved, which is worse than showing none.
  //
  // The buffer itself is the identity rather than a sequence number, because
  // that is the actual predicate: valid iff the file has not changed since.
  let inFlight = 0;

  async function refresh() {
    const asked = get({ subscribe }).buffer;
    inFlight++;
    try {
      const out = (await MibEditorAnalyse(asked)) || {};
      update((s) => {
        if (s.buffer !== asked) return s; // superseded; leave what is shown
        return {
          ...s,
          diagnostics: out.diagnostics || [],
          missingImports: out.missing || [],
        };
      });
    } catch (e) {
      // Nothing usable came back; the previous diagnostics are still the best
      // description of the file we have.
    } finally {
      inFlight--;
      // Only stop saying "checking" when nothing is still running, or a
      // discarded answer would clear the indicator while work continues.
      if (inFlight === 0) update((s) => ({ ...s, checking: false }));
    }
  }

  // The draft is written for the file that was open when the timer was SET,
  // not for whatever happens to be open when it fires. Reading the current
  // state instead meant that switching files inside the delay wrote one file's
  // buffer under another file's name.
  function scheduleDraft() {
    clearTimeout(draftTimer);
    const s = get({ subscribe });
    if (!s.source) return;
    const name = s.source.name;

    draftTimer = setTimeout(async () => {
      const now = get({ subscribe });
      // Still the same file? If not, that file's draft was already settled
      // when it was closed.
      if (!now.source || now.source.name !== name) return;
      try {
        if (isDirty(now)) {
          await MibEditorSaveDraft(name, now.buffer);
        } else {
          await MibEditorDiscardDraft(name);
        }
      } catch (e) {
        // A draft is a safety net, not a feature; failing to write one must
        // never interrupt typing.
      }
    }, 1200);
  }

  /**
   * Forget the stored buffer for a file.
   *
   * Abandoning changes has to reach the DISK, not just the screen. Without
   * this, saying yes to "discard your changes?" left the draft in place, and
   * reopening the file recovered the very edits that had just been thrown
   * away — so the prompt came back, for changes the user had already refused.
   */
  async function discardDraft(name) {
    clearTimeout(draftTimer);
    if (!name) return;
    try {
      await MibEditorDiscardDraft(name);
    } catch (e) {
      /* nothing recoverable to do */
    }
  }

  /** Called after a successful save: the file on disk is now the buffer. */
  async function markSaved(sha256, diagnostics) {
    const s = get({ subscribe });
    update((st) => ({
      ...st,
      source: { ...st.source, content: st.buffer, sha256, external: false },
      diagnostics: diagnostics || st.diagnostics,
    }));
    if (s.source) {
      try {
        await MibEditorDiscardDraft(s.source.name);
      } catch (e) {
        /* nothing recoverable to do */
      }
    }
  }

  // Reverting is a decision, so the draft goes NOW rather than in a second and
  // a bit — long enough to leave the file and have it survive.
  function revert() {
    const name = get({ subscribe }).source?.name;
    update((s) => (s.source ? { ...s, buffer: s.source.content, diagnostics: s.source.diagnostics || [] } : s));
    discardDraft(name);
  }

  function close() {
    clearTimeout(checkTimer);
    clearTimeout(draftTimer);
    set({ ...EMPTY });
  }

  return { subscribe, open, openSource, setBuffer, refresh, markSaved, revert, close, dirty, discardDraft };
}

export const mibEditorStore = createMibEditorStore();
