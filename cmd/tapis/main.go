// Command tapis turns a phone into a trackpad and keyboard for this computer,
// over the local Wi-Fi network only.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"image/png"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/remigl/tapis/internal/firewall"
	"github.com/remigl/tapis/internal/input"
	"github.com/remigl/tapis/internal/lockscreen"
	"github.com/remigl/tapis/internal/media"
	"github.com/remigl/tapis/internal/qr"
	"github.com/remigl/tapis/internal/server"
	"github.com/remigl/tapis/internal/store"
	"github.com/remigl/tapis/internal/system"
	"github.com/remigl/tapis/web"
)

var version = "dev"

const (
	helpURL   = "https://github.com/remigl/tapis/blob/main/TROUBLESHOOTING.md"
	helpURLFR = "https://github.com/remigl/tapis/blob/main/DEPANNAGE.md"
)

func defaultDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "tapis")
}

func usage() {
	fmt.Fprintf(os.Stderr, `Tapis %s - use your phone as a trackpad and keyboard (local network only)

Usage:
  tapis [flags]          start the server
  tapis devices          list paired phones
  tapis revoke <id|all>  unpair a phone (or all of them)
  tapis version          print the version

Flags:
`, version)
	flag.PrintDefaults()
}

func main() {
	// Internal modes of the Windows lock-screen helper (see internal/lockscreen).
	if len(os.Args) == 2 {
		switch os.Args[1] {
		case "--lockscreen-service":
			lockscreen.RunService()
			return
		case "--lockscreen-agent":
			lockscreen.RunAgent()
			return
		}
	}
	port := flag.Int("port", 47800, "TCP port to listen on")
	dir := flag.String("config", defaultDir(), "configuration directory")
	layout := flag.String("layout", "auto", "keyboard layout of this computer, Linux only ("+strings.Join(input.LayoutNames(), ", ")+")")
	pair := flag.Bool("pair", false, "show a pairing QR code at startup even if phones are already paired")
	dry := flag.Bool("dry-run", false, "print input events instead of injecting them")
	noConfirm := flag.Bool("no-confirm", false, "pair new phones without asking for confirmation on this computer")
	noSmooth := flag.Bool("no-smooth", false, "disable pointer motion smoothing")
	noFW := flag.Bool("no-firewall", false, "do not configure Windows Firewall automatically")
	anyNet := flag.Bool("allow-any-network", false, "accept clients from non-private addresses (NOT recommended)")
	forceConsole := flag.Bool("console", false, "Windows: run in this console instead of the notification area")
	flag.Usage = usage
	if len(os.Args) > 1 {
		attachConsole() // Windows GUI build started from a terminal: print there
	}
	flag.Parse()

	switch flag.Arg(0) {
	case "":
	case "version":
		fmt.Println("tapis", version, runtime.GOOS+"/"+runtime.GOARCH)
		return
	case "devices":
		listDevices(*dir)
		return
	case "revoke":
		revoke(*dir, flag.Arg(1))
		return
	default:
		usage()
		os.Exit(2)
	}

	// On Windows, Tapis normally lives in the notification area; it uses
	// this console only when started from a terminal (or with --console).
	gui := guiAvailable() && !interactive() && !*forceConsole
	if gui && alreadyRunning() {
		return
	}
	var out io.Writer = os.Stdout
	if gui {
		out = openLog(*dir)
	}
	logger := log.New(out, "", log.Ldate|log.Ltime)
	fail := func(err error) {
		if gui {
			guiError(err)
		} else {
			fmt.Fprintln(os.Stderr, "error:", err)
			fmt.Fprintln(os.Stderr, "Help:", helpURL)
			pauseIfDoubleClicked()
		}
		os.Exit(1)
	}
	var inj input.Injector
	if *dry {
		inj = &input.Logger{W: out}
	} else {
		var err error
		if inj, err = input.New(input.Options{Layout: *layout}); err != nil {
			fail(err)
		}
	}
	inj = lockscreen.Wrap(inj, logger) // type on the Windows lock screen when the helper is installed
	defer inj.Close()

	cfg := server.Config{Port: *port, Dir: *dir, Injector: inj, Web: web.FS(), Log: logger,
		AllowAnyNetwork: *anyNet, Smooth: !*noSmooth, OnPairingUsed: func() { removeQR(*dir) }}
	if *dry {
		cfg.SystemAction = func(a system.Action) error { fmt.Println("[system]", a); return nil }
		cfg.MediaWatch = func(ctx context.Context, fn func(media.Info)) {
			fn(media.Info{Title: "Big Buck Bunny (demo)", Artist: "Blender Foundation", App: "Chrome", Playing: true})
		}
	}
	// New phones must be approved on this computer when someone is there
	// to answer (a console, or a dialog box in the Windows tray mode).
	switch {
	case *noConfirm:
	case gui:
		cfg.ConfirmPairing = guiConfirm
	case interactive():
		cfg.ConfirmPairing = approvals.ask
	}
	if gui {
		cfg.OnPairingUsed = guiPairingUsed
	}
	srv, err := server.New(cfg)
	if err != nil {
		fail(err)
	}
	if gui {
		runGUI(srv, logger, *port, *dir, *pair, *noFW)
		return
	}

	fmt.Printf("\nTapis %s - phone trackpad & keyboard\n\n", version)
	if len(srv.IPs) == 0 {
		fmt.Println("warning: no private network address found. Is this computer connected to Wi-Fi/Ethernet?")
	}
	for _, u := range srv.URLs() {
		fmt.Println("  Address:     ", u)
	}
	fmt.Println("  Certificate:  SHA-256", srv.Identity.Fingerprint())
	fmt.Println("  Authority:    SHA-256", srv.Identity.CAFingerprint())
	fmt.Println("  Config dir:  ", *dir)
	if !*noFW {
		st, err := firewall.Ensure(*port, func() {
			fmt.Println()
			fmt.Println("  Windows Firewall must allow phones on your local network to reach Tapis.")
			fmt.Println("  Windows will ask for administrator permission (first time only).")
			fmt.Println("  The rule only allows this program, on port", *port, "and from your local network.")
		})
		switch st {
		case firewall.AlreadyOK:
			fmt.Println("  Firewall:     OK (local network only)")
		case firewall.Configured:
			fmt.Println("  Firewall:     configured (local network only)")
		case firewall.Failed:
			fmt.Println("  Firewall:     NOT configured:", err)
			fmt.Println("                Your phone may not be able to connect. See", helpURL)
		}
	}
	fmt.Println()

	devs, _ := srv.Store.List()
	if len(devs) == 0 || *pair {
		showPairing(srv, *dir)
	} else {
		fmt.Printf("%d paired phone(s). Open the address above on your phone.\n", len(devs))
	}
	fmt.Println("Commands: [p]air a new phone, [d]evices, [r <id>] revoke, [q]uit")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	defer removeQR(*dir)
	go console(ctx, cancel, srv, *dir)

	if err := srv.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		fmt.Fprintln(os.Stderr, "Help:", helpURL)
		pauseIfDoubleClicked()
		os.Exit(1)
	}
}

