<script>
  import { onMount, tick } from 'svelte';
  import { _ } from 'svelte-i18n';
  import { get } from 'svelte/store';
  import Icon from './Icon.svelte';
  import { notificationStore } from './stores/notifications';
  import { highlight, SNIPPETS } from './mibeditor/tokenize.js';
  import { charWidth, positionOf, lineColumnAt, wordAt, offsetAt } from './mibeditor/metrics.js';
  import { mibEditorStore } from './stores/mibEditorStore';
  import { mibPathsStore } from './stores/mibPathsStore';
  import { mibStore } from './stores/mibStore';
  import {
    MibEditorList,
    MibEditorOpenExternal,
    MibEditorSave,
    MibEditorRestoreBundled,
    MibEditorReload,
    MibEditorSymbols,
    MibEditorFixImports,
  } from '../wailsjs/go/main/App';

  let files = [];
  let filter = '';
  let saving = false;
  let showSnippets = false;
  let showSymbols = false;
  let symbolFilter = '';
  let catalogue = { modules: [], symbols: [] };
  let textarea;
  let mirror;
  let gutter;
  let surface;

  // Overlay geometry. The mirror lays out identically to the text — that is
  // unit-tested — and both are monospace with a fixed line height, so a
  // position is arithmetic once the character width is measured.
  const LINE_HEIGHT = 18;
  let cw = 7.2;
  let scrollTop = 0;
  let scrollLeft = 0;

  let hover = null;        // { x, y, symbol }
  let completion = null;   // { x, y, items, index, range }
  let findOpen = false;
  let findTerm = '';
  let findCount = 0;

  $: lines = buffer.split('\n');
  // Only errors and warnings get an underline; advice would be visual noise.
  $: marks = diagnostics
    .filter((d) => d.line > 0 && d.severity !== 'info')
    .slice(0, 200)
    .map((d) => {
      const pos = positionOf(lines, d.line, d.column || 1, cw, LINE_HEIGHT);
      const text = lines[d.line - 1] || '';
      const width = Math.max(cw * 2, cw * Math.max(1, (d.symbol || '').length || wordAt(text, (d.column || 1) - 1).word.length || 6));
      return { ...d, x: pos.x, y: pos.y, width };
    });

  // The buffer lives in a store, not here: this component is destroyed on
  // every tab switch, and component state would take the user's edits with it.
  $: source = $mibEditorStore.source;
  $: buffer = $mibEditorStore.buffer;
  $: diagnostics = $mibEditorStore.diagnostics;
  $: missingImports = $mibEditorStore.missingImports;
  $: checking = $mibEditorStore.checking;
  $: dirty = source !== null && buffer !== source.content;
  $: matchingSymbols = symbolFilter.length < 2
    ? []
    : catalogue.symbols
        .filter((sy) => sy.name.toLowerCase().includes(symbolFilter.toLowerCase()))
        .slice(0, 60);
  $: lineCount = buffer.split('\n').length;
  // Highlighting a 185 KB MIB takes ~45 ms. Doing it on every keystroke makes
  // typing stutter on a big file, so the mirror lags the text by a frame or
  // two — invisible while typing, and the caret is the textarea's own.
  let highlighted = '';
  let highlightTimer;
  $: scheduleHighlight(buffer);
  function scheduleHighlight(text) {
    clearTimeout(highlightTimer);
    if (text.length < 20000) {
      highlighted = highlight(text);
      return;
    }
    highlightTimer = setTimeout(() => (highlighted = highlight(text)), 90);
  }
  $: shown = files.filter((f) => !filter || f.name.toLowerCase().includes(filter.toLowerCase()));
  $: errorCount = diagnostics.filter((d) => d.severity === 'error').length;
  $: warnCount = diagnostics.filter((d) => d.severity === 'warning').length;

  onMount(async () => {
    if (mirror) cw = charWidth(mirror);
    await refreshList();
    try {
      catalogue = (await MibEditorSymbols()) || { modules: [], symbols: [] };
    } catch (e) {
      catalogue = { modules: [], symbols: [] };
    }
  });

  async function refreshList() {
    try {
      files = (await MibEditorList()) || [];
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }

  async function open(name) {
    if (!(await confirmDiscard())) return;
    try {
      const { recovered } = await mibEditorStore.open(name);
      if (recovered) {
        notificationStore.add(get(_)('mibEditor.draftRecovered', { values: { name } }), 'warning');
      }
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }

  async function openExternal() {
    if (!(await confirmDiscard())) return;
    try {
      const s = await MibEditorOpenExternal();
      if (!s || !s.name) return; // cancelled
      mibEditorStore.openSource(s);
      notificationStore.add(get(_)('mibEditor.externalOpened', { values: { name: s.name } }), 'info');
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }

  // Losing an edit to a misclick is the one failure this editor must not have.
  async function confirmDiscard() {
    if (!dirty) return true;
    return window.confirm(get(_)('mibEditor.discardConfirm'));
  }

  // Validation runs in Go and touches nothing: no file, no gosmi state. That is
  // what makes it safe to run while someone types.
  function onInput(e) {
    mibEditorStore.setBuffer(e.target.value);
    updateCompletion();
  }

  async function save(force = false) {
    if (!source) return;
    saving = true;
    try {
      const res = await MibEditorSave(source.name, buffer, source.sha256, force);
      if (res.conflict) {
        const overwrite = window.confirm(get(_)('mibEditor.conflictConfirm'));
        saving = false;
        if (overwrite) await save(true);
        return;
      }
      if (!res.saved) {
        notificationStore.add(get(_)('mibEditor.saveFailed'), 'error');
        return;
      }
      await mibEditorStore.markSaved(res.sha256, res.diagnostics || []);
      notificationStore.add(
        get(_)(res.backupPath ? 'mibEditor.savedWithBackup' : 'mibEditor.saved',
          { values: { name: source.name } }), 'success');
      await refreshList();
      await reload();
    } catch (e) {
      notificationStore.add(String(e), 'error');
    } finally {
      saving = false;
    }
  }

  // The MIBs the user actually enabled. Passing an empty list used to make the
  // backend fall back to "everything on disk", which silently switched back on
  // every MIB somebody had deliberately turned off.
  function enabledMibFiles() {
    const state = get(mibPathsStore);
    const out = [];
    for (const perPath of Object.values(state.enabledMibs || {})) {
      for (const [name, on] of Object.entries(perPath || {})) {
        if (on) out.push(name);
      }
    }
    return [...new Set(out)];
  }

  // Rebuilding is the only way to see the effect of an edit: gosmi has no
  // unload, so without a full teardown the previously parsed module stays.
  async function reload() {
    try {
      const res = await MibEditorReload(enabledMibFiles());
      const failed = (res.diagnostics || []).filter((d) => !d.success);
      if (res.health && !res.health.ok) {
        notificationStore.add(
          get(_)('mibEditor.treeBroken', { values: { failures: res.health.failures.join(', ') } }), 'error');
      } else if (failed.length) {
        notificationStore.add(
          get(_)('mibEditor.reloadedWithFailures', { values: { count: failed.length } }), 'warning');
      } else {
        notificationStore.add(get(_)('mibEditor.reloaded', { values: { count: res.health?.modules ?? 0 } }), 'success');
      }
      // The rebuilt tree was being thrown away, so the MIB browser kept showing
      // the state from before the edit. mibStore owns that tree, so it is the
      // one that has to be told.
      mibStore.loadSilent();
      try {
        catalogue = (await MibEditorSymbols()) || catalogue;
      } catch (e) {
        /* keep the previous catalogue */
      }
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }

  async function restoreBundled() {
    if (!source?.bundled) return;
    if (!window.confirm(get(_)('mibEditor.restoreConfirm', { values: { name: source.name } }))) return;
    try {
      mibEditorStore.openSource(await MibEditorRestoreBundled(source.name));
      await refreshList();
      await reload();
      notificationStore.add(get(_)('mibEditor.restored', { values: { name: source.name } }), 'success');
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }

  function revert() {
    if (!source || !dirty) return;
    if (!window.confirm(get(_)('mibEditor.revertConfirm'))) return;
    mibEditorStore.revert();
  }

  // The one place the editor goes from "here is the problem" to "here is the
  // repair": the diagnostic knows the symbol, the catalogue knows its module.
  async function fixImports() {
    try {
      const fix = await MibEditorFixImports(buffer);
      if (!fix.content) return;
      // Replace the whole buffer as one edit so it stays undoable.
      insertAtCaret(fix.content, [0, buffer.length]);
      notificationStore.add(
        get(_)('mibEditor.importsFixed', { values: { count: fix.missing.length } }), 'success');
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }

  // Inserting by reassigning textarea.value wipes the browser's native undo
  // stack, so Ctrl+Z stopped working the moment you used a snippet or the
  // IMPORTS fix. execCommand('insertText') is deprecated but it is the only
  // way to write into a textarea AS AN EDIT, which is what undo records.
  function insertAtCaret(text, replaceRange) {
    if (!textarea) {
      mibEditorStore.setBuffer(buffer + text);
      return;
    }
    textarea.focus();
    if (replaceRange) {
      textarea.setSelectionRange(replaceRange[0], replaceRange[1]);
    }
    const inserted = document.execCommand && document.execCommand('insertText', false, text);
    if (!inserted) {
      // Fallback for a webview that refuses it: correct, but undo is lost.
      const start = textarea.selectionStart;
      const end = textarea.selectionEnd;
      mibEditorStore.setBuffer(buffer.slice(0, start) + text + buffer.slice(end));
      tick().then(() => textarea.setSelectionRange(start + text.length, start + text.length));
      return;
    }
    mibEditorStore.setBuffer(textarea.value);
  }

  // Find. Deliberately simple: highlight nothing, just move the caret to the
  // next match and scroll it into view. A full find-and-replace over a file
  // that other tools also edit is a different feature.
  function findNext(backwards = false) {
    if (!findTerm || !textarea) return;
    const haystack = buffer.toLowerCase();
    const needle = findTerm.toLowerCase();
    findCount = needle ? haystack.split(needle).length - 1 : 0;
    if (findCount === 0) return;

    let at;
    if (backwards) {
      at = haystack.lastIndexOf(needle, Math.max(0, textarea.selectionStart - 1));
      if (at < 0) at = haystack.lastIndexOf(needle);
    } else {
      at = haystack.indexOf(needle, textarea.selectionEnd);
      if (at < 0) at = haystack.indexOf(needle);
    }
    if (at < 0) return;

    const { line } = lineColumnAt(buffer, at);
    textarea.focus();
    textarea.setSelectionRange(at, at + findTerm.length);
    textarea.scrollTop = Math.max(0, (line - 1) * LINE_HEIGHT - textarea.clientHeight / 3);
    syncScroll();
  }

  function closeFind() {
    findOpen = false;
    findTerm = '';
    findCount = 0;
    textarea?.focus();
  }

  // Clicking a problem puts the caret on it. Without this the line number is
  // just a number.
  async function jumpTo(d) {
    if (!textarea || !d.line) return;
    const lines = buffer.split('\n');
    let offset = 0;
    for (let i = 0; i < Math.min(d.line - 1, lines.length); i++) offset += lines[i].length + 1;
    offset += Math.max(0, (d.column || 1) - 1);

    textarea.focus();
    textarea.setSelectionRange(offset, offset);
    await tick();
    // Scroll the target line to roughly a third down the pane.
    const lineHeight = textarea.scrollHeight / Math.max(1, lineCount);
    textarea.scrollTop = Math.max(0, (d.line - 1) * lineHeight - textarea.clientHeight / 3);
    syncScroll();
  }

  // Hover: the word under the pointer, looked up in the loaded tree.
  let hoverTimer;
  function onMouseMove(e) {
    clearTimeout(hoverTimer);
    const rect = textarea.getBoundingClientRect();
    const px = e.clientX - rect.left + textarea.scrollLeft - 8;
    const py = e.clientY - rect.top + textarea.scrollTop - 8;
    hoverTimer = setTimeout(() => {
      const offset = offsetAt(lines, buffer, px, py, cw, LINE_HEIGHT);
      if (offset === null) { hover = null; return; }
      const { word } = wordAt(buffer, offset);
      if (word.length < 2) { hover = null; return; }
      const symbol = catalogue.symbols.find((sy) => sy.name === word);
      hover = symbol ? { x: e.clientX - rect.left, y: e.clientY - rect.top, symbol } : null;
    }, 220);
  }

  function onMouseLeave() {
    clearTimeout(hoverTimer);
    hover = null;
  }

  // Completion, anchored at the caret. Possible here because the mirror
  // guarantees identical layout; in a bare textarea the caret has no
  // measurable position at all.
  function updateCompletion() {
    if (!textarea) return;
    const offset = textarea.selectionStart;
    const { word, start, end } = wordAt(buffer, offset);
    if (word.length < 2 || offset !== end) { completion = null; return; }

    const items = catalogue.symbols
      .filter((sy) => sy.name.toLowerCase().startsWith(word.toLowerCase()) && sy.name !== word)
      .slice(0, 12);
    if (items.length === 0) { completion = null; return; }

    const { line, column } = lineColumnAt(buffer, start);
    const pos = positionOf(lines, line, column, cw, LINE_HEIGHT);
    completion = {
      x: pos.x - scrollLeft + 8,
      y: pos.y - scrollTop + LINE_HEIGHT + 8,
      items, index: 0, range: [start, end],
    };
  }

  function acceptCompletion() {
    if (!completion) return;
    const chosen = completion.items[completion.index];
    const range = completion.range;
    completion = null;
    insertAtCaret(chosen.name, range);
  }

  function insertSnippet(snippet) {
    showSnippets = false;
    insertAtCaret(snippet.text.replace(/\$\{name\}/g, 'myObject'));
  }

  // The mirror only stays under the text if it scrolls with it.
  function syncScroll() {
    if (textarea) {
      scrollTop = textarea.scrollTop;
      scrollLeft = textarea.scrollLeft;
    }
    if (mirror && textarea) {
      mirror.scrollTop = textarea.scrollTop;
      mirror.scrollLeft = textarea.scrollLeft;
    }
    if (gutter && textarea) gutter.scrollTop = textarea.scrollTop;
  }

  function onKeydown(e) {
    if (completion) {
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        completion.index = (completion.index + 1) % completion.items.length;
        completion = completion;
        return;
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault();
        completion.index = (completion.index - 1 + completion.items.length) % completion.items.length;
        completion = completion;
        return;
      }
      if (e.key === 'Enter' || e.key === 'Tab') {
        e.preventDefault();
        acceptCompletion();
        return;
      }
      if (e.key === 'Escape') {
        completion = null;
        return;
      }
    }
    // A Wails webview has no Ctrl+F of its own, and IP-MIB is 4,993 lines.
    if ((e.ctrlKey || e.metaKey) && e.key === 'f') {
      e.preventDefault();
      findOpen = true;
      tick().then(() => document.getElementById('mib-find')?.focus());
      return;
    }
    if ((e.ctrlKey || e.metaKey) && e.key === 's') {
      e.preventDefault();
      save();
      return;
    }
    // A textarea would otherwise move focus out of the editor on Tab.
    if (e.key === 'Tab') {
      e.preventDefault();
      insertAtCaret('    ');
    }
  }
</script>

<div class="mib-editor">
  <!-- ---------------- file rail ---------------- -->
  <aside class="rail">
    <div class="rail-head">
      <input type="search" bind:value={filter} placeholder={$_('mibEditor.filter')} />
      <button class="btn-copy-small" on:click={refreshList} title={$_('mibEditor.refresh')}>
        <Icon name="refresh-cw" size={13} />
      </button>
    </div>
    <button class="btn btn-small full" on:click={openExternal}>
      <Icon name="folder-open" size={13} /> {$_('mibEditor.openExternal')}
    </button>

    <ul class="files">
      {#each shown as f (f.name)}
        <li>
          <button class="file" class:active={source && !source.external && source.name === f.name}
            on:click={() => open(f.name)}>
            <span class="fname">{f.name}</span>
            {#if f.bundled}
              <span class="tag" class:changed={f.modified}
                title={f.modified ? $_('mibEditor.bundledModified') : $_('mibEditor.bundled')}>
                {f.modified ? '±' : '◆'}
              </span>
            {/if}
          </button>
        </li>
      {/each}
      {#if shown.length === 0}
        <li class="empty">{$_('mibEditor.noFiles')}</li>
      {/if}
    </ul>
  </aside>

  <!-- ---------------- editor ---------------- -->
  <section class="main">
    {#if !source}
      <div class="placeholder">
        <Icon name="book-marked" size={28} />
        <p>{$_('mibEditor.placeholder')}</p>
      </div>
    {:else}
      <div class="toolbar">
        <span class="title">
          {source.name}
          {#if source.external}<span class="chip">{$_('mibEditor.external')}</span>{/if}
          {#if source.bundled}<span class="chip warn">{$_('mibEditor.bundled')}</span>{/if}
          {#if dirty}<span class="chip dot">{$_('mibEditor.unsaved')}</span>{/if}
        </span>
        <span class="spacer"></span>

        <div class="snip">
          <button class="btn btn-small" on:click={() => (showSnippets = !showSnippets)}>
            <Icon name="file-plus" size={13} /> {$_('mibEditor.insert')}
          </button>
          {#if showSnippets}
            <ul class="snip-menu">
              {#each SNIPPETS as s (s.key)}
                <li><button on:click={() => insertSnippet(s)}>{$_('mibEditor.snippet.' + s.key)}</button></li>
              {/each}
            </ul>
          {/if}
        </div>

        <div class="snip">
          <button class="btn btn-small" on:click={() => (showSymbols = !showSymbols)}>
            <Icon name="book-marked" size={13} /> {$_('mibEditor.symbols')}
          </button>
          {#if showSymbols}
            <div class="sym-menu">
              <input type="search" bind:value={symbolFilter}
                placeholder={$_('mibEditor.symbolSearch')} />
              <ul>
                {#each matchingSymbols as sy (sy.module + '.' + sy.name)}
                  <li>
                    <button on:click={() => { showSymbols = false; insertAtCaret(sy.name); }}
                      title={sy.description}>
                      <span class="sy-name">{sy.name}</span>
                      <span class="sy-mod">{sy.module}</span>
                    </button>
                  </li>
                {/each}
                {#if symbolFilter.length < 2}
                  <li class="sy-hint">{$_('mibEditor.symbolHint', { values: { count: catalogue.symbols.length } })}</li>
                {:else if matchingSymbols.length === 0}
                  <li class="sy-hint">{$_('mibEditor.symbolNone')}</li>
                {/if}
              </ul>
            </div>
          {/if}
        </div>

        {#if source.bundled}
          <button class="btn btn-small" on:click={restoreBundled}>{$_('mibEditor.restore')}</button>
        {/if}
        <button class="btn btn-small" on:click={revert} disabled={!dirty}>{$_('mibEditor.revert')}</button>
        <button class="btn btn-small" on:click={() => save()} disabled={saving || !dirty}>
          <Icon name={saving ? 'loader-circle' : 'download'} size={13} class={saving ? 'icon-spin' : ''} />
          {$_('common.save')}
        </button>
      </div>

      {#if source.bundled}
        <p class="banner">
          <Icon name="triangle-alert" size={13} /> {$_('mibEditor.bundledWarning')}
        </p>
      {/if}
      {#if source.external}
        <p class="banner">
          <Icon name="triangle-alert" size={13} /> {$_('mibEditor.externalWarning', { values: { name: source.name } })}
        </p>
      {/if}

      <!-- The textarea is transparent and sits over the coloured mirror. Both
           layers must use the same font metrics or they drift apart. -->
      {#if findOpen}
        <div class="findbar">
          <Icon name="search" size={13} />
          <input id="mib-find" type="search" bind:value={findTerm}
            on:input={() => findNext()}
            on:keydown={(e) => {
              if (e.key === 'Enter') { e.preventDefault(); findNext(e.shiftKey); }
              if (e.key === 'Escape') { e.preventDefault(); closeFind(); }
            }}
            placeholder={$_('mibEditor.find')} />
          <span class="find-count">{findCount}</span>
          <button class="btn-copy-small" on:click={() => findNext(true)} title={$_('mibEditor.findPrev')}>
            <Icon name="chevron-up" size={13} />
          </button>
          <button class="btn-copy-small" on:click={() => findNext()} title={$_('mibEditor.findNext')}>
            <Icon name="chevron-down" size={13} />
          </button>
          <button class="btn-copy-small" on:click={closeFind}><Icon name="circle-x" size={13} /></button>
        </div>
      {/if}

      <div class="surface" bind:this={surface}>
        <pre class="gutter" bind:this={gutter} aria-hidden="true">{#each Array(lineCount) as _unused, i}{i + 1}
{/each}</pre>
        <div class="code">
          <pre class="mirror" bind:this={mirror} aria-hidden="true">{@html highlighted}<br /></pre>
          <!-- Underlines sit between the mirror and the textarea, so they are
               visible through the transparent text layer without catching the
               pointer. -->
          <div class="marks" aria-hidden="true"
            style="transform: translate({-scrollLeft}px, {-scrollTop}px)">
            {#each marks as m, i (i)}
              <span class="mark {m.severity}"
                style="left: {m.x}px; top: {m.y}px; width: {m.width}px;"></span>
            {/each}
          </div>
          <textarea
            bind:this={textarea}
            value={buffer}
            on:input={onInput}
            on:scroll={syncScroll}
            on:keydown={onKeydown}
            on:click={() => (completion = null)}
            on:blur={() => (completion = null)}
            on:mousemove={onMouseMove}
            on:mouseleave={onMouseLeave}
            spellcheck="false"
            autocomplete="off"
            autocapitalize="off"
            wrap="off"></textarea>

          {#if hover}
            <div class="hovercard" style="left: {hover.x + 12}px; top: {hover.y + 18}px;">
              <div class="hc-head">
                <code>{hover.symbol.name}</code>
                <span class="hc-mod">{hover.symbol.module}</span>
              </div>
              {#if hover.symbol.oid}<div class="hc-oid">{hover.symbol.oid}</div>{/if}
              {#if hover.symbol.description}<p>{hover.symbol.description}</p>{/if}
            </div>
          {/if}

          {#if completion}
            <ul class="complete" style="left: {completion.x}px; top: {completion.y}px;">
              {#each completion.items as item, i (item.module + '.' + item.name)}
                <li>
                  <button class:sel={i === completion.index}
                    on:mousedown|preventDefault={() => { completion.index = i; acceptCompletion(); }}>
                    <span class="sy-name">{item.name}</span>
                    <span class="sy-mod">{item.module}</span>
                  </button>
                </li>
              {/each}
            </ul>
          {/if}
        </div>
      </div>

      <div class="status">
        <span>{$_('mibEditor.lines', { values: { count: lineCount } })}</span>
        <span>{source.eol === 'crlf' ? 'CRLF' : 'LF'}</span>
        <span class="spacer"></span>
        {#if checking}
          <span class="pill">{$_('mibEditor.checking')}</span>
        {:else if errorCount > 0}
          <span class="pill err">{$_('mibEditor.errorCount', { values: { count: errorCount } })}</span>
        {:else if warnCount > 0}
          <span class="pill warn">{$_('mibEditor.warnCount', { values: { count: warnCount } })}</span>
        {:else}
          <span class="pill ok">{$_('mibEditor.parses')}</span>
        {/if}
      </div>

      {#if missingImports.length}
        <div class="fixbar">
          <Icon name="circle-alert" size={13} />
          <span>{$_('mibEditor.missingImports', {
            values: { count: missingImports.length, symbols: missingImports.slice(0, 4).map((m) => m.symbol).join(', ') } })}</span>
          <span class="spacer"></span>
          <button class="btn btn-small" on:click={fixImports}>{$_('mibEditor.fixImports')}</button>
        </div>
      {/if}

      {#if diagnostics.length}
        <ul class="problems">
          {#each diagnostics as d, i (i)}
            <li>
              <button on:click={() => jumpTo(d)}>
                <span class="sev {d.severity}">{d.severity}</span>
                <span class="pos">{d.line || '?'}:{d.column || '?'}</span>
                <span class="msg">{d.message}</span>
                <span class="code">{d.code}</span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    {/if}
  </section>
</div>

<style>
  .mib-editor {
    display: grid;
    grid-template-columns: 230px minmax(0, 1fr);
    gap: var(--space-sm);
    height: 100%;
    min-height: 0;
  }

  .rail {
    display: flex;
    flex-direction: column;
    gap: var(--space-xs);
    min-height: 0;
    border-right: 1px solid var(--border-color);
    padding-right: var(--space-sm);
  }

  .rail-head {
    display: flex;
    gap: 4px;
  }

  .rail-head input {
    flex: 1;
    min-width: 0;
    padding: 5px 7px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
    font-size: 0.8em;
  }

  .full {
    width: 100%;
    justify-content: center;
  }

  .files {
    list-style: none;
    margin: 0;
    padding: 0;
    overflow-y: auto;
    min-height: 0;
    flex: 1;
  }

  .file {
    display: flex;
    align-items: center;
    gap: 5px;
    width: 100%;
    padding: 4px 6px;
    background: none;
    border: none;
    border-radius: 3px;
    color: var(--text-color);
    font-size: 0.8em;
    text-align: left;
    cursor: pointer;
  }

  .file:hover {
    background-color: var(--hover-overlay);
  }

  .file.active {
    background-color: var(--accent-subtle);
    color: var(--accent-color);
  }

  .fname {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tag {
    color: var(--text-muted);
    font-size: 0.9em;
  }

  .tag.changed {
    color: var(--warning-color);
  }

  .empty {
    padding: 8px 6px;
    font-size: 0.78em;
    color: var(--text-muted);
  }

  .main {
    display: flex;
    flex-direction: column;
    min-height: 0;
    min-width: 0;
    gap: var(--space-xs);
  }

  .placeholder {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    color: var(--text-muted);
    font-size: 0.85em;
  }

  .toolbar {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }

  .title {
    font-size: 0.9em;
    font-weight: 600;
    color: var(--text-color);
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .chip {
    padding: 1px 6px;
    border: 1px solid var(--border-color);
    border-radius: 10px;
    font-size: 0.7em;
    font-weight: 400;
    color: var(--text-muted);
  }

  .chip.warn {
    color: var(--warning-color);
    border-color: var(--warning-border);
  }

  .chip.dot {
    color: var(--accent-color);
    border-color: var(--accent-border);
  }

  .spacer {
    flex: 1;
  }

  .snip {
    position: relative;
  }

  .snip-menu {
    position: absolute;
    right: 0;
    top: 100%;
    z-index: 20;
    list-style: none;
    margin: 4px 0 0;
    padding: 4px;
    min-width: 200px;
    background-color: var(--bg-light-color);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    box-shadow: 0 6px 18px var(--shadow-color);
  }

  .snip-menu button {
    width: 100%;
    padding: 5px 8px;
    background: none;
    border: none;
    border-radius: 3px;
    color: var(--text-color);
    font-size: 0.8em;
    text-align: left;
    cursor: pointer;
  }

  .snip-menu button:hover {
    background-color: var(--hover-overlay);
  }

  .banner {
    margin: 0;
    padding: 5px 8px;
    background-color: var(--warning-subtle);
    border-radius: 4px;
    font-size: 0.76em;
    color: var(--warning-color);
    display: flex;
    align-items: center;
    gap: 6px;
  }

  /* ---- the editing surface ----
     A transparent textarea over a coloured mirror. Every font and spacing
     property below MUST be identical between .mirror and textarea, or the two
     layers drift apart line by line. */
  .surface {
    flex: 1;
    min-height: 0;
    display: flex;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    overflow: hidden;
    background-color: var(--bg-lighter-color);
  }

  .gutter,
  .mirror,
  .surface textarea {
    margin: 0;
    padding: 8px 0;
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 12px;
    line-height: 18px;
    tab-size: 4;
  }

  .gutter {
    flex: 0 0 auto;
    padding-right: 8px;
    padding-left: 8px;
    text-align: right;
    color: var(--text-dimmed);
    background-color: var(--bg-light-color);
    border-right: 1px solid var(--border-color);
    overflow: hidden;
    user-select: none;
    white-space: pre;
  }

  .code {
    position: relative;
    flex: 1;
    min-width: 0;
  }

  .mirror,
  .surface textarea {
    position: absolute;
    inset: 0;
    padding-left: 8px;
    padding-right: 8px;
    white-space: pre;
    overflow: auto;
    border: none;
    word-wrap: normal;
  }

  .mirror {
    pointer-events: none;
    color: var(--text-color);
  }

  .surface textarea {
    background: transparent;
    color: transparent;
    caret-color: var(--text-color);
    resize: none;
    outline: none;
  }

  .surface textarea::selection {
    background-color: var(--accent-subtle-strong);
  }

  /* Token colours. Deliberately few: a MIB is mostly prose inside
     DESCRIPTION, and colouring everything would make none of it stand out. */
  .mirror :global(.mac) { color: var(--accent-color); font-weight: 600; }
  .mirror :global(.kw)  { color: var(--name-color); }
  .mirror :global(.ty)  { color: var(--oid-color); }
  .mirror :global(.val) { color: var(--success-color); }
  .mirror :global(.str) { color: var(--text-light); }
  .mirror :global(.com) { color: var(--text-dimmed); font-style: italic; }
  .mirror :global(.num) { color: var(--oid-color); }
  .mirror :global(.op)  { color: var(--favorites-color); }
  .mirror :global(.pun) { color: var(--text-muted); }

  .sym-menu {
    position: absolute;
    right: 0;
    top: 100%;
    z-index: 20;
    width: 300px;
    margin-top: 4px;
    padding: 6px;
    background-color: var(--bg-light-color);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    box-shadow: 0 6px 18px var(--shadow-color);
  }

  .sym-menu input {
    width: 100%;
    padding: 5px 7px;
    margin-bottom: 4px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
    font-size: 0.8em;
  }

  .sym-menu ul {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 260px;
    overflow-y: auto;
  }

  .sym-menu li button {
    display: flex;
    align-items: baseline;
    gap: 8px;
    width: 100%;
    padding: 3px 6px;
    background: none;
    border: none;
    border-radius: 3px;
    color: var(--text-color);
    font-size: 0.78em;
    text-align: left;
    cursor: pointer;
  }

  .sym-menu li button:hover {
    background-color: var(--hover-overlay);
  }

  .sy-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  }

  .sy-mod {
    color: var(--text-muted);
    font-size: 0.9em;
  }

  .sy-hint {
    padding: 6px;
    font-size: 0.75em;
    color: var(--text-muted);
  }

  .fixbar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 8px;
    background-color: var(--accent-subtle);
    border-radius: 4px;
    font-size: 0.76em;
    color: var(--text-color);
  }

  .findbar {
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 4px 6px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
  }

  .findbar input {
    flex: 1;
    min-width: 0;
    padding: 4px 6px;
    background-color: var(--bg-color);
    border: 1px solid var(--border-color);
    border-radius: 3px;
    color: var(--text-color);
    font-size: 0.8em;
  }

  .find-count {
    font-size: 0.75em;
    color: var(--text-muted);
    min-width: 28px;
    text-align: right;
  }

  /* Underlines live under the transparent textarea, so they never intercept
     the pointer or the caret. */
  .marks {
    position: absolute;
    inset: 0;
    padding: 8px;
    pointer-events: none;
    overflow: hidden;
  }

  .mark {
    position: absolute;
    height: 18px;
    border-bottom: 2px solid transparent;
    box-sizing: border-box;
  }

  .mark.error {
    border-bottom-color: var(--error-color);
  }

  .mark.warning {
    border-bottom-color: var(--warning-color);
  }

  .hovercard {
    position: absolute;
    z-index: 30;
    max-width: 320px;
    padding: 7px 9px;
    background-color: var(--bg-light-color);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    box-shadow: 0 6px 18px var(--shadow-color);
    pointer-events: none;
    font-size: 0.75em;
  }

  .hc-head {
    display: flex;
    align-items: baseline;
    gap: 8px;
    margin-bottom: 3px;
  }

  .hc-head code {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    color: var(--accent-color);
  }

  .hc-mod {
    color: var(--text-muted);
  }

  .hc-oid {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    color: var(--oid-color);
    margin-bottom: 3px;
  }

  .hovercard p {
    margin: 0;
    color: var(--text-muted);
  }

  .complete {
    position: absolute;
    z-index: 30;
    list-style: none;
    margin: 0;
    padding: 3px;
    min-width: 220px;
    max-height: 200px;
    overflow-y: auto;
    background-color: var(--bg-light-color);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    box-shadow: 0 6px 18px var(--shadow-color);
  }

  .complete button {
    display: flex;
    align-items: baseline;
    gap: 8px;
    width: 100%;
    padding: 3px 6px;
    background: none;
    border: none;
    border-radius: 3px;
    color: var(--text-color);
    font-size: 0.78em;
    text-align: left;
    cursor: pointer;
  }

  .complete button.sel,
  .complete button:hover {
    background-color: var(--accent-subtle);
  }

  .status {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 0.74em;
    color: var(--text-muted);
  }

  .pill {
    padding: 1px 8px;
    border-radius: 10px;
    border: 1px solid var(--border-color);
  }

  .pill.ok { color: var(--success-color); border-color: var(--success-border); }
  .pill.warn { color: var(--warning-color); border-color: var(--warning-border); }
  .pill.err { color: var(--error-color); border-color: var(--error-border); }

  .problems {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 150px;
    overflow-y: auto;
    border: 1px solid var(--border-color);
    border-radius: 4px;
  }

  .problems button {
    display: flex;
    align-items: baseline;
    gap: 8px;
    width: 100%;
    padding: 3px 8px;
    background: none;
    border: none;
    color: var(--text-color);
    font-size: 0.76em;
    text-align: left;
    cursor: pointer;
  }

  .problems button:hover {
    background-color: var(--hover-overlay);
  }

  .sev {
    flex: 0 0 52px;
    text-transform: uppercase;
    font-size: 0.85em;
  }

  .sev.error { color: var(--error-color); }
  .sev.warning { color: var(--warning-color); }
  .sev.info { color: var(--text-muted); }

  .pos {
    flex: 0 0 56px;
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    color: var(--text-muted);
  }

  .msg {
    flex: 1;
    min-width: 0;
  }

  .problems .code {
    color: var(--text-dimmed);
    font-size: 0.9em;
    position: static;
    flex: none;
  }
</style>
