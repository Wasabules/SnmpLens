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
  async function refresh() {
    const { buffer } = get({ subscribe });
    try {
      const out = (await MibEditorAnalyse(buffer)) || {};
      update((s) => ({
        ...s,
        diagnostics: out.diagnostics || [],
        missingImports: out.missing || [],
        checking: false,
      }));
    } catch (e) {
      update((s) => ({ ...s, checking: false }));
    }
  }

  function scheduleDraft() {
    clearTimeout(draftTimer);
    draftTimer = setTimeout(async () => {
      const s = get({ subscribe });
      if (!s.source) return;
      try {
        if (isDirty(s)) {
          await MibEditorSaveDraft(s.source.name, s.buffer);
        } else {
          await MibEditorDiscardDraft(s.source.name);
        }
      } catch (e) {
        // A draft is a safety net, not a feature; failing to write one must
        // never interrupt typing.
      }
    }, 1200);
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

  function revert() {
    update((s) => (s.source ? { ...s, buffer: s.source.content, diagnostics: s.source.diagnostics || [] } : s));
    scheduleDraft();
  }

  function close() {
    clearTimeout(checkTimer);
    clearTimeout(draftTimer);
    set({ ...EMPTY });
  }

  return { subscribe, open, openSource, setBuffer, refresh, markSaved, revert, close, dirty };
}

export const mibEditorStore = createMibEditorStore();
