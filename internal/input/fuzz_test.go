package input

import "testing"

func FuzzLayouts(f *testing.F) {
	f.Add("Bonjour ê ~ @ |")
	f.Fuzz(func(t *testing.T, s string) {
		for _, name := range LayoutNames() {
			l, _ := LoadLayout(name)
			for _, r := range s {
				for _, st := range l.Lookup(r) {
					if st.level < 1 || st.level > 4 || st.code == 0 || st.code > 255 {
						t.Fatalf("%s %q: bad stroke %+v", name, r, st)
					}
				}
			}
		}
		ValidKey(s)
	})
}
