package main

import "testing"

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		current, candidate string
		want               bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.1.0", "v0.1.1", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.2.0", "v0.1.0", false},
		{"v0.1.0", "v1.0.0", true},
		{"dev", "v0.1.0", true},
		{"v0.1.0", "dev", false},
		{"v0.1.0", "v0.1.0-rc1", false},
		{"v0.1.0-rc1", "v0.1.0", true},
		{"v0.9.9", "v0.10.0", true},
		{"v1.0.0", "v0.99.99", false},
	}
	for _, c := range cases {
		if got := versionNewer(c.current, c.candidate); got != c.want {
			t.Errorf("versionNewer(%q, %q) = %v, want %v", c.current, c.candidate, got, c.want)
		}
	}
}

func TestIsDockerized(t *testing.T) {
	// Just make sure it runs without panicking; the result depends on the
	// environment so we cannot assert a fixed value.
	_ = isDockerized()
}
