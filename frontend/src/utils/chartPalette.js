/**
 * Categorical palette for monitoring charts.
 *
 * Both modes are selected (the dark column is the same eight hues re-stepped
 * for the dark surface, not an automatic flip) and were validated against this
 * app's real chart surfaces — light #f5f5f5, dark #2a2a2a — for the lightness
 * band, chroma floor, colour-vision-deficiency separation, the normal-vision
 * floor and contrast. Do not re-order or edit a hex without re-running the
 * validator: the slot ORDER is what keeps adjacent series distinguishable.
 *
 * Slots are assigned in fixed order and never cycled: a 9th series would reuse
 * a hue and break identity, so callers cap the plotted series (see MAX_SERIES)
 * and fold the rest into the table view.
 */
const LIGHT = ['#2a78d6', '#eb6834', '#1baf7a', '#eda100', '#e87ba4', '#008300', '#4a3aa7', '#e34948'];
const DARK  = ['#3987e5', '#d95926', '#199e70', '#c98500', '#d55181', '#008300', '#9085e9', '#e66767'];

/** Maximum number of series that can be drawn while keeping colours distinct. */
export const MAX_SERIES = LIGHT.length;

/** Status colours — reserved, never reused as a series colour. */
export const STATUS = {
  good: '#0ca30c',
  warning: '#fab219',
  serious: '#ec835a',
  critical: '#d03b3b',
};

/** True when the app is currently rendering its dark theme. */
export function isDarkTheme() {
  return document.documentElement.getAttribute('data-theme') !== 'light';
}

/** Colour for series `index` in the current theme. Never cycles. */
export function seriesColor(index, dark = isDarkTheme()) {
  const palette = dark ? DARK : LIGHT;
  return palette[Math.min(index, palette.length - 1)];
}

/** The whole ordered palette for the current theme. */
export function palette(dark = isDarkTheme()) {
  return dark ? [...DARK] : [...LIGHT];
}

/** Chart chrome (axes, grid, ink) pulled from the app's theme tokens. */
export function chartChrome() {
  const css = (name, fallback) =>
    getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback;
  return {
    ink: css('--text-color', '#e0e0e0'),
    muted: css('--text-muted', '#898781'),
    grid: css('--border-color', '#2c2c2a'),
    surface: css('--bg-light-color', '#2a2a2a'),
  };
}
