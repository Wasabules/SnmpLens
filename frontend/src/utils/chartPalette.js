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

/** The key a series is identified by, everywhere. */
export function seriesKey(target, oid) {
  return target + '|' + oid;
}

/**
 * The ordered series of a session, with the colour each one gets.
 *
 * ONE function because there were four, and they disagreed.
 *
 * `MonitorChart.buildStacked` ran a counter over the (OID, target) pairs it
 * actually plotted; `MonitorChart.buildDatasets` indexed targets within one
 * OID; `MetricTiles` computed `oidIdx * targets.length + tIdx`, which equals
 * the chart's counter only when every OID has the SAME number of targets; and
 * `ChannelsModal` ran a fourth counter over a target list built by a different
 * rule — it falls back to the session's configured targets for an OID with no
 * data yet, where the chart skipped that OID entirely. So a swatch in the
 * channel picker, a swatch on a tile and the line on the chart could each be a
 * different colour for the same series, and the disagreement appeared only with
 * an uneven number of targets per OID or before the first sample of one OID
 * arrived — which is to say, in normal use, intermittently.
 *
 * Colour is POSITIONAL and never depends on what is drawn: hiding a series
 * cannot recolour the others, and a series keeps its colour when data for
 * another OID starts arriving. The palette has eight slots and never cycles, so
 * position past the eighth is `capped` — reported rather than silently dropped.
 *
 * @param {object} session   a pollingStore session
 * @param {object} opts      {layout: 'separate'|'stacked', oids: string[]|null}
 * @returns {{series: Array, dropped: number}}
 */
export function seriesPlan(session, { layout = 'separate', oids = null } = {}) {
  const results = session?.results || [];
  const list = (oids && oids.length ? oids : (session?.oids?.length ? session.oids : [session?.oid]))
    .filter(Boolean);
  const wanted = [...new Set(list)];

  // ONE rule for a target list, so every consumer sees the same one: the order
  // the targets first appear in the data, falling back to the session's
  // configured targets while an OID has none yet.
  const configured = [...new Set(session?.targets || [])];
  const targetsFor = (oid) => {
    const seen = [...new Set(results.filter((r) => (r.oid || session?.oid) === oid).map((r) => r.target))];
    return seen.length ? seen : configured;
  };

  const series = [];
  let running = 0;
  for (const oid of wanted) {
    const targets = targetsFor(oid);
    targets.forEach((target, idx) => {
      const colorIndex = layout === 'stacked' ? running : idx;
      series.push({
        oid,
        target,
        key: seriesKey(target, oid),
        colorIndex,
        capped: colorIndex >= MAX_SERIES,
      });
      running++;
    });
  }

  return { series, dropped: series.filter((s) => s.capped).length };
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
