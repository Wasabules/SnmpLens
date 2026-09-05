/**
 * The browser demo's opening state.
 *
 * The same build as the screenshots — the real Svelte application with the Wails
 * bridge replaced by fixtures — but a person is going to use this one, so three
 * things differ from the screenshot director.
 *
 * NO localStorage.clear(). The demo is served from the same ORIGIN as the
 * project site, so they share one storage area: clearing it would throw away the
 * visitor's theme choice for the whole site every time they opened the demo.
 * Keys are seeded only when absent, which also means anything they change here
 * survives until they clear it themselves.
 *
 * NO forced locale. The screenshots pin English so the images are legible to
 * everyone; a demo should come up in the reader's own language, since being
 * translated into five is one of the things worth showing.
 *
 * __SNMPLENS_DEMO__ is set BEFORE the bundle evaluates. It is what makes the
 * bridge refuse the calls that would ask the operating system for something a
 * web page cannot have, instead of answering "ok" to a request to install a
 * service.
 */

(function () {
  var SEEDS = __SEEDS__;

  window.__SNMPLENS_DEMO__ = true;

  // A scene, for the stubbed bridge to consult. No overrides and no scripted
  // events — everything comes from the fixtures — but a FEED, so the monitoring
  // charts are alive rather than a picture of a chart.
  window.__SNMPLENS_SCENE__ = {
    name: 'demo',
    bindings: {},
    events: [],
    feed: { from: 'monitorSamples', everyMs: 1500 },
    latency: {
      // Not decoration: an answer that arrives in the same frame as the click
      // reads as a canned response, which is exactly the impression a demo
      // built on fixtures has to work against. These are roughly what the
      // fixtures themselves claim in their own responseTimeMs.
      SnmpGet: 260,
      SnmpWalk: 900,
      SnmpGetNext: 220,
      SnmpGetBulk: 480,
      SnmpSet: 340,
      SnmpDiscover: 3600,
      TestConnection: 700,
      NetworkPing: 1200,
      NetworkTraceroute: 2600,
    },
  };

  try {
    for (var k in SEEDS) {
      if (localStorage.getItem(k) === null) localStorage.setItem(k, SEEDS[k]);
    }

    // Arrive in the theme the visitor chose on the site.
    //
    // The demo is served from the same origin, so the site's own choice is
    // right there in storage — and coming up dark immediately after someone has
    // set the site to light reads as a different product rather than the same
    // one. Only on first run: after that the application's own theme setting
    // owns it, which is the setting a person would go looking for.
    if (localStorage.getItem('snmplens-demo-themed') === null) {
      var site = localStorage.getItem('snmplens-theme');
      var wanted = site === 'light' || site === 'dark'
        ? site
        : (window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches
          ? 'light' : 'dark');
      var settings = JSON.parse(localStorage.getItem('settings') || '{}');
      settings.theme = wanted;
      localStorage.setItem('settings', JSON.stringify(settings));
      localStorage.setItem('snmplens-demo-themed', '1');
    }
  } catch (e) {
    // Private browsing. The application reads its defaults and the demo still
    // works, with less in it.
  }
})();
