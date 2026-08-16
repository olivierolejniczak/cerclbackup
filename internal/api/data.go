package api

import (
	"fmt"
	"os"

	"github.com/cerclbackup/cerclbackup/internal/archive"
	"github.com/cerclbackup/cerclbackup/internal/storage"
	"github.com/cerclbackup/cerclbackup/pkg/protocol"
)

// ExportResult is the outcome of a successful Export call.
type ExportResult struct {
	OutPath string
	Entry   *protocol.ManifestEntry
}

// Export packages one backed-up version of filePath (version 0 = latest)
// into a portable .cbk archive at outPath (or a default name if empty).
func Export(password, filePath string, version int, outPath, storeDir string) (*ExportResult, error) {
	if password == "" || filePath == "" {
		return nil, fmt.Errorf("password and file path are required")
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

	var entry *protocol.ManifestEntry
	if version > 0 {
		for _, e := range mf.ListVersions(filePath) {
			if e.Version == version {
				entry = e
				break
			}
		}
	} else {
		entry = mf.Latest(filePath)
	}
	if entry == nil {
		return nil, fmt.Errorf("%q not found in manifest", filePath)
	}

	st, err := OpenStore(storeDir)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	total := entry.Scheme.TotalShards()
	shards := make([][]byte, total)
	for i := 0; i < total; i++ {
		data, _ := st.Get(entry.FileID, i)
		// Leave nil on error — RS can reconstruct if enough data shards present.
		shards[i] = data
	}

	if outPath == "" {
		outPath = archive.Filename(entry)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return nil, fmt.Errorf("create %q: %w", outPath, err)
	}
	defer f.Close()

	if err := archive.Write(f, entry, shards); err != nil {
		return nil, fmt.Errorf("write archive: %w", err)
	}

	return &ExportResult{OutPath: outPath, Entry: entry}, nil
}

// ImportResult is the outcome of a successful Import call.
type ImportResult struct {
	Entry *protocol.ManifestEntry
}

// Import reads a .cbk archive and restores its shards + manifest entry into
// the local store, so the file becomes restorable again.
func Import(password, cbkPath, storeDir string) (*ImportResult, error) {
	if password == "" || cbkPath == "" {
		return nil, fmt.Errorf("password and file path are required")
	}
	if storeDir == "" {
		storeDir = storage.DefaultStorePath()
	}

	f, err := os.Open(cbkPath)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", cbkPath, err)
	}
	defer f.Close()

	entry, shards, err := archive.Read(f)
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}

	st, err := OpenStore(storeDir)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	for i, data := range shards {
		if len(data) == 0 {
			continue
		}
		isParity := i >= entry.Scheme.DataShards
		if err := st.Put(entry.FileID, i, isParity, data); err != nil {
			return nil, fmt.Errorf("store shard %d: %w", i, err)
		}
	}

	ks, err := OpenOrCreateKeystore(password)
	if err != nil {
		return nil, err
	}
	mf, err := OpenManifest(ks.MasterKey())
	if err != nil {
		return nil, err
	}

	if mf.Get(entry.FileID) == nil {
		mf.ImportEntry(entry)
		if err := mf.Save(); err != nil {
			return nil, fmt.Errorf("save manifest: %w", err)
		}
	}

	return &ImportResult{Entry: entry}, nil
}
