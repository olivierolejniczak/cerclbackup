package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
	bbcrypto "github.com/cerclbackup/cerclbackup/internal/crypto"
	"github.com/cerclbackup/cerclbackup/internal/manifdist"
	"github.com/cerclbackup/cerclbackup/internal/manifest"
	p2pmod "github.com/cerclbackup/cerclbackup/internal/p2p"
	"github.com/cerclbackup/cerclbackup/internal/rebalance"
	scrubpkg "github.com/cerclbackup/cerclbackup/internal/scrub"
	"github.com/cerclbackup/cerclbackup/internal/storage"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// Rebalance redistributes shards for every backed-up file across the
// currently registered buddies (e.g. after a buddy was removed). An empty
// storeDir defaults to storage.DefaultStorePath().
func Rebalance(password, storeDir string) (rebalance.Result, error) {
	if password == "" {
		return rebalance.Result{}, fmt.Errorf("password is required")
	}
	ks, err := OpenKeystore(password)
	if err != nil {
		return rebalance.Result{}, err
	}
	privKey, err := p2pmod.EnsurePeerIdentity(ks, password)
	if err != nil {
		return rebalance.Result{}, fmt.Errorf("peer identity: %w", err)
	}
	h, err := p2pmod.NewHost(privKey, 0)
	if err != nil {
		return rebalance.Result{}, fmt.Errorf("host: %w", err)
	}
	defer h.Close()

	reg, err := OpenRegistry(ks)
	if err != nil {
		return rebalance.Result{}, fmt.Errorf("registry: %w", err)
	}

	localStore, err := OpenStore(storeDir)
	if err != nil {
		return rebalance.Result{}, fmt.Errorf("open local store: %w", err)
	}

	mf, err := OpenManifest(ks.MasterKey())
	if err != nil {
		return rebalance.Result{}, err
	}
	entries := mf.All()
	fileIDs := make([]string, 0, len(entries))
	for _, e := range entries {
		fileIDs = append(fileIDs, e.FileID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	ownerID := h.ID().String()
	rb := rebalance.New(ownerID, localStore, reg, h)
	return rb.Run(ctx, fileIDs)
}

// AuditResult summarizes an Audit run.
type AuditResult struct {
	Checked   int
	Valid     int
	Corrupted int
	Orphaned  int
}

// Audit walks every shard in storeDir and verifies it decrypts correctly
// against the manifest, reporting valid/corrupted/orphaned counts.
func Audit(password, storeDir string) (*AuditResult, error) {
	if password == "" {
		return nil, fmt.Errorf("password is required")
	}
	if storeDir == "" {
		storeDir = storage.DefaultStorePath()
	}

	ks, err := OpenOrCreateKeystore(password)
	if err != nil {
		return nil, err
	}
	masterKey := ks.MasterKey()

	st, err := storage.New(storeDir)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	fileIDs, err := st.ListFiles()
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}

	mf, err := OpenManifest(masterKey)
	if err != nil {
		return nil, err
	}

	result := &AuditResult{}
	for _, fileID := range fileIDs {
		entry := mf.Get(fileID)

		maxShards := 8
		if entry != nil {
			maxShards = entry.Scheme.DataShards + entry.Scheme.ParityShards
		}

		for idx := 0; idx < maxShards; idx++ {
			blob, err := st.Get(fileID, idx)
			if err != nil {
				break // no more shards for this fileID
			}
			result.Checked++

			if entry == nil {
				result.Orphaned++
				continue
			}

			hashBytes, err := hexToHash(entry.Hash)
			if err != nil {
				result.Corrupted++
				continue
			}
			fileKey, err := bbcrypto.DeriveFileKey(masterKey, hashBytes)
			if err != nil {
				result.Corrupted++
				continue
			}
			if _, decErr := bbcrypto.DecryptShard(fileKey, idx, blob); decErr != nil {
				result.Corrupted++
			} else {
				result.Valid++
			}
		}
	}
	return result, nil
}

// PruneParams configures Prune.
type PruneParams struct {
	Password       string
	KeepAllDays    int
	KeepWeeklyDays int
	MaxVersions    int
	DryRun         bool
	StoreDir       string
}

// PruneResult is the outcome of a Prune call.
type PruneResult struct {
	PrunedIDs []string // fileIDs of pruned shard sets (DryRun: what would be pruned)
	Deleted   int      // shard sets actually deleted from the store (0 if DryRun)
}

// Prune applies a retention policy to the manifest, deleting shard sets for
// versions that fall outside it (unless DryRun is set).
func Prune(params PruneParams) (*PruneResult, error) {
	if params.Password == "" {
		return nil, fmt.Errorf("password is required")
	}
	storeDir := params.StoreDir
	if storeDir == "" {
		storeDir = storage.DefaultStorePath()
	}

	ks, err := OpenOrCreateKeystore(params.Password)
	if err != nil {
		return nil, err
	}
	mf, err := OpenManifest(ks.MasterKey())
	if err != nil {
		return nil, err
	}

	policy := manifest.RetentionPolicy{
		KeepAllDays:    params.KeepAllDays,
		KeepWeeklyDays: params.KeepWeeklyDays,
		MaxVersions:    params.MaxVersions,
	}
	pruned := mf.PruneVersions(policy)
	result := &PruneResult{PrunedIDs: pruned}
	if len(pruned) == 0 || params.DryRun {
		return result, nil
	}

	st, err := storage.New(storeDir)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	for _, fileID := range pruned {
		if err := st.Delete(fileID); err != nil && !os.IsNotExist(err) {
			continue
		}
		result.Deleted++
	}

	if err := mf.Save(); err != nil {
		return nil, fmt.Errorf("save manifest: %w", err)
	}
	return result, nil
}

// StorageStats summarizes manifest and on-disk shard store usage.
type StorageStats struct {
	UniquePaths   int
	TotalVersions int
	MultiVersion  int   // files with >1 version
	LogicalBytes  int64 // sum of latest-version sizes
	DiskBytes     int64 // on-disk shard store footprint
}

// Storage computes manifest and shard-store usage statistics.
func Storage(password, storeDir string) (*StorageStats, error) {
	if password == "" {
		return nil, fmt.Errorf("password is required")
	}
	if storeDir == "" {
		storeDir = storage.DefaultStorePath()
	}
	ks, err := OpenOrCreateKeystore(password)
	if err != nil {
		return nil, err
	}
	mf, err := OpenManifest(ks.MasterKey())
	if err != nil {
		return nil, err
	}
	entries := mf.All()

	type pathStat struct {
		versions int
	}
	byPath := make(map[string]*pathStat)
	stats := &StorageStats{}
	for _, e := range entries {
		s := byPath[e.Path]
		if s == nil {
			s = &pathStat{}
			byPath[e.Path] = s
		}
		s.versions++
		if lat := mf.Latest(e.Path); lat != nil && lat.FileID == e.FileID {
			stats.LogicalBytes += e.Size
		}
	}
	stats.UniquePaths = len(byPath)
	stats.TotalVersions = len(entries)
	for _, s := range byPath {
		if s.versions > 1 {
			stats.MultiVersion++
		}
	}

	filepath.WalkDir(storeDir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			stats.DiskBytes += info.Size()
		}
		return nil
	})

	return stats, nil
}

