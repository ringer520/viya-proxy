package main

import (
	"net"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestLoopbackCompanions(t *testing.T) {
	cases := []struct {
		addr string
		want []string
	}{
		{"127.0.0.1:8181", nil},
		{"0.0.0.0:8181", nil},
		{"[::]:8181", nil},
		{"[::1]:8181", nil},
		{"localhost:8181", nil},
		{"not-an-address", nil},
		{"192.168.1.16:8181", []string{"127.0.0.1:8181", "[::1]:8181"}},
		{"[fd00::1]:9000", []string{"[::1]:9000", "127.0.0.1:9000"}},
	}
	for _, tc := range cases {
		t.Run(tc.addr, func(t *testing.T) {
			got := loopbackCompanions(tc.addr)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("loopbackCompanions(%q) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}
}

func freeTCPPort(t *testing.T, host string) string {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatalf("pick a free port on %s: %v", host, err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	ln.Close()
	return port
}

func closeListeners(listeners []net.Listener) {
	for _, ln := range listeners {
		ln.Close()
	}
}

func bindableNonLoopbackIPv4s(t *testing.T) []string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Skipf("cannot enumerate interfaces: %v", err)
	}
	out := []string{}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP.To4()
		// Windows often lists an APIPA 169.254 address first; it cannot be
		// bound, so verify every candidate instead of trusting the order.
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		probe, err := net.Listen("tcp", net.JoinHostPort(ip.String(), "0"))
		if err != nil {
			continue
		}
		probe.Close()
		out = append(out, ip.String())
	}
	if len(out) == 0 {
		t.Skip("no bindable non-loopback IPv4 interface on this host")
	}
	return out
}

func TestOpenPanelListenersServesLANAndLoopback(t *testing.T) {
	var listeners []net.Listener
	var bound []string
	var addr string
	for _, ip := range bindableNonLoopbackIPv4s(t) {
		for attempt := 0; attempt < 5 && len(listeners) == 0; attempt++ {
			candidate := net.JoinHostPort(ip, freeTCPPort(t, ip))
			var err error
			listeners, bound, err = openPanelListeners(candidate)
			if err == nil {
				addr = candidate
			}
		}
		if len(listeners) > 0 {
			break
		}
	}
	if len(listeners) == 0 {
		t.Fatal("could not bind a LAN listener on any interface")
	}
	defer closeListeners(listeners)

	if bound[0] != addr || len(bound) < 2 {
		t.Fatalf("bound addresses = %v, want primary %s plus loopback companions", bound, addr)
	}
	_, port, _ := net.SplitHostPort(addr)
	wantLoopback := net.JoinHostPort("127.0.0.1", port)
	found := false
	for _, b := range bound {
		if b == wantLoopback {
			found = true
		}
	}
	if !found {
		t.Fatalf("bound addresses %v do not include %s", bound, wantLoopback)
	}
	for _, target := range []string{addr, wantLoopback} {
		conn, err := net.DialTimeout("tcp", target, 2*time.Second)
		if err != nil {
			t.Fatalf("dial %s: %v (bound=%v)", target, err, bound)
		}
		conn.Close()
	}
}

func TestOpenPanelListenersDoesNotDuplicateLoopback(t *testing.T) {
	addr := net.JoinHostPort("127.0.0.1", freeTCPPort(t, "127.0.0.1"))
	listeners, bound, err := openPanelListeners(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer closeListeners(listeners)
	if len(listeners) != 1 || !reflect.DeepEqual(bound, []string{addr}) {
		t.Fatalf("loopback bind produced %d listeners / %v, want exactly one", len(listeners), bound)
	}
}
