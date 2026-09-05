/* ==========================================================================
   SnmpLens — live data from the GitHub API
   --------------------------------------------------------------------------
   Every widget here UPGRADES markup that is already correct without it. The
   unauthenticated API allows 60 requests an hour per address, so a visitor who
   is rate-limited, offline, or blocking scripts must still get a page that
   reads properly and links to GitHub — never an empty box or a spinner that
   never resolves.

   Responses are cached in sessionStorage so moving between pages costs no
   further requests.
   ========================================================================== */

(function () {
  'use strict';

  var REPO = 'Wasabules/SnmpLens';
  var API = 'https://api.github.com/repos/' + REPO;
  var WEB = 'https://github.com/' + REPO;
  var TTL = 10 * 60 * 1000; // ten minutes

  /* --- fetch with a session cache; null on any failure ------------------- */

  function cached(key) {
    try {
      var raw = sessionStorage.getItem(key);
      if (!raw) return null;
      var box = JSON.parse(raw);
      if (Date.now() - box.at > TTL) return null;
      return box.data;
    } catch (e) {
      return null; // private mode, blocked storage, corrupt entry
    }
  }

  function store(key, data) {
    try {
      sessionStorage.setItem(key, JSON.stringify({ at: Date.now(), data: data }));
    } catch (e) { /* quota or blocked storage: the cache is a convenience */ }
  }

  function gh(path) {
    var key = 'snmplens:gh:' + path;
    var hit = cached(key);
    if (hit) return Promise.resolve(hit);

    return fetch(API + path, { headers: { Accept: 'application/vnd.github+json' } })
      .then(function (r) {
        // 403 is the rate limit, 404 means the repo has no releases yet.
        if (!r.ok) return null;
        return r.json();
      })
      .then(function (data) {
        if (data) store(key, data);
        return data;
      })
      .catch(function () { return null; });
  }

  /* --- helpers ---------------------------------------------------------- */

  function $(sel, root) { return (root || document).querySelector(sel); }
  function $$(sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); }

  function compact(n) {
    if (typeof n !== 'number') return null;
    if (n < 1000) return String(n);
    if (n < 10000) return (n / 1000).toFixed(1).replace(/\.0$/, '') + 'k';
    return Math.round(n / 1000) + 'k';
  }

  function bytes(n) {
    if (!n && n !== 0) return '';
    if (n < 1024 * 1024) return Math.round(n / 1024) + ' KB';
    return (n / 1024 / 1024).toFixed(1) + ' MB';
  }

  function when(iso) {
    if (!iso) return '';
    var d = new Date(iso);
    if (isNaN(d)) return '';
    return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
  }

  function fill(el, text) {
    if (!el || text === null || text === undefined || text === '') return;
    el.textContent = text;
    el.classList.remove('skeleton');
  }

  // Anything still showing a loading state when a request fails must fall back
  // to whatever the markup said before, not sit there pulsing forever.
  function settle(root) {
    $$('.skeleton', root || document).forEach(function (el) {
      el.classList.remove('skeleton');
      // Keyed off the ATTRIBUTE, not the text. Every skeleton carries
      // placeholder text — that text is what gives the pulsing box its width —
      // so `!textContent.trim()` was false for all of them and the fallback
      // never once applied. A rate-limited API left the placeholders on screen
      // as though they were data.
      //
      // fill() removes the class, so anything already filled is not in this
      // list and cannot be overwritten.
      if (el.dataset.fallback !== undefined) el.textContent = el.dataset.fallback;
      else if (!el.textContent.trim()) el.textContent = '—';
    });
  }

  /* --- repository statistics -------------------------------------------- */

  function repoStats() {
    var host = $('[data-gh="stats"]');
    if (!host) return Promise.resolve();

    return gh('').then(function (r) {
      if (!r) { settle(host); return; }
      fill($('[data-stat="stars"]', host), compact(r.stargazers_count));
      fill($('[data-stat="forks"]', host), compact(r.forks_count));
      fill($('[data-stat="issues"]', host), compact(r.open_issues_count));
      fill($('[data-stat="updated"]', host), when(r.pushed_at));
      settle(host);
    });
  }

  /* --- latest release ---------------------------------------------------- */

  // The asset a visitor most likely wants, from what the browser will admit to.
  function guessPlatform() {
    var ua = (navigator.userAgent || '') + ' ' + (navigator.platform || '');
    if (/Win/i.test(ua)) return 'windows';
    if (/Mac|iPhone|iPad/i.test(ua)) return 'macos';
    if (/Linux|X11|Android/i.test(ua)) return 'linux';
    return null;
  }

  function latestRelease() {
    var hosts = $$('[data-gh="release"]');
    if (!hosts.length) return Promise.resolve();

    return gh('/releases/latest').then(function (rel) {
      if (!rel || !rel.tag_name) { hosts.forEach(settle); return; }

      hosts.forEach(function (host) {
        fill($('[data-release="tag"]', host), rel.tag_name);
        fill($('[data-release="date"]', host), when(rel.published_at));

        var link = $('[data-release="link"]', host);
        if (link) link.href = rel.html_url || (WEB + '/releases/latest');

        // Point every platform button at the real asset for this release.
        $$('[data-asset]', host).forEach(function (a) {
          var wanted = a.getAttribute('data-asset');
          var asset = (rel.assets || []).filter(function (x) { return x.name === wanted; })[0];
          if (!asset) return;
          a.href = asset.browser_download_url;
          var sz = $('.sz', a) || $('[data-asset-size="' + wanted + '"]', host);
          if (sz) sz.textContent = bytes(asset.size);
        });

        // The full asset list, when the page asks for one.
        var list = $('[data-release="assets"]', host);
        if (list && rel.assets && rel.assets.length) {
          list.innerHTML = '';
          rel.assets.forEach(function (a) {
            var li = document.createElement('li');
            var link = document.createElement('a');
            link.href = a.browser_download_url;
            link.textContent = a.name;
            var sz = document.createElement('span');
            sz.className = 'sz';
            sz.textContent = bytes(a.size);
            li.appendChild(link);
            li.appendChild(sz);
            list.appendChild(li);
          });
        }

        settle(host);
      });

      // Nudge the visitor's own platform to the front.
      var plat = guessPlatform();
      if (plat) {
        $$('[data-platform="' + plat + '"]').forEach(function (el) {
          el.setAttribute('data-yours', 'true');
          var hint = $('[data-platform-hint]', el);
          if (hint) hint.hidden = false;
        });
      }
    });
  }

  /* --- contributors ------------------------------------------------------ */

  function contributors() {
    var host = $('[data-gh="contributors"]');
    if (!host) return Promise.resolve();

    return gh('/contributors?per_page=24').then(function (people) {
      if (!people || !people.length) { settle(host); return; }
      host.innerHTML = '';
      people.forEach(function (p) {
        var li = document.createElement('li');
        var a = document.createElement('a');
        a.href = p.html_url;
        a.title = p.login + ' — ' + p.contributions + ' commits';
        var img = document.createElement('img');
        img.src = p.avatar_url + '&s=80';
        img.alt = p.login;
        img.loading = 'lazy';
        img.width = 40;
        img.height = 40;
        a.appendChild(img);
        li.appendChild(a);
        host.appendChild(li);
      });
    });
  }

  /* --- release history --------------------------------------------------- */

  // Release notes are Markdown written by a human. They are rendered as TEXT,
  // never as HTML: this is remote content and the page has no business
  // executing it. Only the few shapes that matter for legibility are turned
  // into elements, by hand.
  /**
   * The inline span of a release note: **bold**, `code`, and [text](url).
   *
   * Built as DOM NODES, never as innerHTML. A release note is Markdown written
   * by a person and fetched over the network, so the only safe way to give it
   * any structure at all is to make the elements and set textContent on each —
   * a regex that produces an HTML string is a regex that will one day produce
   * someone else's script tag.
   *
   * The alternative — leaving the asterisks alone, which is what this did —
   * puts literal `**SNMPv3 AuthNoPriv**` on the changelog page, which is worse
   * than either.
   */
  function inline(text, into) {
    // One pass, alternating between plain text and the three things recognised.
    // The link case takes only http(s), so a note cannot introduce a javascript:
    // URL by writing one in Markdown.
    // Order matters: ** is tried before *, or every bold marker is read as an
    // empty italic. Underscores are deliberately NOT italics — `snmp_test_agent`
    // and `--user-data-dir` turn up in these notes far more often than emphasis
    // written that way does.
    var re = /(\*\*|__)(.+?)\1|`([^`]+)`|\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)|\*(\S(?:[^*]*\S)?)\*/g;
    var at = 0;
    var m;

    while ((m = re.exec(text)) !== null) {
      if (m.index > at) into.appendChild(document.createTextNode(text.slice(at, m.index)));

      var el;
      if (m[2] !== undefined) {
        el = document.createElement('strong');
        el.textContent = m[2];
      } else if (m[3] !== undefined) {
        el = document.createElement('code');
        el.textContent = m[3];
      } else if (m[4] !== undefined) {
        el = document.createElement('a');
        el.href = m[5];
        el.textContent = m[4];
      } else {
        el = document.createElement('em');
        el.textContent = m[6];
      }
      into.appendChild(el);
      at = m.index + m[0].length;
    }

    if (at < text.length) into.appendChild(document.createTextNode(text.slice(at)));
  }

  function renderNotes(md, into) {
    var lines = String(md || '').split(/\r?\n/);
    var list = null;

    lines.forEach(function (raw) {
      var line = raw.trim();
      if (!line) { list = null; return; }

      // A Markdown rule. Rendering it as the three hyphens it is written with
      // was the other thing leaking onto the page.
      if (/^(-{3,}|\*{3,}|_{3,})$/.test(line)) {
        list = null;
        into.appendChild(document.createElement('hr'));
        return;
      }

      var heading = line.match(/^#{1,4}\s+(.*)$/);
      if (heading) {
        list = null;
        var h = document.createElement('h4');
        inline(heading[1], h);
        into.appendChild(h);
        return;
      }

      var bullet = line.match(/^[-*]\s+(.*)$/);
      if (bullet) {
        if (!list) { list = document.createElement('ul'); into.appendChild(list); }
        var li = document.createElement('li');
        inline(bullet[1], li);
        list.appendChild(li);
        return;
      }

      list = null;
      var p = document.createElement('p');
      inline(line, p);
      into.appendChild(p);
    });
  }

  function releaseHistory() {
    var host = $('[data-gh="releases"]');
    if (!host) return Promise.resolve();

    return gh('/releases?per_page=20').then(function (rels) {
      if (!rels || !rels.length) { settle(host); return; }

      host.innerHTML = '';
      rels.forEach(function (r) {
        var section = document.createElement('section');
        section.className = 'release-entry';

        var head = document.createElement('div');
        head.className = 'release-head';

        var h2 = document.createElement('h2');
        h2.id = 'v' + String(r.tag_name || '').replace(/[^\w.-]/g, '');
        var a = document.createElement('a');
        a.href = r.html_url;
        a.textContent = r.tag_name;
        h2.appendChild(a);
        head.appendChild(h2);

        var date = document.createElement('span');
        date.className = 'muted small';
        date.textContent = when(r.published_at);
        head.appendChild(date);

        if (r.prerelease) {
          var pre = document.createElement('span');
          pre.className = 'badge';
          pre.textContent = 'pre-release';
          head.appendChild(pre);
        }

        section.appendChild(head);
        renderNotes(r.body, section);
        host.appendChild(section);
      });
    });
  }

  /* --- go ----------------------------------------------------------------- */

  function start() {
    Promise.all([repoStats(), latestRelease(), contributors(), releaseHistory()])
      .catch(function () { settle(); })
      .then(function () { settle(); });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', start);
  } else {
    start();
  }
})();