func showPairing(srv *server.Server, dir string) {
	urls := srv.NewPairing()
	code, err := qr.Encode(urls[0])
	if err != nil {
		fmt.Println("Pairing URL:", urls[0])
		return
	}
	fmt.Println("Scan this QR code with your phone's camera to pair it:")
	fmt.Println()
	fmt.Print(code.Terminal())
	fmt.Println()
	fmt.Println("  or open:", urls[0])
	for _, u := range urls[1:] {
		fmt.Println("  other network:", u)
	}
	pngPath := filepath.Join(dir, "pairing-qr.png")
	if f, err := os.OpenFile(pngPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600); err == nil {
		png.Encode(f, code.Image(8))
		f.Close()
		fmt.Println("  QR image:", pngPath)
		// The image holds a secret: delete it once the code has expired.
		time.AfterFunc(5*time.Minute+time.Second, func() { removeQR(dir) })
	}
	fmt.Println("  This code works once and expires in 5 minutes.")
	fmt.Println("  The phone must be on the same Wi-Fi. Open the link in Chrome (or Safari on iPhone).")
	fmt.Println("  The browser warns about the certificate the first time: this is expected, it was")
	fmt.Println("  generated on this computer. Tap Advanced > Proceed. Problems? " + helpURL)
	fmt.Println()
}

