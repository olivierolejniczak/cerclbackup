// CerclBackup — Phase 1 CLI entry point.
//
// Usage:
//
//	cerclbackup backup  --src <path> --store <dir> --password <pwd>
//	cerclbackup restore --file-id <uuid> --store <dir> --out <path> --password <pwd>
//	cerclbackup list    --store <dir> --password <pwd>
//
// Phase 1 runs entirely locally: no network, no buddies.
// The pipeline is:
//
//	File → Chunker → Reed-Solomon → AES-256-GCM → Local Store
//
// Restore reverses the pipeline using the encrypted manifest.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/cerclbackup/cerclbackup/internal/api"
	cerclConfig "github.com/cerclbackup/cerclbackup/internal/config"
	"github.com/cerclbackup/cerclbackup/internal/keyring"
	"github.com/cerclbackup/cerclbackup/internal/manifest"
	p2pmod "github.com/cerclbackup/cerclbackup/internal/p2p"
	"github.com/cerclbackup/cerclbackup/internal/storage"
	"github.com/cerclbackup/cerclbackup/internal/version"
	ipfslog "github.com/ipfs/go-log/v2"
)

// cfg holds values loaded from the user's config.yaml, applied as flag defaults.
var cfg cerclConfig.Config

func main() {
	// Suppress third-party log.Printf noise that cannot be filtered via
	// ipfs/go-log subsystem levels (zeroconf multicast, quic-go buffer).
	log.SetOutput(&serveLogFilter{out: os.Stderr})

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cfg = cerclConfig.Load()
	// Password resolution order: env var → OS keyring → config file.
	if p := os.Getenv("CERCLBACKUP_PASSWORD"); p != "" {
		cfg.Password = p
	} else if p, err := keyring.Get(); err == nil && p != "" {
		cfg.Password = p
	}

	switch os.Args[1] {
	case "init":
		os.Exit(runInit(os.Args[2:]))
	case "backup":
		runBackup(os.Args[2:])
	case "restore":
		runRestore(os.Args[2:])
	case "list":
		runList(os.Args[2:])
	case "serve":
		runServe(os.Args[2:])
	case "invite":
		runInvite(os.Args[2:])
	case "invite-email":
		runInviteEmail(os.Args[2:])
	case "join-email":
		runJoinEmail(os.Args[2:])
	case "join":
		runJoin(os.Args[2:])
	case "buddy":
		os.Exit(runBuddy(os.Args[2:]))
	case "revoke":
		runRevoke(os.Args[2:])
	case "rebalance":
		runRebalance(os.Args[2:])
	case "manifest-pull":
		runManifestPull(os.Args[2:])
	case "show-phrase":
		runShowPhrase(os.Args[2:])
	case "recover":
		runRecover(os.Args[2:])
	case "watch":
		runWatch(os.Args[2:])
	case "prune":
		os.Exit(runPrune(os.Args[2:]))
	case "storage":
		os.Exit(runStorage(os.Args[2:]))
	case "scrub":
		os.Exit(runScrub(os.Args[2:]))
	case "audit":
		os.Exit(runAudit(os.Args[2:]))
	case "export":
		os.Exit(runExport(os.Args[2:]))
	case "import":
		os.Exit(runImport(os.Args[2:]))
	case "diff":
		os.Exit(runDiff(os.Args[2:]))
	case "doctor":
		os.Exit(runDoctor(os.Args[2:]))
	case "passwd":
		os.Exit(runPasswd(os.Args[2:]))
	case "config":
		os.Exit(runConfig(os.Args[2:]))
	case "circle":
		os.Exit(runCircle(os.Args[2:]))
	case "versions":
		os.Exit(runVersions(os.Args[2:]))
	case "set-password":
		os.Exit(runSetPassword(os.Args[2:]))
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "CerclBackup %s\n\n", version.AppVersion)
	fmt.Fprintln(os.Stderr, `Commands (Phase 1 — local):
  backup   --src <path> --store <dir> --password <pwd> [--buddies N]
  restore  --file-id <uuid> --store <dir> --out <path> --password <pwd>
  list     --store <dir> --password <pwd>

Commands (Phase 2a — P2P):
  serve    --password <pwd> [--port N]          start P2P daemon
  invite   --password <pwd>                      generate invite code
  join     --addr <multiaddr> --words "<mnemonic>" --password <pwd>
  buddy    list --password <pwd>                 list known buddies
  revoke    --peer-id <id> --password <pwd>       remove a buddy and rebalance
  rebalance    --password <pwd> [--store <dir>]     redistribute shards to all buddies
  invite-email --to <email> --circle <name> --password <pwd>  print email MFA invite to paste and send yourself
  join-email   --payload <file> --words "<12 words>" --password <pwd>    accept email invite
  manifest-pull --buddy-addr <multiaddr> --password <pwd>               recover manifest from buddy
  show-phrase   --password <pwd>                                         show 12-word recovery phrase
  recover       --phrase "<12 words>" --password <pwd>                   restore identity from phrase

Commands (Phase 3 -- multi-circle & versioning):
  circle add  --name <n> --scheme <d/p> --password <pwd>
  circle list --password <pwd>
  circle rm   --name <n> --confirm-name <n> --password <pwd>
  versions    --file <path> --password <pwd>                             list file version history`)
}

// ─── BACKUP ──────────────────────────────────────────────────────────────────

