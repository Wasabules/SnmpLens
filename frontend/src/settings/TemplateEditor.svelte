<script>
  import { onMount, tick } from 'svelte';
  import { _ } from 'svelte-i18n';
  import Icon from '../Icon.svelte';
  import {
    NotifyTemplateVariables,
    NotifyPreviewTemplate,
    NotifyDefaultJsonPayload,
  } from '../../wailsjs/go/main/App';

  /**
   * The sink being edited, bound two-way.
   *
   * The whole sink rather than the template alone: the preview has to know the
   * kind and the payload mode to show what will actually be sent, and passing
   * those as separate props is how they came to disagree with each other.
   */
  export let sink;

  /** The body IS the request payload, so it has to be valid JSON. */
  $: jsonPayload = sink.kind === 'webhook' && sink.webhook?.payloadMode === 'template';

  // The vocabulary comes from Go rather than being mirrored here: a hand-copied
  // list is a list that drifts, and the first symptom would be a variable the
  // editor offers and the renderer does not know.
  let variables = [];
  let preview = null;
  let previewKind = 'threshold';
  let showVariables = false;
  let subjectEl;
  let bodyEl;
  let lastFocused = 'body';

  onMount(async () => {
    try {
      variables = (await NotifyTemplateVariables()) || [];
    } catch (e) {
      variables = [];
    }
    refreshPreview();
  });

  // A preview is not decoration. buildMessage silently substitutes the event
  // summary for an empty subject, so a template that renders to nothing looks
  // exactly like one that works — the only way to tell them apart is to look.
  let previewTimer;
  function refreshPreview() {
    clearTimeout(previewTimer);
    previewTimer = setTimeout(async () => {
      try {
        preview = await NotifyPreviewTemplate(sink, previewKind);
      } catch (e) {
        preview = null;
      }
    }, 200);
  }

  // `sink` is watched whole: the preview depends on the template, the kind, the
  // payload mode and the redaction flag, and naming them one by one is how one
  // of them gets forgotten.
  $: sink, previewKind, refreshPreview();

  // Whether the preview is showing a request body rather than a message. Read
  // from the answer, not guessed alongside it — a webhook posts an object in
  // both modes, so this is true wider than `jsonPayload` is.
  $: asPayload = !!preview?.json;

  async function insert(name) {
    const token = '{{' + name + '}}';
    const el = lastFocused === 'subject' ? subjectEl : bodyEl;
    const field = lastFocused === 'subject' ? 'subject' : 'body';
    const current = sink.template[field] || '';

    if (!el) {
      sink.template[field] = current + token;
      return;
    }
    const start = el.selectionStart ?? current.length;
    const end = el.selectionEnd ?? current.length;
    sink.template[field] = current.slice(0, start) + token + current.slice(end);

    await tick();
    el.focus();
    el.setSelectionRange(start + token.length, start + token.length);
  }

  // Starting from something that already works beats starting from a blank
  // field that will not save.
  async function useDefaultPayload() {
    try {
      sink.template.body = await NotifyDefaultJsonPayload();
    } catch (e) {
      /* leave the body alone */
    }
  }

  function errorsFor(field) {
    return (preview?.errors || []).filter((e) => e.field === field);
  }
</script>

