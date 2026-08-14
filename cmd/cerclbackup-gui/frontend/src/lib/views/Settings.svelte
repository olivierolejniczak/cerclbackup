<script lang="ts">
  import { onMount } from 'svelte';
  import { t, lang, setLang } from '../stores/i18n';
  import { unlocked } from '../stores/session';
  import {
    CircleList, CircleAdd, CircleRemove,
    ConfigShow, ConfigInit,
    Lock, Passwd, SetPassword, DeletePassword,
    StartServe, StopServe, GetServeStatus,
    ShowPhrase,
  } from '../../../wailsjs/go/main/App';
  import type { circle, main } from '../../../wailsjs/go/models';

  let circles: circle.Circle[] = [];
  let newCircleName = '';
  let newCircleScheme = '3/2';
  let error = '';

  let configPath = '';
  let configText = '';

  let oldPassword = '';
  let newPassword = '';

  let serveStatus: main.ServeStatus = { Running: false, PeerID: '', Addrs: [] } as any;
  let servePort = 4001;

  let phrase = '';

  async function loadCircles() {
    try {
      circles = (await CircleList()) ?? [];
    } catch (e: any) {
      error = String(e);
    }
  }

  async function loadConfig() {
    try {
      const [cfg, path] = await ConfigShow();
      configPath = path;
      configText = JSON.stringify(cfg, null, 2);
    } catch (e: any) {
      error = String(e);
    }
  }

  async function loadServeStatus() {
    serveStatus = await GetServeStatus();
  }

  onMount(() => {
    loadCircles();
    loadConfig();
    loadServeStatus();
  });

  async function addCircle() {
    error = '';
    try {
      await CircleAdd(newCircleName, newCircleScheme);
      newCircleName = '';
      await loadCircles();
    } catch (e: any) {
      error = String(e);
    }
  }

  async function removeCircle(name: string) {
    try {
      await CircleRemove(name);
      await loadCircles();
    } catch (e: any) {
      error = String(e);
    }
  }

  async function initConfig() {
    try {
      await ConfigInit();
      await loadConfig();
    } catch (e: any) {
      error = String(e);
    }
  }

  async function changePassword() {
    error = '';
    try {
      await Passwd(oldPassword, newPassword);
      oldPassword = '';
      newPassword = '';
    } catch (e: any) {
      error = String(e);
    }
  }

  async function toggleServe() {
    error = '';
    try {
      if (serveStatus.Running) {
        await StopServe();
      } else {
        await StartServe(servePort, 0, '');
      }
      await loadServeStatus();
    } catch (e: any) {
      error = String(e);
    }
  }

  async function doLock() {
    await Lock();
    unlocked.set(false);
  }

  async function revealPhrase() {
    error = '';
    try {
      phrase = await ShowPhrase();
    } catch (e: any) {
      error = String(e);
    }
  }
</script>

<div class="settings">
  <h1>{$t('settings.title')}</h1>
  {#if error}<p class="error">{error}</p>{/if}

  <div class="grid">
    <div class="card">
      <h3>{$t('settings.language')}</h3>
      <select value={$lang} on:change={(e) => setLang((e.target as HTMLSelectElement).value)}>
        <option value="en">English</option>
        <option value="fr">Français</option>
      </select>
    </div>

    <div class="card">
      <h3>{$t('serve.title')}</h3>
      {#if serveStatus.Running}
        <p class="ok">{$t('serve.running', { peerId: serveStatus.PeerID })}</p>
      {:else}
        <label>Port<input type="number" bind:value={servePort} /></label>
      {/if}
      <button on:click={toggleServe}>{serveStatus.Running ? $t('serve.stop') : $t('serve.start')}</button>
    </div>

    <div class="card">
      <h3>{$t('setup.passwd.title')}</h3>
      <label>{$t('setup.passwd.old')}<input type="password" bind:value={oldPassword} /></label>
      <label>{$t('setup.passwd.new')}<input type="password" bind:value={newPassword} /></label>
      <button on:click={changePassword}>{$t('setup.passwd.button')}</button>
      <hr />
      <button on:click={() => SetPassword(newPassword || oldPassword)}>{$t('setup.savePassword')}</button>
      <button on:click={() => DeletePassword()}>{$t('setup.deletePassword')}</button>
      <hr />
      <button on:click={revealPhrase}>{$t('setup.showPhrase')}</button>
      {#if phrase}<p class="phrase">{phrase}</p>{/if}
    </div>

    <div class="card">
      <h3>{$t('settings.config')}</h3>
      <p class="dim">{configPath}</p>
      <pre>{configText}</pre>
      <button on:click={initConfig}>Init config file</button>
    </div>
  </div>

  <div class="card">
    <h3>{$t('settings.circles')}</h3>
    <table>
      <thead><tr><th>{$t('common.name')}</th><th>Scheme</th><th></th></tr></thead>
      <tbody>
        {#each circles as c}
          <tr>
            <td>{c.name}</td>
            <td>{c.scheme}</td>
            <td><button on:click={() => removeCircle(c.name)}>{$t('common.remove')}</button></td>
          </tr>
        {/each}
      </tbody>
    </table>
    <div class="row">
      <input type="text" placeholder={$t('common.name')} bind:value={newCircleName} />
      <input type="text" placeholder={$t('settings.circles.scheme')} bind:value={newCircleScheme} />
      <button on:click={addCircle}>{$t('settings.circles.add')}</button>
    </div>
  </div>

  <button class="lock" on:click={doLock}>{$t('settings.lock')}</button>
</div>

<style>
  .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; margin-bottom: 1rem; }
  .card { background: rgba(255,255,255,0.06); border-radius: 8px; padding: 1rem; text-align: left; display: flex; flex-direction: column; gap: 0.5rem; }
  label { display: flex; flex-direction: column; gap: 0.2rem; font-size: 0.85rem; }
  input, select { padding: 0.4rem; border-radius: 4px; border: 1px solid rgba(255,255,255,0.2); background: rgba(0,0,0,0.2); color: white; }
  button { padding: 0.5rem 1rem; border-radius: 4px; border: none; background: rgba(255,255,255,0.15); color: white; cursor: pointer; align-self: flex-start; }
  button:hover { background: rgba(255,255,255,0.25); }
  button.lock { margin-top: 1rem; background: #dc2626; }
  table { width: 100%; border-collapse: collapse; text-align: left; margin: 0.5rem 0; }
  th, td { padding: 0.4rem; border-bottom: 1px solid rgba(255,255,255,0.1); }
  .row { display: flex; gap: 0.5rem; }
  .row input { flex: 1; }
  pre { max-height: 200px; overflow-y: auto; background: rgba(0,0,0,0.3); padding: 0.5rem; border-radius: 4px; font-size: 0.75rem; }
  .dim { opacity: 0.6; font-size: 0.8rem; }
  .error { color: #f87171; }
  .ok { color: #4ade80; }
  .phrase { font-family: monospace; background: rgba(0,0,0,0.3); padding: 0.6rem; border-radius: 4px; }
  hr { border-color: rgba(255,255,255,0.1); width: 100%; }
</style>
