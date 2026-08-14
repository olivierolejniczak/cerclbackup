<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from './lib/stores/i18n';
  import { unlocked } from './lib/stores/session';
  import { IsUnlocked } from '../wailsjs/go/main/App';
  import { EventsOn } from '../wailsjs/runtime/runtime';

  import Setup from './lib/views/Setup.svelte';
  import Dashboard from './lib/views/Dashboard.svelte';
  import Backup from './lib/views/Backup.svelte';
  import Restore from './lib/views/Restore.svelte';
  import Buddies from './lib/views/Buddies.svelte';
  import Maintenance from './lib/views/Maintenance.svelte';
  import Settings from './lib/views/Settings.svelte';

  type Tab = 'dashboard' | 'backup' | 'restore' | 'buddies' | 'maintenance' | 'settings';
  let tab: Tab = 'dashboard';

  onMount(async () => {
    unlocked.set(await IsUnlocked());
    EventsOn('tray:backup-now', () => (tab = 'backup'));
  });

  const tabs: { id: Tab; labelKey: string }[] = [
    { id: 'dashboard', labelKey: 'nav.dashboard' },
    { id: 'backup', labelKey: 'nav.backup' },
    { id: 'restore', labelKey: 'nav.restore' },
    { id: 'buddies', labelKey: 'nav.buddies' },
    { id: 'maintenance', labelKey: 'nav.maintenance' },
    { id: 'settings', labelKey: 'nav.settings' },
  ];
</script>

{#if !$unlocked}
  <Setup />
{:else}
  <div class="shell">
    <nav>
      <div class="brand">{$t('app.title')}</div>
      {#each tabs as tb}
        <button class:active={tab === tb.id} on:click={() => (tab = tb.id)}>{$t(tb.labelKey)}</button>
      {/each}
    </nav>
    <main>
      {#if tab === 'dashboard'}
        <Dashboard />
      {:else if tab === 'backup'}
        <Backup />
      {:else if tab === 'restore'}
        <Restore />
      {:else if tab === 'buddies'}
        <Buddies />
      {:else if tab === 'maintenance'}
        <Maintenance />
      {:else if tab === 'settings'}
        <Settings />
      {/if}
    </main>
  </div>
{/if}

<style>
  .shell {
    display: flex;
    height: 100vh;
  }
  nav {
    width: 190px;
    flex-shrink: 0;
    background: rgba(0, 0, 0, 0.25);
    display: flex;
    flex-direction: column;
    padding: 1rem 0.6rem;
    gap: 0.3rem;
  }
  .brand {
    font-weight: bold;
    font-size: 1.1rem;
    padding: 0.6rem;
    margin-bottom: 0.5rem;
  }
  nav button {
    text-align: left;
    background: transparent;
    border: none;
    color: white;
    padding: 0.6rem 0.8rem;
    border-radius: 6px;
    cursor: pointer;
    font-size: 0.95rem;
  }
  nav button:hover {
    background: rgba(255, 255, 255, 0.08);
  }
  nav button.active {
    background: #3b82f6;
  }
  main {
    flex: 1;
    overflow-y: auto;
    padding: 1.5rem 2rem;
    text-align: left;
  }
</style>
