<script>
  import { icons } from './icons.js';

  export let name;
  export let size = 16;
  export let strokeWidth = 1.75;
  export let filled = false; // e.g. active favorite star
  let cls = '';
  export { cls as class };
</script>

<svg
  class="icon {cls}"
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill={filled ? 'currentColor' : 'none'}
  stroke="currentColor"
  stroke-width={strokeWidth}
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
  focusable="false"
>
  {@html icons[name] || ''}
</svg>
