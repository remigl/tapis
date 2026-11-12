// Package server is the HTTPS + WebSocket server that the phone talks to.
package server

import (
	"bufio"
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
	"syscall"
	"time"
	"unicode"

	"github.com/remigl/tapis/internal/certs"
	"github.com/remigl/tapis/internal/input"
	"github.com/remigl/tapis/internal/media"
	"github.com/remigl/tapis/internal/store"
	"github.com/remigl/tapis/internal/system"
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
	// Smooth enables pointer motion smoothing.
	Smooth bool
	// ConfirmPairing, if set, is asked to approve each new phone (on the
	// computer's side) before it is paired. It may block for a while.
	// The code is also displayed on the phone, so the user can check that
	// the request really comes from their phone.
	ConfirmPairing func(name, ip, code string) bool
	// OnPairingUsed is called when the current pairing code is consumed.
	OnPairingUsed func()
	// SystemAction performs lock/sleep (defaults to system.Do).
	SystemAction func(system.Action) error
	// MediaWatch reports what is playing (defaults to media.Watch).
	MediaWatch func(context.Context, func(media.Info))
}

// Server serves the phone web app and the control WebSocket.
type Server struct {
	cfg      Config
	Store    *store.Store
	Identity *certs.Identity
	IPs      []net.IP
	hostname string

	mu          sync.Mutex
	mover       *mover
	nowPlaying  media.Info
	mediaStop   context.CancelFunc
	pairToken   string
	pairExpires time.Time
	fails       map[string]*failInfo
	pairFails   int
	conns       int
	connsByIP   map[string]int
	notes       map[string]time.Time
	sessions    map[*session]struct{}
	rawConns    int
	rawByIP     map[string]int

	netMu      sync.Mutex
	nets       []*net.IPNet // subnets Tapis serves (refreshed periodically)
	ownIPs     []net.IP
	netsLoaded time.Time
}

type failInfo struct {
	count int
	until time.Time
}

const (
	pairingTTL  = 5 * time.Minute
	maxFailures = 5
	lockout     = time.Minute

	maxPairFailures = 20   // wrong codes before the current code is cancelled
	maxConns        = 16   // simultaneous WebSocket connections
	maxConnsPerIP   = 4    // simultaneous WebSocket connections from one address
	maxTracked      = 1024 // bound on per-address bookkeeping
	confirmTimeout  = 60 * time.Second
	maxRawConns     = 256 // simultaneous TCP connections
	maxRawPerIP     = 32  // simultaneous TCP connections from one address
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
	return &Server{cfg: cfg, Store: st, Identity: id, IPs: ips, hostname: host, mover: newMover(cfg.Injector, cfg.Smooth),
		fails: map[string]*failInfo{}, sessions: map[*session]struct{}{}, connsByIP: map[string]int{}, rawByIP: map[string]int{}}, nil
}