func runBackup(args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	src := fs.String("src", cfg.Src, "Source file to back up")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Store directory")
	password := fs.String("password", cfg.Password, "Encryption password")
	buddies := fs.Int("buddies", 5, "Number of simulated buddies (determines RS scheme)")
	excl := fs.String("exclude", cfg.Exclude, "Comma-separated glob patterns to skip (e.g. '*.tmp,.git')")
	uploadKbps := fs.Int("upload-kbps", cfg.UploadKbps, "Max upload speed in KB/s (0 = unlimited)")
	autoPrune := fs.Bool("auto-prune", cfg.AutoPrune, "Apply default retention policy after each backup")
	_ = fs.Parse(args)

	if *src == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	result, err := api.Backup(api.BackupParams{
		Src:        *src,
		StoreDir:   *storeDir,
		Password:   *password,
		Buddies:    *buddies,
		Exclude:    *excl,
		UploadKbps: *uploadKbps,
		AutoPrune:  *autoPrune,
		Progress:   func(line string) { log.Printf("[backup] %s", line) },
	})
	if err != nil {
		log.Fatalf("[backup] %v", err)
	}
	for _, f := range result.Files {
		if f.Err != "" {
			log.Printf("[backup] %s: %s", f.Path, f.Err)
		}
	}
}

// ─── RESTORE ─────────────────────────────────────────────────────────────────

func runRestore(args []string) {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	fileID := fs.String("file-id", "", "FileID UUID from the manifest (legacy; prefer --file)")
	filePath := fs.String("file", "", "Original file path to restore (looks up latest version)")
	ver := fs.Int("version", 0, "Version number to restore (0 = latest, requires --file)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Store directory")
	out := fs.String("out", "", "Output file path (required)")
	password := fs.String("password", cfg.Password, "Encryption password (required)")
	_ = fs.Parse(args)

	if *out == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}
	if *fileID == "" && *filePath == "" {
		log.Fatal("[restore] one of --file-id or --file is required")
	}

	_, err := api.Restore(api.RestoreParams{
		StoreDir: *storeDir,
		Password: *password,
		Out:      *out,
		FileID:   *fileID,
		FilePath: *filePath,
		Version:  *ver,
		Progress: func(line string) { log.Printf("[restore] %s", line) },
	})
	if err != nil {
		log.Fatalf("[restore] %v", err)
	}
}

// ─── LIST ─────────────────────────────────────────────────────────────────────

func runList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	storeDir := fs.String("store", storage.DefaultStorePath(), "Store directory")
	password := fs.String("password", cfg.Password, "Encryption password")
	all := fs.Bool("all", false, "Show all versions (default: latest per path only)")
	_ = fs.Parse(args)
	_ = storeDir

	if *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	entries, err := api.ListFiles(api.ListParams{Password: *password, All: *all})
	if err != nil {
		log.Fatalf("[list] %v", err)
	}
	if len(entries) == 0 {
		fmt.Println("No files backed up yet.")
		return
	}

	fmt.Printf("%-4s  %-36s  %-50s  %10s  %s\n", "VER", "FILE-ID", "PATH", "SIZE", "BACKED AT")
	fmt.Println("─────────────────────────────────────────────────────────────────────────────────────────────────────────────────")
	for _, e := range entries {
		backedAt := e.BackedAt.Format("2006-01-02 15:04")
		if e.BackedAt.IsZero() {
			backedAt = e.Modified.Format("2006-01-02 15:04")
		}
		fmt.Printf("%-4d  %-36s  %-50s  %10d  %s\n",
			e.Version, e.FileID, e.Path, e.Size, backedAt)
	}
}

// ---------------------------------------------------------------------------
// serve
// ---------------------------------------------------------------------------

// serveLogFilter is a log.Writer that drops known-noisy lines emitted by
// third-party libraries (zeroconf, quic-go) that use log.Printf directly and
// therefore cannot be silenced via ipfs/go-log subsystem levels.
type serveLogFilter struct{ out io.Writer }

func (f *serveLogFilter) Write(p []byte) (int, error) {
	s := string(p)
	if strings.Contains(s, "Failed to set multicast interface") ||
		strings.Contains(s, "failed to sufficiently increase receive buffer size") {
		return len(p), nil
	}
	return f.out.Write(p)
}

