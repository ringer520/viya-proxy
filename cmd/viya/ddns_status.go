package main

import (
	"context"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

func (a *App) setDDNSStage(ctx context.Context, id int64, stage string) {
	_, _ = a.db.ExecContext(ctx, `UPDATE ddns_records SET last_stage=? WHERE id=?`, stage, id)
}
func (a *App) redactDNSError(ctx context.Context, id int64, message string) string {
	_, credentials, err := a.loadDNSProvider(ctx, id)
	if err == nil {
		for _, value := range credentials {
			if len(value) >= 4 {
				message = strings.ReplaceAll(message, value, "[redacted]")
			}
		}
	}
	return clipText(sanitizeProgramText(message), 1200)
}
func ipv6AddressFlags() map[string]uint64 {
	data, _ := os.ReadFile("/proc/net/if_inet6")
	return parseIPv6Flags(string(data))
}
func parseIPv6Flags(data string) map[string]uint64 {
	out := map[string]uint64{}
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || len(fields[0]) != 32 {
			continue
		}
		chunks := []string{}
		for i := 0; i < 32; i += 4 {
			chunks = append(chunks, fields[0][i:i+4])
		}
		ip, err := netip.ParseAddr(strings.Join(chunks, ":"))
		flags, e := strconv.ParseUint(fields[4], 16, 32)
		if err == nil && e == nil {
			out[ip.String()] = flags
		}
	}
	return out
}
