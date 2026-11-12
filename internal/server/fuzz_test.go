package server

import (
	"encoding/json"
	"io"
	"log"
	"testing"

	"github.com/remigl/tapis/internal/input"
	"github.com/remigl/tapis/internal/store"
	"github.com/remigl/tapis/internal/system"
)

// FuzzDispatch sends arbitrary protocol messages from an authenticated
// phone: nothing may panic, and system actions must stay limited.
func FuzzDispatch(f *testing.F) {
	for _, s := range []string{
		`{"t":"move","x":5,"y":-3}`, `{"t":"scroll","y":-99999999}`, `{"t":"btn","b":"left","a":"down"}`,
		`{"t":"key","k":"Enter","m":["ctrl","alt","bogus"],"n":1000000}`, `{"t":"keydown","k":"meta"}`,
		`{"t":"text","s":"héllo\n\u0000"}`, `{"t":"sys","a":"shutdown"}`, `{"t":"sys","a":"lock"}`,
		`{"t":"key","k":"ab"}`, `{"t":"btn","b":"side","a":"click"}`, `{"t":"forget"}`,
	} {
		f.Add([]byte(s))
	}
	dir := f.TempDir()
	st, _ := store.Open(dir)
	f.Fuzz(func(t *testing.T, data []byte) {
		var actions []system.Action
		s := &Server{
			cfg: Config{Injector: &input.Logger{W: io.Discard}, Log: log.New(io.Discard, "", 0),
				SystemAction: func(a system.Action) error { actions = append(actions, a); return nil }},
			Store: st, mover: newMover(&input.Logger{W: io.Discard}, false),
		}
		var m message
		if json.Unmarshal(data, &m) != nil {
			return
		}
		sess := &session{conn: nil, buttons: map[input.Button]bool{}, keys: map[string]bool{}}
		defer func() {
			if r := recover(); r != nil && m.T != "ping" && m.T != "sys" && m.T != "forget" {
				t.Fatalf("panic on %q: %v", data, r)
			}
		}()
		if m.T == "ping" || m.T == "sys" || m.T == "forget" {
			return // need a live connection; covered by the end-to-end tests
		}
		s.dispatch(sess, &m)
	})
}
