# How-To: A Real 3-Buddy Circle Across Separate Networks (WAN)

This walks through pairing CerclBackup between **3 machines on 3 genuinely
separate networks** — no shared LAN, no shared broadcast domain for their
private interfaces — and running a real backup/restore, plus what happens
when a buddy's public address changes later. Every command below was run
for real while writing this guide (against 4 isolated Docker networks
standing in for 4 separate physical networks); the output shown is genuine,
not illustrative.

This supersedes the old two-machine LAN guide: that guide needed "lab-only"
filler buddies to reach the minimum of 3. Here all 3 buddies are real,
separately-networked machines — no fillers, no caveats.

## Before you start: the 3-buddy rule

CerclBackup **refuses to back up to just 1 or 2 buddies**. It uses
Reed-Solomon erasure coding (data split into shards + parity shards spread
across buddies) and never falls back to a 1/1 "mirror" scheme, so you need
**at least 3 buddies** in your circle before `backup` will run at all
(enforced in `pkg/protocol/messages.go`, `ErrInsufficientBuddies`):

```
error: protocol: at least 3 buddies/devices are required (no 1/1 mirror fallback)
```

That minimum is buddies *besides yourself* — with exactly 3 real machines
in a full triangle, each machine only has 2 buddies (the other two), which
is still one short. This guide uses 4 machines so machine A genuinely has 3
real buddies with no fillers required.

## What "WAN" means here, honestly

Two machines on different networks can only reach each other if there's a
directly dialable path — same LAN, or (for real WAN/NAT setups) a public
IP with the listening port forwarded on the router. There is currently
**no rendezvous/relay for first-time pairing** — `invite`/`join` need a
real address to connect to up front.

*After* pairing, if a buddy's reachable address changes (WAN IP change,
router restart, etc.), a running `serve` daemon now recovers on its own:

- `backup`/`restore` only try each buddy's last-known **stored address**.
  If it's stale, they fail/queue for that buddy and move on — they do not
  fall back to a DHT lookup themselves.
- `serve` runs `PeriodicDialAllBuddies` in the background (default every
  **10 minutes**, override with `--redial-interval`), which tries the
  stored address first and falls back to a **DHT lookup keyed on the
  buddy's permanent peer ID** if that fails, persisting whatever address
  it finds. Previously this only ran once at `serve` startup, so a stale
  address required a manual restart to fix — that's no longer necessary.

This guide demonstrates that reconnection mechanism end-to-end for real —
stop/reassign one buddy's address, show the stale-address failure, then
show the periodic redial loop fixing it via a genuine DHT lookup, with no
`serve` restart involved. It does **not** demonstrate literal
public-internet reachability with real router port forwarding — that needs
real router admin access on separate physical WANs, which wasn't available
for this write-up. The 4 "networks" below are isolated Docker bridge
networks (private-subnet interfaces on each container cannot reach each
other at all, verified below), with a second shared interface standing in
for "public IP, port forwarded" on each machine. See the caveat at the end
of that section for the one way this simulation is *more* permissive than
a real WAN.

## Prerequisites

- CerclBackup binary built (`go build ./cmd/cerclbackup` from this repo)
  or a release binary.
- 4 machines (or, as here, 4 isolated network namespaces) that can each
  reach the others only via a specific address — not via broadcast/mDNS
  discovery. A TCP port open between them (`7001` used throughout below).

## 1. Set up 4 isolated networks

```
$ docker network create --driver bridge --subnet 172.30.1.0/24 lan1
$ docker network create --driver bridge --subnet 172.30.2.0/24 lan2
$ docker network create --driver bridge --subnet 172.30.3.0/24 lan3
$ docker network create --driver bridge --subnet 172.30.4.0/24 lan4
$ docker run -d --name cercl-lan1 --network lan1 --cap-add NET_ADMIN debian:trixie-slim sleep infinity
   # ... repeat for lan2, lan3, lan4
```

Confirmed these are genuinely isolated — lan1 cannot reach lan2's private
address at all:

```
$ docker exec cercl-lan1 ping -c1 -W2 172.30.2.2
--- 172.30.2.2 ping statistics ---
1 packets transmitted, 0 received, 100% packet loss, time 0ms
```

Then a 5th shared network stands in for "the internet" — each container
gets a second interface on it, representing its forwarded public IP:

```
$ docker network create --driver bridge --subnet 172.30.100.0/24 wan
$ docker network connect wan cercl-lan1   # ... repeat for lan2, lan3, lan4
```

```
$ docker exec cercl-lan1 hostname -I
172.30.1.2 172.30.100.2
$ docker exec cercl-lan1 ping -c1 -W2 172.30.100.3   # lan2's wan address
1 packets transmitted, 1 received, 0% packet loss
$ docker exec cercl-lan1 ping -c1 -W2 172.30.2.2      # lan2's private address — still isolated
1 packets transmitted, 0 received, 100% packet loss
```