// NewPairing creates a fresh single-use pairing token and returns the URLs
// (one per local address) that embed it. The first URL is the preferred one.
func (s *Server) NewPairing() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pairToken = store.RandomToken(16)
	s.pairExpires = time.Now().Add(pairingTTL)
	s.pairFails = 0
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

	tlsConf := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{s.Identity.TLS},
		NextProtos:   []string{"http/1.1"},
	}
	srv := &http.Server{
		Handler: s.guard(mux),
		// HTTP/1.1 only: WebSockets over HTTP/2 (RFC 8441) are not needed.
		TLSNextProto:      map[string]func(*http.Server, *tls.Conn, http.Handler){},
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second, // requests have no body; WebSockets clear it
		MaxHeaderBytes:    16 << 10,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          log.New(&handshakeLog{s: s}, "", 0),
	}
	// Someone typing the address without "https://" gets redirected
	// instead of a confusing error. Both protocols share the same port.
	redirect := &http.Server{
		Handler: s.guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://"+r.Host+r.URL.RequestURI(), http.StatusFound)
		})),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       10 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          log.New(io.Discard, "", 0),
	}

	ln, err := net.Listen("tcp", ":"+strconv.Itoa(s.cfg.Port))
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) || strings.Contains(err.Error(), "address already in use") ||
			strings.Contains(err.Error(), "Only one usage") {
			return fmt.Errorf("port %d is already in use. Is Tapis already running in another window? "+
				"Close it, or start Tapis with another port, e.g. --port 47801", s.cfg.Port)
		}
		return err
	}
	tlsLn, httpLn := newChanListener(ln.Addr()), newChanListener(ln.Addr())
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				tlsLn.Close()
				httpLn.Close()
				return
			}
			host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
			rip := net.ParseIP(host)
			if !s.allowedClient(rip) || !s.acquireRaw(host) {
				c.Close() // not even a TLS handshake for outsiders or floods
				continue
			}
			c = &countedConn{Conn: c, release: func() { s.releaseRaw(host) }}
			go func() {
				c.SetReadDeadline(time.Now().Add(10 * time.Second))
				br := bufio.NewReader(c)
				first, err := br.Peek(1)
				c.SetReadDeadline(time.Time{})
				if err != nil {
					c.Close()
					return
				}
				pc := &peekConn{Conn: c, r: br}
				if first[0] == 0x16 { // TLS handshake record
					tlsLn.push(tls.Server(pc, tlsConf))
				} else {
					httpLn.push(pc)
				}
			}()
		}
	}()

	go func() {
		<-ctx.Done()
		ln.Close()
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		redirect.Shutdown(sctx)
		srv.Shutdown(sctx)
		s.mu.Lock()
		for sess := range s.sessions {
			sess.conn.Close()
		}
		s.mu.Unlock()
	}()
	go redirect.Serve(httpLn)
	err = srv.Serve(tlsLn)
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
		return nil
	}
	return err
}

// noteOnce logs msg at most once per key and minute, to help users see
// what reaches Tapis without flooding the console.
func (s *Server) noteOnce(key, msg string) {
	s.mu.Lock()
	if s.notes == nil || len(s.notes) >= maxTracked {
		s.notes = map[string]time.Time{}
	}
	last, seen := s.notes[key]
	if seen && time.Since(last) < time.Minute {
		s.mu.Unlock()
		return
	}
	s.notes[key] = time.Now()
	s.mu.Unlock()
	s.cfg.Log.Print(msg)
}

// handshakeLog turns Go's TLS handshake errors into hints: a phone whose
// browser has not accepted the certificate yet shows up here.
type handshakeLog struct{ s *Server }

func (h *handshakeLog) Write(p []byte) (int, error) {
	line := string(p)
	if i := strings.Index(line, "TLS handshake error from "); i >= 0 {
		rest := line[i+len("TLS handshake error from "):]
		host, _, _ := net.SplitHostPort(strings.SplitN(rest, ": ", 2)[0])
		if ip := net.ParseIP(host); ip != nil && !ip.IsLoopback() {
			if strings.Contains(rest, "certificate") || strings.Contains(rest, "EOF") || strings.Contains(rest, "reset") {
				h.s.noteOnce("tls"+host, "device "+host+" reached Tapis; its browser must now accept the certificate (Advanced > Proceed)")
			}
		}
	}
	return len(p), nil
}

func (s *Server) acquireRaw(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rawConns >= maxRawConns || s.rawByIP[ip] >= maxRawPerIP {
		return false
	}
	s.rawConns++
	s.rawByIP[ip]++
	return true
}

func (s *Server) releaseRaw(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rawConns--
	if s.rawByIP[ip]--; s.rawByIP[ip] <= 0 {
		delete(s.rawByIP, ip)
	}
}

// countedConn releases its slot exactly once when closed.
type countedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *countedConn) Close() error {
	c.once.Do(c.release)
	return c.Conn.Close()
}

// peekConn is a connection whose first bytes were already buffered.
type peekConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *peekConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// chanListener hands connections sorted by the accept loop to an http.Server.
type chanListener struct {
	addr net.Addr
	ch   chan net.Conn
	done chan struct{}
	once sync.Once
}

