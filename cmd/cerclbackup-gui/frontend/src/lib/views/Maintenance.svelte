<script lang="ts">
  import { t } from '../stores/i18n';
  import { Prune, Scrub, Rebalance, Audit, Export, Import, ManifestPull } from '../../../wailsjs/go/main/App';

  let error = '';
  let output = '';

  let keepAllDays = 7;
  let keepWeeklyDays = 30;
  let maxVersions = 10;
  let dryRun = true;

  let exportPath = '';
  let exportVersion = 0;
  let exportOut = '';

  let importPath = '';

  let pullAddr = '';
  let pullOut = '';

  let feedbackEl: HTMLElement;

  function show(v: unknown) {
    output = JSON.stringify(v, null, 2);
  }

  async function run(fn: () => Promise<unknown>) {
    error = '';
    output = '';
    try {
      show(await fn());
    } catch (e: any) {
      error = String(e);
    }
    feedbackEl?.scrollIntoView({ behavior: 'smooth', block: 'center' });
  }
</script>

<div class="maintenance">
  <h1>{$t('maintenance.title')}</h1>
  {#if error || output}
    <div class="feedback" bind:this={feedbackEl}>
      {#if error}<p class="error">{error}</p>{/if}
      {#if output}<pre>{output}</pre>{/if}
    </div>
  {/if}

  <div class="grid">
    <div class="card">
      <h3>{$t('maintenance.prune')}</h3>
      <label>Keep-all days<input type="number" bind:value={keepAllDays} /></label>
      <label>Keep-weekly days<input type="number" bind:value={keepWeeklyDays} /></label>
      <label>Max versions<input type="number" bind:value={maxVersions} /></label>
      <label class="checkbox"><input type="checkbox" bind:checked={dryRun} /> {$t('maintenance.prune.dryRun')}</label>
      <button on:click={() => run(() => Prune(keepAllDays, keepWeeklyDays, maxVersions, dryRun, ''))}>{$t('maintenance.prune.run')}</button>
    </div>

    <div class="card">
      <h3>{$t('maintenance.scrub')}</h3>
      <button on:click={() => run(() => Scrub())}>{$t('maintenance.scrub.run')}</button>
    </div>

    <div class="card">
      <h3>{$t('maintenance.rebalance')}</h3>
      <button on:click={() => run(() => Rebalance())}>{$t('maintenance.rebalance.run')}</button>
    </div>

    <div class="card">
      <h3>{$t('maintenance.audit')}</h3>
      <button on:click={() => run(() => Audit(''))}>{$t('maintenance.audit.run')}</button>
    </div>

    <div class="card">
      <h3>{$t('maintenance.export')}</h3>
      <label>File path<input type="text" bind:value={exportPath} /></label>
      <label>Version (0 = latest)<input type="number" bind:value={exportVersion} /></label>
      <label>Output path<input type="text" bind:value={exportOut} /></label>
      <button on:click={() => run(() => Export(exportPath, exportVersion, exportOut, ''))}>{$t('maintenance.export')}</button>
    </div>

    <div class="card">
      <h3>{$t('maintenance.import')}</h3>
      <label>.cbk file path<input type="text" bind:value={importPath} /></label>
      <button on:click={() => run(() => Import(importPath, ''))}>{$t('maintenance.import')}</button>
    </div>

    <div class="card">
      <h3>{$t('maintenance.manifestPull')}</h3>
      <label>Buddy address<input type="text" bind:value={pullAddr} /></label>
      <label>Output path<input type="text" bind:value={pullOut} /></label>
      <button on:click={() => run(() => ManifestPull(pullAddr, pullOut))}>{$t('maintenance.manifestPull')}</button>
    </div>
  </div>
</div>

<style>
  .grid { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 1rem; }
  .card { background: rgba(255,255,255,0.06); border-radius: 8px; padding: 1rem; text-align: left; display: flex; flex-direction: column; gap: 0.5rem; margin-bottom: 1rem; }
  label { display: flex; flex-direction: column; gap: 0.2rem; font-size: 0.85rem; }
  label.checkbox { flex-direction: row; align-items: center; gap: 0.5rem; }
  input { padding: 0.4rem; border-radius: 4px; border: 1px solid rgba(255,255,255,0.2); background: rgba(0,0,0,0.2); color: white; }
  button { padding: 0.5rem 1rem; border-radius: 4px; border: none; background: rgba(255,255,255,0.15); color: white; cursor: pointer; align-self: flex-start; }
  button:hover { background: rgba(255,255,255,0.25); }
  pre { max-height: 300px; overflow-y: auto; background: rgba(0,0,0,0.3); padding: 0.6rem; border-radius: 4px; font-size: 0.8rem; }
  .error { color: #f87171; }
  .feedback { background: rgba(255,255,255,0.06); border-radius: 8px; padding: 0.75rem 1rem; margin-bottom: 1rem; }
  .feedback pre { margin: 0; }
</style>
