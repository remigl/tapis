//go:build linux

package media

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

// Linux players publish their state over D-Bus (MPRIS). We read it with
// busctl, which ships with systemd, so no extra dependency is needed.

type variant struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func busctl(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "busctl", append([]string{"--user", "--json=short"}, args...)...).Output()
}

func players() []string {
	out, err := busctl("list", "--acquired")
	if err != nil {
		return nil
	}
	var list []struct {
		Name string `json:"name"`
	}
	json.Unmarshal(out, &list)
	var names []string
	for _, l := range list {
		if strings.HasPrefix(l.Name, "org.mpris.MediaPlayer2.") {
			names = append(names, l.Name)
		}
	}
	return names
}

func property(name, prop string) (variant, bool) {
	out, err := busctl("get-property", name, "/org/mpris/MediaPlayer2", "org.mpris.MediaPlayer2.Player", prop)
	if err != nil {
		return variant{}, false
	}
	var v variant
	return v, json.Unmarshal(out, &v) == nil
}

func current() Info {
	var paused Info
	for _, name := range players() {
		st, ok := property(name, "PlaybackStatus")
		if !ok {
			continue
		}
		var status string
		json.Unmarshal(st.Data, &status)
		if status != "Playing" && status != "Paused" {
			continue
		}
		md, ok := property(name, "Metadata")
		if !ok {
			continue
		}
		var fields map[string]variant
		json.Unmarshal(md.Data, &fields)
		i := Info{App: strings.TrimPrefix(name, "org.mpris.MediaPlayer2."), Playing: status == "Playing"}
		if t, ok := fields["xesam:title"]; ok {
			json.Unmarshal(t.Data, &i.Title)
		}
		if a, ok := fields["xesam:artist"]; ok {
			var artists []string
			if json.Unmarshal(a.Data, &artists) == nil && len(artists) > 0 {
				i.Artist = strings.Join(artists, ", ")
			}
		}
		if i.Title == "" {
			continue
		}
		if i.Playing {
			return i
		}
		if paused.Title == "" {
			paused = i
		}
	}
	return paused
}

func watch(ctx context.Context, fn func(Info)) {
	if _, err := exec.LookPath("busctl"); err != nil {
		return
	}
	poll(ctx, 1500*time.Millisecond, current, fn)
}
