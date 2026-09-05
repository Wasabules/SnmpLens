/**
 * The clips: the handful of things a still cannot say.
 *
 * This list is deliberately short. A moving picture of something static is
 * WORSE than a still — it asks for attention and returns nothing — so a scene
 * earns a clip only when the information is in the change itself: a flat walk
 * becoming a table, addresses being replaced in place, a file opening and the
 * analysis arriving underneath it. Everything else stays a screenshot.
 *
 * Each clip names a scene from `scenes.js` and re-times it. The scene's own
 * `act` steps are what get played; only the pace differs, because a screenshot
 * wants to reach the end state as fast as it can and a clip wants to be
 * followed by a person reading it for the first time.
 *
 * Fields, all optional except `name` and `scene`:
 *   settle   ms to wait after the scene arms, before the camera rolls
 *   lead     ms of the opening state before the first step
 *   pace     ms between steps — the scene's own default is 450, far too fast
 *   tail     ms of the end state, so it can be read before the loop restarts
 *   timeout  ms to wait for the steps to finish
 */

/** Clips worth recording in both themes, so each matches the site around it. */
const PAIRED = [
  {
    base: 'walk-to-table',
    scene: 'operations',
    // WALK → Execute → Table. The point is the third step: 155 varbinds that
    // arrived as a flat list are pivoted by column and split by INDEX, and no
    // still can show that those are the same 155 varbinds.
    pace: 1400,
    tail: 2600,
    describe: 'A walk arrives flat, then pivots into a table split by INDEX.',
  },
  {
    base: 'anonymous-mode',
    scene: 'anonymous-mode',
    // The walk, then Ctrl+Shift+A. Every address on screen is replaced in
    // place, consistently — which as two side-by-side stills is a puzzle, and
    // as one cut is obvious.
    pace: 1500,
    tail: 3000,
    describe: 'Every address on screen is replaced by a stable alias, in place.',
  },
  {
    base: 'mib-editor',
    scene: 'mib-editor',
    // Opening a MIB: the file is read, highlighted, and the semantic pass comes
    // back and puts a squiggle under the line. The delay before the diagnostic
    // appears is the thing being shown, and a still has already lost it.
    pace: 1200,
    tail: 3200,
    describe: 'A MIB opens, highlights, and the analysis arrives underneath it.',
  },
];

export const CLIPS = PAIRED.flatMap((clip) =>
  ['dark', 'light'].map((theme) => ({
    name: `${clip.base}-${theme}`,
    scene: `${clip.scene}-${theme}`,
    settle: clip.settle,
    lead: clip.lead,
    pace: clip.pace,
    tail: clip.tail,
    timeout: clip.timeout,
    describe: clip.describe,
  })),
);
