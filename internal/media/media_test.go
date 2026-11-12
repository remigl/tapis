package media

import "testing"

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"Chrome": "Chrome", "MSEdge": "Edge", "308046B0AF4A39CB": "Firefox", "Spotify.exe": "Spotify",
		"Microsoft.ZuneVideo_8wekyb3d8bbwe!Microsoft.ZuneVideo": "Films & TV", "chromium.instance4242": "Chromium",
		"vlc": "VLC", `C:\Program Files\Foo\foo.exe`: "foo", "!weird": "!weird", "": "",
	} {
		if got := appName(in); got != want {
			t.Errorf("appName(%q) = %q, want %q", in, got, want)
		}
	}
	if (Normalize(Info{Title: "  ", App: "x"}) != Info{}) {
		t.Error("empty title should give empty info")
	}
	if got := Normalize(Info{Title: "a\x1b[31mb"}).Title; got != "a[31mb" {
		t.Errorf("control characters not removed: %q", got)
	}
}
