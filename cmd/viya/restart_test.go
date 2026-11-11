package main

import (
	"errors"
	"testing"
)

func TestDetectRestartSupervisor(t *testing.T) {
	cases := []struct {
		name             string
		goos             string
		cgroup           string
		pid1Comm         string
		dockerized       bool
		want             bool
	}{
		{"systemd unit (cgroup v2)", "linux", "0::/system.slice/viya.service", "systemd", false, true},
		{"systemd unit (cgroup v1)", "linux", "12:pids:/system.slice/viya.service\n11:memory:/system.slice/viya.service", "systemd", false, true},
		{"manual run in an SSH session", "linux", "0::/user.slice/user-1000.slice/session-7.scope", "systemd", false, false},
		{"user-level unit is treated conservatively", "linux", "0::/user.slice/user-1000.slice/viya.service", "systemd", false, false},
		{"container has no in-process guarantee", "linux", "0::/docker/abc123", "viya", true, false},
		{"OpenWrt procd", "linux", "0::/", "procd", false, true},
		{"OpenWrt without cgroup data", "linux", "", "procd", false, true},
		{"bare Linux with PID 1 systemd but no unit", "linux", "0::/", "systemd", false, false},
		{"non-Linux is never self-restarting", "windows", "0::/system.slice/viya.service", "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := detectRestartSupervisor(tc.goos, tc.cgroup, tc.pid1Comm, tc.dockerized)
			if got != tc.want {
				t.Fatalf("detectRestartSupervisor(%q, %q, %q, %v) = %v, want %v",
					tc.goos, tc.cgroup, tc.pid1Comm, tc.dockerized, got, tc.want)
			}
		})
	}
}

func TestRequestProcessRestartRefusesUnmanaged(t *testing.T) {
	before := plannedRestart.Load()
	err := requestProcessRestartWith(false, "unit test")
	if !errors.Is(err, errNoRestartSupervisor) {
		t.Fatalf("unmanaged restart returned %v, want %v", err, errNoRestartSupervisor)
	}
	if plannedRestart.Load() != before {
		t.Fatal("an unmanaged restart must not mark a planned exit")
	}
}
