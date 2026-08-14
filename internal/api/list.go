package api

import (
	"fmt"

	"github.com/cerclbackup/cerclbackup/pkg/protocol"
)

// ListParams selects which manifest entries to return.
type ListParams struct {
	Password string
	All      bool // false = latest version per path only
}

// ListFiles returns manifest entries, optionally deduplicated to the latest
// version per path.
func ListFiles(params ListParams) ([]*protocol.ManifestEntry, error) {
	if params.Password == "" {
		return nil, fmt.Errorf("password is required")
	}
	ks, err := OpenOrCreateKeystore(params.Password)
	if err != nil {
		return nil, err
	}
	mf, err := OpenManifest(ks.MasterKey())
	if err != nil {
		return nil, err
	}

	entries := mf.All()
	if params.All {
		return entries, nil
	}

	latest := make(map[string]*protocol.ManifestEntry)
	for _, e := range entries {
		if prev, ok := latest[e.Path]; !ok || e.Version > prev.Version {
			latest[e.Path] = e
		}
	}
	out := make([]*protocol.ManifestEntry, 0, len(latest))
	for _, e := range latest {
		out = append(out, e)
	}
	return out, nil
}

// Versions returns every backed-up version of file, newest first as stored
// by the manifest.
func Versions(password, file string) ([]*protocol.ManifestEntry, error) {
	if password == "" || file == "" {
		return nil, fmt.Errorf("password and file are required")
	}
	ks, err := OpenKeystore(password)
	if err != nil {
		return nil, err
	}
	mf, err := OpenManifest(ks.MasterKey())
	if err != nil {
		return nil, err
	}
	return mf.ListVersions(file), nil
}