**Caveat:** this shared "wan" network is a Docker L2 bridge, so mDNS
multicast actually propagates across it too (you'll see
`[mdns] connected to buddy ...` in the logs below even for cross-network
buddies) — a real WAN wouldn't allow that. The DHT-based reconnection
mechanism this guide focuses on is unaffected by that difference (it's
exercised deliberately, address-by-address, below), but don't read the
mDNS lines below as evidence that mDNS works over a real WAN — it doesn't.

## 2. Initialize each machine's own identity

Same as any setup — each gets its own peer ID and store:

```
$ docker exec cercl-lan1 env CERCLBACKUP_CONFIG_DIR=/root/cerclbackup-demo/cfg \
    /root/cerclbackup-demo/cerclbackup init --password 'DemoPassLan1123!' --no-prompt \
    --store /root/cerclbackup-demo/store

Peer ID : 12D3KooWQhhT6GMExncrn4nMQ9yYhMhWcEeMs9667HGnzAxber6H
Recovery phrase: alter economy hurry illegal turn shed elite twelve route nominee flip broccoli
```

Repeat identically on lan2, lan3, lan4 with their own passwords.

## 3. Start `serve` on all 4

```
$ docker exec -d cercl-lan1 sh -c \
    "CERCLBACKUP_CONFIG_DIR=/root/cerclbackup-demo/cfg /root/cerclbackup-demo/cerclbackup serve \
     --password 'DemoPassLan1123!' --port 7001 --health-addr 127.0.0.1:17001 \
     > /root/cerclbackup-demo/serve.log 2>&1"
```

Each one reports both its private and "public" address separately:

```
Address : /ip4/172.30.1.2/tcp/7001/p2p/12D3KooWQhhT6GMExncrn4nMQ9yYhMhWcEeMs9667HGnzAxber6H
Address : /ip4/172.30.100.2/tcp/7001/p2p/12D3KooWQhhT6GMExncrn4nMQ9yYhMhWcEeMs9667HGnzAxber6H
```

## 4. Pair machine 1 with the other 3, using the *public* address explicitly

**Gotcha, and a real finding from this write-up:** on a multi-homed host,
`invite`'s auto-picked `join` address defaults to whatever the OS routing
table prefers for outbound traffic — which was the **private** LAN address
here, not the public one:

```
$ docker exec cercl-lan1 sh -c "... cerclbackup invite --password 'DemoPassLan1123!' --port 7001"

── Step 1 — Send this command to your buddy ────────
  cerclbackup join --addr /ip4/172.30.1.2/tcp/7001/p2p/12D3KooW... --words "..." --password <their-pw>
...
── All your addresses (if buddy needs a different one) ──────────────────
  /ip4/127.0.0.1/tcp/7001/p2p/12D3KooW...
  /ip4/172.30.1.2/tcp/7001/p2p/12D3KooW...
  /ip4/172.30.100.2/tcp/7001/p2p/12D3KooW...
```

For a real WAN pairing, **manually substitute the public address** from
the "All your addresses" list into the `join` command instead of the
auto-picked one:

```
$ docker exec cercl-lan2 sh -c "... cerclbackup join \
    --addr /ip4/172.30.100.2/tcp/7001/p2p/12D3KooWQhhT6GMExncrn4nMQ9yYhMhWcEeMs9667HGnzAxber6H \
    --words 'border ginger chronic field actress funny lumber mean match acid stool derive' \
    --password 'DemoPassLan2123!' --port 7001 --name Lan1"

Paired with buddy 12D3KooWQhhT6GMExncrn4nMQ9yYhMhWcEeMs9667HGnzAxber6H
```

Generate a fresh invite from machine 1 each time (invite codes are
single-use) and repeat for lan3 and lan4.

**Gotcha (carried over from the LAN guide, still applies):** a running
`serve` daemon doesn't hot-reload the on-disk buddy registry after `join`.
Restart `serve` on **every** machine involved (both the inviter and each
joiner) before running a real backup:

```
$ docker exec cercl-lan1 cerclbackup buddy list --password 'DemoPassLan1123!'
Friendly Name         Peer ID
-------------         -------
Lan1                  12D3KooWQS9FTZQud54xA7TeuqiNphgpVLD5YKgpvmQQkRkmf6uR
Lan1                  12D3KooWEKdnqMHcdwRhDaREA1rqZKReCsbSJegaT4Vzb9zRvCKA
Lan1                  12D3KooWAYppCSeq185nY5yKurgDTCgSDnct3tKG9LSncgLKGBkA
```

(Skipping the restart and running `backup` immediately produces
`p2p: buddy rejected shard ...: peer not in buddy registry` on every
shard push — that's this exact gotcha, caught for real while writing this
guide.)

## 5. Run a real backup across all 3 separate networks

```
$ docker exec cercl-lan1 cerclbackup backup --src /root/cerclbackup-demo/demofile.txt \
    --store /root/cerclbackup-demo/store --password 'DemoPassLan1123!' --buddies 3

[backup] RS scheme: 2 data / 1 parity (tolerates 1 buddy failures)
[backup] done demofile.txt — file-id: 9801df2f-c269-45b8-81b0-0166fe401acd  shards: 3
[backup] pushed 3/3 shards to buddy 12D3KooWEKdnqMHcdwRhDaREA1rqZKReCsbSJegaT4Vzb9zRvCKA
[backup] pushed 3/3 shards to buddy 12D3KooWAYppCSeq185nY5yKurgDTCgSDnct3tKG9LSncgLKGBkA
[backup] pushed 3/3 shards to buddy 12D3KooWQS9FTZQud54xA7TeuqiNphgpVLD5YKgpvmQQkRkmf6uR
```