func runServe(args []string) {
	// Silence ipfs/go-log subsystems that are verbose during normal operation.
	ipfslog.SetLogLevel("mdns", "error")                 //nolint:errcheck
	ipfslog.SetLogLevel("dht", "error")                  //nolint:errcheck
	ipfslog.SetLogLevel("dht/RtRefreshManager", "error") //nolint:errcheck

	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "keystore password (required)")
	port := fs.Int("port", p2pmod.DefaultPort, "TCP/UDP port for libp2p")
	uploadKbps := fs.Int("upload-kbps", cfg.UploadKbps, "Max upload speed in KB/s (0 = unlimited)")
	healthAddr := fs.String("health-addr", cfg.HealthAddr, "HTTP health/metrics endpoint address (empty = disabled)")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if *password == "" {
		fs.Usage()
		os.Exit(1)
	}
	handle, err := api.StartServe(api.ServeParams{
		Password:   *password,
		Port:       *port,
		UploadKbps: *uploadKbps,
		HealthAddr: *healthAddr,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("CerclBackup daemon running\n")
	fmt.Printf("Peer ID : %s\n", handle.PeerID)
	for _, a := range handle.Addrs {
		fmt.Printf("Address : %s\n", a)
	}
	if *healthAddr != "" {
		fmt.Printf("Health  : http://%s/health\n", *healthAddr)
	}

	handle.Wait()
	fmt.Println("\nShutting down.")
}

// ---------------------------------------------------------------------------
// invite
// ---------------------------------------------------------------------------

func runInvite(args []string) {
	fs := flag.NewFlagSet("invite", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "keystore password (required)")
	servePort := fs.Int("port", 7742, "port your cerclbackup serve is listening on")
	name := fs.String("name", "", "friendly name to show your buddy (optional)")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	inv, err := api.Invite(*password, *servePort)
	if err != nil {
		log.Fatal(err)
	}

	nameFlag := ""
	if *name != "" {
		nameFlag = fmt.Sprintf(" --name %q", *name)
	}

	fmt.Println()
	fmt.Println("── Step 1 — Send this command to your buddy (chat, email, etc.) ────────")
	fmt.Printf("  cerclbackup join --addr %s --words %q%s --password <their-pw>\n",
		inv.JoinAddr, inv.Words, nameFlag)
	fmt.Println()
	fmt.Println("── Step 2 — Verify by voice or in person (prevents interception) ───────")
	fmt.Printf("  Tell your buddy your last 3 words: %s\n", inv.VerbalWords)
	fmt.Println("  Your buddy must confirm these match before running the command above.")
	fmt.Println()
	fmt.Println("── All your addresses (if buddy needs a different one) ──────────────────")
	for _, a := range inv.Addrs {
		fmt.Printf("  %s\n", a)
	}
	fmt.Println()
	fmt.Printf("Code expires in 24 hours.\n")
}

// ---------------------------------------------------------------------------
// join
// ---------------------------------------------------------------------------

func runJoin(args []string) {
	fs := flag.NewFlagSet("join", flag.ExitOnError)
	addr := fs.String("addr", "", "full multiaddr of the inviter, e.g. /ip4/1.2.3.4/tcp/7742/p2p/<peerID>")
	words := fs.String("words", "", "12-word invite mnemonic from your buddy")
	password := fs.String("password", cfg.Password, "keystore password (required)")
	name := fs.String("name", "", "friendly name for this buddy (optional)")
	port := fs.Int("port", 7742, "port your own cerclbackup serve is (or will be) listening on")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if *addr == "" || *words == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	peerID, err := api.Join(*password, *addr, *words, *name, *port)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Paired with buddy %s\n", peerID)
}

// ---------------------------------------------------------------------------
// buddy list
// ---------------------------------------------------------------------------

func runBuddyLegacy(args []string) {
	if len(args) == 0 || args[0] != "list" {
		fmt.Fprintln(os.Stderr, "usage: cerclbackup buddy list --password <pwd>")
		os.Exit(1)
	}

	fs := flag.NewFlagSet("buddy list", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "keystore password (required)")
	if err := fs.Parse(args[1:]); err != nil {
		log.Fatal(err)
	}
	if *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	entries, err := api.BuddyList(*password)
	if err != nil {
		log.Fatal(err)
	}
	if len(entries) == 0 {
		fmt.Println("No buddies yet.")
		return
	}
	fmt.Printf("%-20s  %s\n", "Friendly Name", "Peer ID")
	fmt.Printf("%-20s  %s\n", "-------------", "-------")
	for _, e := range entries {
		name := e.FriendlyName
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Printf("%-20s  %s\n", name, e.PeerID)
	}
}

// ---------------------------------------------------------------------------
// revoke
// ---------------------------------------------------------------------------

func runRevoke(args []string) {
	fs := flag.NewFlagSet("revoke", flag.ExitOnError)
	peerID := fs.String("peer-id", "", "peer ID to remove (required)")
	password := fs.String("password", cfg.Password, "keystore password (required)")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if *peerID == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	if err := api.BuddyRemove(*password, *peerID, false); err != nil {
		log.Fatalf("revoke: %v", err)
	}
	fmt.Printf("Buddy %s removed.\n", *peerID)
	fmt.Println("Rebalancing shards across remaining buddies...")
}

// ---------------------------------------------------------------------------
// Phase 2b -- P2P push/fetch helpers
// ---------------------------------------------------------------------------
//
// pushToBuddies used to live here; it has moved to internal/api/backup.go
// (called from api.Backup) so the CLI and GUI share the same code path.

// tryFetchFromBuddies has moved to internal/api/restore.go.

// ---------------------------------------------------------------------------
// Phase 2d -- Rebalance
// ---------------------------------------------------------------------------

func runRebalance(args []string) {
	fs := flag.NewFlagSet("rebalance", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "keystore password (required)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "local shard store directory")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if *password == "" {
		fs.Usage()
		os.Exit(1)
	}
	_ = storeDir // rebalance always targets storage.DefaultStorePath() for now

	res, err := api.Rebalance(*password)
	if err != nil {
		log.Fatalf("[rebalance] %v", err)
	}
	fmt.Printf("Rebalance complete: %d file(s), %d/%d shards pushed to buddies.\n",
		res.FilesProcessed, res.ShardsOK, res.ShardsAttempted)
	if len(res.Errors) > 0 {
		fmt.Printf("  %d error(s):\n", len(res.Errors))
		for _, e := range res.Errors {
			fmt.Printf("    - %s\n", e)
		}
	}
}

// ---------------------------------------------------------------------------
// Phase 2i -- Distributed manifest: manifest-pull
// ---------------------------------------------------------------------------

// runManifestPull fetches the encrypted manifest from a buddy and writes it
// to the default local manifest path, overwriting any existing file.
// Used when the owner's machine is replaced and the local manifest is lost.
func runManifestPull(args []string) {
	fs := flag.NewFlagSet("manifest-pull", flag.ExitOnError)
	buddyAddr := fs.String("addr", "", "Buddy multiaddr (required, e.g. /ip4/1.2.3.4/tcp/7742/p2p/<peerID>)")
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	out := fs.String("out", manifest.DefaultManifestPath(), "Output path for recovered manifest")
	_ = fs.Parse(args)
	if *buddyAddr == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	result, err := api.ManifestPull(*password, *buddyAddr, *out)
	if err != nil {
		log.Fatalf("manifest-pull: %v", err)
	}
	fmt.Printf("Manifest recovered from buddy → %s (%d bytes)\n", result.Path, result.Bytes)
}

// ---------------------------------------------------------------------------
// Phase 2g -- Recovery phrase: show-phrase / recover
// ---------------------------------------------------------------------------

func runShowPhrase(args []string) {
	fs := flag.NewFlagSet("show-phrase", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	_ = fs.Parse(args)
	if *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	mnemonic, err := api.ShowPhrase(*password)
	if err != nil {
		log.Fatalf("show-phrase: %v", err)
	}

	fmt.Println("Your 12-word recovery phrase (write this down in a safe place):")
	fmt.Println()
	fmt.Println(" ", mnemonic)
	fmt.Println()
	fmt.Println("Anyone with this phrase can restore your CerclBackup identity.")
}

func runRecover(args []string) {
	fs := flag.NewFlagSet("recover", flag.ExitOnError)
	phrase := fs.String("phrase", "", "12-word recovery phrase (required)")
	password := fs.String("password", cfg.Password, "New keystore password (required)")
	_ = fs.Parse(args)
	if *phrase == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	result, err := api.Recover(*phrase, *password)
	if err != nil {
		log.Fatalf("recover: %v", err)
	}

	fmt.Printf("Identity restored successfully.\nPeer ID: %s\n", result.PeerID)
	fmt.Println("Run `cerclbackup serve` to reconnect with your buddies.")
}

// ---------------------------------------------------------------------------
// Phase 2f -- Email invite (dual-channel MFA)
// ---------------------------------------------------------------------------

func runInviteEmail(args []string) {
	fs := flag.NewFlagSet("invite-email", flag.ExitOnError)
	to := fs.String("to", "", "recipient email address (required)")
	circle := fs.String("circle", "CerclBackup", "circle name shown in email")
	password := fs.String("password", cfg.Password, "keystore password (required)")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if *to == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	result, err := api.InviteEmail(api.InviteEmailParams{Password: *password, Circle: *circle}, *to)
	if err != nil {
		log.Fatalf("invite-email: %v", err)
	}

	fmt.Println("CerclBackup does not send email itself — copy this into your own mail client:")
	fmt.Printf("\nTo: %s\n", *to)
	fmt.Printf("Subject: %s\n\n", result.Subject)
	fmt.Println(result.Body)

	fmt.Println("*** SHARE THIS CODE VIA SMS / SIGNAL / VOICE — NOT BY EMAIL ***")
	fmt.Println("*** Confirm with the recipient by voice/in person that it's really them before they use it ***")
	fmt.Printf("12-word OOB code: %s\n", result.Words)
	fmt.Printf("Peer ID: %s\n", result.PeerID)
}

func runJoinEmail(args []string) {
	fs := flag.NewFlagSet("join-email", flag.ExitOnError)
	payloadFile := fs.String("payload", "", "path to invite JSON file (required)")
	wordsStr := fs.String("words", "", "12-word OOB code received out-of-band (required)")
	password := fs.String("password", cfg.Password, "keystore password (required)")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if *payloadFile == "" || *wordsStr == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	data, err := os.ReadFile(*payloadFile)
	if err != nil {
		log.Fatalf("join-email: read payload: %v", err)
	}

	circleName, peerIDStr, err := api.JoinEmail(*password, data, *wordsStr)
	if err != nil {
		log.Fatalf("join-email: %v", err)
	}
	fmt.Println("Invite verified (signature + OOB commitment match).")
	fmt.Printf("Joined circle \"%s\" — buddy %s added.\n", circleName, peerIDStr)
}

// ── runInit ──────────────────────────────────────────────────────────────────

func runInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "Keystore password (skips interactive prompt)")
	noPrompt := fs.Bool("no-prompt", false, "Skip all interactive prompts (for scripted use)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Shard store directory to create")
	force := fs.Bool("force", false, "Overwrite existing keystore and manifest (WARNING: loses access to previous backups)")
	_ = fs.Parse(args)

	// ── 1. Password ──────────────────────────────────────────────────────────
	pw := *password
	if pw == "" {
		if *noPrompt {
			fmt.Fprintln(os.Stderr, "init: --password is required when --no-prompt is set")
			return 1
		}
		var err error
		pw, err = promptPassword("Choose a keystore password: ")
		if err != nil {
			log.Printf("init: password prompt: %v", err)
			return 1
		}
		confirm, err := promptPassword("Confirm password: ")
		if err != nil {
			log.Printf("init: confirm prompt: %v", err)
			return 1
		}
		if pw != confirm {
			fmt.Fprintln(os.Stderr, "Passwords do not match.")
			return 1
		}
	}

	// ── 2. Init keystore, identity, circle and store ────────────────────────
	result, err := api.Init(api.InitParams{Password: pw, StoreDir: *storeDir, Force: *force})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		if !*force {
			fmt.Fprintln(os.Stderr, "       Run 'cerclbackup init --force' to reinitialize.")
			fmt.Fprintln(os.Stderr, "       WARNING: --force deletes all existing backup metadata.")
		}
		return 1
	}

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║           CerclBackup — First-Run Setup                  ║")
	fmt.Println("╚══════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("Peer ID : %s\n", result.PeerID)
	fmt.Println()

	if result.RecoveryPhrase != "" {
		fmt.Println("Recovery phrase (write this down — it restores your identity):")
		fmt.Println()
		fmt.Printf("  %s\n", result.RecoveryPhrase)
		fmt.Println()
		if !*noPrompt {
			fmt.Print("Press Enter once you have written down the phrase... ")
			bufio.NewReader(os.Stdin).ReadString('\n')
		}
	}

	// ── 3. Summary ────────────────────────────────────────────────────────────
	fmt.Println("Setup complete.")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  cerclbackup backup  --src <file>  --password <pw>")
	fmt.Println("  cerclbackup watch   --src <dir>   --password <pw>")
	fmt.Println("  cerclbackup invite  --buddy-addr <multiaddr> --password <pw>")
	fmt.Printf("\nKeystore : %s\n", result.KeystorePath)
	fmt.Printf("Store    : %s\n", result.StoreDir)
	return 0
}

