package update

import "testing"

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in   string
		want Version
		ok   bool
	}{
		{"v1.7.0", Version{1, 7, 0}, true},
		{"1.7.0", Version{1, 7, 0}, true},
		{"V2.0.1", Version{2, 0, 1}, true},
		{"v0.0.0", Version{0, 0, 0}, true},
		{"v10.20.30", Version{10, 20, 30}, true},
		// Not comparable: dev builds, snapshots, hypothetical pre-releases.
		{"dev", Version{}, false},
		{"unknown", Version{}, false},
		{"v1.6.1-next", Version{}, false},
		{"1.7.0-rc1", Version{}, false},
		{"", Version{}, false},
		{"v", Version{}, false},
		{"v1.7", Version{}, false},
		{"v1.7.0.4", Version{}, false},
		{"v1.7.x", Version{}, false},
		{"v1..0", Version{}, false},
		{"v1.7.-1", Version{}, false},
		{"v99999999999999999999.0.0", Version{}, false},
	}
	for _, tc := range cases {
		got, ok := ParseVersion(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestVersionString(t *testing.T) {
	if got := (Version{1, 7, 0}).String(); got != "v1.7.0" {
		t.Errorf("String() = %q, want v1.7.0", got)
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.7.0", "v1.7.0", 0},
		{"1.7.0", "v1.7.0", 0},
		{"v1.7.0", "v1.7.1", -1},
		{"v1.8.0", "v1.7.9", 1},
		{"v2.0.0", "v1.99.99", 1},
		{"v0.4.2", "v0.10.0", -1},
		{"v1.0.0", "v1.0.1", -1},
	}
	for _, tc := range cases {
		a, aok := ParseVersion(tc.a)
		b, bok := ParseVersion(tc.b)
		if !aok || !bok {
			t.Fatalf("ParseVersion(%q/%q) unexpectedly unparsable", tc.a, tc.b)
		}
		if got := a.Compare(b); got != tc.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
		if got := b.Compare(a); got != -tc.want {
			t.Errorf("Compare(%q, %q) = %d, want %d (antisymmetry)", tc.b, tc.a, got, -tc.want)
		}
	}
}
