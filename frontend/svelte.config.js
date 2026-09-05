/**
 * Deliberately the default configuration, written down.
 *
 * All three Vite configs — the app, the screenshot harness and the browser demo
 * — call `svelte()` with no options, so the plugin was announcing "no Svelte
 * config found, using default configuration" on every build. That is a notice
 * rather than a warning, but it is the only line left in an otherwise silent
 * build, and a build with one permanent line in it is a build nobody reads.
 *
 * No `vitePreprocess()`: the components are plain JavaScript and plain CSS, and
 * adding a preprocessor to a project that does not need one is how a build
 * quietly acquires a step it cannot explain. This is where Svelte options go if
 * that ever changes.
 */
export default {};