// ── runBuddy ──────────────────────────────────────────────────────────────────

// runBuddy dispatches sub-commands: buddy list (existing), buddy status, buddy rm.
func runBuddy(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: cerclbackup buddy <list|status|rm> [flags]")
		return 1
	}
	switch args[0] {
	case "status":
		return runBuddyStatus(args[1:])
	case "list":
		runBuddyLegacy(args) // existing list handler
		return 0
	case "rm":
		return runBuddyRm(args[1:])
	default:
		runBuddyLegacy(args)
		return 0
	}
}

func runBuddyRm(args []string) int {
	fs := flag.NewFlagSet("buddy rm", flag.ExitOnError)
	peerID := fs.String("peer-id", "", "Peer ID to remove (required)")
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	noRebalance := fs.Bool("no-rebalance", false, "Skip automatic rebalance after removal")
	_ = fs.Parse(args)

	if *peerID == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "buddy rm: --peer-id and --password are required")
		return 1
	}

	if !*noRebalance {
		fmt.Println("Rebalancing shards across remaining buddies...")
	}
	if err := api.BuddyRemove(*password, *peerID, *noRebalance); err != nil {
		log.Printf("buddy rm: %v", err)
		return 1
	}
	fmt.Printf("Buddy %s removed.\n", *peerID)
	return 0
}

