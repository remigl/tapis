package input

import "testing"

func TestLayouts(t *testing.T) {
	cases := []struct {
		layout string
		r      rune
		want   []stroke
	}{
		{"us", 'a', []stroke{{30, 1}}},
		{"us", 'A', []stroke{{30, 2}}},
		{"us", '@', []stroke{{3, 2}}},
		{"us", '|', []stroke{{43, 2}}},
		{"us", '¶', nil},
		{"be", 'a', []stroke{{16, 1}}},
		{"be", 'q', []stroke{{30, 1}}},
		{"be", 'm', []stroke{{39, 1}}},
		{"be", '1', []stroke{{2, 2}}},
		{"be", '@', []stroke{{3, 3}}},
		{"be", '|', []stroke{{2, 3}}},
		{"be", '\\', []stroke{{12, 3}}},
		{"be", 'ê', []stroke{{26, 1}, {18, 1}}},
		{"be", 'Ë', []stroke{{26, 2}, {18, 2}}},
		{"be", '~', []stroke{{53, 3}, {57, 1}}},
		{"be", 'œ', []stroke{{24, 3}}},
		{"be", ',', []stroke{{50, 1}}},
		{"fr", '~', []stroke{{41, 2}}},
		{"fr", '<', []stroke{{86, 1}}},
		{"fr", '!', []stroke{{53, 1}}},
		{"fr", 'û', []stroke{{26, 1}, {22, 1}}},
		{"fr", ' ', []stroke{{57, 1}}},
	}
	for _, c := range cases {
		l, err := LoadLayout(c.layout)
		if err != nil {
			t.Fatal(err)
		}
		got := l.Lookup(c.r)
		if len(got) != len(c.want) {
			t.Errorf("%s %q: got %v want %v", c.layout, c.r, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s %q: got %v want %v", c.layout, c.r, got, c.want)
			}
		}
	}
	// every printable ASCII char must be typeable on each layout
	for _, name := range LayoutNames() {
		l, _ := LoadLayout(name)
		for r := rune(32); r < 127; r++ {
			if l.Lookup(r) == nil {
				t.Errorf("%s: %q not reachable", name, r)
			}
		}
	}
}
