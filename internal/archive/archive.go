// Package archive serialises a backed-up file (manifest entry + encrypted
// shards) into a portable .cbk file (tar+gzip) for offline handoff or cold
// storage.  Shards are already AES-256-GCM encrypted, so the archive is
// inherently confidential without any additional layer.
//
// Archive layout (tar entries):
//   manifest.json   – JSON-encoded ManifestEntry
//   shard-0.enc     – encrypted shard 0
//   shard-1.enc     – encrypted shard 1
//   …
package archive

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/cerclbackup/cerclbackup/pkg/protocol"
)

const (
	manifestEntry = "manifest.json"
	shardPrefix   = "shard-"
	shardSuffix   = ".enc"
	magicComment  = "cerclbackup-archive-v1"
)

// Write serialises entry and its shards into w as a gzip-compressed tar.
// shards must be indexed 0..len(shards)-1 in order; nil shards (parity
// that wasn't fetched) are written as zero-length entries so indices stay
// stable on import.
func Write(w io.Writer, entry *protocol.ManifestEntry, shards [][]byte) error {
	gz, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	gz.Comment = magicComment
	tw := tar.NewWriter(gz)

	// 1. manifest.json
	mjson, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("archive: marshal manifest: %w", err)
	}
	if err := addBytes(tw, manifestEntry, mjson); err != nil {
		return err
	}

	// 2. shards
	for i, data := range shards {
		name := fmt.Sprintf("%s%d%s", shardPrefix, i, shardSuffix)
		if data == nil {
			data = []byte{}
		}
		if err := addBytes(tw, name, data); err != nil {
			return fmt.Errorf("archive: write shard %d: %w", i, err)
		}
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("archive: close tar: %w", err)
	}
	return gz.Close()
}

// Read parses a .cbk archive from r, returning the ManifestEntry and shards.
func Read(r io.Reader) (*protocol.ManifestEntry, [][]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, nil, fmt.Errorf("archive: not a gzip stream: %w", err)
	}
	if gz.Comment != magicComment {
		return nil, nil, errors.New("archive: not a cerclbackup archive (wrong magic)")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	var entry *protocol.ManifestEntry
	shardMap := make(map[int][]byte)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("archive: read tar: %w", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, nil, fmt.Errorf("archive: read entry %q: %w", hdr.Name, err)
		}

		switch {
		case hdr.Name == manifestEntry:
			entry = &protocol.ManifestEntry{}
			if err := json.Unmarshal(data, entry); err != nil {
				return nil, nil, fmt.Errorf("archive: parse manifest: %w", err)
			}

		case strings.HasPrefix(hdr.Name, shardPrefix) && strings.HasSuffix(hdr.Name, shardSuffix):
			idxStr := strings.TrimSuffix(strings.TrimPrefix(hdr.Name, shardPrefix), shardSuffix)
			idx, err := strconv.Atoi(idxStr)
			if err != nil {
				return nil, nil, fmt.Errorf("archive: bad shard name %q", hdr.Name)
			}
			shardMap[idx] = data
		}
	}

	if entry == nil {
		return nil, nil, errors.New("archive: manifest.json not found")
	}

	total := entry.Scheme.TotalShards()
	shards := make([][]byte, total)
	for i := 0; i < total; i++ {
		shards[i] = shardMap[i] // nil if shard was absent (tolerated by RS)
	}
	return entry, shards, nil
}

// Filename returns a safe default filename for the archive of entry.
func Filename(entry *protocol.ManifestEntry) string {
	safe := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_").Replace(entry.Path)
	if safe == "" {
		safe = entry.FileID
	}
	ts := entry.BackedAt
	if ts.IsZero() {
		ts = time.Now()
	}
	return fmt.Sprintf("%s_v%d_%s.cbk", strings.TrimPrefix(safe, "_"), entry.Version, ts.Format("20060102"))
}

func addBytes(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o600,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}
