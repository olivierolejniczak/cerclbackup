<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../stores/i18n';
  import { Dashboard } from '../../../wailsjs/go/main/App';
  import type { api } from '../../../wailsjs/go/models';

  let data: api.DashboardResult | null = null;
  let error = '';
  let loading = true;

  async function load() {
    loading = true;
    error = '';
    try {
      data = await Dashboard('');
    } catch (e: any) {
      error = String(e);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  function fmtBytes(n: number): string {
    if (!n) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let i = 0;
    let v = n;
    while (v >= 1024 && i < units.length - 1) {
      v /= 1024;
      i++;
    }
    return `${v.toFixed(1)} ${units[i]}`;
  }
</script>

<div class="dashboard">
  <div class="header">
    <h1>{$t('dashboard.title')}</h1>
    <button on:click={load}>{$t('common.refresh')}</button>
  </div>

  {#if loading}
    <p>{$t('common.loading')}</p>
  {:else if error}
    <p class="error">{error}</p>
  {:else if data}
    <div class="status-banner status-{data.Status}">
      {$t(`dashboard.status.${data.Status}`)}
    </div>

    <div class="grid">
      <div class="card">
        <h3>{$t('nav.buddies')}</h3>
        <p class="big">{$t('dashboard.buddiesOnline', { up: data.BuddiesUp, total: data.BuddiesTotal })}</p>
      </div>
      <div class="card">
        <h3>{$t('dashboard.storage')}</h3>
        {#if data.Storage}
          <p>{$t('dashboard.uniquePaths')}: {data.Storage.UniquePaths}</p>
          <p>{$t('dashboard.totalVersions')}: {data.Storage.TotalVersions}</p>
          <p>{$t('dashboard.logicalBytes')}: {fmtBytes(data.Storage.LogicalBytes)}</p>
          <p>{$t('dashboard.diskBytes')}: {fmtBytes(data.Storage.DiskBytes)}</p>
        {/if}
      </div>
    </div>

    <h3>{$t('dashboard.checks')}</h3>
    <table>
      <tbody>
        {#each data.Doctor?.Checks ?? [] as c}
          <tr class:fail={!c.OK}>
            <td>{c.OK ? '✓' : '✗'}</td>
            <td>{c.Name}</td>
            <td>{c.Msg}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>

<style>
  .header { display: flex; justify-content: space-between; align-items: center; }
  .status-banner { padding: 1rem; border-radius: 8px; font-weight: bold; margin: 1rem 0; text-align: center; }
  .status-green { background: #16a34a; }
  .status-yellow { background: #ca8a04; }
  .status-red { background: #dc2626; }
  .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; margin-bottom: 1.5rem; }
  .card { background: rgba(255,255,255,0.06); border-radius: 8px; padding: 1rem; text-align: left; }
  .big { font-size: 1.4rem; font-weight: bold; }
  table { width: 100%; border-collapse: collapse; text-align: left; }
  td { padding: 0.4rem; border-bottom: 1px solid rgba(255,255,255,0.1); }
  tr.fail td { color: #f87171; }
  .error { color: #f87171; }
</style>
