package main

import "testing"

func TestPullNumberFromURL(t *testing.T) {
	cases := []struct {
		in     string
		want   int
		wantOK bool
	}{
		{"https://github.com/o/r/pull/123", 123, true},
		{"https://github.com/o/r/pull/1", 1, true},
		{"https://github.com/foo/bar/pull/42#issuecomment-1", 42, true},
		{"https://github.com/o/r/issues/9", 0, false},
		{"", 0, false},
		{"PR_OPENED:", 0, false},
	}
	for _, c := range cases {
		got, ok := pullNumberFromURL(c.in)
		if ok != c.wantOK || got != c.want {
			t.Errorf("pullNumberFromURL(%q) = (%d,%v), want (%d,%v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}