<div class="tpl">
  <div class="tpl-head">
    <h5>{$_('notify.templateTitle')}</h5>
    {#if jsonPayload}
      <button class="btn btn-small" on:click={useDefaultPayload}>
        {$_('notify.useDefaultPayload')}
      </button>
    {/if}
    <button class="btn btn-small" on:click={() => (showVariables = !showVariables)}>
      <Icon name={showVariables ? 'chevron-up' : 'chevron-down'} size={13} />
      {$_('notify.insertVariable')}
    </button>
  </div>
  <p class="hint">{$_('notify.templateHint')}</p>

  <div class="tpl-main">
    <div class="tpl-fields">
  {#if showVariables}
    <div class="vars">
      {#each variables as v (v.name)}
        <button class="var" on:click={() => insert(v.name)} title={$_(v.description)}>
          <code>{'{{' + v.name + '}}'}</code>
          <span class="ex">{v.example}</span>
          {#if redact && v.redacted}
            <span class="masked">{$_('notify.tplMasked')}</span>
          {/if}
        </button>
      {/each}
      <p class="note">{$_('notify.tplParamsHint')}</p>
      {#if redact}
        <p class="note warn">
          <Icon name="triangle-alert" size={13} /> {$_('notify.tplRedactNote')}
        </p>
      {/if}
    </div>
  {/if}

  <label class="fld">
    <span>{$_('notify.templateSubject')}</span>
    <input type="text" bind:this={subjectEl} bind:value={sink.template.subject}
      on:focus={() => (lastFocused = 'subject')}
      placeholder={$_('notify.templateDefault')} />
    {#each errorsFor('subject') as err}
      <span class="err"><Icon name="circle-x" size={12} /> {err.message}</span>
    {/each}
  </label>

  <label class="fld">
    <span>{$_('notify.templateBody')}</span>
    <textarea rows="7" bind:this={bodyEl} bind:value={sink.template.body}
      on:focus={() => (lastFocused = 'body')}
      placeholder={$_('notify.templateDefault')}></textarea>
    {#each errorsFor('body') as err}
      <span class="err"><Icon name="circle-x" size={12} /> {err.message}</span>
    {/each}
  </label>

    </div>

  <div class="preview">
    <div class="preview-head">
      <span>{asPayload ? $_('notify.previewPayload') : $_('notify.templatePreview')}</span>
      {#if asPayload && preview}
        {#if preview.jsonValid}
          <span class="pv-ok">{$_('notify.jsonValid', { values: { bytes: preview.bytes } })}</span>
        {:else}
          <span class="pv-bad" title={preview.jsonError}>{$_('notify.jsonInvalid')}</span>
        {/if}
      {/if}
      <select bind:value={previewKind}>
        <option value="threshold">{$_('events.category.threshold')}</option>
        <option value="trap">{$_('events.category.trap')}</option>
        <option value="reachability">{$_('events.category.reachability')}</option>
      </select>
    </div>
    {#if preview}
      <div class="preview-body">
        {#if !asPayload}
          <div class="pv-subject">{preview.subject}</div>
        {/if}
        <pre class="pv-text" class:pv-json={asPayload}>{preview.body}</pre>
        {#if asPayload && !preview.jsonValid && preview.jsonError}
          <p class="pv-error">{preview.jsonError}</p>
        {/if}
      </div>
    {/if}
  </div>
  </div>

  {#if jsonPayload}
    <p class="note warn">
      <Icon name="triangle-alert" size={13} /> {$_('notify.tplJsonNote')}
    </p>
  {/if}
  <p class="note">{$_('notify.tplQueuedNote')}</p>
  <p class="note">{$_('notify.tplSharedNote')}</p>
</div>

<style>
  .tpl {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding-top: 8px;
    border-top: 1px solid var(--border-color);
  }

  /* One column by default. NotifySettings decides when this splits, so the
     breakpoint has a single home rather than one per component. */
  .tpl-main {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }

  .tpl-fields {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }

  .tpl-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
  }

  h5 {
    margin: 0;
    font-size: 0.85em;
    color: var(--text-color);
  }

  .hint,
  .note {
    margin: 0;
    font-size: 0.75em;
    color: var(--text-muted);
    display: flex;
    align-items: center;
    gap: 5px;
  }

  .note.warn {
    color: var(--warning-color);
  }

  .vars {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
    padding: 8px;
    background-color: var(--bg-lighter-color);
    border-radius: 4px;
  }

  .var {
    display: flex;
    align-items: baseline;
    gap: 5px;
    padding: 3px 7px;
    background: none;
    border: 1px solid var(--border-color);
    border-radius: 3px;
    cursor: pointer;
    font-size: 0.75em;
    color: var(--text-color);
  }

  .var:hover {
    border-color: var(--accent-color);
  }

  .var code {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  }

  .ex {
    color: var(--text-muted);
    max-width: 130px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .masked {
    color: var(--warning-color);
  }

  .fld {
    display: flex;
    flex-direction: column;
    gap: 3px;
    font-size: 0.8em;
    color: var(--text-muted);
  }

  .fld input,
  .fld textarea {
    width: 100%;
    padding: 6px 8px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 0.95em;
  }

  .fld textarea {
    resize: vertical;
    white-space: pre;
    overflow-x: auto;
  }

  .err {
    color: var(--error-color);
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .preview {
    border: 1px solid var(--border-color);
    border-radius: 4px;
    overflow: hidden;
  }

  .preview-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 5px 8px;
    background-color: var(--bg-lighter-color);
    font-size: 0.75em;
    color: var(--text-muted);
  }

  .preview-head select {
    background-color: var(--bg-color);
    border: 1px solid var(--border-color);
    border-radius: 3px;
    color: var(--text-color);
    font-size: 1em;
    padding: 2px 4px;
  }

  .preview-body {
    padding: 8px;
  }

  .pv-subject {
    font-weight: 600;
    font-size: 0.82em;
    color: var(--text-color);
    margin-bottom: 6px;
    word-break: break-word;
  }

  .pv-ok {
    color: var(--success-color);
  }

  .pv-bad {
    color: var(--error-color);
  }

  .pv-error {
    margin: 4px 0 0;
    font-size: 0.72em;
    color: var(--error-color);
    word-break: break-word;
  }

  .pv-json {
    color: var(--text-color);
  }

  .pv-text {
    margin: 0;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 0.75em;
    color: var(--text-muted);
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 190px;
    overflow-y: auto;
    flex: 1 1 auto;
    min-height: 0;
  }
</style>