func runBuddyStatus(args []string) int {
	fs := flag.NewFlagSet("buddy status", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	timeout := fs.Duration("timeout", 5*time.Second, "Connect timeout per buddy")
	_ = fs.Parse(args)

	if *password == "" {
		fmt.Fprintln(os.Stderr, "buddy status: --password is required")
		return 1
	}

	results, err := api.BuddyStatus(*password, *timeout)
	if err != nil {
		log.Printf("buddy status: %v", err)
		return 1
	}
	if len(results) == 0 {
		fmt.Println("No buddies registered yet.  Use 'cerclbackup invite' to add one.")
		return 0
	}

	fmt.Printf("%-20s  %-12s  %-10s  %s\n", "NAME", "STATUS", "LATENCY", "PEER ID")
	fmt.Println("──────────────────────────────────────────────────────────────────────")
	exitCode := 0
	for _, r := range results {
		name := r.Entry.FriendlyName
		if name == "" {
			name = r.Entry.PeerID[:12] + "..."
		}
		status := "OFFLINE"
		lat := "-"
		if r.Online {
			status = "online"
			lat = fmt.Sprintf("%dms", r.Latency.Milliseconds())
		} else {
			exitCode = 2 // at least one buddy unreachable
		}
		fmt.Printf("%-20s  %-12s  %-10s  %s\n", name, status, lat, r.Entry.PeerID)
	}
	return exitCode
}

// ── runAudit ──────────────────────────────────────────────────────────────────

func runAudit(args []string) int {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Shard store to audit")
	_ = fs.Parse(args)

	if *password == "" {
		fmt.Fprintln(os.Stderr, "audit: --password is required")
		return 1
	}

	result, err := api.Audit(*password, *storeDir)
	if err != nil {
		log.Printf("audit: %v", err)
		return 1
	}

	fmt.Println("Audit complete")
	fmt.Printf("  Shards checked  : %d\n", result.Checked)
	fmt.Printf("  Valid           : %d\n", result.Valid)
	fmt.Printf("  Corrupted       : %d  (AES-GCM tag mismatch)\n", result.Corrupted)
	fmt.Printf("  Orphaned        : %d  (in store but not in manifest)\n", result.Orphaned)

	if result.Corrupted > 0 {
		fmt.Fprintln(os.Stderr, "WARNING: corruption detected — run 'cerclbackup scrub' to attempt recovery.")
		return 1
	}
	return 0
}

// ── runExport ─────────────────────────────────────────────────────────────────

func runExport(args []string) int {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	filePath := fs.String("file", "", "File path to export (required)")
	ver := fs.Int("version", 0, "Version to export (0 = latest)")
	out := fs.String("out", "", "Output .cbk file (default: <name>_v<N>_<date>.cbk)")
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Shard store")
	_ = fs.Parse(args)

	if *filePath == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "export: --file and --password are required")
		return 1
	}

	result, err := api.Export(*password, *filePath, *ver, *out, *storeDir)
	if err != nil {
		log.Printf("export: %v", err)
		return 1
	}

	fmt.Printf("Exported: %s\n", result.OutPath)
	fmt.Printf("  File   : %s\n", result.Entry.Path)
	fmt.Printf("  Version: %d  (backed %s)\n", result.Entry.Version, result.Entry.BackedAt.Format("2006-01-02 15:04"))
	fmt.Printf("  Shards : %d data + %d parity\n", result.Entry.Scheme.DataShards, result.Entry.Scheme.ParityShards)
	return 0
}

// ── runImport ─────────────────────────────────────────────────────────────────

func runImport(args []string) int {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	cbk := fs.String("file", "", ".cbk archive to import (required)")
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Shard store")
	_ = fs.Parse(args)

	if *cbk == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "import: --file and --password are required")
		return 1
	}

	result, err := api.Import(*password, *cbk, *storeDir)
	if err != nil {
		log.Printf("import: %v", err)
		return 1
	}

	fmt.Printf("Imported: %s\n", *cbk)
	fmt.Printf("  File   : %s\n", result.Entry.Path)
	fmt.Printf("  Version: %d\n", result.Entry.Version)
	fmt.Printf("  FileID : %s\n", result.Entry.FileID)
	fmt.Println("Run 'cerclbackup restore --file <path>' to recover the file.")
	return 0
}

// ── runDiff ───────────────────────────────────────────────────────────────────

