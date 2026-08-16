package api

import (
	"context"
	"fmt"
	"time"

	traystatus "github.com/cerclbackup/cerclbackup/internal/tray"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	p2pmod "github.com/cerclbackup/cerclbackup/internal/p2p"
	"github.com/cerclbackup/cerclbackup/internal/storage"
)

// DoctorCheck is a single named health check result.
type DoctorCheck struct {
	Name string
	OK   bool
	Msg  string
}

// DoctorResult is the full set of health checks from a Doctor call.
type DoctorResult struct {
	Checks []DoctorCheck
	AllOK  bool
}

// DoctorParams configures Doctor.
type DoctorParams struct {
	Password     string
	StoreDir     string
	CheckBuddies bool
	MaxAge       time.Duration // warn if last backup is older than this
}

// Doctor runs a battery of local health checks (keystore, peer identity,
// shard store, manifest, last backup age, buddy connectivity, disk space)
// and reports pass/fail with a human-readable message for each.
func Doctor(params DoctorParams) (*DoctorResult, error) {
	if params.Password == "" {
		return nil, fmt.Errorf("password is required")
	}
	storeDir := params.StoreDir
	if storeDir == "" {
		storeDir = storage.DefaultStorePath()
	}
	maxAge := params.MaxAge
	if maxAge == 0 {
		maxAge = 25 * time.Hour
	}

	result := &DoctorResult{AllOK: true}
	add := func(name string, ok bool, msg string) {
		result.Checks = append(result.Checks, DoctorCheck{Name: name, OK: ok, Msg: msg})
		if !ok {
			result.AllOK = false
		}
	}

	// 1. Keystore
	ks, err := OpenKeystore(params.Password)
	if err != nil {
		add("keystore", false, fmt.Sprintf("cannot open: %v", err))
	} else {
		add("keystore", true, "opened OK")
	}

	// 2. Peer identity
	var privKey libp2pcrypto.PrivKey
	if ks != nil {
		privKey, err = p2pmod.EnsurePeerIdentity(ks, params.Password)
		if err != nil {
			add("peer identity", false, fmt.Sprintf("%v", err))
		} else {
			pid, _ := peer.IDFromPrivateKey(privKey)
			add("peer identity", true, pid.String()[:20]+"…")
		}
	}

	// 3. Store writable
	st, err := OpenStore(storeDir)
	if err != nil {
		add("shard store", false, fmt.Sprintf("cannot open %s: %v", storeDir, err))
	} else {
		fileIDs, err := st.ListFiles()
		if err != nil {
			add("shard store", false, fmt.Sprintf("list error: %v", err))
		} else {
			add("shard store", true, fmt.Sprintf("%s — %d file(s) stored", storeDir, len(fileIDs)))
		}
	}

	// 4. Manifest
	if ks != nil {
		mf, err := OpenManifest(ks.MasterKey())
		if err != nil {
			add("manifest", false, fmt.Sprintf("load error: %v", err))
		} else {
			entries := mf.All()
			add("manifest", true, fmt.Sprintf("%d version(s) tracked", len(entries)))
		}
	}

	// 5. Last backup age
	if ks != nil {
		cfgDir, _ := ConfigDir()
		st2, err := traystatus.Read(cfgDir)
		if err != nil || st2.LastBackupAt.IsZero() {
			add("last backup", false, "no backup recorded yet")
		} else {
			age := time.Since(st2.LastBackupAt)
			msg := fmt.Sprintf("%s ago — %s", age.Round(time.Second), st2.LastFile)
			add("last backup", age <= maxAge, msg)
		}
	}

	// 6. Buddy connectivity
	if params.CheckBuddies && ks != nil {
		reg, err := OpenRegistry(ks)
		if err != nil {
			add("buddies", false, fmt.Sprintf("registry: %v", err))
		} else {
			buddies := reg.List()
			if len(buddies) == 0 {
				add("buddies", false, "no buddies registered")
			} else if privKey != nil {
				h, err := p2pmod.NewHost(privKey, 0)
				if err == nil {
					defer h.Close()
					reachable := 0
					for _, b := range buddies {
						pid, err := peer.Decode(b.PeerID)
						if err != nil {
							continue
						}
						addrs := make([]multiaddr.Multiaddr, 0)
						for _, a := range b.Addrs {
							if ma, err := multiaddr.NewMultiaddr(a); err == nil {
								addrs = append(addrs, ma)
							}
						}
						ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
						if err := h.Connect(ctx, peer.AddrInfo{ID: pid, Addrs: addrs}); err == nil {
							reachable++
						}
						cancel()
					}
					ok := reachable > 0
					add("buddies", ok, fmt.Sprintf("%d/%d reachable", reachable, len(buddies)))
				} else {
					add("buddies", false, fmt.Sprintf("host: %v", err))
				}
			}
		}
	}

	// 7. Disk space
	if free, ok := diskFreeBytes(storeDir); ok {
		add("disk space", free > 100*1024*1024, fmt.Sprintf("%d bytes free in %s", free, storeDir))
	}

	return result, nil
}
