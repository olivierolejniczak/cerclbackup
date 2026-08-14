import { writable } from 'svelte/store';

// Tracks whether the GUI has an unlocked keystore session. The App backend
// holds the real password in memory (see cmd/cerclbackup-gui/app.go); this
// store just mirrors that boolean state for routing between the setup
// wizard and the rest of the app.
export const unlocked = writable(false);
export const initialized = writable<boolean | null>(null); // null = not yet checked