func runDiff(args []string) int {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	since := fs.String("since", "", "Show changes since this time (RFC3339 or YYYY-MM-DD)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Shard store (for deleted detection)")
	_ = fs.Parse(args)

	if *password == "" || *since == "" {
		fmt.Fprintln(os.Stderr, "diff: --password and --since are required")
		fmt.Fprintln(os.Stderr, "  example: cerclbackup diff --since 2026-06-01 --password <pw>")
		return 1
	}
	_ = storeDir

	var cutoff time.Time
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, *since, time.Local); err == nil {
			cutoff = t
			break
		}
	}
	if cutoff.IsZero() {
		log.Printf("diff: cannot parse --since %q (try YYYY-MM-DD or RFC3339)", *since)
		return 1
	}

	changes, err := api.Diff(*password, cutoff)
	if err != nil {
		log.Printf("diff: %v", err)
		return 1
	}

	if len(changes) == 0 {
		fmt.Printf("No changes since %s.\n", cutoff.Format("2006-01-02 15:04"))
		return 0
	}

	fmt.Printf("Changes since %s\n", cutoff.Format("2006-01-02 15:04"))
	fmt.Printf("%-8s  %-4s  %-26s  %-10s  %s\n", "KIND", "VER", "BACKED AT", "SIZE", "PATH")
	fmt.Println("─────────────────────────────────────────────────────────────────────────────")
	for _, c := range changes {
		fmt.Printf("%-8s  %-4d  %-26s  %-10s  %s\n",
			c.Kind, c.Version,
			c.BackedAt.Format("2006-01-02 15:04:05"),
			formatBytes(c.Size),
			c.Path)
	}
	return 0
}

// ── runDoctor ─────────────────────────────────────────────────────────────────

func runDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Shard store")
	checkBuddies := fs.Bool("check-buddies", true, "Probe buddy connectivity")
	maxAge := fs.Duration("max-age", 25*time.Hour, "Warn if last backup is older than this")
	_ = fs.Parse(args)

	if *password == "" {
		fmt.Fprintln(os.Stderr, "doctor: --password is required")
		return 1
	}

	result, err := api.Doctor(api.DoctorParams{
		Password:     *password,
		StoreDir:     *storeDir,
		CheckBuddies: *checkBuddies,
		MaxAge:       *maxAge,
	})
	if err != nil {
		log.Printf("doctor: %v", err)
		return 1
	}

	fmt.Printf("CerclBackup %s — doctor\n\n", version.AppVersion)
	for _, c := range result.Checks {
		mark := "✓"
		if !c.OK {
			mark = "✗"
		}
		fmt.Printf("  %s  %-20s  %s\n", mark, c.Name, c.Msg)
	}
	fmt.Println()
	if result.AllOK {
		fmt.Println("All checks passed.")
		return 0
	}
	fmt.Fprintln(os.Stderr, "One or more checks failed.")
	return 1
}

// Falls back to plain line read when running under a test harness
// that is not a real TTY.
func promptPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	// Try syscall-level no-echo read; fall back to plain line if not a TTY.
	pw, err := readPassword()
	fmt.Println()
	return pw, err
}

// ── runPrune ────────────────────────────────────────────────────────────────

