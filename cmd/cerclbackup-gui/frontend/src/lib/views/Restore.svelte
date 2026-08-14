<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../stores/i18n';
  import { ListFiles, Versions, Restore } from '../../../wailsjs/go/main/App';
  import type { protocol } from '../../../wailsjs/go/models';

  let files: protocol.ManifestEntry[] = [];
  let showAll = false;
  let selected: protocol.ManifestEntry | null = null;
  let versions: protocol.ManifestEntry[] = [];
  let selectedVersion: protocol.ManifestEntry | null = null;
  let outPath = '';
  let error = '';
  let result = '';

  async function load() {
    error = '';
    try {
      files = await ListFiles(showAll);
    } catch (e: any) {
      error = String(e);
    }
  }
  onMount(load);
  $: showAll, load();

  async function select(f: protocol.ManifestEntry) {
    selected = f;
    selectedVersion = f;
    outPath = f.path;
    try {
      versions = await Versions(f.path);
    } catch (e: any) {
      error = String(e);
    }
  }

  async function doRestore() {
    if (!selected) return;
    error = '';
    result = '';
    try {
      const r = await Restore('', outPath, '', selected.path, selectedVersion?.version ?? 0);
      result = `Restored to ${outPath} (integrity: ${r.IntegrityPassed ? 'OK' : 'FAILED'})`;
    } catch (e: any) {
      error = String(e);
    }
  }
</script>

<div class="restore">
  <h1>{$t('restore.title')}</h1>
  {#if error}<p class="error">{error}</p>{/if}
  {#if result}<p class="ok">{result}</p>{/if}

  <label class="checkbox"><input type="checkbox" bind:checked={showAll} /> {$t('restore.showAllVersions')}</label>

  <div class="layout">
    <div class="card list">
      <h3>{$t('restore.files')}</h3>
      <ul>
        {#each files as f}
          <li>
            <button class:active={selected?.file_id === f.file_id} on:click={() => select(f)}>
              {f.path} <span class="dim">v{f.version} · {f.size}b</span>
            </button>
          </li>
        {/each}
      </ul>
    </div>

    {#if selected}
      <div class="card detail">
        <h3>{$t('restore.versions')}</h3>
        <ul>
          {#each versions as v}
            <li>
              <button class:active={selectedVersion?.file_id === v.file_id} on:click={() => (selectedVersion = v)}>
                v{v.version} — {new Date(v.backed_at).toLocaleString()}
              </button>
            </li>
          {/each}
        </ul>
        <label>{$t('restore.restoreTo')}<input type="text" bind:value={outPath} /></label>
        <button on:click={doRestore}>{$t('restore.button')}</button>
      </div>
    {/if}
  </div>
</div>

<style>
  .layout { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; margin-top: 1rem; }
  .card { background: rgba(255,255,255,0.06); border-radius: 8px; padding: 1rem; text-align: left; }
  ul { list-style: none; padding: 0; margin: 0; max-height: 320px; overflow-y: auto; }
  li button { display: block; width: 100%; text-align: left; padding: 0.4rem; border-radius: 4px; cursor: pointer; background: transparent; border: none; color: white; font: inherit; }
  li button:hover { background: rgba(255,255,255,0.1); }
  li button.active { background: #3b82f6; }
  .dim { opacity: 0.6; font-size: 0.8rem; }
  label { display: flex; flex-direction: column; gap: 0.2rem; font-size: 0.9rem; margin: 0.8rem 0; }
  label.checkbox { flex-direction: row; align-items: center; gap: 0.5rem; }
  input { padding: 0.5rem; border-radius: 4px; border: 1px solid rgba(255,255,255,0.2); background: rgba(0,0,0,0.2); color: white; }
  button { padding: 0.5rem 1rem; border-radius: 4px; border: none; background: rgba(255,255,255,0.15); color: white; cursor: pointer; }
  button:hover { background: rgba(255,255,255,0.25); }
  .error { color: #f87171; }
  .ok { color: #4ade80; }
</style>
