package media

import (
	"testing"
	"unicode/utf8"
)

func FuzzNormalize(f *testing.F) {
	f.Add("Title", "Artist", "Microsoft.ZuneVideo_8wekyb3d8bbwe!App")
	f.Add("\x1b[2J", "!", "!")
	f.Fuzz(func(t *testing.T, title, artist, app string) {
		i := Normalize(Info{Title: title, Artist: artist, App: app})
		for _, s := range []string{i.Title, i.Artist, i.App} {
			for _, r := range s {
				if r < 0x20 || r == 0x7f {
					t.Fatalf("control character kept: %q", s)
				}
			}
		}
		if utf8.RuneCountInString(i.Title) > 200 || utf8.RuneCountInString(i.Artist) > 120 {
			t.Fatal("not bounded")
		}
	})
}
