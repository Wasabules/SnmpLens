<script>
  import { _ } from 'svelte-i18n';

  /**
   * One file's load diagnosis, from MibDiagnose or a load result.
   *
   * A component rather than markup in two places: the import dialog and the
   * MIB settings both need it, and the second copy is the one that stops
   * getting the new field.
   */
  export let diagnosis;
  /** The file name, when the diagnosis is not the whole story on screen. */
  export let fileName = '';
  /** The summary to show; defaults to the diagnosis's own. */
  export let summary = '';

  $: shownName = fileName || diagnosis?.fileName || '';
  $: shownSummary = summary || diagnosis?.summary || '';
</script>

{#if diagnosis}
  <div class="diag">
    <div class="diag-head">
      <span class="mono diag-file">{shownName}</span>
      {#if diagnosis.stage}
        <span class="diag-stage" title={$_('app.mibDrop.stageHint')}>
          {$_('app.mibDrop.stage.' + diagnosis.stage)}
        </span>
      {/if}
      {#if diagnosis.moduleName}
        <span class="diag-module">{diagnosis.moduleName}</span>
      {/if}
    </div>

    <p class="diag-summary" class:ok={diagnosis.loaded && diagnosis.stage === 'loaded'}>
      {shownSummary}
    </p>

    {#if diagnosis.diagnostics?.length}
      <ul class="diag-list">
        {#each diagnosis.diagnostics.slice(0, 12) as d}
          <li>
            {#if d.line > 0}
              <span class="diag-pos">{$_('app.mibDrop.at', { values: { line: d.line, column: d.column } })}</span>
            {/if}
            {d.message}
          </li>
        {/each}
      </ul>
    {/if}

    {#if diagnosis.missing?.length}
      <p class="diag-label">{$_('app.mibDrop.missingImports')}</p>
      <ul class="diag-list">
        {#each diagnosis.missing as m}
          <li>
            <span class="mono">{m.module}</span>
            <span class="diag-reason">{$_('app.mibDrop.reason.' + m.reason)}</span>
            {#if m.symbols?.length}
              <span class="diag-symbols">{$_('app.mibDrop.usedFor', { values: { symbols: m.symbols.join(', ') } })}</span>
            {/if}
            {#if m.cause}<div class="diag-cause">{m.cause}</div>{/if}
          </li>
        {/each}
      </ul>
    {/if}

    {#each diagnosis.hints || [] as hint}
      <pre class="diag-hint">{hint}</pre>
    {/each}

    {#if diagnosis.detail}
      <details class="diag-detail">
        <summary>{$_('app.mibDrop.rawDetail')}</summary>
        <pre>{diagnosis.detail}</pre>
      </details>
    {/if}
  </div>
{/if}

<style>
  .diag {
    border: 1px solid var(--border-color, #ddd);
    border-radius: 6px;
    padding: 10px 12px;
    margin-bottom: 10px;
  }

  .mono {
    font-family: monospace;
  }

  .diag-head {
    display: flex;
    align-items: baseline;
    gap: 8px;
    flex-wrap: wrap;
    margin-bottom: 4px;
  }

  .diag-file {
    font-weight: 600;
  }

  .diag-stage {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    padding: 1px 6px;
    border-radius: 10px;
    background: var(--bg-hover, #eee);
    color: var(--text-secondary, #555);
  }

  .diag-module {
    font-size: 11px;
    color: var(--text-muted, #888);
  }

  .diag-summary {
    margin: 0 0 6px;
    color: var(--danger, #c0392b);
  }

  .diag-summary.ok {
    color: var(--success, #27ae60);
  }

  .diag-label {
    margin: 8px 0 2px;
    font-size: 11px;
    font-weight: 600;
    color: var(--text-secondary, #555);
  }

  .diag-list {
    margin: 0;
    padding-left: 18px;
    font-size: 12px;
  }

  .diag-list li {
    margin-bottom: 3px;
  }

  .diag-pos {
    font-family: monospace;
    color: var(--text-muted, #888);
    margin-right: 6px;
  }

  .diag-reason {
    font-size: 11px;
    color: var(--text-muted, #888);
  }

  .diag-symbols {
    display: block;
    font-size: 11px;
    color: var(--text-muted, #888);
  }

  .diag-cause {
    font-size: 11px;
    color: var(--text-muted, #888);
    margin-top: 2px;
    padding-left: 8px;
    border-left: 2px solid var(--border-color, #ddd);
  }

  /* Monospace on purpose: the caret has to land under the character it names,
     which proportional text makes impossible. */
  .diag-hint {
    font-family: monospace;
    font-size: 11px;
    white-space: pre;
    overflow-x: auto;
    background: var(--bg-hover, #f5f5f5);
    border-radius: 4px;
    padding: 6px 8px;
    margin: 6px 0 0;
  }

  .diag-detail {
    margin-top: 6px;
    font-size: 11px;
  }

  .diag-detail pre {
    white-space: pre-wrap;
    word-break: break-all;
    color: var(--text-muted, #888);
  }
</style>
