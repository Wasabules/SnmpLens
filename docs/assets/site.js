/* ==========================================================================
   SnmpLens — project site behaviour
   --------------------------------------------------------------------------
   Two things only: the theme toggle, and a copy button on the code blocks.
   Everything else on this site is HTML and CSS, and should stay that way.

   Deferred, so it never blocks the first paint. The one piece that CANNOT wait
   is choosing the theme — a deferred script would paint the wrong one first and
   then correct it — so that lives inline in each page's <head>, above the
   stylesheet link. This file only takes over once there is a reader to serve.
   ========================================================================== */

(function () {
  'use strict';

  /* --- theme ------------------------------------------------------------ */

  var STORE = 'snmplens-theme';
  var MODES = ['auto', 'light', 'dark'];

  var ICON = {
    // A monitor: "whatever this machine says".
    auto: '<path d="M1.75 2h12.5A1.75 1.75 0 0 1 16 3.75v7.5A1.75 1.75 0 0 1 14.25 13H9.5v1h2.25a.75.75 0 0 1 0 1.5h-6a.75.75 0 0 1 0-1.5H8v-1H3.25A1.75 1.75 0 0 1 1.5 11.25v-7.5A1.75 1.75 0 0 1 3.25 2Zm0 1.5a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h11a.25.25 0 0 0 .25-.25v-7.5a.25.25 0 0 0-.25-.25Z"/>',
    light: '<path d="M8 11a3 3 0 1 1 0-6 3 3 0 0 1 0 6Zm0-1.5a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3ZM8 0a.75.75 0 0 1 .75.75v1a.75.75 0 0 1-1.5 0v-1A.75.75 0 0 1 8 0Zm0 13a.75.75 0 0 1 .75.75v1a.75.75 0 0 1-1.5 0v-1A.75.75 0 0 1 8 13ZM0 8a.75.75 0 0 1 .75-.75h1a.75.75 0 0 1 0 1.5h-1A.75.75 0 0 1 0 8Zm13 0a.75.75 0 0 1 .75-.75h1a.75.75 0 0 1 0 1.5h-1A.75.75 0 0 1 13 8ZM2.34 2.34a.75.75 0 0 1 1.06 0l.7.7a.75.75 0 0 1-1.06 1.07l-.7-.71a.75.75 0 0 1 0-1.06Zm9.56 9.56a.75.75 0 0 1 1.06 0l.7.7a.75.75 0 1 1-1.06 1.06l-.7-.7a.75.75 0 0 1 0-1.06Zm1.76-9.56a.75.75 0 0 1 0 1.06l-.7.71a.75.75 0 1 1-1.06-1.07l.7-.7a.75.75 0 0 1 1.06 0ZM4.1 11.9a.75.75 0 0 1 0 1.06l-.7.7a.75.75 0 0 1-1.06-1.06l.7-.7a.75.75 0 0 1 1.06 0Z"/>',
    dark: '<path d="M9.598 1.591a.749.749 0 0 1 .785-.175 7.001 7.001 0 1 1-8.967 8.967.75.75 0 0 1 .961-.96 5.5 5.5 0 0 0 7.046-7.046.75.75 0 0 1 .175-.786Zm1.616 1.945a7 7 0 0 1-7.678 7.678 5.499 5.499 0 1 0 7.678-7.678Z"/>',
  };

  var LABEL = { auto: 'Follow system theme', light: 'Light theme', dark: 'Dark theme' };

  function stored() {
    try {
      var v = localStorage.getItem(STORE);
      return MODES.indexOf(v) >= 0 ? v : 'auto';
    } catch (e) {
      // A private window can throw on read. Following the system is the right
      // answer there anyway.
      return 'auto';
    }
  }

  function apply(mode) {
    if (mode === 'auto') document.documentElement.removeAttribute('data-theme');
    else document.documentElement.setAttribute('data-theme', mode);
  }

  function buildToggle() {
    var host = document.querySelector('[data-theme-toggle]');
    if (!host) return;

    var current = stored();
    host.className = 'theme-toggle';
    host.setAttribute('role', 'group');
    host.setAttribute('aria-label', 'Theme');
    host.hidden = false;
    host.innerHTML = '';

    var buttons = MODES.map(function (mode) {
      var b = document.createElement('button');
      b.type = 'button';
      b.title = LABEL[mode];
      b.setAttribute('aria-label', LABEL[mode]);
      b.setAttribute('aria-pressed', String(mode === current));
      b.innerHTML = '<svg viewBox="0 0 16 16" aria-hidden="true">' + ICON[mode] + '</svg>';
      b.addEventListener('click', function () {
        current = mode;
        apply(mode);
        try {
          if (mode === 'auto') localStorage.removeItem(STORE);
          else localStorage.setItem(STORE, mode);
        } catch (e) { /* the choice still holds for this page */ }
        buttons.forEach(function (other, i) {
          other.setAttribute('aria-pressed', String(MODES[i] === mode));
        });
      });
      host.appendChild(b);
      return b;
    });
  }

  /* --- copy buttons ----------------------------------------------------- */

  // Every <pre> on this site is a command someone is meant to run, and the
  // longest of them is a build invocation nobody should be retyping.
  function addCopyButtons() {
    if (!navigator.clipboard) return;

    document.querySelectorAll('pre').forEach(function (pre) {
      if (pre.closest('.no-copy')) return;

      var wrap = document.createElement('div');
      wrap.className = 'copy-wrap';
      pre.parentNode.insertBefore(wrap, pre);
      wrap.appendChild(pre);

      var btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'copy-btn';
      btn.textContent = 'Copy';
      btn.setAttribute('aria-label', 'Copy this to the clipboard');

      btn.addEventListener('click', function () {
        navigator.clipboard.writeText(pre.innerText.replace(/\n+$/, '')).then(function () {
          btn.textContent = 'Copied';
          btn.classList.add('is-done');
          setTimeout(function () {
            btn.textContent = 'Copy';
            btn.classList.remove('is-done');
          }, 1600);
        }, function () {
          btn.textContent = 'Press ⌘/Ctrl+C';
        });
      });

      wrap.appendChild(btn);
    });
  }

  /* --- zoom ------------------------------------------------------------- */

  /**
   * Click a screenshot or a clip to see it at full size.
   *
   * These are pictures of an interface: the detail IS the content, and at the
   * width of a two-column grid a reader can see that there is a table but not
   * what is in it. The enlarged copy is the SAME element cloned, so it keeps its
   * srcset — the browser then picks the 2000 px rung it declined at card size —
   * and a clip keeps playing rather than restarting from its poster.
   *
   * A native <dialog>, so Escape and the focus trap are the browser's job rather
   * than ours.
   */
  function addZoom() {
    var figures = document.querySelectorAll('figure.shot, figure.clip');
    if (!figures.length || !window.HTMLDialogElement) return;

    var dialog = document.createElement('dialog');
    dialog.className = 'zoom';
    dialog.innerHTML = '<button class="zoom-close" type="button" aria-label="Close">&times;</button>'
      + '<div class="zoom-body"></div>';
    document.body.appendChild(dialog);
    var body = dialog.querySelector('.zoom-body');

    function close() {
      dialog.close();
      body.innerHTML = '';
    }

    dialog.querySelector('.zoom-close').addEventListener('click', close);

    // Emptied on `close` so a paused video stops downloading — but only if the
    // dialog is still shut. The close EVENT is queued as a task rather than
    // fired synchronously, so closing one figure and opening the next in the
    // same gesture ran this handler AFTER the new content was in place, and
    // showed an empty box.
    dialog.addEventListener('close', function () {
      if (!dialog.open) body.innerHTML = '';
    });
    // A click on the backdrop lands on the dialog itself, never on its content.
    dialog.addEventListener('click', function (e) { if (e.target === dialog) close(); });

    figures.forEach(function (fig) {
      // The whole figure is the target, but only the media is cloned: a caption
      // repeated under a full-bleed image is the one part that does not need to
      // be bigger.
      var media = fig.querySelector('img, video');
      if (!media) return;

      fig.tabIndex = 0;
      fig.setAttribute('role', 'button');
      fig.setAttribute('aria-label', 'Enlarge: ' + (media.alt || media.getAttribute('aria-label') || 'screenshot'));

      function open() {
        var copy = media.cloneNode(true);
        copy.removeAttribute('class');
        copy.removeAttribute('loading');
        if (copy.tagName === 'VIDEO') {
          copy.autoplay = true;
          copy.loop = true;
          copy.muted = true;
          copy.controls = true;
        }
        body.innerHTML = '';
        body.appendChild(copy);

        var cap = fig.querySelector('figcaption');
        if (cap) {
          var p = document.createElement('p');
          p.className = 'zoom-caption';
          p.innerHTML = cap.innerHTML;
          body.appendChild(p);
        }
        dialog.showModal();
      }

      fig.addEventListener('click', open);
      fig.addEventListener('keydown', function (e) {
        if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); open(); }
      });
    });
  }

  /* --- clips ------------------------------------------------------------ */

  /**
   * Play a clip while it is on screen, and only then.
   *
   * The markup used to carry `autoplay`, and measuring what a browser actually
   * fetched showed why that was wrong: all EIGHT clips were downloaded on load —
   * including the four the theme hides, since `display: none` does not stop an
   * autoplaying video from loading. Half the bytes were for pictures nobody
   * would ever see, and four videos looped for ever whether or not the reader
   * had scrolled anywhere near them.
   *
   * A hidden element never intersects, so the wrong-theme clips are now never
   * requested at all.
   *
   * Reduced motion is honoured by not starting them and giving them controls
   * instead: the poster is the last frame, so a reader who does not want motion
   * still sees the result, and can play it if they choose.
   */
  function playVisibleClips() {
    var clips = document.querySelectorAll('.clip video');
    if (!clips.length) return;

    var still = window.matchMedia
      && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

    /**
     * When a clip will not play by itself, offer it rather than force it.
     *
     * NOT `video.controls = true`, which is what this did first: a row of
     * native player chromes changes the look of the whole section, and a reader
     * who asked for less motion did not ask for a different page. The poster is
     * the LAST frame — the result — so the still is already informative, and
     * this adds one small button over it.
     */
    function offer(v) {
      var fig = v.closest('figure');
      if (!fig || fig.querySelector('.clip-play')) return;

      var btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'clip-play';
      btn.setAttribute('aria-label', 'Play this clip');
      btn.innerHTML = '<svg viewBox="0 0 24 24" aria-hidden="true">'
        + '<path d="M8 5.5a1 1 0 0 1 1.53-.85l9 6.5a1 1 0 0 1 0 1.7l-9 6.5A1 1 0 0 1 8 18.5Z"/></svg>';

      btn.addEventListener('click', function (e) {
        // The figure opens the lightbox; the button plays in place.
        e.stopPropagation();
        btn.remove();
        v.controls = true;
        v.play().catch(function () { /* nothing more to offer */ });
      });

      fig.appendChild(btn);
    }

    if (still || !window.IntersectionObserver) {
      clips.forEach(offer);
      return;
    }

    var seen = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (e.isIntersecting) {
          // play() rejects when the browser declines — a stricter autoplay
          // policy, a saver mode, a background tab. Not an error worth
          // surfacing: the poster is already the informative frame, so offer
          // the clip instead of reporting a failure nobody can act on.
          var p = e.target.play();
          if (p && p.catch) p.catch(function () { offer(e.target); });
        } else {
          e.target.pause();
        }
      });
    }, { rootMargin: '150px 0px', threshold: 0.15 });

    clips.forEach(function (v) { seen.observe(v); });
  }

  buildToggle();
  addCopyButtons();
  addZoom();
  playVisibleClips();
})();
