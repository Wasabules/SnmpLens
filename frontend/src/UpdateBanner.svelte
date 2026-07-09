<script>
  import { _ } from 'svelte-i18n';
  import Icon from './Icon.svelte';
  import { updateStore } from './stores/updateStore';

  let showNotes = false;
</script>

{#if $updateStore.available}
  <div class="update-banner" role="status">
    <div class="update-row">
      <Icon name="arrow-up-circle" size={22} class="icon-accent" />
      <div class="update-text">
        <div class="update-title">
          {$_('update.available', { values: { version: $updateStore.latestVersion } })}
        </div>
        <div class="update-sub">
          <span>{$_('update.currentVersion', { values: { version: $updateStore.currentVersion } })}</span>
          {#if $updateStore.releaseNotes}
            <button type="button" class="notes-toggle" on:click={() => (showNotes = !showNotes)}>
              {$_('update.releaseNotes')}
              <Icon name={showNotes ? 'chevron-down' : 'chevron-right'} size={12} />
            </button>
          {/if}
        </div>
      </div>

      <div class="update-actions">
        {#if $updateStore.downloading}
          <span class="progress-label">{$_('update.downloading')} {$updateStore.progress}%</span>
        {:else}
          <button class="btn btn-primary" on:click={() => updateStore.apply()}>
            <Icon name="download" size={15} />
            {$updateStore.canSelfApply ? $_('update.install') : $_('update.download')}
          </button>
          <button class="btn tertiary" on:click={() => updateStore.dismiss()}>
            {$_('update.later')}
          </button>
        {/if}
      </div>
    </div>

    {#if $updateStore.downloading}
      <div class="progress-track">
        <div class="progress-fill" style="width: {$updateStore.progress}%"></div>
      </div>
    {/if}

    {#if showNotes && $updateStore.releaseNotes}
      <pre class="release-notes">{$updateStore.releaseNotes}</pre>
    {/if}

    {#if $updateStore.error}
      <div class="update-error">
        <Icon name="triangle-alert" class="icon-warning" size={14} />
        <span>{$updateStore.error}</span>
      </div>
    {/if}
  </div>
{/if}

<style>
  .update-banner {
    background-color: var(--accent-subtle);
    border-bottom: 1px solid var(--accent-border);
    padding: 10px 16px;
    font-size: 0.9em;
  }

  .update-row {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .update-text {
    flex: 1;
    min-width: 0;
  }

  .update-title {
    font-weight: 600;
    color: var(--text-color);
  }

  .update-sub {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--text-muted);
    font-size: 0.9em;
    margin-top: 1px;
  }

  .notes-toggle {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
    color: var(--accent-color);
    font: inherit;
  }

  .notes-toggle:hover {
    text-decoration: underline;
  }

  .update-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }

  .btn-primary {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }

  .progress-label {
    color: var(--accent-color);
    font-variant-numeric: tabular-nums;
  }

  .progress-track {
    height: 4px;
    background-color: var(--bg-lighter-color);
    border-radius: 2px;
    overflow: hidden;
    margin-top: 8px;
  }

  .progress-fill {
    height: 100%;
    background-color: var(--accent-color);
    transition: width 0.15s ease;
  }

  .release-notes {
    margin: 10px 0 2px;
    padding: 10px 12px;
    max-height: 220px;
    overflow-y: auto;
    background-color: var(--bg-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    font-family: inherit;
    font-size: 0.9em;
    white-space: pre-wrap;
    word-break: break-word;
    color: var(--text-light);
  }

  .update-error {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 8px;
    color: var(--warning-color);
    font-size: 0.9em;
  }
</style>
