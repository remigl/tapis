// Package media reports what is currently playing on the computer (the
// same information the system shows in its media overlay or lock screen).
//
//   - Windows: System Media Transport Controls (browsers, Spotify, the
//     Films & TV app, VLC 4…).
//   - Linux: MPRIS over D-Bus (almost every Linux player and browser).
//
// Nothing is sent anywhere but to paired phones.
package media

import (
	"context"
	"strings"
	"time"
)

// Info describes the current media. An empty Title means "nothing".
type Info struct {
	Title   string `json:"title"`
	Artist  string `json:"artist,omitempty"`
	App     string `json:"app,omitempty"`
	Playing bool   `json:"playing"`
}

// Watch calls fn with the current media whenever it changes, until ctx is
// cancelled. It returns immediately if the platform is not supported.
func Watch(ctx context.Context, fn func(Info)) { watch(ctx, fn) }

func clean(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	if r := []rune(s); len(r) > max {
		s = string(r[:max-1]) + "…"
	}
	return s
}

// Normalize trims and bounds the fields, and gives apps a readable name.
func Normalize(i Info) Info {
	i.Title = clean(i.Title, 200)
	i.Artist = clean(i.Artist, 120)
	i.App = appName(i.App)
	if i.Title == "" {
		return Info{}
	}
	return i
}

var knownApps = map[string]string{
	"chrome": "Chrome", "chromium": "Chromium", "msedge": "Edge", "308046b0af4a39cb": "Firefox",
	"firefox": "Firefox", "spotify": "Spotify", "vlc": "VLC", "brave": "Brave", "opera": "Opera",
	"microsoft.zunevideo": "Films & TV", "microsoft.zunemusic": "Media Player", "mpv": "mpv",
	"rhythmbox": "Rhythmbox", "totem": "Videos", "celluloid": "Celluloid", "elisa": "Elisa",
	"strawberry": "Strawberry", "audacious": "Audacious", "kodi": "Kodi",
}

func appName(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	low := strings.ToLower(id)
	if strings.Contains(low, "!") { // UWP: Package_publisherhash!App
		if j := strings.Index(low, "_"); j > 0 {
			low = low[:j]
		} else {
			low = low[strings.LastIndex(low, "!")+1:]
		}
	}
	low = strings.TrimSuffix(low, ".exe")
	if i := strings.Index(low, ".instance"); i > 0 { // MPRIS: chromium.instance1234
		low = low[:i]
	}
	if i := strings.LastIndexAny(low, `\/`); i >= 0 {
		low = low[i+1:]
	}
	if n, ok := knownApps[low]; ok {
		return n
	}
	for k, n := range knownApps {
		if strings.HasPrefix(low, k) {
			return n
		}
	}
	base := id
	if i := strings.LastIndexAny(base, `\/`); i >= 0 {
		base = base[i+1:]
	}
	if strings.HasSuffix(strings.ToLower(base), ".exe") {
		base = base[:len(base)-4]
	}
	return clean(base, 40)
}

// poll is a helper for backends that sample periodically.
func poll(ctx context.Context, every time.Duration, get func() Info, fn func(Info)) {
	var last Info
	first := true
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		cur := Normalize(get())
		if first || cur != last {
			fn(cur)
			last, first = cur, false
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
