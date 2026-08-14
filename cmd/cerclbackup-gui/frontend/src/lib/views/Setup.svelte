<script lang="ts">
  import { t } from '../stores/i18n';
  import { unlocked, initialized } from '../stores/session';
  import { Init, Unlock, Recover, IsInitialized } from '../../../wailsjs/go/main/App';

  let mode: 'unlock' | 'init' | 'recover' = 'unlock';
  let password = '';
  let confirmPassword = '';
  let storeDir = '';
  let phrase = '';
  let error = '';
  let busy = false;
  let recoveryPhrase = '';

  async function refreshInitialized() {
    initialized.set(await IsInitialized());
    mode = $initialized ? 'unlock' : 'init';
  }
  refreshInitialized();

  async function doUnlock() {
    error = '';
    busy = true;
    try {
      await Unlock(password);
      unlocked.set(true);
    } catch (e: any) {
      error = String(e);
    } finally {
      busy = false;
    }
  }

  async function doInit() {
    error = '';
    if (password.length < 8) {
      error = 'Password must be at least 8 characters.';
      return;
    }
    if (password !== confirmPassword) {
      error = 'Passwords do not match.';
      return;
    }
    busy = true;
    try {
      const res = await Init({ Password: password, StoreDir: storeDir, Force: false });
      recoveryPhrase = res.RecoveryPhrase;
      unlocked.set(true);
    } catch (e: any) {
      error = String(e);
    } finally {
      busy = false;
    }
  }

  async function doRecover() {
    error = '';
    busy = true;
    try {
      await Recover(phrase, password);
      unlocked.set(true);
    } catch (e: any) {
      error = String(e);
    } finally {
      busy = false;
    }
  }
</script>

<div class="setup">
  <h1>{$t('setup.title')}</h1>
  <p class="subtitle">{$t('setup.subtitle')}</p>

  {#if recoveryPhrase}
    <div class="card">
      <h2>{$t('setup.showPhrase')}</h2>
      <p class="phrase">{recoveryPhrase}</p>
      <p class="warn">Write this down and store it somewhere safe — it's the only way to recover your identity if you lose this device.</p>
      <button on:click={() => (recoveryPhrase = '')}>{$t('common.close')}</button>
    </div>
  {:else}
    <div class="tabs">
      {#if $initialized}
        <button class:active={mode === 'unlock'} on:click={() => (mode = 'unlock')}>{$t('setup.unlock.title')}</button>
      {/if}
      <button class:active={mode === 'init'} on:click={() => (mode = 'init')}>{$t('setup.init.title')}</button>
      <button class:active={mode === 'recover'} on:click={() => (mode = 'recover')}>{$t('setup.recover.title')}</button>
    </div>

    <div class="card">
      {#if mode === 'unlock'}
        <label>{$t('common.password')}<input type="password" bind:value={password} on:keydown={(e) => e.key === 'Enter' && doUnlock()} /></label>
        <button disabled={busy} on:click={doUnlock}>{$t('setup.unlock.button')}</button>
      {:else if mode === 'init'}
        <label>{$t('common.password')}<input type="password" bind:value={password} /></label>
        <label>Confirm password<input type="password" bind:value={confirmPassword} /></label>
        <label>{$t('setup.init.storeDir')}<input type="text" bind:value={storeDir} placeholder="(default location)" /></label>
        <button disabled={busy} on:click={doInit}>{$t('setup.init.button')}</button>
      {:else}
        <label>{$t('setup.recover.phrase')}<textarea bind:value={phrase} rows="2"></textarea></label>
        <label>{$t('common.password')}<input type="password" bind:value={password} /></label>
        <button disabled={busy} on:click={doRecover}>{$t('setup.recover.button')}</button>
      {/if}
      {#if error}<p class="error">{error}</p>{/if}
    </div>
  {/if}
</div>

<style>
  .setup {
    max-width: 480px;
    margin: 4rem auto;
    text-align: left;
  }
  h1, .subtitle { text-align: center; }
  .subtitle { opacity: 0.7; margin-bottom: 2rem; }
  .tabs { display: flex; gap: 0.5rem; margin-bottom: 1rem; justify-content: center; }
  .tabs button { flex: 1; }
  .tabs button.active { background: #3b82f6; color: white; }
  .card {
    background: rgba(255,255,255,0.06);
    border-radius: 8px;
    padding: 1.5rem;
    display: flex;
    flex-direction: column;
    gap: 0.9rem;
  }
  label { display: flex; flex-direction: column; gap: 0.3rem; font-size: 0.9rem; }
  input, textarea { padding: 0.5rem; border-radius: 4px; border: 1px solid rgba(255,255,255,0.2); background: rgba(0,0,0,0.2); color: white; font-family: inherit; }
  button { padding: 0.6rem; border-radius: 4px; border: none; background: rgba(255,255,255,0.15); color: white; cursor: pointer; }
  button:hover { background: rgba(255,255,255,0.25); }
  .error { color: #f87171; }
  .warn { color: #fbbf24; font-size: 0.85rem; }
  .phrase { font-family: monospace; font-size: 1.1rem; background: rgba(0,0,0,0.3); padding: 1rem; border-radius: 4px; text-align: center; }
</style>
