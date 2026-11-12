// Package server is the HTTPS + WebSocket server that the phone talks to.
package server

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/remigl/tapis/internal/certs"
	"github.com/remigl/tapis/internal/input"
	"github.com/remigl/tapis/internal/store"
	"github.com/remigl/tapis/internal/ws"
)

// Config configures a Server.
type Config struct {
	Port     int
	Dir      string // configuration directory
	Injector input.Injector
	Web      fs.FS
	Log      *log.Logger
	// AllowAnyNetwork disables the private-address check (not recommended).
	AllowAnyNetwork bool
}

// Server serves the phone web app and the control WebSocket.
type Server struct {
	cfg      Config
	Store    *store.Store
	Identity *certs.Identity
	IPs      []net.IP
	hostname string

	mu          sync.Mutex
	pairToken   string
	pairExpires time.Time
	fails       map[string]*failInfo
	sessions    map[*session]struct{}
}

type failInfo struct {
	count int
	until time.Time
}

const (
	pairingTTL  = 5 * time.Minute
	maxFailures = 5
	lockout     = time.Minute
)

// New prepares the server: loads the device store and TLS identity.
func New(cfg Config) (*Server, error) {
	st, err := store.Open(cfg.Dir)
	if err != nil {
		return nil, err
	}
	ips := LocalIPs()
	id, err := certs.Load(cfg.Dir, ips)
	if err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	return &Server{cfg: cfg, Store: st, Identity: id, IPs: ips, hostname: host,
		fails: map[string]*failInfo{}, sessions: map[*session]struct{}{}}, nil
}

// NewPairing creates a fresh single-use pairing token and returns the URLs
// (one per local address) that embed it. The first URL is the preferred one.
func (s *Server) NewPairing() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pairToken = store.RandomToken(16)
	s.pairExpires = time.Now().Add(pairingTTL)
	var urls []string
	for _, ip := range s.IPs {
		urls = append(urls, s.baseURL(ip)+"/#pair="+s.pairToken)
	}
	if len(urls) == 0 {
		urls = append(urls, s.baseURL(net.ParseIP("127.0.0.1"))+"/#pair="+s.pairToken)
	}
	return urls
}

func (s *Server) baseURL(ip net.IP) string {
	return "https://" + net.JoinHostPort(ip.String(), strconv.Itoa(s.cfg.Port))
}

// URLs returns the base URLs the server can be reached at.
func (s *Server) URLs() []string {
	var u []string
	for _, ip := range s.IPs {
		u = append(u, s.baseURL(ip))
	}
	return u
}

// Run serves until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/ca.crt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-x509-ca-cert")
		w.Header().Set("Content-Disposition", `attachment; filename="tapis-ca.crt"`)
		w.Write(s.Identity.CAPEM)
	})
	mux.Handle("/", http.FileServer(http.FS(s.cfg.Web)))

	srv := &http.Server{
		Addr:    ":" + strconv.Itoa(s.cfg.Port),
		Handler: s.guard(mux),
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{s.Identity.TLS},
			NextProtos:   []string{"http/1.1"},
		},
		// HTTP/1.1 only: WebSockets over HTTP/2 (RFC 8441) are not needed.
		TLSNextProto:      map[string]func(*http.Server, *tls.Conn, http.Handler){},
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          log.New(io.Discard, "", 0), // TLS handshake noise from untrusted-cert probes
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
		s.mu.Lock()
		for sess := range s.sessions {
			sess.conn.Close()
		}
		s.mu.Unlock()
	}()
	err := srv.ListenAndServeTLS("", "")
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func remoteIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return nil
	}
	return net.ParseIP(host)
}

// IsLocal reports whether ip belongs to a local (non-routable) network.
func IsLocal(ip net.IP) bool {
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

// guard rejects non-local clients and adds security headers.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.AllowAnyNetwork && !IsLocal(remoteIP(r)) {
			s.cfg.Log.Printf("rejected connection from non-local address %s", r.RemoteAddr)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; "+
			"img-src 'self' data:; manifest-src 'self'; connect-src 'self' wss://"+r.Host+"; "+
			"base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) blocked(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fails[ip]
	return f != nil && time.Now().Before(f.until)
}

