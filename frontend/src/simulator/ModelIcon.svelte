<script>
  import Icon from '../Icon.svelte';
  import { categoryIcon } from '../utils/simulator.js';

  /**
   * A model as a picture: the icon a custom model came with, or else its
   * category's, on the category's tint.
   */
  export let category = 'other';
  /** A data URI Go served (ListSimulatorModelIcons): a PNG, JPEG or GIF it decoded itself. */
  export let icon = '';
  export let size = 32;
</script>

<span class="model-icon" class:picture={!!icon} data-category={category} style="--size: {size}px">
  {#if icon}
    <img src={icon} alt="" width={size} height={size} />
  {:else}
    <Icon name={categoryIcon(category)} size={Math.round(size * 0.52)} />
  {/if}
</span>

<style>
  .model-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: var(--size);
    height: var(--size);
    border-radius: 8px;
    background-color: var(--accent-subtle);
    color: var(--accent-color);
  }

  .model-icon[data-category='network'],
  .model-icon[data-category='wireless'] {
    background-color: var(--oid-subtle);
    color: var(--oid-color);
  }

  .model-icon[data-category='security'] {
    background-color: var(--favorites-subtle);
    color: var(--favorites-color);
  }

  .model-icon[data-category='power'] {
    background-color: var(--warning-subtle);
    color: var(--warning-color);
  }

  .model-icon[data-category='environment'] {
    background-color: var(--success-subtle);
    color: var(--success-color);
  }

  .model-icon[data-category='printing'],
  .model-icon[data-category='other'] {
    background-color: var(--hover-overlay-medium);
    color: var(--text-muted);
  }

  /* Last, so that a picture is never drawn on a category's tint. */
  .model-icon.picture {
    overflow: hidden;
    background-color: var(--bg-color);
    border: 1px solid var(--border-color);
  }

  .model-icon img {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
</style>
