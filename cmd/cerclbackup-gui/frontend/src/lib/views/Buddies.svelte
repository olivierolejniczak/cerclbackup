<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../stores/i18n';
  import { BuddyStatus, BuddyRemove, Invite, Join } from '../../../wailsjs/go/main/App';
  import type { api } from '../../../wailsjs/go/models';

  let statuses: api.BuddyStatusEntry[] = [];
  let error = '';
  let loading = false;

  let invite: api.InviteResult | null = null;
  let servePort = 4001;

  let joinAddr = '';
  let joinWords = '';
  let joinName = '';
  let joinResult = '';

  async function load() {
    loading = true;
    error = '';
    try {
      statuses = (await BuddyStatus(5)) ?? [];
    } catch (e: any) {
      error = String(e);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  async function remove(peerId: string) {
    try {
      await BuddyRemove(peerId, false);
      await load();
    } catch (e: any) {
      error = String(e);
    }
  }

  async function generateInvite() {
    error = '';
    try {
      invite = await Invite(servePort);
    } catch (e: any) {
      error = String(e);
    }
  }

  async function doJoin() {
    error = '';
    joinResult = '';
    try {
      const peerId = await Join(joinAddr, joinWords, joinName, servePort);
      joinResult = `Joined ${peerId}`;
      await load();
    } catch (e: any) {
      error = String(e);
    }
  }

  let copied = false;
  let copiedTimer: ReturnType<typeof setTimeout>;

  function copy(text: string) {
    navigator.clipboard?.writeText(text);
    copied = true;
    clearTimeout(copiedTimer);
    copiedTimer = setTimeout(() => (copied = false), 1500);
  }
</script>

<div class="buddies">
  <div class="header">
    <h1>{$t('buddies.title')}</h1>
    <button on:click={load}>{$t('common.refresh')}</button>
  </div>
  {#if error}<p class="error">{error}</p>{/if}

  {#if loading}
    <p>{$t('common.loading')}</p>
  {:else}
    <table>
      <thead><tr><th>{$t('common.name')}</th><th>{$t('buddies.status')}</th><th>{$t('buddies.latency')}</th><th></th></tr></thead>
      <tbody>
        {#each statuses as s}
          <tr>
            <td>{s.Entry?.friendly_name || s.Entry?.peer_id}</td>
            <td class:online={s.Online} class:offline={!s.Online}>{s.Online ? $t('buddies.online') : $t('buddies.offline')}</td>
            <td>{s.Online ? `${Math.round(s.Latency / 1e6)}ms` : '-'}</td>
            <td><button on:click={() => remove(s.Entry!.peer_id)}>{$t('common.remove')}</button></td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}

  <div class="grid">
    <div class="card">
      <h3>{$t('buddies.invite.title')}</h3>
      <label>Serve port<input type="number" bind:value={servePort} /></label>
      <button on:click={generateInvite}>{$t('buddies.invite.generate')}</button>
      {#if invite}
        <p><strong>{$t('buddies.invite.words')}:</strong> {invite.Words}</p>
        <p><strong>{$t('buddies.invite.joinAddr')}:</strong> {invite.JoinAddr} <button on:click={() => copy(invite!.JoinAddr)}>{copied ? $t('common.copied') : $t('common.copy')}</button></p>
      {/if}
    </div>

    <div class="card">
      <h3>{$t('buddies.join.title')}</h3>
      <label>{$t('buddies.join.addr')}<input type="text" bind:value={joinAddr} /></label>
      <label>{$t('buddies.join.words')}<input type="text" bind:value={joinWords} /></label>
      <label>{$t('buddies.join.name')}<input type="text" bind:value={joinName} /></label>
      <label>Serve port<input type="number" bind:value={servePort} /></label>
      <button on:click={doJoin}>{$t('buddies.join.button')}</button>
      {#if joinResult}<p class="ok">{joinResult}</p>{/if}
    </div>
  </div>
</div>

<style>
  .header { display: flex; justify-content: space-between; align-items: center; }
  table { width: 100%; border-collapse: collapse; text-align: left; margin: 1rem 0; }
  th, td { padding: 0.5rem; border-bottom: 1px solid rgba(255,255,255,0.1); }
  .online { color: #4ade80; }
  .offline { color: #f87171; }
  .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; min-width: 0; }
  .card { background: rgba(255,255,255,0.06); border-radius: 8px; padding: 1rem; text-align: left; display: flex; flex-direction: column; gap: 0.6rem; min-width: 0; }
  .card p { overflow-wrap: anywhere; }
  label { display: flex; flex-direction: column; gap: 0.2rem; font-size: 0.9rem; }
  input { padding: 0.5rem; border-radius: 4px; border: 1px solid rgba(255,255,255,0.2); background: rgba(0,0,0,0.2); color: white; width: 100%; box-sizing: border-box; }
  button { padding: 0.5rem 1rem; border-radius: 4px; border: none; background: rgba(255,255,255,0.15); color: white; cursor: pointer; align-self: flex-start; }
  button:hover { background: rgba(255,255,255,0.25); }
  .error { color: #f87171; }
  .ok { color: #4ade80; }

  @media (max-width: 900px) {
    .grid { grid-template-columns: 1fr; }
  }
</style>