func runPrune(args []string) int {
	fs := flag.NewFlagSet("prune", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	keepAll := fs.Int("keep-all-days", 30, "Keep every version within this many days")
	keepWeekly := fs.Int("keep-weekly-days", 90, "Keep one version/week within this many days")
	maxVersions := fs.Int("max-versions", 50, "Hard cap: max versions per file path")
	dryRun := fs.Bool("dry-run", false, "Show what would be pruned without deleting")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Local shard store")
	_ = fs.Parse(args)

	if *password == "" {
		fmt.Fprintln(os.Stderr, "prune: --password is required")
		return 1
	}

	result, err := api.Prune(api.PruneParams{
		Password:       *password,
		KeepAllDays:    *keepAll,
		KeepWeeklyDays: *keepWeekly,
		MaxVersions:    *maxVersions,
		DryRun:         *dryRun,
		StoreDir:       *storeDir,
	})
	if err != nil {
		log.Printf("prune: %v", err)
		return 1
	}
	if len(result.PrunedIDs) == 0 {
		fmt.Println("Nothing to prune.")
		return 0
	}

	if *dryRun {
		fmt.Printf("Would prune %d shard set(s):\n", len(result.PrunedIDs))
		for _, id := range result.PrunedIDs {
			fmt.Printf("  %s\n", id)
		}
		return 0
	}

	fmt.Printf("Pruned %d version(s), freed %d shard set(s) from store.\n", len(result.PrunedIDs), result.Deleted)
	return 0
}

// ── runStorage ───────────────────────────────────────────────────────────────

func runStorage(args []string) int {
	fs := flag.NewFlagSet("storage", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Local shard store")
	_ = fs.Parse(args)

	if *password == "" {
		fmt.Fprintln(os.Stderr, "storage: --password is required")
		return 1
	}

	stats, err := api.Storage(*password, *storeDir)
	if err != nil {
		log.Printf("storage: %v", err)
		return 1
	}

	fmt.Printf("Manifest\n")
	fmt.Printf("  Files tracked (unique paths) : %d\n", stats.UniquePaths)
	fmt.Printf("  Total versions               : %d\n", stats.TotalVersions)
	fmt.Printf("  Files with >1 version        : %d\n", stats.MultiVersion)
	fmt.Printf("  Logical size (latest only)   : %s\n", formatBytes(stats.LogicalBytes))
	fmt.Printf("\nLocal shard store (%s)\n", *storeDir)
	fmt.Printf("  On-disk usage                : %s\n", formatBytes(stats.DiskBytes))
	if stats.LogicalBytes > 0 {
		ratio := float64(stats.DiskBytes) / float64(stats.LogicalBytes)
		fmt.Printf("  Storage amplification        : %.2fx  (RS+encryption overhead)\n", ratio)
	}
	return 0
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// ── runScrub ─────────────────────────────────────────────────────────────────

func runScrub(args []string) int {
	fs := flag.NewFlagSet("scrub", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	_ = fs.Parse(args)

	if *password == "" {
		fmt.Fprintln(os.Stderr, "scrub: --password is required")
		return 1
	}

	fmt.Println("Running scrub pass...")
	r, err := api.Scrub(*password)
	if err != nil {
		log.Printf("scrub: %v", err)
		return 1
	}

	fmt.Printf("Scrub complete\n")
	fmt.Printf("  Checked   : %d shards\n", r.Checked)
	fmt.Printf("  Healthy   : %d\n", r.OK)
	fmt.Printf("  Corrupted : %d\n", r.Corrupted)
	fmt.Printf("  Revived   : %d\n", r.Revived)
	fmt.Printf("  Failed    : %d\n", r.Failed)

	if r.Failed > 0 {
		fmt.Fprintln(os.Stderr, "WARNING: some shards could not be recovered.")
		return 1
	}
	return 0
}

// runWatch monitors a directory tree and backs up each file when it settles.
// It runs until interrupted (SIGINT/SIGTERM or Ctrl-C).
func runWatch(args []string) {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	srcDir := fs.String("src", cfg.Src, "Directory to monitor (required)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Store directory")
	password := fs.String("password", cfg.Password, "Encryption password (required)")
	buddies := fs.Int("buddies", 1, "Reed-Solomon parity shards")
	debounce := fs.Duration("debounce", 3*time.Second, "Quiet period before backup fires")
	excl := fs.String("exclude", ".git,node_modules,*.tmp,*.swp", "Comma-separated glob patterns to skip")
	autoPrune := fs.Bool("auto-prune", cfg.AutoPrune, "Apply default retention policy after each backup (default on)")
	_ = fs.Parse(args)

	if *srcDir == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	log.Printf("[watch] monitoring %s (debounce %s, exclude %q)", *srcDir, *debounce, *excl)

	handle, err := api.Watch(api.WatchParams{
		SrcDir:    *srcDir,
		StoreDir:  *storeDir,
		Password:  *password,
		Buddies:   *buddies,
		Debounce:  *debounce,
		Exclude:   *excl,
		AutoPrune: *autoPrune,
		Event: func(ev api.WatchEvent) {
			if ev.Err != "" {
				log.Printf("[watch] %s: %s", ev.Path, ev.Err)
			} else {
				log.Printf("[watch] %s", ev.Progress)
			}
		},
	})
	if err != nil {
		log.Fatalf("[watch] %v", err)
	}

	// Handle SIGINT / SIGTERM for clean shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	<-sigCh
	log.Println("[watch] shutting down...")
	handle.Stop()
}

// runCircle handles: circle add / circle list / circle rm
func runCircle(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: cerclbackup circle <add|list|rm> [flags]\n")
		return 1
	}
	sub := args[0]
	rest := args[1:]

	switch sub {
	case "add":
		fs := flag.NewFlagSet("circle add", flag.ExitOnError)
		name := fs.String("name", "", "Circle name (required)")
		scheme := fs.String("scheme", "3/2", "RS scheme data/parity")
		password := fs.String("password", cfg.Password, "Keystore password (required)")
		fs.Parse(rest)
		if *name == "" || *password == "" {
			fmt.Fprintln(os.Stderr, "circle add: --name and --password are required")
			return 1
		}
		c, err := api.CircleAdd(*password, *name, *scheme)
		if err != nil {
			log.Printf("circle add: %v", err)
			return 1
		}
		fmt.Printf("Circle added: %s (id=%s scheme=%s)\n", c.Name, c.ID, c.Scheme)
		return 0

	case "list":
		fs := flag.NewFlagSet("circle list", flag.ExitOnError)
		password := fs.String("password", cfg.Password, "Keystore password (required)")
		fs.Parse(rest)
		if *password == "" {
			fmt.Fprintln(os.Stderr, "circle list: --password is required")
			return 1
		}
		circles, err := api.CircleList(*password)
		if err != nil {
			log.Printf("circle list: %v", err)
			return 1
		}
		if len(circles) == 0 {
			fmt.Println("No circles configured.")
			return 0
		}
		fmt.Printf("%-24s %-36s %-6s %s\n", "NAME", "ID", "SCHEME", "CREATED")
		for _, c := range circles {
			fmt.Printf("%-24s %-36s %-6s %s\n", c.Name, c.ID, c.Scheme, c.CreatedAt.Format("2006-01-02"))
		}
		return 0

	case "rm":
		fs := flag.NewFlagSet("circle rm", flag.ExitOnError)
		name := fs.String("name", "", "Circle name to remove (required)")
		confirm := fs.String("confirm-name", "", "Must match --name to confirm deletion")
		password := fs.String("password", cfg.Password, "Keystore password (required)")
		fs.Parse(rest)
		if *name == "" || *password == "" {
			fmt.Fprintln(os.Stderr, "circle rm: --name and --password are required")
			return 1
		}
		if *confirm != *name {
			fmt.Fprintf(os.Stderr, "circle rm: --confirm-name must equal %q\n", *name)
			return 1
		}
		if err := api.CircleRemove(*password, *name); err != nil {
			log.Printf("circle rm: %v", err)
			return 1
		}
		fmt.Printf("Circle %q removed.\n", *name)
		return 0

	default:
		fmt.Fprintf(os.Stderr, "Unknown circle sub-command: %q\n", sub)
		return 1
	}
}

// runVersions lists all backed-up versions of a file.
func runVersions(args []string) int {
	fs := flag.NewFlagSet("versions", flag.ExitOnError)
	filePath := fs.String("file", "", "Path of the backed-up file (required)")
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	fs.Parse(args)
	if *filePath == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "versions: --file and --password are required")
		return 1
	}
	versions, err := api.Versions(*password, *filePath)
	if err != nil {
		log.Printf("versions: %v", err)
		return 1
	}
	if len(versions) == 0 {
		fmt.Printf("No versions found for: %s\n", *filePath)
		return 0
	}
	fmt.Printf("%-4s %-26s %-64s %s\n", "VER", "BACKED AT", "FILE ID", "HASH")
	for _, v := range versions {
		backedAt := v.BackedAt.Format("2006-01-02 15:04:05 UTC")
		if v.BackedAt.IsZero() {
			backedAt = "(legacy)"
		}
		fmt.Printf("%-4d %-26s %-64s %s\n", v.Version, backedAt, v.FileID, v.Hash[:16]+"...")
	}
	return 0
}

// ---------------------------------------------------------------------------
// passwd -- change keystore password
// ---------------------------------------------------------------------------

// runSetPassword stores the backup password in the OS keyring (Windows
// Credential Manager, macOS Keychain, Linux Secret Service).  It is intended
// to be opened in a terminal by the tray app so the password never has to be
// typed on the command line or stored in a plain-text file.
func runSetPassword(args []string) int {
	fs := flag.NewFlagSet("set-password", flag.ExitOnError)
	del := fs.Bool("delete", false, "Remove the stored password from the credential store")
	_ = fs.Parse(args)

	if *del {
		if err := api.DeletePassword(); err != nil {
			fmt.Fprintln(os.Stderr, "error: could not delete from credential store:", err)
			return 1
		}
		fmt.Println("Password removed from credential store.")
		return 0
	}

	// When stdin is not a terminal (e.g. piped in a test), promptPassword falls
	// back to reading a plain line — no echo suppression needed.
	pass, err := promptPassword("Enter CerclBackup password: ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: could not read password:", err)
		return 1
	}
	if err := api.SetPassword(pass); err != nil {
		fmt.Fprintln(os.Stderr, "error: could not save to credential store:", err)
		fmt.Fprintln(os.Stderr, "  Tip: set the CERCLBACKUP_PASSWORD environment variable instead.")
		return 1
	}
	fmt.Println("Password saved to credential store.")
	fmt.Println("The tray app will use it automatically on next backup cycle.")
	return 0
}

