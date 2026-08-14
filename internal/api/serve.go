package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
	p2pmod "github.com/cerclbackup/cerclbackup/internal/p2p"
	scrubpkg "github.com/cerclbackup/cerclbackup/internal/scrub"
	"github.com/cerclbackup/cerclbackup/internal/version"
)

// ServeParams configures StartServe.
type ServeParams struct {
	Password   string
	Port       int    // 0 = p2pmod.DefaultPort
	UploadKbps int    // 0 = unlimited
	HealthAddr string // empty = no HTTP health/metrics endpoint
}

// ServeHandle controls a running daemon started by StartServe.
type ServeHandle struct {
	PeerID string
	Addrs  []string

	stopOnce sync.Once
	stop     context.CancelFunc
	done     chan struct{}
}

// Stop shuts the daemon down and blocks until cleanup finishes.
func (h *ServeHandle) Stop() {
	h.stopOnce.Do(func() {
		h.stop()
		<-h.done
	})
}

// Wait blocks until the daemon exits (e.g. via Stop, or SIGINT/SIGTERM).
func (h *ServeHandle) Wait() {
	<-h.done
}

// StartServe brings up the P2P host, registers protocol handlers, starts
// mDNS/DHT discovery and the periodic scrub loop, and (if HealthAddr is set)
// an HTTP /health and /metrics endpoint. It returns immediately with a
// handle to control the running daemon; call Wait or Stop as needed.
func StartServe(params ServeParams) (*ServeHandle, error) {
	if params.Password == "" {
		return nil, fmt.Errorf("password is required")
	}
	port := params.Port
	if port == 0 {
		port = p2pmod.DefaultPort
	}
	if params.UploadKbps > 0 {
		p2pmod.SetUploadRate(params.UploadKbps * 1024)
	}

	ks, err := OpenKeystore(params.Password)
	if err != nil {
		return nil, err
	}
	privKey, err := p2pmod.EnsurePeerIdentity(ks, params.Password)
	if err != nil {
		return nil, err
	}
	h, err := p2pmod.NewHost(privKey, port)
	if err != nil {
		return nil, err
	}

	reg, err := OpenRegistry(ks)
	if err != nil {
		h.Close()
		return nil, err
	}

	cfgDir, err := ConfigDir()
	if err != nil {
		h.Close()
		return nil, err
	}
	storeDir := filepath.Join(cfgDir, "shards")
	bs := buddy.NewStore(storeDir)
	invMgr := OpenInviteManager()

	p2pmod.RegisterHandlers(h, reg, bs, invMgr)

	q := p2pmod.NewQueue(filepath.Join(cfgDir, "queue.json"))
	p2pmod.StartMDNS(h, reg, q) //nolint:errcheck // non-fatal: LAN discovery just won't work

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	d, err := p2pmod.StartDHT(ctx, h)
	if err == nil {
		go p2pmod.DialAllBuddies(ctx, h, d, reg)
	}

	scrubpkg.New(bs, h, reg).Start(ctx, 6*time.Hour)

	serveStart := time.Now()
	var httpSrv *http.Server
	if params.HealthAddr != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			peers := len(h.Network().Peers())
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":   "ok",
				"version":  version.AppVersion,
				"peer_id":  h.ID().String(),
				"peers":    peers,
				"uptime_s": int(time.Since(serveStart).Seconds()),
			})
		})
		mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			uptime := int(time.Since(serveStart).Seconds())
			peers := len(h.Network().Peers())
			buddies := reg.List()
			shards, _ := bs.ListAll()
			fmt.Fprintf(w, "# HELP cerclbackup_uptime_seconds Seconds since daemon start\n")
			fmt.Fprintf(w, "cerclbackup_uptime_seconds %d\n", uptime)
			fmt.Fprintf(w, "# HELP cerclbackup_peers_connected Connected libp2p peers\n")
			fmt.Fprintf(w, "cerclbackup_peers_connected %d\n", peers)
			fmt.Fprintf(w, "# HELP cerclbackup_buddies_registered Registered buddy count\n")
			fmt.Fprintf(w, "cerclbackup_buddies_registered %d\n", len(buddies))
			fmt.Fprintf(w, "# HELP cerclbackup_shards_stored Shard files on disk\n")
			fmt.Fprintf(w, "cerclbackup_shards_stored %d\n", len(shards))
		})
		httpSrv = &http.Server{Addr: params.HealthAddr, Handler: mux}
		go httpSrv.ListenAndServe() //nolint:errcheck
	}

	var addrs []string
	for _, a := range h.Addrs() {
		addrs = append(addrs, fmt.Sprintf("%s/p2p/%s", a, h.ID()))
	}

	handle := &ServeHandle{
		PeerID: h.ID().String(),
		Addrs:  addrs,
		stop:   stop,
		done:   make(chan struct{}),
	}

	go func() {
		<-ctx.Done()
		if httpSrv != nil {
			httpSrv.Close()
		}
		if d != nil {
			d.Close()
		}
		h.Close()
		close(handle.done)
	}()

	return handle, nil
}
