package main

import (
	"errors"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// EX_TEMPFAIL. A planned restart must not look like a clean exit: systemd's
// Restart=always ignores the code, but Restart=on-failure only respawns
// non-zero exits. Exiting 0 there used to leave the panel permanently down.
const plannedRestartExitCode = 75

var plannedRestart atomic.Int32

var errNoRestartSupervisor = errors.New("当前环境没有检测到服务管理器，无法自动重启；请在服务器上手动重启 Viya")

// detectRestartSupervisor is pure so every deployment shape can be tested.
// The process must only exit when something outside it is guaranteed to start
// it again. A systemd *unit* scope is that guarantee; a user session scope is
// a human running the binary by hand and must not be mistaken for supervision.
func detectRestartSupervisor(goos, cgroup, pid1Comm string, dockerized bool) bool {
	if goos != "linux" || dockerized {
		return false
	}
	cgroup = strings.TrimSpace(cgroup)
	comm := strings.TrimSpace(pid1Comm)
	if strings.Contains(cgroup, ".service") && !strings.Contains(cgroup, "/user.slice/") {
		return true
	}
	// OpenWrt procd respawns managed services; its cgroup hierarchy is usually
	// empty or just "/". Never treat a bare systemd machine as supervision.
	if comm == "procd" && !strings.Contains(cgroup, "/user.slice/") &&
		(cgroup == "" || cgroup == "0::/" || strings.Contains(cgroup, "/system.slice/")) {
		return true
	}
	return false
}

func supervisedRestart() bool {
	if runtime.GOOS != "linux" || isDockerized() {
		return false
	}
	cgroup, _ := os.ReadFile("/proc/self/cgroup")
	comm, _ := os.ReadFile("/proc/1/comm")
	return detectRestartSupervisor("linux", string(cgroup), string(comm), false)
}

// requestProcessRestartWith hands control to the service manager. Refusing to
// stop an unmanaged process is the whole point: exiting there used to make the
// panel unreachable until somebody logged into the host.
func requestProcessRestartWith(supervised bool, reason string) error {
	if !supervised {
		return errNoRestartSupervisor
	}
	plannedRestart.Store(1)
	slog.Info("planned restart requested", "reason", reason, "exit_code", plannedRestartExitCode)
	time.AfterFunc(2*time.Second, func() {
		if p, err := os.FindProcess(os.Getpid()); err == nil {
			_ = p.Signal(os.Interrupt)
		}
	})
	return nil
}

func requestProcessRestart(reason string) error {
	return requestProcessRestartWith(supervisedRestart(), reason)
}
