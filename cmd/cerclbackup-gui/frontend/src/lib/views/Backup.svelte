<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t } from '../stores/i18n';
  import { Backup, StartWatch, StopWatch, IsWatching } from '../../../wailsjs/go/main/App';
  import { EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime';

  let paths: string[] = [''];
  let buddies = 5;
  let exclude = '.git,node_modules,*.tmp,*.swp';
  let uploadKbps = 0;
  let autoPrune = false;
  let running = false;
  let progress: string[] = [];
  let error = '';
  let result: unknown = null;

  let watchDir = '';
  let watching = false;
  let debounceSeconds = 3;

  let feedbackEl: HTMLElement;

  function addPath() {
    paths = [...paths, ''];
  }
  function removePath(i: number) {
    paths = paths.filter((_, idx) => idx !== i);
  }

  function onProgress(line: string) {
    progress = [...progress, line].slice(-200);
  }
  function onWatchEvent(ev: any) {
    const line = ev.Err ? `error (${ev.Path}): ${ev.Err}` : ev.Progress;
    progress = [...progress, line].slice(-200);
  }

  onMount(async () => {
    EventsOn('backup:progress', onProgress);
    EventsOn('watch:event', onWatchEvent);
    watching = await IsWatching();
  });
  onDestroy(() => {
    EventsOff('backup:progress');
    EventsOff('watch:event');
  });

  async function run() {
    error = '';
    result = null;
    running = true;
    progress = [];
    try {
      result = await Backup('', buddies, exclude, uploadKbps, autoPrune, paths.filter((p) => p.trim()));
    } catch (e: any) {
      error = String(e);
    } finally {
      running = false;
      feedbackEl?.scrollIntoView({ behavior: 'smooth', block: 'center' });
    }
  }

  async function toggleWatch() {
    error = '';
    try {
      if (watching) {
        await StopWatch();
        watching = false;
      } else {
        await StartWatch(watchDir, '', buddies, debounceSeconds, exclude, uploadKbps, autoPrune);
        watching = true;
      }
    } catch (e: any) {
      error = String(e);
    }
  }
</script>

<div class="backup">
  <h1>{$t('backup.title')}</h1>
  {#if error || result || progress.length > 0}
    <div class="feedback" bind:this={feedbackEl}>
      {#if error}<p class="error">{error}</p>{/if}
      {#if progress.length > 0}
        <h3>{$t('backup.progress')}</h3>
        <pre>{progress.join('\n')}</pre>
      {/if}
      {#if result}
        <h3>{$t('backup.result')}</h3>
        <pre>{JSON.stringify(result, null, 2)}</pre>
      {/if}
    </div>
  {/if}

  <div class="card">
    <div class="label-only">{$t('backup.srcPaths')}</div>
    {#each paths as _, i}
      <div class="row">
        <input type="text" bind:value={paths[i]} placeholder="/path/to/file/or/folder" />
        {#if paths.length > 1}<button on:click={() => removePath(i)}>-</button>{/if}
      </div>
    {/each}
    <button on:click={addPath}>{$t('backup.addPath')}</button>

    <label>{$t('backup.buddies')}<input type="number" min="1" bind:value={buddies} /></label>
    <label>{$t('backup.exclude')}<input type="text" bind:value={exclude} /></label>
    <label>{$t('backup.uploadKbps')}<input type="number" min="0" bind:value={uploadKbps} /></label>
    <label class="checkbox"><input type="checkbox" bind:checked={autoPrune} /> {$t('backup.autoPrune')}</label>

    <button disabled={running} on:click={run}>{$t('backup.run')}</button>
  </div>

  <div class="card">
    <h3>{$t('backup.watch.title')}</h3>
    <label>{$t('backup.srcPaths')}<input type="text" bind:value={watchDir} disabled={watching} placeholder="/path/to/folder" /></label>
    <label>Debounce (s)<input type="number" min="1" bind:value={debounceSeconds} disabled={watching} /></label>
    <button on:click={toggleWatch}>{watching ? $t('backup.watch.stop') : $t('backup.watch.start')}</button>
    {#if watching}<p>{$t('backup.watch.running', { dir: watchDir })}</p>{/if}
  </div>

</div>

<style>
  .card { background: rgba(255,255,255,0.06); border-radius: 8px; padding: 1rem; margin-bottom: 1rem; display: flex; flex-direction: column; gap: 0.6rem; text-align: left; }
  .row { display: flex; gap: 0.5rem; }
  .row input { flex: 1; }
  label, .label-only { display: flex; flex-direction: column; gap: 0.2rem; font-size: 0.9rem; }
  label.checkbox { flex-direction: row; align-items: center; gap: 0.5rem; }
  input { padding: 0.5rem; border-radius: 4px; border: 1px solid rgba(255,255,255,0.2); background: rgba(0,0,0,0.2); color: white; }
  button { padding: 0.5rem 1rem; border-radius: 4px; border: none; background: rgba(255,255,255,0.15); color: white; cursor: pointer; align-self: flex-start; }
  button:hover { background: rgba(255,255,255,0.25); }
  pre { max-height: 260px; overflow-y: auto; background: rgba(0,0,0,0.3); padding: 0.6rem; border-radius: 4px; font-size: 0.8rem; white-space: pre-wrap; }
  .error { color: #f87171; }
  .feedback { background: rgba(255,255,255,0.06); border-radius: 8px; padding: 0.75rem 1rem; margin-bottom: 1rem; text-align: left; }
  .feedback pre { margin: 0; }
</style>
