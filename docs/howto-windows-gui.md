# How-To: CerclBackup on Windows, GUI Only

This is the guide for a normal Windows user setting up CerclBackup with
**zero command-line use**. Everything below is a click in the desktop app.
If you're comfortable with PowerShell and want the full picture (all
subcommands, WAN pairing internals, fault-tolerance drills), see
[`howto-3-buddy-wan-backup.md`](howto-3-buddy-wan-backup.md) and
[`docs/testplan/`](testplan/README.md) instead — this guide deliberately
leaves that detail out.

## What you need before you start

- Windows 11 (or 10).
- **3 friends running CerclBackup** (on Windows or Linux — it doesn't
  matter which). CerclBackup splits your files into encrypted pieces and
  spreads them across your circle, so it needs at least 3 buddies before
  it will back up anything — see [Why 3 buddies?](#why-3-buddies) below.
- Each of you needs a way to send one one short link and one 12-word
  phrase to the other, once, when you first connect (chat app, email,
  whatever you'd use normally — see [Sharing the invite](#sharing-the-invite-safely)).

## 1. Install

1. Download `CerclBackup-X.Y.Z.msi` from the
   [Releases page](../../releases).
2. Double-click it and click through the installer. No options to configure.
3. CerclBackup starts automatically (hidden, in the system tray) and will
   do so every time you log in — you won't need to launch it manually
   after this.

Look for the CerclBackup icon in your system tray (bottom-right, next to
the clock — click the little `^` to show hidden icons if you don't see it
right away).

## 2. First-time setup

Right-click the tray icon → **Show window**. The very first time, you'll
see a welcome screen instead of a dashboard:

![Windows first-run setup](runbook-3-buddy-exchange/07-windows-gui-setup.png)

Click **First-time setup**, then:

1. Choose a password. This encrypts everything CerclBackup stores on your
   machine — it never leaves your computer, and CerclBackup can't recover
   it for you.
2. You'll be shown a **12-word recovery phrase**. Write it down on paper
   and keep it somewhere safe (not a screenshot on the same computer). If
   you ever lose your password *and* your computer, this phrase is the
   only way to get your key back.
3. Click through — the app creates your identity and a default circle,
   then takes you straight to the Dashboard.

That's the entire setup. No file paths, no ports, no config files.

## 3. Connect your 3 buddies

Go to the **Buddies** tab.

**To invite someone:**
1. Click **Generate invite**.
2. Windows Firewall may ask to allow CerclBackup on your network — click
   **Allow** (this is what lets your buddy's computer actually reach
   yours).
3. You'll get a short address and a 12-word phrase, each with a **Copy**
   button.

**To join someone who invited you:**
1. Paste the address and phrase they sent you into the **Join a buddy**
   fields, give them a name you'll recognize, and click **Join**.

Do this pairwise with each of your 3 buddies (both directions — you invite
them, or they invite you; either way works). Once done, the Buddies tab
shows all of them with a live online/offline status.

### Sharing the invite safely

Send the address and the words through **two different channels** if you
can (e.g. the address by email, the words by text/chat) — that way, someone
who only intercepts one of the two can't complete a join. This isn't
mandatory, just good practice, since either piece alone is useless.

### Why 3 buddies?

CerclBackup splits each file into pieces (some data, some "parity" for
recovery) and never falls back to a simple 1-copy mirror — that's what
lets you get your files back even if one buddy is offline or their
computer breaks. With the default settings that means **3 buddies
minimum**; you'll get a clear error if you try to back up with fewer.

## 4. Back up something

Go to the **Backup** tab, pick a folder, and click **Run backup now**.
Progress streams live in the window. When it's done, you'll see a summary
of what was backed up and to which buddies.

For "set it and forget it," toggle **Watch this folder** instead — every
change you make gets backed up automatically in the background, even with
the window closed (closing the window just hides it to the tray; the app
keeps running).

You can also right-click the tray icon → **Backup now** at any time, which
opens straight to the Backup tab.

## 5. Restore a file

Go to the **Restore** tab, find the file (there's a version history if
you've backed it up more than once), pick a destination, and click
**Restore**. CerclBackup verifies the restored file's integrity
automatically and tells you if anything doesn't check out.

## 6. Everyday use

Day to day, there's nothing to do — CerclBackup runs quietly in the tray,
watches your chosen folders, and keeps your buddies' addresses fresh in
the background (if a buddy's internet address changes, the app finds them
again automatically within about 10 minutes, no restart required).

Check in occasionally via the **Dashboard** tab, which shows one glance at:
- overall health (green/yellow/red),
- which buddies are currently online,
- how much you've backed up.

**Tray menu, right-click:**

| Item | What it does |
|---|---|
| Show window | Opens the dashboard |
| Backup now | Opens straight to the Backup tab |
| Quit | Actually exits CerclBackup (closing the window just hides it) |

## Language

Settings → Language toggles the whole app between English and Français
instantly, including the setup screen if you log out and back in.

## If something looks wrong

The **Maintenance** tab has one-click buttons for the things you'd
otherwise need the command line for: verify shard integrity (**Audit**,
**Scrub**), rebalance storage after a buddy leaves, and export/import a
portable backup file. Each button reports success or a specific error
message right there — no terminal needed.
