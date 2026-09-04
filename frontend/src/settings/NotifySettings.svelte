<script>
  import { onMount } from 'svelte';
  import { _ } from 'svelte-i18n';
  import { get } from 'svelte/store';
  import Icon from '../Icon.svelte';
  import TemplateEditor from './TemplateEditor.svelte';
  import { notificationStore } from '../stores/notifications';
  import {
    NotifyListSinks,
    NotifySaveSink,
    NotifyDeleteSink,
    NotifyTestSink,
    NotifyListRoutes,
    NotifySaveRoute,
    NotifyDeleteRoute,
    NotifyClearSinkSecret,
    SecretsBackend,
  } from '../../wailsjs/go/main/App';

  const CATEGORIES = ['trap', 'threshold', 'reachability', 'system'];
  const SEVERITIES = ['info', 'warning', 'minor', 'major', 'critical'];

  let sinks = [];
  let routes = [];
  let editingSink = null;
  let editingRoute = null;
  let testing = null;
  let backend = '';

  async function reload() {
    try {
      sinks = (await NotifyListSinks()) || [];
      routes = (await NotifyListRoutes()) || [];
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }

  onMount(async () => {
    await reload();
    try {
      backend = await SecretsBackend();
    } catch (e) {
      backend = 'unavailable';
    }
  });

  function newSink(kind) {
    return {
      id: '',
      name: '',
      kind,
      enabled: true,
      redact: false,
      secret: '',
      hasSecret: false,
      template: { subject: '', body: '' },
      syslog: {
        address: '', protocol: 'udp', facility: 16, hostname: '', appName: 'SnmpLens', timeout: 5,
        caCert: '', serverName: '', insecureSkipVerify: false, clientCert: '',
      },
      webhook: {
        url: '', method: 'POST', headers: {}, timeout: 10, payloadMode: 'envelope',
        allowPlaintextHttp: false, caCert: '', serverName: '', insecureSkipVerify: false,
      },
      email: {
        host: '', port: 587, username: '', from: '', to: [],
        encryption: 'starttls', authMethod: 'plain', insecureSkipVerify: false, timeout: 20,
        caCert: '', serverName: '',
      },
    };
  }

  function newRoute() {
    return {
      id: '',
      name: '',
      enabled: true,
      priority: 100,
      stop: false,
      sinkIds: [],
      match: { categories: [], kinds: [], minSeverity: '', sources: [], oidPrefix: '', sessionIds: [], states: [], contains: '', quietHours: null },
    };
  }

  async function saveSink() {
    if (!editingSink.name.trim()) {
      notificationStore.add(get(_)('notify.nameRequired'), 'error');
      return;
    }
    try {
      // The recipient list is edited as one line, stored as an array.
      if (typeof editingSink.email.toRaw === 'string') {
        editingSink.email.to = editingSink.email.toRaw.split(/[,;]+/).map((x) => x.trim()).filter(Boolean);
        delete editingSink.email.toRaw;
      }
      await NotifySaveSink(editingSink);
      editingSink = null;
      await reload();
      notificationStore.add(get(_)('notify.sinkSaved'), 'success');
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }

  async function clearSecret(id) {
    if (!id) return;
    try {
      await NotifyClearSinkSecret(id);
      editingSink.hasSecret = false;
      editingSink.secret = '';
      notificationStore.add(get(_)('notify.secretCleared'), 'success');
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }

  async function removeSink(id) {
    try {
      await NotifyDeleteSink(id);
      await reload();
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }

  async function testSink(cfg) {
    testing = cfg.id || 'new';
    try {
      await NotifyTestSink(cfg);
      notificationStore.add(get(_)('notify.testSent'), 'success');
    } catch (e) {
      // The verbatim error is the whole point of a test button.
      notificationStore.add(get(_)('notify.testFailed', { values: { error: String(e) } }), 'error');
    } finally {
      testing = null;
    }
  }

  async function saveRoute() {
    if (!editingRoute.name.trim()) {
      notificationStore.add(get(_)('notify.nameRequired'), 'error');
      return;
    }
    try {
      if (typeof editingRoute.match.sourcesRaw === 'string') {
        editingRoute.match.sources = editingRoute.match.sourcesRaw.split(/[,;\s]+/).map((x) => x.trim()).filter(Boolean);
        delete editingRoute.match.sourcesRaw;
      }
      editingRoute.priority = Number(editingRoute.priority) || 100;
      await NotifySaveRoute(editingRoute);
      editingRoute = null;
      await reload();
      notificationStore.add(get(_)('notify.routeSaved'), 'success');
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }

  async function removeRoute(id) {
    try {
      await NotifyDeleteRoute(id);
      await reload();
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }

  // Must match notify.SecretPlaceholder in Go: a header value containing it
  // draws on the stored credential instead of the value being written to the
  // database with the rest of the configuration.
  const SECRET_PLACEHOLDER = '{{secret}}';

  // Headers are edited as "Name: value" lines, which is how anyone who has
  // configured a webhook elsewhere expects to type them.
  function headersToText(headers) {
    return Object.entries(headers || {}).map(([k, v]) => `${k}: ${v}`).join('\n');
  }

  function textToHeaders(text) {
    const out = {};
    for (const line of String(text || '').split('\n')) {
      const i = line.indexOf(':');
      if (i <= 0) continue;
      const name = line.slice(0, i).trim();
      if (name) out[name] = line.slice(i + 1).trim();
    }
    return out;
  }

  function isPlaintextURL(url) {
    return /^http:\/\//i.test(String(url || '').trim());
  }

  function toggleIn(list, value) {
    return list.includes(value) ? list.filter((v) => v !== value) : [...list, value];
  }

  function sinkName(id) {
    const s = sinks.find((x) => x.id === id);
    return s ? s.name : id;
  }

  function describeSink(s) {
    if (s.kind === 'syslog') return `${s.syslog?.protocol || 'udp'}://${s.syslog?.address || '—'}`;
    if (s.kind === 'webhook') return s.webhook?.url || '—';
    if (s.kind === 'email') return `${s.email?.host || '—'} → ${(s.email?.to || []).join(', ') || '—'}`;
    return '';
  }
</script>

<!-- Escape on the window, not on the overlay: a keydown starts at whatever
     has focus — a field inside the dialog — and the box below stops clicks,
     which used to stop keys with them. The overlay handler never ran. -->
<svelte:window on:keydown={(e) => {
  if (e.key !== 'Escape') return;
    if (editingSink) { editingSink = null; return; }
    if (editingRoute) editingRoute = null;
}} />

<div class="notify-settings">
  <!-- ================= SINKS ================= -->
  <section>
    <div class="sec-head">
      <h4>{$_('notify.sinksTitle')}</h4>
      <div class="add-row">
        <button class="btn btn-small" on:click={() => (editingSink = newSink('syslog'))}>+ Syslog</button>
        <button class="btn btn-small" on:click={() => (editingSink = newSink('webhook'))}>+ Webhook</button>
        <button class="btn btn-small" on:click={() => (editingSink = newSink('email'))}>+ Email</button>
      </div>
    </div>
    <p class="hint">{$_('notify.sinksHint')}</p>

    {#if sinks.length === 0}
      <p class="empty-state">{$_('notify.noSinks')}</p>
    {:else}
      <ul class="list">
        {#each sinks as s (s.id)}
          <li>
            <span class="badge kind-{s.kind}">{s.kind}</span>
            <span class="name" class:off={!s.enabled}>{s.name}</span>
            <span class="detail" title={describeSink(s)}>{describeSink(s)}</span>
            {#if s.hasSecret}<span class="chip-flag ok">{$_('notify.secretSet')}</span>{/if}
            {#if s.redact}<span class="chip-flag">{$_('notify.redacted')}</span>{/if}
            <button class="btn-copy-small" on:click={() => testSink(s)} title={$_('notify.test')} disabled={testing === s.id}>
              <Icon name={testing === s.id ? 'loader-circle' : 'zap'} size={13} class={testing === s.id ? 'icon-spin' : ''} />
            </button>
            <button class="btn-copy-small" on:click={() => (editingSink = { ...s, template: { ...(s.template || { subject: '', body: '' }) } })} title={$_('common.edit')}>
              <Icon name="pencil" size={13} />
            </button>
            <button class="btn-copy-small" on:click={() => removeSink(s.id)} title={$_('common.delete')}>
              <Icon name="trash-2" size={13} />
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  <!-- ================= ROUTES ================= -->
  <section>
    <div class="sec-head">
      <h4>{$_('notify.routesTitle')}</h4>
      <button class="btn btn-small" on:click={() => (editingRoute = newRoute())} disabled={sinks.length === 0}>
        + {$_('notify.addRoute')}
      </button>
    </div>
    <p class="hint">{$_('notify.routesHint')}</p>

    {#if routes.length === 0}
      <p class="empty-state">{sinks.length === 0 ? $_('notify.sinkFirst') : $_('notify.noRoutes')}</p>
    {:else}
      <ul class="list">
        {#each routes as r (r.id)}
          <li>
            <span class="prio" title={$_('notify.priority')}>{r.priority}</span>
            <span class="name" class:off={!r.enabled}>{r.name}</span>
            <span class="detail">
              {(r.match.categories || []).join(', ') || $_('notify.allCategories')}
              {#if r.match.minSeverity}· ≥ {$_('events.severity.' + r.match.minSeverity)}{/if}
              {#if (r.match.sources || []).length}· {(r.match.sources || []).join(' ')}{/if}
              → {(r.sinkIds || []).map(sinkName).join(', ') || '—'}
            </span>
            {#if r.stop}<span class="chip-flag">{$_('notify.stops')}</span>{/if}
            <button class="btn-copy-small" on:click={() => (editingRoute = JSON.parse(JSON.stringify(r)))} title={$_('common.edit')}>
              <Icon name="pencil" size={13} />
            </button>
            <button class="btn-copy-small" on:click={() => removeRoute(r.id)} title={$_('common.delete')}>
              <Icon name="trash-2" size={13} />
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  </section>
</div>

<!-- ================= SINK EDITOR ================= -->
{#if editingSink}
  <div class="editor-overlay" on:click={() => (editingSink = null)} role="button" tabindex="-1">
    <div class="editor sink-editor" on:click|stopPropagation role="dialog">
      <h3>
        {$_('notify.sinkEditor', { values: { kind: editingSink.kind } })}
        {#if editingSink.name}<span class="sink-name">— {editingSink.name}</span>{/if}
      </h3>

      <div class="editor-body">
      <div class="pane pane-config">
      <label class="fld"><span>{$_('notify.name')}</span>
        <input type="text" bind:value={editingSink.name} placeholder="NOC syslog" />
      </label>

      {#if editingSink.kind === 'syslog'}
        <h4 class="grp-head">{$_('notify.grpConnection')}</h4>
        <label class="fld"><span>{$_('notify.address')}</span>
          <input type="text" bind:value={editingSink.syslog.address} placeholder="10.0.0.50:514" />
        </label>
        <div class="fld-row">
          <label class="fld"><span>{$_('notify.protocol')}</span>
            <select bind:value={editingSink.syslog.protocol}>
              <option value="udp">UDP</option>
              <option value="tcp">TCP</option>
              <option value="tls">TLS (RFC 5425)</option>
            </select>
          </label>
          <label class="fld"><span>{$_('notify.facility')}</span>
            <input type="number" min="0" max="23" bind:value={editingSink.syslog.facility} />
          </label>
        </div>
        {#if editingSink.syslog.protocol === 'tls'}
          <h4 class="grp-head">{$_('notify.grpServerTrust')}</h4>
          <p class="note">{$_('notify.tlsNote')}</p>
          <label class="fld"><span>{$_('notify.caCert')}</span>
            <textarea rows="3" bind:value={editingSink.syslog.caCert}
              placeholder={'-----BEGIN CERTIFICATE-----'}></textarea>
            <span class="sub">{$_('notify.caCertHint')}</span>
          </label>
          <label class="fld"><span>{$_('notify.serverName')}</span>
            <input type="text" bind:value={editingSink.syslog.serverName} placeholder="collector.example.com" />
            <span class="sub">{$_('notify.serverNameHint')}</span>
          </label>
          <label class="toggle">
            <input type="checkbox" bind:checked={editingSink.syslog.insecureSkipVerify} />
            <span>{$_('notify.insecureSkipVerify')}</span>
          </label>
          {#if editingSink.syslog.insecureSkipVerify}
            <p class="note warn">
              <Icon name="triangle-alert" size={14} /> {$_('notify.insecureWarning')}
            </p>
          {/if}
          <h4 class="grp-head">{$_('notify.grpClientAuth')}</h4>
          <label class="fld"><span>{$_('notify.clientCert')}</span>
            <textarea rows="3" bind:value={editingSink.syslog.clientCert}
              placeholder={'-----BEGIN CERTIFICATE-----'}></textarea>
            <span class="sub">{$_('notify.clientCertHint')}</span>
          </label>
          <label class="fld"><span>{$_('notify.clientKey')}</span>
            <textarea rows="3" bind:value={editingSink.secret}
              placeholder={editingSink.hasSecret ? $_('notify.secretOnFile') : '-----BEGIN PRIVATE KEY-----'}></textarea>
            <span class="sub">{$_('notify.secretHint', { values: { backend } })}</span>
          </label>
          {#if editingSink.hasSecret}
            <button type="button" class="btn-mode" on:click={() => clearSecret(editingSink.id)}>{$_('notify.clearSecret')}</button>
          {/if}
        {:else}
          <p class="note">{$_('notify.udpNote')}</p>
        {/if}
      {:else if editingSink.kind === 'webhook'}
        <h4 class="grp-head">{$_('notify.grpEndpoint')}</h4>
        <label class="fld"><span>URL</span>
          <input type="text" bind:value={editingSink.webhook.url} placeholder="https://hooks.example.com/snmplens" />
        </label>
        <div class="fld-row">
          <label class="fld"><span>{$_('notify.method')}</span>
            <select bind:value={editingSink.webhook.method}>
              <option value="POST">POST</option>
              <option value="PUT">PUT</option>
              <option value="PATCH">PATCH</option>
            </select>
          </label>
          <label class="fld"><span>{$_('notify.token')}</span>
            <input type="password" bind:value={editingSink.secret}
              placeholder={editingSink.hasSecret ? $_('notify.secretOnFile') : ''} />
          </label>
          {#if editingSink.hasSecret}
            <button type="button" class="btn-mode" on:click={() => clearSecret(editingSink.id)}>{$_('notify.clearSecret')}</button>
          {/if}
        </div>
        <span class="sub">{$_('notify.secretHint', { values: { backend } })}</span>
        <h4 class="grp-head">{$_('notify.grpPayload')}</h4>
        <label class="fld"><span>{$_('notify.payloadMode')}</span>
          <select bind:value={editingSink.webhook.payloadMode}>
            <option value="envelope">{$_('notify.payloadEnvelope')}</option>
            <option value="template">{$_('notify.payloadTemplate')}</option>
          </select>
          <span class="sub">
            {editingSink.webhook.payloadMode === 'template'
              ? $_('notify.payloadTemplateHint')
              : $_('notify.payloadEnvelopeHint')}
          </span>
        </label>

        <h4 class="grp-head">{$_('notify.grpHeaders')}</h4>
        <label class="fld"><span>{$_('notify.headers')}</span>
          <textarea rows="3" value={headersToText(editingSink.webhook.headers)}
            on:input={(e) => (editingSink.webhook.headers = textToHeaders(e.target.value))}
            placeholder={'X-Api-Key: ' + SECRET_PLACEHOLDER}></textarea>
          <span class="sub">{$_('notify.headersHint', { values: { placeholder: SECRET_PLACEHOLDER } })}</span>
        </label>
        <p class="note">{$_('notify.webhookNote')}</p>
        <h4 class="grp-head">{$_('notify.grpTransport')}</h4>
        {#if isPlaintextURL(editingSink.webhook.url)}
          <label class="toggle">
            <input type="checkbox" bind:checked={editingSink.webhook.allowPlaintextHttp} />
            <span>{$_('notify.allowPlaintextHttp')}</span>
          </label>
          <p class="note warn">
            <Icon name="triangle-alert" size={14} /> {$_('notify.plaintextHttpWarning')}
          </p>
        {:else}
          <label class="fld"><span>{$_('notify.caCert')}</span>
            <textarea rows="3" bind:value={editingSink.webhook.caCert}
              placeholder={'-----BEGIN CERTIFICATE-----'}></textarea>
            <span class="sub">{$_('notify.caCertHint')}</span>
          </label>
          <label class="fld"><span>{$_('notify.serverName')}</span>
            <input type="text" bind:value={editingSink.webhook.serverName} placeholder="hooks.example.com" />
            <span class="sub">{$_('notify.serverNameHint')}</span>
          </label>
          <label class="toggle">
            <input type="checkbox" bind:checked={editingSink.webhook.insecureSkipVerify} />
            <span>{$_('notify.insecureSkipVerify')}</span>
          </label>
          {#if editingSink.webhook.insecureSkipVerify}
            <p class="note warn">
              <Icon name="triangle-alert" size={14} /> {$_('notify.insecureWarning')}
            </p>
          {/if}
        {/if}
      {:else}
        <div class="fld-row">
          <h4 class="grp-head">{$_('notify.grpSmtp')}</h4>
          <label class="fld"><span>{$_('notify.host')}</span>
            <input type="text" bind:value={editingSink.email.host} placeholder="smtp.example.com" />
          </label>
          <label class="fld narrow"><span>{$_('notify.port')}</span>
            <input type="number" bind:value={editingSink.email.port} />
          </label>
        </div>
        <div class="fld-row">
          <label class="fld"><span>{$_('notify.encryption')}</span>
            <select bind:value={editingSink.email.encryption}>
              <option value="starttls">STARTTLS</option>
              <option value="tls">TLS</option>
              <option value="none">{$_('notify.none')}</option>
            </select>
          </label>
          <label class="fld"><span>{$_('notify.authMethod')}</span>
            <select bind:value={editingSink.email.authMethod}>
              <option value="plain">PLAIN</option>
              <option value="login">LOGIN</option>
              <option value="none">{$_('notify.none')}</option>
            </select>
          </label>
        </div>
        <h4 class="grp-head">{$_('notify.grpCredentials')}</h4>
        <div class="fld-row">
          <label class="fld"><span>{$_('notify.username')}</span>
            <input type="text" bind:value={editingSink.email.username} />
          </label>
          <label class="fld"><span>{$_('notify.password')}</span>
            <input type="password" bind:value={editingSink.secret}
              placeholder={editingSink.hasSecret ? $_('notify.secretOnFile') : ''} />
          </label>
          {#if editingSink.hasSecret}
            <button type="button" class="btn-mode" on:click={() => clearSecret(editingSink.id)}>{$_('notify.clearSecret')}</button>
          {/if}
        </div>
        <h4 class="grp-head">{$_('notify.grpEnvelope')}</h4>
        <label class="fld"><span>{$_('notify.from')}</span>
          <input type="text" bind:value={editingSink.email.from} placeholder="snmplens@example.com" />
        </label>
        <label class="fld"><span>{$_('notify.to')}</span>
          <input type="text" value={(editingSink.email.to || []).join(', ')}
            on:input={(e) => (editingSink.email.toRaw = e.target.value)} placeholder="noc@example.com, oncall@example.com" />
        </label>
        <p class="note">{$_('notify.secretHint', { values: { backend } })}</p>

        <h4 class="grp-head">{$_('notify.grpTrust')}</h4>
        {#if editingSink.email.encryption !== 'none'}
          <label class="fld"><span>{$_('notify.caCert')}</span>
            <textarea rows="3" bind:value={editingSink.email.caCert}
              placeholder={'-----BEGIN CERTIFICATE-----'}></textarea>
            <span class="sub">{$_('notify.caCertHint')}</span>
          </label>
          <label class="fld"><span>{$_('notify.serverName')}</span>
            <input type="text" bind:value={editingSink.email.serverName} placeholder="smtp.example.com" />
            <span class="sub">{$_('notify.serverNameHint')}</span>
          </label>
          <label class="toggle">
            <input type="checkbox" bind:checked={editingSink.email.insecureSkipVerify} />
            <span>{$_('notify.insecureSkipVerify')}</span>
          </label>
          {#if editingSink.email.insecureSkipVerify}
            <p class="note warn">
              <Icon name="triangle-alert" size={14} /> {$_('notify.insecureWarning')}
            </p>
          {/if}
        {:else}
          <p class="note warn">
            <Icon name="triangle-alert" size={14} /> {$_('notify.noEncryptionWarning')}
          </p>
        {/if}
      {/if}

      </div>

      <div class="pane pane-template">
        <TemplateEditor bind:sink={editingSink} />
      </div>
      </div>

      <div class="editor-actions">
        <label class="toggle">
          <input type="checkbox" bind:checked={editingSink.enabled} /> {$_('notify.enabled')}
        </label>
        <label class="toggle" title={$_('notify.redactHint')}>
          <input type="checkbox" bind:checked={editingSink.redact} /> {$_('notify.redact')}
        </label>
        <span class="sep"></span>
        <button class="btn tertiary" on:click={() => testSink(editingSink)} disabled={testing === 'new'}>
          <Icon name="zap" size={13} /> {$_('notify.test')}
        </button>
        <span class="spacer"></span>
        <button class="btn secondary" on:click={() => (editingSink = null)}>{$_('common.cancel')}</button>
        <button class="btn" on:click={saveSink}>{$_('common.save')}</button>
      </div>
    </div>
  </div>
{/if}

<!-- ================= ROUTE EDITOR ================= -->
{#if editingRoute}
  <div class="editor-overlay" on:click={() => (editingRoute = null)} role="button" tabindex="-1">
    <div class="editor" on:click|stopPropagation role="dialog">
      <h3>{$_('notify.routeEditor')}</h3>

      <label class="fld"><span>{$_('notify.name')}</span>
        <input type="text" bind:value={editingRoute.name} placeholder="Critique → NOC" />
      </label>

      <div class="fld"><span>{$_('notify.matchCategories')}</span>
        <div class="chips">
          {#each CATEGORIES as c}
            <button class="chip" class:on={(editingRoute.match.categories || []).includes(c)}
              on:click={() => (editingRoute.match.categories = toggleIn(editingRoute.match.categories || [], c))}>
              {$_('events.category.' + c)}
            </button>
          {/each}
        </div>
        <span class="sub">{$_('notify.emptyMeansAll')}</span>
      </div>

      <div class="fld-row">
        <label class="fld"><span>{$_('notify.minSeverity')}</span>
          <select bind:value={editingRoute.match.minSeverity}>
            <option value="">{$_('notify.any')}</option>
            {#each SEVERITIES as s}<option value={s}>{$_('events.severity.' + s)}</option>{/each}
          </select>
        </label>
        <label class="fld narrow"><span>{$_('notify.priority')}</span>
          <input type="number" bind:value={editingRoute.priority} />
        </label>
      </div>

      <label class="fld"><span>{$_('notify.sources')}</span>
        <input type="text" value={(editingRoute.match.sources || []).join(' ')}
          on:input={(e) => (editingRoute.match.sourcesRaw = e.target.value)}
          placeholder="10.0.0.0/8 sw-*" />
        <span class="sub">{$_('notify.sourcesHint')}</span>
      </label>

      <label class="fld"><span>{$_('notify.oidPrefix')}</span>
        <input type="text" bind:value={editingRoute.match.oidPrefix} placeholder="1.3.6.1.2.1.2" />
      </label>

      <div class="fld"><span>{$_('notify.quietHours')}</span>
        <div class="fld-row">
          <input type="time" value={editingRoute.match.quietHours?.from || ''}
            on:input={(e) => (editingRoute.match.quietHours = { ...(editingRoute.match.quietHours || {}), from: e.target.value })} />
          <span class="arrow">→</span>
          <input type="time" value={editingRoute.match.quietHours?.to || ''}
            on:input={(e) => (editingRoute.match.quietHours = { ...(editingRoute.match.quietHours || {}), to: e.target.value })} />
          <button class="btn-mode" on:click={() => (editingRoute.match.quietHours = null)}>{$_('common.clear')}</button>
        </div>
        <span class="sub">{$_('notify.quietHoursHint')}</span>
      </div>

      <div class="fld"><span>{$_('notify.deliverTo')}</span>
        <div class="chips">
          {#each sinks as s (s.id)}
            <button class="chip" class:on={(editingRoute.sinkIds || []).includes(s.id)}
              on:click={() => (editingRoute.sinkIds = toggleIn(editingRoute.sinkIds || [], s.id))}>
              {s.name}
            </button>
          {/each}
        </div>
      </div>

      <label class="toggle"><input type="checkbox" bind:checked={editingRoute.enabled} /> {$_('notify.enabled')}</label>
      <label class="toggle" title={$_('notify.stopHint')}><input type="checkbox" bind:checked={editingRoute.stop} /> {$_('notify.stopAfter')}</label>

      <div class="editor-actions">
        <span class="spacer"></span>
        <button class="btn secondary" on:click={() => (editingRoute = null)}>{$_('common.cancel')}</button>
        <button class="btn" on:click={saveRoute}>{$_('common.save')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .notify-settings {
    display: flex;
    flex-direction: column;
    gap: 22px;
  }

  .sec-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
  }

  .sec-head h4 {
    margin: 0;
    font-size: 1em;
  }

  .add-row {
    display: flex;
    gap: 6px;
  }

  .hint,
  .note,
  .sub {
    font-size: 0.78em;
    color: var(--text-muted);
    margin: 4px 0 8px;
  }

  .note.warn {
    color: var(--warning-color);
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    border: 1px solid var(--border-color);
    border-radius: 5px;
  }

  .list li {
    display: grid;
    grid-template-columns: auto minmax(90px, 1fr) minmax(0, 2fr) auto auto auto auto auto;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    border-bottom: 1px solid var(--border-color);
    font-size: 0.85em;
  }

  .list li:last-child {
    border-bottom: none;
  }

  .badge {
    padding: 1px 7px;
    border-radius: 9px;
    font-size: 0.82em;
    font-weight: 600;
    background-color: var(--bg-lighter-color);
    color: var(--text-muted);
  }

  .prio {
    font-variant-numeric: tabular-nums;
    color: var(--text-muted);
    font-size: 0.85em;
    min-width: 26px;
  }

  .name {
    font-weight: 600;
  }

  /* A disabled destination stays visible but must not read as active. */
  .name.off {
    opacity: 0.5;
    text-decoration: line-through;
  }

  .detail {
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .chip-flag.ok {
    color: var(--success-color);
    border-color: var(--success-color);
  }

  .chip-flag {
    font-size: 0.72em;
    padding: 1px 6px;
    border-radius: 8px;
    color: var(--warning-color);
    border: 1px solid var(--warning-color);
  }

  .editor-overlay {
    position: fixed;
    inset: 0;
    background-color: var(--backdrop-color-strong);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1400;
  }

  .editor {
    background-color: var(--bg-light-color);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    padding: 20px;
    width: min(560px, 92vw);
    max-height: 86vh;
    overflow-y: auto;
  }

  .editor h3 {
    margin: 0 0 14px;
    font-size: 1.05em;
  }

  .fld {
    display: block;
    margin-bottom: 10px;
  }

  .fld > span {
    display: block;
    font-size: 0.8em;
    color: var(--text-dimmed);
    margin-bottom: 3px;
  }

  .fld input,
  .fld select,
  .fld textarea {
    width: 100%;
    padding: 6px 8px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
  }

  /* PEM blocks are long, fixed-width and meant to be pasted, not typed. */
  .fld textarea {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 0.72em;
    resize: vertical;
    white-space: pre;
    overflow-wrap: normal;
    overflow-x: auto;
  }

  .fld-row {
    display: flex;
    gap: 10px;
    align-items: flex-end;
  }

  .fld-row .fld {
    flex: 1;
  }

  .fld.narrow {
    max-width: 110px;
  }

  .arrow {
    color: var(--text-muted);
    padding-bottom: 6px;
  }

  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .chip {
    padding: 3px 10px;
    font-size: 0.8em;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 12px;
    color: var(--text-muted);
    cursor: pointer;
  }

  .chip.on {
    color: var(--accent-color);
    border-color: var(--accent-border);
    background-color: var(--accent-subtle);
    font-weight: 600;
  }

  .toggle {
    display: flex;
    align-items: center;
    gap: 7px;
    font-size: 0.85em;
    margin-bottom: 6px;
    cursor: pointer;
  }

  .toggle input {
    width: auto;
  }

  /* ---------- sink editor: two panes instead of one long column ----------
     .editor is shared with the ROUTE editor below, so every rule here is
     scoped to .sink-editor. Widening .editor itself would have silently
     restyled a dialog nobody asked about. */
  .editor.sink-editor {
    width: min(1200px, 94vw);
    max-height: 88vh;
    padding: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }

  .sink-editor > h3 {
    flex: 0 0 auto;
    margin: 0;
    padding: var(--space-md) var(--space-xl);
    border-bottom: 1px solid var(--border-color);
    min-width: 0;
  }

  .sink-editor > h3 .sink-name {
    color: var(--text-muted);
    font-weight: 400;
  }

  /* The body scrolls; the dialog never does, so the title and the buttons
     stay put however long the form gets. */
  .sink-editor .editor-body {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
  }

  .sink-editor .pane {
    min-width: 0;
    padding: var(--space-lg) var(--space-xl);
  }

  .sink-editor .pane-template {
    border-top: 1px solid var(--border-color);
  }

  .sink-editor .grp-head {
    margin: var(--space-lg) 0 var(--space-sm);
    font-size: 0.72em;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--text-muted);
  }

  .sink-editor .grp-head:first-child {
    margin-top: 0;
  }

  /* A hint under one half of a row was pushing its partner's input upwards.
     Scoped, because the route editor's rows are tuned for flex-end. */
  .sink-editor .fld-row {
    align-items: flex-start;
  }

  /* Two panes only when each still gets a full measure. A PEM line at the
     textarea's size needs ~460px of content box; below that the split would
     make the form narrower than the single column it replaced, which is the
     opposite of the point. */
  @media (min-width: 1200px) {
    .sink-editor .editor-body {
      display: grid;
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
      overflow: hidden;
    }

    .sink-editor .pane {
      overflow-y: auto;
      min-height: 0;
    }

    .sink-editor .pane-template {
      border-top: none;
      border-left: 1px solid var(--border-color);
      background-color: var(--bg-color);
      display: flex;
      flex-direction: column;
    }
  }

  /* The template editor's own split is driven from here, so the breakpoint
     has one home rather than one per component. */
  .sink-editor .pane-template :global(.tpl) {
    padding-top: 0;
    border-top: none;
  }

  /* One wide column: the preview fits beside the fields. */
  @media (min-width: 980px) and (max-width: 1199px) {
    .sink-editor .pane-template :global(.tpl-main) {
      flex-direction: row;
      align-items: stretch;
      gap: var(--space-lg);
    }

    .sink-editor .pane-template :global(.tpl-fields) {
      flex: 1.15 1 0;
      min-width: 0;
    }

    .sink-editor .pane-template :global(.preview) {
      flex: 0.85 1 0;
      min-width: 0;
    }
  }

  /* Two panes: the template pane is tall and narrow, so the preview sits
     below the fields and takes whatever height is left. */
  @media (min-width: 1200px) {
    .sink-editor .pane-template :global(.tpl),
    .sink-editor .pane-template :global(.tpl-main) {
      flex: 1 1 auto;
      min-height: 0;
    }

    .sink-editor .pane-template :global(.preview) {
      flex: 1 1 auto;
      min-height: 160px;
      display: flex;
      flex-direction: column;
    }

    .sink-editor .pane-template :global(.preview-body) {
      flex: 1 1 auto;
      min-height: 0;
      overflow-y: auto;
    }

    .sink-editor .pane-template :global(.pv-text) {
      max-height: none;
    }
  }

  .sink-editor .editor-actions {
    flex: 0 0 auto;
    flex-wrap: wrap;
    margin-top: 0;
    padding: var(--space-sm) var(--space-xl);
    border-top: 1px solid var(--border-color);
    background-color: var(--bg-light-color);
  }

  .sink-editor .editor-actions .toggle {
    margin-bottom: 0;
  }

  .sink-editor .editor-actions .sep {
    width: 1px;
    align-self: stretch;
    margin: 0 var(--space-sm);
    background-color: var(--border-color);
  }

  .editor-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 16px;
  }

  .editor-actions .spacer {
    flex: 1;
  }

  .editor-actions .btn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
</style>