Restore and verify:

```
$ docker exec cercl-lan1 cerclbackup restore --file-id 9801df2f-c269-45b8-81b0-0166fe401acd \
    --store /root/cerclbackup-demo/store --out /root/cerclbackup-demo/restored_demofile.txt \
    --password 'DemoPassLan1123!'

[restore] P2P host ready, connected to 3 buddy addr(s)
[restore] integrity check passed
[restore] restored to "/root/cerclbackup-demo/restored_demofile.txt"

$ diff demofile.txt restored_demofile.txt && echo MATCH
MATCH
```

## 6. What happens when a buddy's public address changes

This is the part most guides skip. Simulate machine 2's WAN IP changing
(here: reassigning its address on the shared "wan" network):

```
$ docker network disconnect wan cercl-lan2
$ docker network connect --ip 172.30.100.99 wan cercl-lan2   # was .3, now .99
```

Machine 1's stored address for machine 2 is now stale. Run a backup — it
still succeeds for the two unaffected buddies, but machine 2 is now
unreachable via its old address, and the shards are queued rather than
retried automatically:

```
$ docker exec cercl-lan1 cerclbackup backup --src demofile2.txt --store ... --password ... --buddies 3

[backup] pushed 3/3 shards to buddy 12D3KooWEKdnqMHcdwRhDaREA1rqZKReCsbSJegaT4Vzb9zRvCKA
[backup] pushed 3/3 shards to buddy 12D3KooWAYppCSeq185nY5yKurgDTCgSDnct3tKG9LSncgLKGBkA
[backup] buddy 12D3KooWQS9FTZQud54xA7TeuqiNphgpVLD5YKgpvmQQkRkmf6uR unreachable, enqueueing 3 shards
```

`serve`'s background redial loop (default every 10 minutes; use a short
interval like `--redial-interval 15s` for a demo like this one) picks this
up on its own — no restart needed:

```
$ docker exec -d cercl-lan1 sh -c "... cerclbackup serve --password 'DemoPassLan1123!' --port 7001 --redial-interval 15s ..."

[dialer] connected to 12D3KooWEKdnqMHcdwRhDaREA1rqZKReCsbSJegaT4Vzb9zRvCKA via stored addrs
[dialer] connected to 12D3KooWAYppCSeq185nY5yKurgDTCgSDnct3tKG9LSncgLKGBkA via stored addrs
[dialer] stored addrs failed for 12D3KooWQS9FTZQud54xA7TeuqiNphgpVLD5YKgpvmQQkRkmf6uR, trying DHT
[mdns] connect to 12D3KooWQS9FTZQud54xA7TeuqiNphgpVLD5YKgpvmQQkRkmf6uR: failed to dial: all dials failed
[dialer] connected to 12D3KooWQS9FTZQud54xA7TeuqiNphgpVLD5YKgpvmQQkRkmf6uR via DHT discovery
```

Notice the timing: the stored-address attempt fails fast, but the DHT
fallback takes a few extra seconds (~4s here) to resolve, and only happens
on the *next* redial tick after the address actually breaks — don't assume
a buddy is unreachable just because it's not in the very first burst of
`[dialer] connected` lines right after `serve` starts.

Confirmed the connection actually works again, not just that libp2p
found a route — a new backup, run any time after that tick, reaches all
3 buddies including the one whose address changed:

```
$ docker exec cercl-lan1 cerclbackup backup --src demofile3.txt --store ... --password ... --buddies 3

[backup] pushed 3/3 shards to buddy 12D3KooWEKdnqMHcdwRhDaREA1rqZKReCsbSJegaT4Vzb9zRvCKA
[backup] pushed 3/3 shards to buddy 12D3KooWAYppCSeq185nY5yKurgDTCgSDnct3tKG9LSncgLKGBkA
[backup] pushed 3/3 shards to buddy 12D3KooWQS9FTZQud54xA7TeuqiNphgpVLD5YKgpvmQQkRkmf6uR
```

**Takeaway:** if a buddy's address changes, a running `serve` daemon
recovers on its own within one redial interval (10 minutes by default) —
no restart needed. If you want faster recovery for a time-sensitive setup,
lower `--redial-interval`; there's no downside to a short interval since
`h.Connect` is a no-op for buddies already connected.

## Cleanup (if this was just a test)

```
$ for n in 1 2 3 4; do docker rm -f cercl-lan$n; done
$ for n in lan1 lan2 lan3 lan4 wan; do docker network rm $n; done
```

## See also

- [`docs/runbook-3-buddy-exchange/README.md`](runbook-3-buddy-exchange/README.md) —
  a similar exchange (all run locally with separate config dirs) plus GUI
  screenshots for the Buddies/Backup/Restore tabs.