func newChanListener(a net.Addr) *chanListener {
	return &chanListener{addr: a, ch: make(chan net.Conn), done: make(chan struct{})}
}

func (l *chanListener) push(c net.Conn) {
	select {
	case l.ch <- c:
	case <-l.done:
		c.Close()
	}
}

func (l *chanListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *chanListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *chanListener) Addr() net.Addr { return l.addr }

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

// served returns the subnets of the network interfaces Tapis is offered
// on (the ones shown in its addresses), and those addresses. It is
// refreshed every 15 seconds so that changing Wi-Fi network just works.
func (s *Server) served() ([]*net.IPNet, []net.IP) {
	s.netMu.Lock()
	defer s.netMu.Unlock()
	if time.Since(s.netsLoaded) < 15*time.Second && s.netsLoaded.After(time.Time{}) {
		return s.nets, s.ownIPs
	}
	s.nets, s.ownIPs = servedNets()
	s.netsLoaded = time.Now()
	return s.nets, s.ownIPs
}

func servedNets() ([]*net.IPNet, []net.IP) {
	ips := LocalIPs()
	var nets []*net.IPNet
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			for _, ip := range ips {
				if ipn.IP.Equal(ip) {
					nets = append(nets, &net.IPNet{IP: ipn.IP.Mask(ipn.Mask), Mask: ipn.Mask})
				}
			}
		}
	}
	return nets, ips
}

// allowedClient accepts loopback and devices on the same subnet as one of
// the addresses Tapis is offered on. Peers reached through a VPN, a
// virtual machine network or a router are refused, even if their address
// is private.
func (s *Server) allowedClient(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if s.cfg.AllowAnyNetwork || ip.IsLoopback() {
		return true
	}
	nets, _ := s.served()
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// validHost accepts IP literals of local networks, localhost and our
// mDNS-style name. Domain names that could be re-pointed by an attacker
// (DNS rebinding) are refused.
func (s *Server) validHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip != nil {
		if s.cfg.AllowAnyNetwork || ip.IsLoopback() {
			return true
		}
		_, own := s.served()
		for _, o := range own {
			if o.Equal(ip) {
				return true
			}
		}
		return false
	}
	host = strings.ToLower(host)
	return host == "localhost" || host == s.Identity.LeafCert.Subject.CommonName
}

