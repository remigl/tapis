package dbus

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startBus runs a private dbus-daemon for the test (skipped if absent).
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

func TestBusRoundTrip(t *testing.T) {
	addr := startBus(t)
	a, err := Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if !strings.HasPrefix(a.UniqueName(), ":") {
		t.Fatalf("unique name %q", a.UniqueName())
	}
	// b exports an object; a calls it with a complex signature.
	b.Export("/test", func(m *Message) (string, []byte, string) {
		if m.Member != "Echo" {
			return "", nil, "org.freedesktop.DBus.Error.UnknownMethod"
		}
		var e Encoder
		e.String(m.Body[0].(string) + "!")
		v := m.Body[1].(Variant)
		e.Int32(v.Value.(int32) * 2)
		e.Array(8, func() {
			for _, it := range m.Body[2].([]any) {
				kv := it.([]any)
				e.Struct(func() { e.String(kv[0].(string)); e.Variant("b", func() { e.Bool(kv[1].(Variant).Value.(bool)) }) })
			}
		})
		return "sia{sv}", e.Bytes(), ""
	})
	var e Encoder
	e.String("hi")
	e.Variant("i", func() { e.Int32(21) })
	e.Array(8, func() {
		e.Struct(func() { e.String("enabled"); e.Variant("b", func() { e.Bool(true) }) })
	})
	r, err := a.Call(b.UniqueName(), "/test", "x.Test", "Echo", "sva{sv}", e.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if r.Body[0] != "hi!" || r.Body[1] != int32(42) {
		t.Fatalf("reply %#v", r.Body)
	}
	kv := r.Body[2].([]any)[0].([]any)
	if kv[0] != "enabled" || kv[1].(Variant).Value != true {
		t.Fatalf("dict %#v", kv)
	}
	if _, err := a.Call(b.UniqueName(), "/test", "x.Test", "Nope", "", nil); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := a.Call(b.UniqueName(), "/missing", "x.Test", "Echo", "", nil); err == nil || !strings.Contains(err.Error(), "UnknownObject") {
		t.Fatalf("expected UnknownObject, got %v", err)
	}
}

func FuzzDecode(f *testing.F) {
	f.Add([]byte{1, 0, 0, 0, 'a', 0}, "s")
	f.Add([]byte{0, 0, 0, 0}, "a{sv}")
	f.Fuzz(func(t *testing.T, b []byte, sig string) {
		if len(sig) > 64 {
			return
		}
		decodeBody(b, sig, 0) // must not panic
	})
}
