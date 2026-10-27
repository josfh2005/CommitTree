package update

import "testing"

func TestParseVersion(t *testing.T) {
	ok := map[string]Version{
		"0.9.0":   {0, 9, 0},
		"10.2.33": {10, 2, 33},
	}
	for s, want := range ok {
		got, err := ParseVersion(s)
		if err != nil || got != want {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v", s, got, err, want)
		}
	}
	for _, s := range []string{"v0.9.0", "0.9", "0.9.0-beta", "", "a.b.c", "1.2.3.4", "1..3", "-1.0.0", "+1.0.0"} {
		if _, err := ParseVersion(s); err == nil {
			t.Errorf("ParseVersion(%q) should fail", s)
		}
	}
}

func TestVersionLess(t *testing.T) {
	cases := []struct {
		a, b Version
		want bool
	}{
		{Version{0, 9, 0}, Version{0, 10, 0}, true},
		{Version{0, 10, 0}, Version{1, 0, 0}, true},
		{Version{0, 9, 0}, Version{0, 9, 0}, false},
		{Version{0, 9, 1}, Version{0, 9, 0}, false},
		{Version{0, 9, 0}, Version{0, 9, 1}, true},
	}
	for _, c := range cases {
		if got := c.a.Less(c.b); got != c.want {
			t.Errorf("%v.Less(%v) = %v; want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestVersionString(t *testing.T) {
	if s := (Version{0, 10, 3}).String(); s != "0.10.3" {
		t.Errorf("String = %q", s)
	}
}
