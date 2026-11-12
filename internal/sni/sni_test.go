package sni

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/remigl/tapis/internal/dbus"
)

func startBus(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon not installed")
	}
	dir := t.TempDir()
	sock := filepath.Join(dir, "bus")
	conf := filepath.Join(dir, "bus.conf")
	os.WriteFile(conf, []byte(`<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig><type>session</type><listen>unix:path=`+sock+`</listen>
<auth>EXTERNAL</auth><policy context="default"><allow send_destination="*" eavesdrop="true"/>
<allow eavesdrop="true"/><allow own="*"/></policy></busconfig>`), 0o600)
	cmd := exec.Command(bin, "--config-file="+conf, "--nofork", "--nopidfile")
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return "unix:path=" + sock
}

func TestTray(t *testing.T) {
	addr := startBus(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)

	if _, err := Start(Options{Title: "Tapis"}); err == nil {
		t.Fatal("expected an error without a tray host")
	}

	// A fake panel: the StatusNotifierWatcher.
	host, err := dbus.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	var e dbus.Encoder
	e.String(watcher)
	e.Uint32(4)
	host.Call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "RequestName", "su", e.Bytes())
	registered := make(chan string, 1)
	host.Export("/StatusNotifierWatcher", func(m *dbus.Message) (string, []byte, string) {
		if m.Member == "RegisterStatusNotifierItem" {
			registered <- m.Body[0].(string)
		}
		return "", nil, ""
	})

	clicked := make(chan string, 4)
	icon, _ := os.ReadFile("../../assets/logo-256.png")
	tr, err := Start(Options{
		Title:    "Tapis",
		IconPNGs: [][]byte{icon},
		Tooltip:  func() string { return "2 phones connected" },
		Activate: func() { clicked <- "activate" },
		Menu: func() []Item {
			return []Item{
				{Label: "2 phones connected:", Disabled: true},
				{Label: "    Pixel 8", Disabled: true},
				{Sep: true},
				{Label: "Show the QR code", OnClick: func() { clicked <- "qr" }},
				{Label: "Start at login", Toggle: true, Checked: true, OnClick: func() { clicked <- "autostart" }},
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	name := <-registered

	r, err := host.Call(name, itemPath, propIface, "GetAll", "s", func() []byte { var e dbus.Encoder; e.String(itemIface); return e.Bytes() }())
	if err != nil {
		t.Fatal(err)
	}
	props := map[string]any{}
	for _, kv := range r.Body[0].([]any) {
		f := kv.([]any)
		props[f[0].(string)] = f[1].(dbus.Variant).Value
	}
	if props["Title"] != "Tapis" || props["Menu"] != dbus.ObjectPath(menuPath) {
		t.Fatalf("props %v", props)
	}
	pix := props["IconPixmap"].([]any)[0].([]any)
	if pix[0] != int32(256) || len(pix[2].([]any)) != 256*256*4 {
		t.Fatalf("bad pixmap %v %v", pix[0], len(pix[2].([]any)))
	}
	tip := props["ToolTip"].([]any)
	if tip[3] != "2 phones connected" {
		t.Fatalf("tooltip %v", tip)
	}

	var g dbus.Encoder
	g.Int32(0)
	g.Int32(-1)
	g.Array(4, func() {})
	r, err = host.Call(name, menuPath, menuIface, "GetLayout", "iias", g.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	children := r.Body[1].([]any)[2].([]any)
	if len(children) != 5 {
		t.Fatalf("%d menu items", len(children))
	}
	first := children[1].(dbus.Variant).Value.([]any)
	labels := map[string]any{}
	for _, kv := range first[1].([]any) {
		f := kv.([]any)
		labels[f[0].(string)] = f[1].(dbus.Variant).Value
	}
	if labels["label"] != "    Pixel 8" || labels["enabled"] != false {
		t.Fatalf("item %v", labels)
	}

	click := func(id int32) {
		var ev dbus.Encoder
		ev.Int32(id)
		ev.String("clicked")
		ev.Variant("i", func() { ev.Int32(0) })
		ev.Uint32(0)
		if _, err := host.Call(name, menuPath, menuIface, "Event", "isvu", ev.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	click(2) // disabled: nothing
	click(4)
	if got := <-clicked; got != "qr" {
		t.Fatalf("clicked %q", got)
	}
	var a dbus.Encoder
	a.Int32(0)
	a.Int32(0)
	host.Call(name, itemPath, itemIface, "Activate", "ii", a.Bytes())
	if got := <-clicked; got != "activate" {
		t.Fatalf("activate %q", got)
	}
}