func (s *Server) fail(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fails[ip]
	if f == nil {
		f = &failInfo{}
		s.fails[ip] = f
	}
	f.count++
	if f.count >= maxFailures {
		f.until = time.Now().Add(lockout)
		f.count = 0
		s.cfg.Log.Printf("too many failed attempts from %s, blocked for %s", ip, lockout)
	}
}

func (s *Server) succeed(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.fails, ip)
}

func (s *Server) consumePairing(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pairToken == "" || time.Now().After(s.pairExpires) {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.pairToken)) != 1 {
		return false
	}
	s.pairToken = ""
	return true
}

// Sessions returns the names of connected devices.
func (s *Server) Sessions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n []string
	for sess := range s.sessions {
		n = append(n, sess.dev.Name+" ("+sess.dev.ID+")")
	}
	sort.Strings(n)
	return n
}

// Disconnect closes the sessions of a device (after revocation).
func (s *Server) Disconnect(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sess := range s.sessions {
		if sess.dev.ID == id {
			sess.conn.Close()
		}
	}
}

func cleanName(n string) string {
	n = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, n)
	if r := []rune(strings.TrimSpace(n)); len(r) > 60 {
		n = string(r[:60])
	}
	if n == "" {
		n = "phone"
	}
	return n
}

// message is the JSON protocol between the phone and the server.
type message struct {
	T     string   `json:"t"`
	X     int      `json:"x,omitempty"`
	Y     int      `json:"y,omitempty"`
	B     string   `json:"b,omitempty"`
	A     string   `json:"a,omitempty"`
	K     string   `json:"k,omitempty"`
	M     []string `json:"m,omitempty"`
	N     int      `json:"n,omitempty"`
	S     string   `json:"s,omitempty"`
	Token string   `json:"token,omitempty"`
	ID    string   `json:"id,omitempty"`
	Key   string   `json:"key,omitempty"`
	Name  string   `json:"name,omitempty"`
}

type session struct {
	conn    *ws.Conn
	dev     store.Device
	buttons map[input.Button]bool
	keys    map[string]bool
}

func (s *Server) send(c *ws.Conn, v any) {
	b, _ := json.Marshal(v)
	c.WriteText(b)
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	ip := remoteIP(r).String()
	if origin := r.Header.Get("Origin"); origin != "https://"+r.Host {
		s.cfg.Log.Printf("rejected WebSocket with origin %q from %s", origin, ip)
		http.Error(w, "bad origin", http.StatusForbidden)
		return
	}
	if s.blocked(ip) {
		http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
		return
	}
	conn, err := ws.Upgrade(w, r)
	if err != nil {
		return
	}
	defer conn.Close()

	// The first message must authenticate (or pair) within 10 seconds.
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	raw, err := conn.ReadMessage()
	if err != nil {
		return
	}
	var m message
	if json.Unmarshal(raw, &m) != nil {
		return
	}
	var dev store.Device
	switch m.T {
	case "pair":
		if !s.consumePairing(m.Token) {
			s.fail(ip)
			s.send(conn, map[string]string{"t": "error", "code": "pairing"})
			return
		}
		d, key, err := s.Store.Add(cleanName(m.Name))
		if err != nil {
			s.send(conn, map[string]string{"t": "error", "code": "internal"})
			return
		}
		dev = d
		s.send(conn, map[string]string{"t": "paired", "id": d.ID, "key": key})
		s.cfg.Log.Printf("paired new device %q (id %s) from %s", d.Name, d.ID, ip)
	case "auth":
		d, ok := s.Store.Verify(m.ID, m.Key)
		if !ok {
			s.fail(ip)
			s.send(conn, map[string]string{"t": "error", "code": "auth"})
			return
		}
		dev = d
	default:
		return
	}
	s.succeed(ip)
	s.send(conn, map[string]string{"t": "ok", "host": s.hostname, "os": runtime.GOOS})

	sess := &session{conn: conn, dev: dev, buttons: map[input.Button]bool{}, keys: map[string]bool{}}
	s.mu.Lock()
	s.sessions[sess] = struct{}{}
	s.mu.Unlock()
	s.cfg.Log.Printf("%s connected from %s", dev.Name, ip)
	defer func() {
		s.release(sess)
		s.mu.Lock()
		delete(s.sessions, sess)
		s.mu.Unlock()
		s.cfg.Log.Printf("%s disconnected", dev.Name)
	}()

	stop := make(chan struct{})
	defer close(stop)
	go func() { // keepalive + revocation check
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if !s.Store.Exists(dev.ID) {
					s.cfg.Log.Printf("%s was revoked, closing", dev.Name)
					conn.Close()
					return
				}
				conn.Ping()
			}
		}
	}()

	for {
		conn.SetReadDeadline(time.Now().Add(45 * time.Second))
		raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var m message
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		if err := s.dispatch(sess, &m); err != nil {
			s.cfg.Log.Printf("input error: %v", err)
		}
	}
}

