/**
 * The scene director: the classic script injected into the head of the
 * screenshot bundle.
 *
 * It lives in its own file rather than in a template literal inside
 * `vite.screenshots.config.js`, and that is not tidiness. It was a template
 * literal, and a backtick written in one of its own comments ended the string —
 * twice, each time as a build error pointing at a line eighty lines away from
 * the one that caused it. A file cannot do that, and it gets syntax highlighting
 * and linting as a bonus.
 *
 * The two placeholders below the comment are substituted by the config at build
 * time. They are placeholders rather than interpolations for the same reason —
 * and they are deliberately not NAMED here, because the substitution replaces
 * the first occurrence of each and a mention in this comment would consume it,
 * leaving the code reading an identifier that does not exist.
 */

(function () {
  var SCENES = __SCENES__;
  var TAB_KEY = __TAB_KEY__;
  var want = new URLSearchParams(location.search).get('scene');
  var s = SCENES.filter(function (x) { return x.name === want; })[0] || SCENES[0];

  window.__SNMPLENS_SCENE__ = s;

  try {
    localStorage.clear();
    for (var k in s.seeds) localStorage.setItem(k, s.seeds[k]);
  } catch (e) {}

  // Tell the capture when the page is worth photographing. Chrome cannot ask,
  // so the page says so by setting a flag the harness polls.
  window.__SNMPLENS_READY__ = false;

  function pickTab() {
    var key = TAB_KEY[s.seeds.snmplens_active_tab] || '1';
    window.dispatchEvent(new KeyboardEvent('keydown', {
      key: key, ctrlKey: true, bubbles: true,
    }));
  }

  // Some things cannot be seeded because they are not state anyone stores: a
  // walk's results live in the component that ran it. Those scenes name the
  // buttons to press, BY THEIR TEXT — a label is what a person would look for,
  // and unlike a class name it does not change when the styling does.
  //
  // A step that finds nothing is skipped rather than failing: the scene then
  // captures whatever it would have captured anyway, which is a duller picture
  // and not a broken one.
  function clickText(text) {
    var nodes = document.querySelectorAll('button, .tab-btn, [role="button"]');
    var loose = null;

    // TWO passes. An exact match anywhere beats a prefix match earlier in the
    // document: searching for "SNMP" with prefix matching alone finds the
    // "SNMP Operations" workspace tab long before the "SNMP" settings tab, and
    // clicks the wrong thing while reporting success.
    for (var i = 0; i < nodes.length; i++) {
      // \s, not \\s. This was written inside a template literal, where the
      // double backslash was needed to emit one; in a file it means a literal
      // backslash followed by an "s", which quietly turned "Notifications" into
      // "Notification" and made that step match nothing.
      var label = (nodes[i].textContent || '').replace(/\s+/g, ' ').trim();
      if (label === text) { nodes[i].click(); return true; }
      // The prefix case exists for labels with an adornment after them: the MIB
      // list appends a bundled marker, so "IF-MIB" arrives as "IF-MIB ◆".
      if (loose === null && label.indexOf(text + ' ') === 0) loose = nodes[i];
    }
    if (loose) { loose.click(); return true; }
    return false;
  }

  // `pace` is either one number for every gap, or an array of them — because a
  // clip is paced by what the viewer has to READ, and that is not constant. In
  // the Anonymous Mode clip the gap before the mask is the whole point, since it
  // is the only time the real addresses are on screen, while the gap that merely
  // switches a tab is dead air.
  function runSteps(steps, done, pace) {
    var i = 0;
    var gapAt = function (n) {
      if (Array.isArray(pace)) return pace[n] != null ? pace[n] : pace[pace.length - 1];
      return pace || 450;
    };
    (function next() {
      if (i >= steps.length) { setTimeout(done, 700); return; }
      var step = steps[i++];
      // "key:," presses Ctrl+comma; "key:shift+A" adds Shift. Some things have
      // no button at all — the settings dialog is opened by a shortcut and
      // nothing else, and Anonymous Mode CANNOT be seeded: settingsStore.js
      // forces it off on load, deliberately, so the only way in is its own
      // Ctrl+Shift+A.
      if (step.indexOf('key:') === 0) {
        var combo = step.slice(4);
        var shift = combo.indexOf('shift+') === 0;
        window.dispatchEvent(new KeyboardEvent('keydown', {
          key: shift ? combo.slice(6) : combo,
          ctrlKey: true, shiftKey: shift, bubbles: true,
        }));

      // "sel:.entry-header|1" clicks the second element matching a selector.
      // Rows do not have labels worth searching for — a history entry's text is
      // its OID and its duration, and a trap's is a timestamp that moves every
      // run — so repeated things are addressed by position instead.
      } else if (step.indexOf('sel:') === 0) {
        var bar = step.lastIndexOf('|');
        var css = bar > 3 ? step.slice(4, bar) : step.slice(4);
        var at = bar > 3 ? parseInt(step.slice(bar + 1), 10) : 0;
        var els = document.querySelectorAll(css);
        if (els[at]) els[at].click();

      // "type:.search-bar input|ifOperStatus" fills a field.
      //
      // Assigning .value alone would show the text and change nothing: Svelte's
      // bind:value is an "input" listener, so without the event the component's
      // own variable never updates and the tree never filters.
      } else if (step.indexOf('type:') === 0) {
        var cut = step.indexOf('|');
        var field = document.querySelector(step.slice(5, cut));
        if (field) {
          field.focus();
          field.value = step.slice(cut + 1);
          field.dispatchEvent(new Event('input', { bubbles: true }));
        }

      } else {
        clickText(step);
      }
      setTimeout(next, gapAt(i - 1));
    })();
  }

  // "?record=1" holds the scene at its opening frame instead of playing it.
  //
  // A still wants the END state and gets there as fast as it can; a clip wants
  // the TRANSITION, and cannot start until the camera is rolling. So in record
  // mode the steps are parked behind __SNMPLENS_PLAY__, which tools/record.mjs
  // calls once the screencast is live, at a pace a person can follow rather
  // than the pace a screenshot wants.
  var recording = new URLSearchParams(location.search).get('record') === '1';

  window.addEventListener('load', function () {
    // Let the application mount and its first bindings resolve, then choose the
    // workspace, then let that workspace's own loads settle.
    setTimeout(function () {
      pickTab();
      setTimeout(function () {
        if (recording) {
          // Parked, and SAYING so: the recorder waits on this flag rather than
          // on a fixed delay, so a slow machine records the same clip as a fast
          // one instead of one that starts halfway through.
          window.__SNMPLENS_ARMED__ = true;
          window.__SNMPLENS_PLAY__ = function (pace) {
            runSteps(s.act || [], function () { window.__SNMPLENS_READY__ = true; }, pace);
          };
          return;
        }
        runSteps(s.act || [], function () { window.__SNMPLENS_READY__ = true; });
      }, 800);
    }, 700);
  });
})();