func runPasswd(args []string) int {
	fs := flag.NewFlagSet("passwd", flag.ExitOnError)
	oldFlag := fs.String("old", "", "Current password (prompted if empty)")
	newFlag := fs.String("new", "", "New password (prompted if empty)")
	_ = fs.Parse(args)

	oldPwd := *oldFlag
	if oldPwd == "" {
		if p := os.Getenv("CERCLBACKUP_PASSWORD"); p != "" {
			oldPwd = p
		} else {
			fmt.Fprint(os.Stderr, "Current password: ")
			b, err := readPassword()
			fmt.Fprintln(os.Stderr)
			if err != nil {
				log.Printf("passwd: read old: %v", err)
				return 1
			}
			oldPwd = b
		}
	}

	newPwd := *newFlag
	if newPwd == "" {
		fmt.Fprint(os.Stderr, "New password: ")
		b, err := readPassword()
		fmt.Fprintln(os.Stderr)
		if err != nil {
			log.Printf("passwd: read new: %v", err)
			return 1
		}
		newPwd = b

		fmt.Fprint(os.Stderr, "Confirm new password: ")
		b2, err := readPassword()
		fmt.Fprintln(os.Stderr)
		if err != nil {
			log.Printf("passwd: read confirm: %v", err)
			return 1
		}
		if b2 != newPwd {
			fmt.Fprintln(os.Stderr, "passwd: passwords do not match")
			return 1
		}
	}

	if err := api.Passwd(oldPwd, newPwd); err != nil {
		log.Printf("passwd: %v", err)
		return 1
	}

	fmt.Println("Keystore password changed successfully.")
	fmt.Println("Update CERCLBACKUP_PASSWORD or your config.yaml if applicable.")
	return 0
}

// ---------------------------------------------------------------------------
// config -- show / init config file
// ---------------------------------------------------------------------------

func runConfig(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: cerclbackup config <show|init>")
		return 1
	}
	switch args[0] {
	case "show":
		loaded, path := api.ConfigShow()
		fmt.Printf("Config file: %s\n\n", path)
		fmt.Printf("password    : %s\n", maskPassword(loaded.Password))
		fmt.Printf("src         : %s\n", loaded.Src)
		fmt.Printf("exclude     : %s\n", loaded.Exclude)
		fmt.Printf("upload_kbps : %d\n", loaded.UploadKbps)
		fmt.Printf("health_addr : %s\n", loaded.HealthAddr)
		fmt.Printf("port        : %d\n", loaded.Port)
		fmt.Printf("debounce    : %s\n", loaded.Debounce)
		fmt.Printf("auto_prune  : %v\n", loaded.AutoPrune)
		fmt.Printf("store_dir   : %s\n", loaded.StoreDir)
		return 0
	case "init":
		path, err := api.ConfigInit()
		if err != nil {
			fmt.Fprintf(os.Stderr, "config init: %v\n", err)
			return 1
		}
		fmt.Printf("Sample config written to %s\n", path)
		fmt.Println("Edit it to set your defaults, then uncomment the relevant lines.")
		return 0
	default:
		fmt.Fprintln(os.Stderr, "Usage: cerclbackup config <show|init>")
		return 1
	}
}

func maskPassword(p string) string {
	if p == "" {
		return "(not set)"
	}
	return "***"
}
