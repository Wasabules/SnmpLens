/**
 * The scene the page is rendering, for the stubbed bridge to consult.
 *
 * The catalogue itself is serialised into the document by the Vite plugin in
 * vite.screenshots.config.js, as a CLASSIC script in the head. That placement is
 * not incidental: `stores/settingsStore.js` reads localStorage at
 * module-evaluation time, before anything can be awaited, and module scripts are
 * deferred. Seeding from a module would therefore sometimes run after the store
 * had already read an empty localStorage — sometimes, depending on the shape of
 * the import graph, which is the worst kind of bug to have in a tool whose whole
 * job is to be reproducible.
 */

const EMPTY = { name: 'default', bindings: {}, events: [] };

export function scene() {
  return (typeof window !== 'undefined' && window.__SNMPLENS_SCENE__) || EMPTY;
}
