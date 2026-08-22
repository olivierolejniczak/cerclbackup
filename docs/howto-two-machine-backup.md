# How-To: Set Up a Backup Between Two Machines (Newbie Guide)

This walks through pairing CerclBackup between two real computers over a
LAN — Machine A (this computer) and Machine B (`192.168.142.136`) — and
running an actual backup/restore. Every command below was run for real
while writing this guide; the output shown is genuine, not illustrative.

## Before you start: the 3-buddy rule

CerclBackup **refuses to back up to just 1 buddy**. It uses Reed-Solomon
erasure coding (data split into shards + parity shards spread across
buddies), and by design there's no "just mirror to my one friend" fallback
— you need **at least 3 buddies** in your circle before `backup` will run
at all:

```
error: protocol: at least 3 buddies/devices are required (no 1/1 mirror fallback)
```

So a real 2-machine setup (just you and one friend) can pair fine, but
can't back anything up until a 3rd device joins. In production, that 3rd
device should be another real machine (a second friend, another one of
your own devices, etc.).

**For this guide**, since we only have two real machines available, steps
7–8 add two extra lightweight CerclBackup instances running locally on
Machine A purely to satisfy the minimum. They are clearly marked
"lab-only" — they are not real redundancy (a disk failure on Machine A
would take all of them out along with Machine A's own data). Skip steps
7–8 and use a real 3rd machine's IP instead, if you have one.

## Prerequisites

- CerclBackup binary built or installed on both machines
  (`go build ./cmd/cerclbackup` from this repo, or a release binary).
- Both machines reachable over the network (same LAN here; a WAN/NAT
  setup needs port forwarding or relay — out of scope for this guide).
- A TCP port open between them (`7001` used throughout below).

## 1. Copy the binary to both machines

```
$ cp ./cerclbackup ~/cerclbackup-demo/cerclbackup                      # Machine A
$ scp ./cerclbackup root@192.168.142.136:~/cerclbackup-demo/cerclbackup # Machine B
```

## 2. Initialize the keystore on Machine A

Each machine gets its own identity (peer ID + recovery phrase) and its own
shard store directory:

```
$ export CERCLBACKUP_CONFIG_DIR=~/cerclbackup-demo/cfg
$ ./cerclbackup init --password 'DemoPassA123!' --no-prompt --store ~/cerclbackup-demo/store

╔══════════════════════════════════════════════════════════╗
║           CerclBackup — First-Run Setup                  ║
╚══════════════════════════════════════════════════════════╝

Peer ID : 12D3KooWBvCRihtKdmJvfGPxcFA1RbHexo92UFrRuKCbadXpyrbt

Recovery phrase (write this down — it restores your identity):

  bonus system matter picnic negative cool column budget sun phrase material skull

Setup complete.
Keystore : /root/cerclbackup-demo/cfg/keystore.enc
Store    : /root/cerclbackup-demo/store
```

**Write down the recovery phrase somewhere safe.** It's the only way to
recover your identity if you lose the keystore file.

`--no-prompt --password ...` is for scripting; without it, `init` will
interactively prompt you to type and confirm a password instead.

`CERCLBACKUP_CONFIG_DIR` is optional — leave it unset to use the OS
default config directory (`~/.config/cerclbackup` on Linux). We set it
explicitly here only so this demo doesn't collide with any keystore you
already have.

## 3. Initialize the keystore on Machine B

Same command, run on the other machine, with its own password:

```
root@192.168.142.136$ export CERCLBACKUP_CONFIG_DIR=~/cerclbackup-demo/cfg
root@192.168.142.136$ ./cerclbackup init --password 'DemoPassB123!' --no-prompt --store ~/cerclbackup-demo/store

Peer ID : 12D3KooWHeYmYX5yF7qH1qFmFLUk7Z3ZbEN4jHrEtp8KKmt2Jkig
...
```

## 4. Start the P2P daemon on both machines

`serve` is the long-running daemon that listens for connections from
buddies and handles incoming backup/restore traffic. Run it in the
background on each machine:

```
$ export CERCLBACKUP_CONFIG_DIR=~/cerclbackup-demo/cfg
$ nohup ./cerclbackup serve --password 'DemoPassA123!' --port 7001 \
    --health-addr 127.0.0.1:17001 > ~/cerclbackup-demo/serve.log 2>&1 &
```

Check the log to confirm it started and see its addresses:

```
$ cat ~/cerclbackup-demo/serve.log
CerclBackup daemon running
Peer ID : 12D3KooWBvCRihtKdmJvfGPxcFA1RbHexo92UFrRuKCbadXpyrbt
Address : /ip4/192.168.142.146/tcp/7001/p2p/12D3KooWBvCRihtKdmJvfGPxcFA1RbHexo92UFrRuKCbadXpyrbt
...
```

Repeat identically on Machine B (its own password, `--port 7001` again —
each machine has its own address space, so reusing the port number is
fine).

**Gotcha:** if a machine has Docker installed, you'll also see addresses
like `/ip4/172.17.0.1/...` in this list — that's Docker's internal bridge
network, not reachable from another machine. Ignore those; the invite
step below already knows to prefer your real LAN/outbound address.

## 5. Generate an invite on Machine B

```
root@192.168.142.136$ ./cerclbackup invite --password 'DemoPassB123!' --port 7001

── Step 1 — Send this command to your buddy (chat, email, etc.) ────────
  cerclbackup join --addr /ip4/192.168.142.136/tcp/7001/p2p/12D3KooWHeYmYX5yF7qH1qFmFLUk7Z3ZbEN4jHrEtp8KKmt2Jkig --words "city city crew decrease general polar purity ivory swear result rib absorb" --password <their-pw>

── Step 2 — Verify by voice or in person (prevents interception) ───────
  Tell your buddy your last 3 words: result rib absorb

Code expires in 24 hours.
```

Send the `join` command (Step 1) to whoever is setting up Machine A —
over chat, email, whatever. Separately, tell them the **last 3 words**
**by voice or in person** (phone call, SMS, in-person). This is what
proves the invite really came from you and not from someone who
intercepted your chat/email.

## 6. Join from Machine A

Machine A's own `--port` here is the port **its own** `serve` daemon
listens on (7001), so Machine B can reach back:

```
$ ./cerclbackup join --addr /ip4/192.168.142.136/tcp/7001/p2p/12D3KooWHeYmYX5yF7qH1qFmFLUk7Z3ZbEN4jHrEtp8KKmt2Jkig \
    --words "city city crew decrease general polar purity ivory swear result rib absorb" \
    --password 'DemoPassA123!' --port 7001 --name BuddyB

Paired with buddy 12D3KooWHeYmYX5yF7qH1qFmFLUk7Z3ZbEN4jHrEtp8KKmt2Jkig
```

Machine A and Machine B are now buddies. Confirm with:

```
$ ./cerclbackup buddy list --password 'DemoPassA123!'
Friendly Name         Peer ID
-------------         -------
BuddyB                12D3KooWHeYmYX5yF7qH1qFmFLUk7Z3ZbEN4jHrEtp8KKmt2Jkig
```

**If you have a real 3rd machine, repeat steps 3–6 with it and skip to
step 9.** The rest below is only for filling the minimum-3 requirement
when you don't have a 3rd real machine handy.

## 7. (Lab-only) Add two throwaway buddies to reach the minimum of 3

These run on Machine A itself, each with their own identity/store/port —
useful for testing the pairing/backup flow, **not** for real backups:

```
$ export CERCLBACKUP_CONFIG_DIR=~/cerclbackup-demo/labpeer1/cfg
$ ./cerclbackup init --password 'labpeer1Pass123!' --no-prompt --store ~/cerclbackup-demo/labpeer1/store
$ nohup ./cerclbackup serve --password 'labpeer1Pass123!' --port 7002 \
    --health-addr 127.0.0.1:17002 > ~/cerclbackup-demo/labpeer1/serve.log 2>&1 &
```

Repeat for `labpeer2` on port `7003`. Then pair each with Machine A the
same way as steps 5–6, generating a fresh invite from Machine A each time
(invite codes are single-use) and joining from each lab peer's own config:

```
$ export CERCLBACKUP_CONFIG_DIR=~/cerclbackup-demo/cfg
$ ./cerclbackup invite --password 'DemoPassA123!' --port 7001
...
$ export CERCLBACKUP_CONFIG_DIR=~/cerclbackup-demo/labpeer1/cfg
$ ./cerclbackup join --addr /ip4/127.0.0.1/tcp/7001/p2p/<Machine A peer ID> \
    --words "<the words>" --password 'labpeer1Pass123!' --port 7002
Paired with buddy <Machine A peer ID>
```

**Gotcha:** if a lab peer's `serve` daemon was already running when you
ran `join` for it, that daemon won't see the new buddy until restarted —
`join` only updates the on-disk registry; a running daemon doesn't
hot-reload it. Kill and restart the daemon after joining:

```
$ ps aux | grep "serve --password labpeer1"
$ kill -9 <pid>       # kill by exact PID — avoid `pkill -f serve`, it can
                       # match and kill your own shell if the pattern
                       # appears in your terminal's own command history
$ nohup ./cerclbackup serve --password 'labpeer1Pass123!' --port 7002 \
    --health-addr 127.0.0.1:17002 > ~/cerclbackup-demo/labpeer1/serve.log 2>&1 &
```

## 8. Confirm the buddy list has 3 members

```
$ ./cerclbackup buddy list --password 'DemoPassA123!'
Friendly Name         Peer ID
-------------         -------
BuddyB                12D3KooWHeYmYX5yF7qH1qFmFLUk7Z3ZbEN4jHrEtp8KKmt2Jkig
(unnamed)              12D3KooWHtsd74aBDAfXWhtt2M6KFExKvgdCvmtNg48qpw3wYbu5
(unnamed)              12D3KooWRUpE1tZjX4wGRmdrpNtxyRAeD3YFqS18AhjqypCoa8p6
```

## 9. Run a backup

```
$ ./cerclbackup backup --src ~/cerclbackup-demo/demofile.txt \
    --store ~/cerclbackup-demo/store --password 'DemoPassA123!' --buddies 3

[backup] RS scheme: 2 data / 1 parity (tolerates 1 buddy failures)
[backup] chunking "/root/cerclbackup-demo/demofile.txt" ...
[backup] done demofile.txt — file-id: e112e6eb-817c-4866-95bc-ea9d7da69dbf  shards: 3
[backup] pushed 3/3 shards to buddy 12D3KooWHtsd74aBDAfXWhtt2M6KFExKvgdCvmtNg48qpw3wYbu5
[backup] pushed 3/3 shards to buddy 12D3KooWRUpE1tZjX4wGRmdrpNtxyRAeD3YFqS18AhjqypCoa8p6
[backup] pushed 3/3 shards to buddy 12D3KooWHeYmYX5yF7qH1qFmFLUk7Z3ZbEN4jHrEtp8KKmt2Jkig
```

"RS scheme: 2 data / 1 parity" means your file was split into 2 data
shards plus 1 parity shard, one per buddy — you can lose any 1 buddy and
still fully recover the file.

## 10. Restore and verify

```
$ ./cerclbackup restore --file-id e112e6eb-817c-4866-95bc-ea9d7da69dbf \
    --store ~/cerclbackup-demo/store --out ~/cerclbackup-demo/restored_demofile.txt \
    --password 'DemoPassA123!'

[restore] restoring "/root/cerclbackup-demo/demofile.txt" (72 bytes, scheme 2/1) ...
[restore] P2P host ready, connected to 3 buddy addr(s)
[restore] integrity check passed
[restore] restored to "/root/cerclbackup-demo/restored_demofile.txt"

$ diff ~/cerclbackup-demo/demofile.txt ~/cerclbackup-demo/restored_demofile.txt && echo MATCH
MATCH
```

Byte-for-byte identical. You now have a working 3-buddy backup circle
spanning two real machines (plus, in this guide, two lab-only fillers).

## Cleanup (if this was just a test)

```
$ kill <serve PIDs on Machine A>          # find with: ps aux | grep "cerclbackup serve"
root@192.168.142.136$ kill <serve PID>
$ rm -rf ~/cerclbackup-demo               # on both machines
```

## See also

- [`docs/runbook-3-buddy-exchange/README.md`](runbook-3-buddy-exchange/README.md) —
  a similar 4-node exchange (all run locally with separate config dirs)
  plus GUI screenshots for the Buddies/Backup/Restore tabs.
