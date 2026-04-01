<script>
  import { onMount } from 'svelte';
  import { _ } from 'svelte-i18n';
  import { GetOidDetails } from '../wailsjs/go/main/App';
  import { anonMode, anonymizeIp } from './utils/anonymize';

  export let trap;

  let isOpen = false;
  let mainTrapMessage = $_('common.working');
  let translatedVars = [];

  onMount(async () => {
    // 1. Find the main trap OID to get its details
    const trapOidVar = trap.variables.find(v => v.oid === 'snmpTrapOID.0' || v.oid.endsWith('.1.6.3.1.1.4.1.0'));
    let trapDescription = '';
    if (trapOidVar && trapOidVar.value) {
      const details = await GetOidDetails(trapOidVar.value);
      trapDescription = details.name; // Use the translated name as the base message
    } else if (trap.version === 'SNMPv1') {
      trapDescription = $_('traps.snmpv1Trap');
    } else {
      trapDescription = $_('traps.snmpv2v3Inform');
    }

    // 2. Find the 'messageString' variable, if it exists
    const messageStringVar = trap.variables.find(v => v.oid.endsWith('.1.4.1.8955.1.8.1.6')); // Assuming this is the OID for messageString
    if (messageStringVar && messageStringVar.value) {
      mainTrapMessage = `${trapDescription}: ${messageStringVar.value}`;
    } else {
      mainTrapMessage = trapDescription;
    }

    // 3. Translate all variable OIDs in the background for the details table
    translatedVars = await Promise.all(
      trap.variables.map(async (variable) => {
        const details = await GetOidDetails(variable.oid);
        return { ...variable, name: details.name };
      })
    );
  });

  function toggle() {
    isOpen = !isOpen;
  }
</script>

<div class="trap-item" class:open={isOpen}>
  <div class="trap-summary" on:click={toggle} on:keydown={(e) => e.key === 'Enter' && toggle()} role="button" tabindex="0">
    <span class="chevron">{isOpen ? '▼' : '►'}</span>
    <span class="timestamp">{trap.timestamp ? new Date(trap.timestamp).toLocaleTimeString() : new Date().toLocaleTimeString()}</span>
    <span class="source">{$anonMode ? anonymizeIp(trap.source) : trap.source}</span>
    <span class="version">{trap.version}</span>
    <span class="pdu-type-badge" class:inform={trap.pduType === 'Inform'}>{trap.pduType || 'Trap'}</span>
    <span class="main-oid" title={mainTrapMessage}>{mainTrapMessage}</span>
  </div>

  {#if isOpen}
    <div class="trap-details">
      <table>
        <thead>
          <tr>
            <th>{$_('common.name')}</th>
            <th>{$_('common.oid')}</th>
            <th>{$_('common.type')}</th>
            <th>{$_('common.value')}</th>
          </tr>
        </thead>
        <tbody>
          {#each translatedVars as v}
            <tr>
              <td class="name" title={v.name}>{v.name}</td>
              <td class="oid" title={v.oid}>{v.oid}</td>
              <td class="type">{v.type}</td>
              <td class="value" title={JSON.stringify(v.value)}>{JSON.stringify(v.value)}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<style>
  .trap-item {
    border-bottom: 1px solid var(--border-color);
  }
  .trap-summary {
    display: grid;
    grid-template-columns: 20px 80px 120px 80px 60px 1fr;
    align-items: center;
    gap: 10px;
    padding: 10px;
    cursor: pointer;
    transition: background-color 0.2s;
  }
  .trap-summary:hover {
    background-color: var(--bg-lighter-color);
  }
  .trap-item.open .trap-summary {
    background-color: var(--accent-color);
    color: white;
  }
  .chevron {
    font-size: 0.8em;
  }
  .pdu-type-badge {
    font-size: 0.75em;
    font-weight: 600;
    padding: 2px 6px;
    border-radius: 3px;
    text-align: center;
    background-color: var(--accent-color);
    color: white;
    text-transform: uppercase;
  }
  .pdu-type-badge.inform {
    background-color: var(--warning-color);
    color: var(--bg-color);
  }
  .timestamp, .source, .version, .main-oid {
    font-family: monospace;
    font-size: 0.9em;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .trap-details {
    padding: 15px;
    background-color: var(--bg-lighter-color);
  }
  table {
    width: 100%;
    border-collapse: collapse;
  }
  th, td {
    text-align: left;
    padding: 8px;
    border-bottom: 1px solid var(--border-color);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 200px; /* Adjust as needed */
  }
  th {
    font-size: 0.85em;
    color: var(--text-dimmed);
  }
  td {
    font-size: 0.9em;
  }
  .name {
    font-weight: 500;
    max-width: 150px;
  }
  .oid {
    font-family: monospace;
    color: var(--text-light);
    max-width: 200px;
  }
  .type {
    max-width: 100px;
  }
  .value {
    font-family: monospace;
    color: var(--text-light);
    max-width: 250px;
  }
</style>