// guard rejects non-local clients and adds security headers.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allowedClient(remoteIP(r)) {
			s.noteOnce("net"+r.RemoteAddr, "rejected connection from "+r.RemoteAddr+" (not on the local network Tapis serves)")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if !s.validHost(r.Host) {
			// Blocks DNS-rebinding attacks: only IP addresses and our own
			// names are accepted, never an arbitrary domain.
			s.noteOnce("host"+r.Host, "rejected request for unexpected host "+strconv.Quote(r.Host))
			http.Error(w, "unexpected host", http.StatusMisdirectedRequest)
			return
		}
		if r.URL.Path == "/" {
			ip := remoteIP(r).String()
			s.noteOnce("page"+ip, "device "+ip+" opened the Tapis page")
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; "+
			"img-src 'self' data:; manifest-src 'self'; connect-src 'self' wss://"+r.Host+"; "+
			"base-uri 'none'; form-action 'none'; frame-ancestors 'none'; require-trusted-types-for 'script'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=(), bluetooth=(), interest-cohort=()")
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
		if len(s.fails) >= maxTracked { // drop stale entries
			for k, v := range s.fails {
				if time.Now().After(v.until) {
					delete(s.fails, k)
				}
			}
		}
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
		// Even across many addresses, a code can only be guessed so often.
		if s.pairFails++; s.pairFails >= maxPairFailures {
			s.pairToken = ""
			s.cfg.Log.Printf("too many wrong pairing codes: the current code was cancelled (type p for a new one)")
		}
		return false
	}
	s.pairToken = ""
	if s.cfg.OnPairingUsed != nil {
		go s.cfg.OnPairingUsed()
	}
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
	s.mu.Lock()
	if s.conns >= maxConns || s.connsByIP[ip] >= maxConnsPerIP {
		s.mu.Unlock()
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}
	s.conns++
	s.connsByIP[ip]++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.conns--
		if s.connsByIP[ip]--; s.connsByIP[ip] <= 0 {
			delete(s.connsByIP, ip)
		}
		s.mu.Unlock()
	}()
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
		name := cleanName(m.Name)
		if s.cfg.ConfirmPairing != nil {
			code := store.RandomDigits(6)
			s.send(conn, map[string]string{"t": "confirm", "code": code})
			conn.SetReadDeadline(time.Now().Add(confirmTimeout + 5*time.Second))
			if !s.cfg.ConfirmPairing(name, ip, code) {
				s.cfg.Log.Printf("pairing of %q from %s refused on this computer", name, ip)
				s.send(conn, map[string]string{"t": "error", "code": "refused"})
				return
			}
		}
		d, key, err := s.Store.Add(name)
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
	s.startMediaLocked()
	now := s.nowPlaying
	s.mu.Unlock()
	s.send(conn, mediaMessage(now))
	s.cfg.Log.Printf("%s connected from %s", dev.Name, ip)
	defer func() {
		s.release(sess)
		s.mu.Lock()
		delete(s.sessions, sess)
		if len(s.sessions) == 0 && s.mediaStop != nil {
			s.mediaStop() // nobody is watching: stop polling the system
			s.mediaStop = nil
			s.nowPlaying = media.Info{}
		}
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

func mediaMessage(i media.Info) map[string]any {
	return map[string]any{"t": "media", "title": i.Title, "artist": i.Artist, "app": i.App, "playing": i.Playing}
}

// startMediaLocked starts watching what is playing, once. s.mu must be held.
func (s *Server) startMediaLocked() {
	if s.mediaStop != nil {
		return
	}
	watch := s.cfg.MediaWatch
	if watch == nil {
		watch = media.Watch
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.mediaStop = cancel
	go watch(ctx, func(i media.Info) {
		s.mu.Lock()
		if ctx.Err() != nil {
			s.mu.Unlock()
			return
		}
		s.nowPlaying = i
		conns := make([]*ws.Conn, 0, len(s.sessions))
		for sess := range s.sessions {
			conns = append(conns, sess.conn)
		}
		s.mu.Unlock()
		msg := mediaMessage(i)
		for _, c := range conns {
			s.send(c, msg)
		}
	})
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
	if m.T == "move" {
		return s.mover.Add(clamp(m.X, 4000), clamp(m.Y, 4000))
	}
	s.mover.Flush() // everything else happens where the pointer really is
	switch m.T {
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
	case "forget": // the phone forgot this computer: drop its key here too
		if _, err := s.Store.Revoke(sess.dev.ID); err != nil {
			return err
		}
		s.cfg.Log.Printf("%s unpaired itself", sess.dev.Name)
		sess.conn.Close()
		return nil
	case "sys":
		a := system.Action(m.A)
		if !system.Valid(a) {
			return fmt.Errorf("bad system action %q", m.A)
		}
		s.cfg.Log.Printf("%s requested: %s", sess.dev.Name, a)
		s.send(sess.conn, map[string]string{"t": "sys", "a": m.A})
		// Let the reply reach the phone before the screen locks or sleeps.
		go func() {
			time.Sleep(300 * time.Millisecond)
			do := s.cfg.SystemAction
			if do == nil {
				do = system.Do
			}
			if err := do(a); err != nil {
				s.cfg.Log.Printf("%s failed: %v", a, err)
			}
		}()
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
	// The address used by the default route is almost always the Wi-Fi or
	// Ethernet one. A UDP "connect" only asks the OS routing table: no
	// packet is sent.
	var routed net.IP
	if c, err := net.Dial("udp4", "8.8.8.8:53"); err == nil {
		if a, ok := c.LocalAddr().(*net.UDPAddr); ok {
			routed = a.IP.To4()
		}
		c.Close()
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
			case routed != nil && ip.Equal(routed):
				score = -1
			case virtual:
				continue
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