func clamp(v, lim int) int {
	if v > lim {
		return lim
	}
	if v < -lim {
		return -lim
	}
	return v
}

func (s *Server) dispatch(sess *session, m *message) error {
	in := s.cfg.Injector
	switch m.T {
	case "move":
		return in.Move(clamp(m.X, 4000), clamp(m.Y, 4000))
	case "scroll":
		return in.Scroll(clamp(m.X, 12000), clamp(m.Y, 12000))
	case "btn":
		b, ok := input.ParseButton(m.B)
		if !ok {
			return fmt.Errorf("bad button %q", m.B)
		}
		switch m.A {
		case "down", "up":
			down := m.A == "down"
			sess.buttons[b] = down
			return in.Button(b, down)
		case "click":
			if err := in.Button(b, true); err != nil {
				return err
			}
			sess.buttons[b] = false
			return in.Button(b, false)
		}
		return fmt.Errorf("bad button action %q", m.A)
	case "key": // tap a key, with optional modifiers, n times
		if !input.ValidKey(m.K) {
			return fmt.Errorf("bad key %q", m.K)
		}
		var mods []string
		for _, mod := range m.M {
			if input.Modifiers[mod] && !sess.keys[mod] {
				mods = append(mods, mod)
			}
		}
		n := m.N
		if n < 1 {
			n = 1
		}
		if n > 100 {
			n = 100
		}
		for _, mod := range mods {
			if err := in.Key(mod, true); err != nil {
				return err
			}
		}
		var err error
		for i := 0; i < n && err == nil; i++ {
			if err = in.Key(m.K, true); err == nil {
				err = in.Key(m.K, false)
			}
		}
		for i := len(mods) - 1; i >= 0; i-- {
			in.Key(mods[i], false)
		}
		return err
	case "keydown", "keyup": // hold / release (used for locked modifiers)
		if !input.ValidKey(m.K) {
			return fmt.Errorf("bad key %q", m.K)
		}
		down := m.T == "keydown"
		sess.keys[m.K] = down
		return in.Key(m.K, down)
	case "text":
		r := []rune(m.S)
		if len(r) > 4096 {
			r = r[:4096]
		}
		return in.Type(string(r))
	case "ping":
		s.send(sess.conn, map[string]string{"t": "pong"})
		return nil
	}
	return nil
}

// release lets go of anything still pressed when a phone disconnects.
func (s *Server) release(sess *session) {
	for b, down := range sess.buttons {
		if down {
			s.cfg.Injector.Button(b, false)
		}
	}
	for k, down := range sess.keys {
		if down {
			s.cfg.Injector.Key(k, false)
		}
	}
}

// LocalIPs returns the computer's private IPv4 addresses, best first.
// Virtual adapters (Docker, VMs, VPNs, WSL) are skipped.
func LocalIPs() []net.IP {
	ifaces, _ := net.Interfaces()
	skip := []string{"docker", "veth", "br-", "virbr", "vmnet", "vboxnet", "virtualbox", "vmware",
		"vethernet", "wsl", "hyper-v", "tailscale", "zerotier", "tun", "tap", "wg", "utun", "lxc", "podman"}
	type cand struct {
		ip    net.IP
		score int
	}
	var cands []cand
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(ifc.Name)
		virtual := false
		for _, p := range skip {
			if strings.Contains(name, p) {
				virtual = true
				break
			}
		}
		if virtual {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipn.IP.To4()
			if ip == nil || !ip.IsPrivate() {
				continue
			}
			score := 2
			switch {
			case ip[0] == 192 && ip[1] == 168:
				score = 0
			case ip[0] == 10:
				score = 1
			}
			cands = append(cands, cand{ip, score})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score < cands[j].score })
	var out []net.IP
	for _, c := range cands {
		out = append(out, c.ip)
	}
	return out
}