// Scrub runs a single scrub pass over the local buddy shard store, verifying
// and attempting to revive any corrupted shards from peers.
func Scrub(password string) (*scrubpkg.Report, error) {
	if password == "" {
		return nil, fmt.Errorf("password is required")
	}
	ks, err := OpenOrCreateKeystore(password)
	if err != nil {
		return nil, err
	}
	cfgDir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	bs := buddy.NewStore(filepath.Join(cfgDir, "shards"))

	privKey, err := p2pmod.EnsurePeerIdentity(ks, password)
	if err != nil {
		return nil, fmt.Errorf("peer identity: %w", err)
	}
	h, err := p2pmod.NewHost(privKey, 0)
	if err != nil {
		return nil, fmt.Errorf("host: %w", err)
	}
	defer h.Close()

	reg, err := OpenRegistry(ks)
	if err != nil {
		return nil, err
	}

	mgr := scrubpkg.New(bs, h, reg)
	report, err := mgr.RunOnce(context.Background())
	if err != nil {
		return nil, err
	}
	return &report, nil
}

// DiffChange describes a single manifest change since a cutoff time.
type DiffChange struct {
	Path     string
	Version  int
	BackedAt time.Time
	FileID   string
	Size     int64
	Kind     string // "new" or "updated"
}

// Diff returns manifest entries backed up after since.
func Diff(password string, since time.Time) ([]DiffChange, error) {
	if password == "" {
		return nil, fmt.Errorf("password is required")
	}
	ks, err := OpenOrCreateKeystore(password)
	if err != nil {
		return nil, err
	}
	mf, err := OpenManifest(ks.MasterKey())
	if err != nil {
		return nil, err
	}
	entries := mf.All()

	latestBefore := make(map[string]int) // path → highest version before cutoff
	for _, e := range entries {
		t := e.BackedAt
		if t.IsZero() {
			t = e.Modified
		}
		if t.Before(since) && e.Version > latestBefore[e.Path] {
			latestBefore[e.Path] = e.Version
		}
	}

	var changes []DiffChange
	for _, e := range entries {
		t := e.BackedAt
		if t.IsZero() {
			t = e.Modified
		}
		if !t.After(since) {
			continue
		}
		kind := "updated"
		if latestBefore[e.Path] == 0 {
			kind = "new"
		}
		changes = append(changes, DiffChange{
			Path:     e.Path,
			Version:  e.Version,
			BackedAt: t,
			FileID:   e.FileID,
			Size:     e.Size,
			Kind:     kind,
		})
	}
	return changes, nil
}

// ManifestPullResult is the outcome of a ManifestPull call.
type ManifestPullResult struct {
	Path  string
	Bytes int
}

// ManifestPull fetches the encrypted manifest from a buddy at addr and
// writes it to out (or the default manifest path if out is empty),
// overwriting any existing file. Used when the owner's machine is replaced
// and the local manifest is lost.
func ManifestPull(password, addr, out string) (*ManifestPullResult, error) {
	if password == "" || addr == "" {
		return nil, fmt.Errorf("password and addr are required")
	}
	if out == "" {
		out = manifest.DefaultManifestPath()
	}

	ks, err := OpenKeystore(password)
	if err != nil {
		return nil, err
	}
	priv, err := p2pmod.EnsurePeerIdentity(ks, password)
	if err != nil {
		return nil, fmt.Errorf("peer identity: %w", err)
	}
	h, err := p2pmod.NewHost(priv, 0)
	if err != nil {
		return nil, fmt.Errorf("host: %w", err)
	}
	defer h.Close()

	ma, err := multiaddr.NewMultiaddr(addr)
	if err != nil {
		return nil, fmt.Errorf("parse addr %q: %w", addr, err)
	}
	pi, err := peer.AddrInfoFromP2pAddr(ma)
	if err != nil {
		return nil, fmt.Errorf("addr info: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := h.Connect(ctx, *pi); err != nil {
		return nil, fmt.Errorf("connect to buddy: %w", err)
	}

	blob, err := manifdist.PullFromBuddy(ctx, h, pi.ID, h.ID().String())
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(out), 0700); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(out, blob, 0600); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	return &ManifestPullResult{Path: out, Bytes: len(blob)}, nil
}
