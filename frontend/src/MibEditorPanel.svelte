<script>
  import { onMount, tick } from 'svelte';
  import { _ } from 'svelte-i18n';
  import { get } from 'svelte/store';
  import Icon from './Icon.svelte';
  import { notificationStore } from './stores/notifications';
  import { highlight, SNIPPETS } from './mibeditor/tokenize.js';
  import {
    MibEditorList,
    MibEditorRead,
    MibEditorOpenExternal,
    MibEditorValidate,
    MibEditorSave,
    MibEditorRestoreBundled,
    MibEditorReload,
  } from '../wailsjs/go/main/App';

  let files = [];
  let filter = '';
  let source = null;      // the open file, as read
  let buffer = '';        // what is in the textarea
  let diagnostics = [];
  let checking = false;
  let saving = false;
  let showSnippets = false;
  let textarea;
  let mirror;
  let gutter;

  $: dirty = source !== null && buffer !== source.content;
  $: lineCount = buffer.split('\n').length;
  $: highlighted = highlight(buffer);
  $: shown = files.filter((f) => !filter || f.name.toLowerCase().includes(filter.toLowerCase()));
  $: errorCount = diagnostics.filter((d) => d.severity === 'error').length;
  $: warnCount = diagnostics.filter((d) => d.severity === 'warning').length;

  onMount(refreshList);

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
      source = await MibEditorRead(name);
      buffer = source.content;
      diagnostics = source.diagnostics || [];
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }

  async function openExternal() {
    if (!(await confirmDiscard())) return;
    try {
      const s = await MibEditorOpenExternal();
      if (!s || !s.name) return; // cancelled
      source = s;
      buffer = s.content;
      diagnostics = s.diagnostics || [];
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
  let checkTimer;
  function scheduleCheck() {
    clearTimeout(checkTimer);
    checking = true;
    checkTimer = setTimeout(async () => {
      try {
        diagnostics = (await MibEditorValidate(buffer)) || [];
      } catch (e) {
        diagnostics = [];
      } finally {
        checking = false;
      }
    }, 350);
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
      source = { ...source, content: buffer, sha256: res.sha256, external: false };
      diagnostics = res.diagnostics || [];
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

  // Rebuilding is the only way to see the effect of an edit: gosmi has no
  // unload, so without a full teardown the previously parsed module stays.
  async function reload() {
    try {
      const res = await MibEditorReload([]);
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
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }

  async function restoreBundled() {
    if (!source?.bundled) return;
    if (!window.confirm(get(_)('mibEditor.restoreConfirm', { values: { name: source.name } }))) return;
    try {
      source = await MibEditorRestoreBundled(source.name);
      buffer = source.content;
      diagnostics = source.diagnostics || [];
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
    buffer = source.content;
    diagnostics = source.diagnostics || [];
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

  function insertSnippet(snippet) {
    const text = snippet.text.replace(/\$\{name\}/g, 'myObject');
    const start = textarea?.selectionStart ?? buffer.length;
    const end = textarea?.selectionEnd ?? buffer.length;
    buffer = buffer.slice(0, start) + text + buffer.slice(end);
    showSnippets = false;
    scheduleCheck();
  }

  // The mirror only stays under the text if it scrolls with it.
  function syncScroll() {
    if (mirror && textarea) {
      mirror.scrollTop = textarea.scrollTop;
      mirror.scrollLeft = textarea.scrollLeft;
    }
    if (gutter && textarea) gutter.scrollTop = textarea.scrollTop;
  }

  function onKeydown(e) {
    if ((e.ctrlKey || e.metaKey) && e.key === 's') {
      e.preventDefault();
      save();
      return;
    }
    // A textarea would otherwise move focus out of the editor on Tab.
    if (e.key === 'Tab') {
      e.preventDefault();
      const start = textarea.selectionStart;
      const end = textarea.selectionEnd;
      buffer = buffer.slice(0, start) + '    ' + buffer.slice(end);
      tick().then(() => textarea.setSelectionRange(start + 4, start + 4));
      scheduleCheck();
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
      <div class="surface">
        <pre class="gutter" bind:this={gutter} aria-hidden="true">{#each Array(lineCount) as _unused, i}{i + 1}
{/each}</pre>
        <div class="code">
          <pre class="mirror" bind:this={mirror} aria-hidden="true">{@html highlighted}<br /></pre>
          <textarea
            bind:this={textarea}
            bind:value={buffer}
            on:input={scheduleCheck}
            on:scroll={syncScroll}
            on:keydown={onKeydown}
            spellcheck="false"
            autocomplete="off"
            autocapitalize="off"
            wrap="off"></textarea>
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