func console(ctx context.Context, cancel func(), srv *server.Server, dir string) {
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		if approvals.answer(sc.Text()) {
			continue
		}
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		switch strings.ToLower(f[0]) {
		case "p", "pair":
			showPairing(srv, dir)
		case "d", "devices":
			listDevices(dir)
			if s := srv.Sessions(); len(s) > 0 {
				fmt.Println("Connected now:", strings.Join(s, ", "))
			}
		case "r", "revoke":
			if len(f) < 2 {
				fmt.Println("usage: r <id|all>")
				continue
			}
			devs, _ := srv.Store.List()
			revoke(dir, f[1])
			for _, d := range devs {
				if f[1] == "all" || d.ID == f[1] {
					srv.Disconnect(d.ID)
				}
			}
		case "q", "quit", "exit":
			cancel()
			return
		default:
			fmt.Println("Commands: [p]air a new phone, [d]evices, [r <id>] revoke, [q]uit")
		}
	}
}

func listDevices(dir string) {
	st, err := store.Open(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return
	}
	devs, err := st.List()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return
	}
	if len(devs) == 0 {
		fmt.Println("No paired phones.")
		return
	}
	fmt.Printf("%-10s %-28s %-17s %s\n", "ID", "NAME", "PAIRED", "LAST SEEN")
	for _, d := range devs {
		seen := "never"
		if !d.LastSeen.IsZero() {
			seen = d.LastSeen.Local().Format(time.DateTime)
		}
		fmt.Printf("%-10s %-28s %-17s %s\n", d.ID, d.Name, d.Created.Local().Format("2006-01-02 15:04"), seen)
	}
}

func revoke(dir, id string) {
	st, err := store.Open(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return
	}
	if id == "" {
		fmt.Println("usage: tapis revoke <id|all>")
		return
	}
	if id == "all" {
		if err := st.RevokeAll(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return
		}
		fmt.Println("All phones unpaired.")
		return
	}
	ok, err := st.Revoke(id)
	switch {
	case err != nil:
		fmt.Fprintln(os.Stderr, "error:", err)
	case !ok:
		fmt.Println("No phone with id", id)
	default:
		fmt.Println("Phone", id, "unpaired.")
	}
}

func removeQR(dir string) { os.Remove(filepath.Join(dir, "pairing-qr.png")) }

// approvals lets the console answer pairing requests.
var approvals approver

type approver struct {
	mu      sync.Mutex
	pending chan bool
}

func (a *approver) ask(name, ip string) bool {
	ch := make(chan bool, 1)
	a.mu.Lock()
	if a.pending != nil { // one request at a time
		a.mu.Unlock()
		return false
	}
	a.pending = ch
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.pending = nil; a.mu.Unlock() }()
	fmt.Printf("\n>>> New phone %q (%s) wants to pair. Allow it? Type y (yes) or n (no), then Enter: ", name, ip)
	select {
	case ok := <-ch:
		return ok
	case <-time.After(60 * time.Second):
		fmt.Println("\n>>> No answer: pairing refused.")
		return false
	}
}

// answer consumes a console line if a pairing request is waiting.
func (a *approver) answer(line string) bool {
	a.mu.Lock()
	ch := a.pending
	a.mu.Unlock()
	if ch == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "o", "oui":
		ch <- true
		fmt.Println(">>> Phone allowed.")
	case "n", "no", "non":
		ch <- false
		fmt.Println(">>> Phone refused.")
	default:
		fmt.Print(">>> Type y (yes) or n (no), then Enter: ")
		return true
	}
	a.mu.Lock()
	a.pending = nil
	a.mu.Unlock()
	return true
}
