package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/libdns/alidns"
	"github.com/libdns/cloudflare"
	"github.com/libdns/libdns"
	"github.com/libdns/tencentcloud"
)

type DDNSRecord struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	Comment         string `json:"comment"`
	DNSProviderID   int64  `json:"dns_provider_id"`
	Zone            string `json:"zone"`
	RecordName      string `json:"record_name"`
	RecordType      string `json:"record_type"`
	CNAMETarget     string `json:"cname_target,omitempty"`
	IPSource        string `json:"ip_source"`
	InterfaceName   string `json:"interface_name,omitempty"`
	InterfaceIP     string `json:"interface_ip,omitempty"`
	TTLSeconds      int    `json:"ttl_seconds"`
	IntervalMinutes int    `json:"interval_minutes"`
	Enabled         bool   `json:"enabled"`
	LastIP          string `json:"last_ip"`
	LastRunAt       int64  `json:"last_run_at"`
	NextRunAt       int64  `json:"next_run_at"`
	LastError       string `json:"last_error,omitempty"`
}
type NetworkInterfaceAddress struct {
	IP        string `json:"ip"`
	Family    string `json:"family"`
	Scope     string `json:"scope"`
	Temporary bool   `json:"temporary,omitempty"`
}
type NetworkInterface struct {
	Name      string                    `json:"name"`
	Index     int                       `json:"index"`
	Addresses []NetworkInterfaceAddress `json:"addresses"`
}
type DDNSRun struct {
	ID         int64  `json:"id"`
	ObservedIP string `json:"observed_ip"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	CreatedAt  string `json:"created_at"`
}
type ddnsProvider interface {
	libdns.RecordGetter
	libdns.RecordSetter
}

func (a *App) listDDNSRecords(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,name,comment,dns_provider_id,zone,record_name,record_type,cname_target,ip_source,interface_name,interface_ip,ttl_seconds,interval_minutes,enabled,last_ip,last_run_at,next_run_at,last_error FROM ddns_records ORDER BY name`)
	if err != nil {
		fail(w, 500, errors.New("could not read DDNS records"))
		return
	}
	defer rows.Close()
	out := []DDNSRecord{}
	for rows.Next() {
		var v DDNSRecord
		var en int
		if err := rows.Scan(&v.ID, &v.Name, &v.Comment, &v.DNSProviderID, &v.Zone, &v.RecordName, &v.RecordType, &v.CNAMETarget, &v.IPSource, &v.InterfaceName, &v.InterfaceIP, &v.TTLSeconds, &v.IntervalMinutes, &en, &v.LastIP, &v.LastRunAt, &v.NextRunAt, &v.LastError); err != nil {
			fail(w, 500, errors.New("could not read DDNS records"))
			return
		}
		v.Enabled = en == 1
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		fail(w, 500, errors.New("could not read DDNS records"))
		return
	}
	writeJSON(w, 200, out)
}
func (a *App) listNetworkInterfaces(w http.ResponseWriter, r *http.Request) {
	interfaces, err := networkInterfaces()
	if err != nil {
		fail(w, 500, errors.New("could not read network interfaces"))
		return
	}
	writeJSON(w, 200, interfaces)
}

func networkInterfaces() ([]NetworkInterface, error) {
	temporaryAddresses := temporaryIPv6Addresses()
	items, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]NetworkInterface, 0, len(items))
	for _, item := range items {
		if item.Flags&net.FlagUp == 0 || item.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := item.Addrs()
		if err != nil {
			continue
		}
		entry := NetworkInterface{Name: item.Name, Index: item.Index, Addresses: []NetworkInterfaceAddress{}}
		for _, raw := range addresses {
			prefix, err := netip.ParsePrefix(raw.String())
			if err != nil {
				continue
			}
			ip := prefix.Addr().Unmap()
			if !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
				continue
			}
			family := "ipv6"
			if ip.Is4() {
				family = "ipv4"
			}
			scope := "公网"
			if ip.IsPrivate() {
				scope = "内网"
			}
			entry.Addresses = append(entry.Addresses, NetworkInterfaceAddress{IP: ip.String(), Family: family, Scope: scope, Temporary: family == "ipv6" && temporaryAddresses[ip.String()]})
		}
		sort.Slice(entry.Addresses, func(i, j int) bool {
			if entry.Addresses[i].Temporary != entry.Addresses[j].Temporary {
				return !entry.Addresses[i].Temporary
			}
			if entry.Addresses[i].Scope != entry.Addresses[j].Scope {
				return entry.Addresses[i].Scope == "公网"
			}
			return entry.Addresses[i].IP < entry.Addresses[j].IP
		})
		if len(entry.Addresses) > 0 {
			out = append(out, entry)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Linux exposes IPv6 temporary-address flags in /proc/net/if_inet6. When
// available, mark privacy addresses so the UI can default to a stable address.
func temporaryIPv6Addresses() map[string]bool {
	result := map[string]bool{}
	data, err := os.ReadFile("/proc/net/if_inet6")
	if err != nil {
		return result
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || len(fields[0]) != 32 {
			continue
		}
		parts := make([]string, 8)
		for i := range parts {
			parts[i] = fields[0][i*4 : (i+1)*4]
		}
		ip, err := netip.ParseAddr(strings.Join(parts, ":"))
		flags, flagErr := strconv.ParseUint(fields[4], 16, 32)
		if err == nil && flagErr == nil && flags&0x01 != 0 {
			result[ip.String()] = true
		}
	}
	return result
}

func interfaceIP(name string, v6 bool) (netip.Addr, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return netip.Addr{}, errors.New("请选择网络接口")
	}
	iface, err := net.InterfaceByName(name)
	if err != nil || iface.Flags&net.FlagUp == 0 {
		return netip.Addr{}, fmt.Errorf("网络接口 %q 不存在或未启用", name)
	}
	addresses, err := iface.Addrs()
	if err != nil {
		return netip.Addr{}, fmt.Errorf("无法读取网络接口 %q 的地址", name)
	}
	candidates := []netip.Addr{}
	for _, raw := range addresses {
		prefix, err := netip.ParsePrefix(raw.String())
		if err != nil {
			continue
		}
		ip := prefix.Addr().Unmap()
		if !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() || ip.Is6() != v6 {
			continue
		}
		candidates = append(candidates, ip)
	}
	if len(candidates) == 0 {
		family := "IPv4"
		if v6 {
			family = "IPv6"
		}
		return netip.Addr{}, fmt.Errorf("网络接口 %q 没有可用的 %s 地址", name, family)
	}
	temporary := temporaryIPv6Addresses()
	sort.Slice(candidates, func(i, j int) bool {
		if v6 && temporary[candidates[i].String()] != temporary[candidates[j].String()] {
			return !temporary[candidates[i].String()]
		}
		if candidates[i].IsPrivate() != candidates[j].IsPrivate() {
			return !candidates[i].IsPrivate()
		}
		return candidates[i].Less(candidates[j])
	})
	return candidates[0], nil
}

func interfaceIPSelected(name string, v6 bool, selected string) (netip.Addr, error) {
	if strings.TrimSpace(selected) == "" {
		return interfaceIP(name, v6)
	}
	want, err := netip.ParseAddr(strings.TrimSpace(selected))
	if err != nil || want.Is6() != v6 || !want.IsGlobalUnicast() || want.IsLoopback() || want.IsLinkLocalUnicast() || want.IsMulticast() || want.IsUnspecified() {
		return netip.Addr{}, errors.New("所选网卡 IP 地址无效，请重新选择")
	}
	iface, err := net.InterfaceByName(strings.TrimSpace(name))
	if err != nil || iface.Flags&net.FlagUp == 0 {
		return netip.Addr{}, fmt.Errorf("网络接口 %q 不存在或未启用", name)
	}
	addresses, err := iface.Addrs()
	if err != nil {
		return netip.Addr{}, fmt.Errorf("无法读取网络接口 %q 的地址", name)
	}
	for _, raw := range addresses {
		prefix, parseErr := netip.ParsePrefix(raw.String())
		if parseErr == nil && prefix.Addr().Unmap() == want.Unmap() {
			return want, nil
		}
	}
	return netip.Addr{}, fmt.Errorf("所选地址 %s 已不在网卡 %s 上，请刷新并重新选择", want, name)
}
func validateDDNS(v *DDNSRecord) error {
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" || len(v.Name) > 100 {
		return errors.New("DDNS name must be 1-100 characters")
	}
	v.Zone = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(v.Zone)), ".")
	if !domainRE.MatchString(v.Zone) || strings.Contains(v.Zone, "*") {
		return errors.New("zone must be a DNS zone name")
	}
	v.RecordName = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(v.RecordName)), ".")
	if v.RecordName == "" {
		return errors.New("record name is required (use @ for the zone apex)")
	}
	if v.RecordName != "@" && !domainRE.MatchString(v.RecordName) {
		return errors.New("record name must be @ or a relative DNS name")
	}
	if strings.HasSuffix(v.RecordName, "."+v.Zone) || v.RecordName == v.Zone {
		return errors.New("record name must be relative to the selected zone")
	}
	if v.RecordType != "A" && v.RecordType != "AAAA" && v.RecordType != "CNAME" {
		return errors.New("record type must be A, AAAA or CNAME")
	}
	if v.RecordType == "CNAME" {
		v.CNAMETarget = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(v.CNAMETarget)), ".")
		if !domainRE.MatchString(v.CNAMETarget) || strings.Contains(v.CNAMETarget, "*") || net.ParseIP(v.CNAMETarget) != nil {
			return errors.New("CNAME target must be a hostname, not an IP address")
		}
		owner := v.RecordName
		if owner == "@" {
			owner = v.Zone
		} else {
			owner += "." + v.Zone
		}
		if strings.EqualFold(owner, v.CNAMETarget) {
			return errors.New("CNAME target cannot point to the record itself")
		}
		v.IPSource, v.InterfaceName, v.InterfaceIP = "", "", ""
	} else {
		v.CNAMETarget = ""
		if v.IPSource != "interface" {
			v.InterfaceName, v.InterfaceIP = "", ""
		} else if v.InterfaceName == "" {
			return errors.New("请选择网络接口")
		}
	}
	if v.DNSProviderID < 1 {
		return errors.New("a DNS provider is required")
	}
	if v.RecordType != "CNAME" {
		if v.IPSource == "" {
			v.IPSource = "http"
		}
		switch v.IPSource {
		case "http":
			v.InterfaceName = ""
		case "interface":
			if _, err := interfaceIP(v.InterfaceName, v.RecordType == "AAAA"); err != nil {
				return err
			}
		default:
			return errors.New("IP 获取方式只支持公网接口或指定网卡")
		}
	}
	if v.TTLSeconds == 0 {
		v.TTLSeconds = 300
	}
	if v.TTLSeconds < 60 || v.TTLSeconds > 86400 {
		return errors.New("TTL must be between 60 and 86400 seconds")
	}
	if v.IntervalMinutes == 0 {
		v.IntervalMinutes = 5
	}
	if v.IntervalMinutes < 5 || v.IntervalMinutes > 1440 {
		return errors.New("DDNS interval must be between 5 and 1440 minutes")
	}
	return nil
}

func sameDNSValue(recordType, current, desired string) bool {
	if recordType == "CNAME" {
		return strings.EqualFold(strings.TrimSuffix(strings.TrimSpace(current), "."), strings.TrimSuffix(strings.TrimSpace(desired), "."))
	}
	return current == desired
}

func (a *App) saveDDNS(w http.ResponseWriter, r *http.Request, id int64, create bool) {
	var v DDNSRecord
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&v); err != nil {
		fail(w, 400, errors.New("invalid JSON body"))
		return
	}
	if err := validateDDNS(&v); err != nil {
		fail(w, 400, err)
		return
	}
	a.ddnsMu.Lock()
	defer a.ddnsMu.Unlock()
	var exists int
	if err := a.db.QueryRowContext(r.Context(), `SELECT 1 FROM dns_providers WHERE id=?`, v.DNSProviderID).Scan(&exists); err != nil {
		fail(w, 400, errors.New("selected DNS provider does not exist"))
		return
	}
	v.Comment = strings.TrimSpace(v.Comment)
	if v.Comment == "" {
		v.Comment = "viya"
	}
	if len(v.Comment) > 500 {
		fail(w, 400, errors.New("备注不能超过 500 字节"))
		return
	}
	var res sqlResult
	var err error
	if create {
		rr, e := a.db.ExecContext(r.Context(), `INSERT INTO ddns_records(name,comment,dns_provider_id,zone,record_name,record_type,cname_target,ip_source,interface_name,interface_ip,ttl_seconds,interval_minutes,enabled,next_run_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.Name, v.Comment, v.DNSProviderID, v.Zone, v.RecordName, v.RecordType, v.CNAMETarget, v.IPSource, v.InterfaceName, v.InterfaceIP, v.TTLSeconds, v.IntervalMinutes, boolInt(v.Enabled), time.Now().Unix())
		err = e
		res = rr
	} else {
		rr, e := a.db.ExecContext(r.Context(), `UPDATE ddns_records SET name=?,comment=?,dns_provider_id=?,zone=?,record_name=?,record_type=?,cname_target=?,ip_source=?,interface_name=?,interface_ip=?,ttl_seconds=?,interval_minutes=?,enabled=?,next_run_at=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, v.Name, v.Comment, v.DNSProviderID, v.Zone, v.RecordName, v.RecordType, v.CNAMETarget, v.IPSource, v.InterfaceName, v.InterfaceIP, v.TTLSeconds, v.IntervalMinutes, boolInt(v.Enabled), time.Now().Unix(), id)
		err = e
		res = rr
	}
	if err != nil {
		fail(w, 409, errors.New("DDNS record could not be saved"))
		return
	}
	if create {
		v.ID, _ = res.LastInsertId()
	} else {
		n, _ := res.RowsAffected()
		if n == 0 {
			fail(w, 404, errors.New("DDNS record not found"))
			return
		}
		v.ID = id
	}
	writeJSON(w, map[bool]int{true: 201, false: 200}[create], v)
}

// sqlResult keeps mutation code small without exposing driver-specific result types.
type sqlResult interface {
	LastInsertId() (int64, error)
	RowsAffected() (int64, error)
}

func (a *App) createDDNSRecord(w http.ResponseWriter, r *http.Request) { a.saveDDNS(w, r, 0, true) }
func (a *App) updateDDNSRecord(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		fail(w, 400, errors.New("invalid DDNS record id"))
		return
	}
	a.saveDDNS(w, r, id, false)
}
func (a *App) deleteDDNSRecord(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		fail(w, 400, errors.New("invalid DDNS record id"))
		return
	}
	a.ddnsMu.Lock()
	defer a.ddnsMu.Unlock()
	res, e := a.db.ExecContext(r.Context(), `DELETE FROM ddns_records WHERE id=?`, id)
	if e != nil {
		fail(w, 500, errors.New("could not delete DDNS record"))
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404, errors.New("DDNS record not found"))
		return
	}
	w.WriteHeader(204)
}
func (a *App) listDDNSRuns(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		fail(w, 400, errors.New("invalid DDNS record id"))
		return
	}
	rows, e := a.db.QueryContext(r.Context(), `SELECT id,observed_ip,status,error_text,created_at FROM ddns_runs WHERE ddns_record_id=? ORDER BY id DESC LIMIT 100`, id)
	if e != nil {
		fail(w, 500, errors.New("could not read DDNS history"))
		return
	}
	defer rows.Close()
	out := []DDNSRun{}
	for rows.Next() {
		var v DDNSRun
		if rows.Scan(&v.ID, &v.ObservedIP, &v.Status, &v.Error, &v.CreatedAt) != nil {
			fail(w, 500, errors.New("could not read DDNS history"))
			return
		}
		out = append(out, v)
	}
	writeJSON(w, 200, out)
}
func (a *App) runDDNSRecord(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		fail(w, 400, errors.New("invalid DDNS record id"))
		return
	}
	a.ddnsMu.Lock()
	if a.ddnsRunning[id] {
		a.ddnsMu.Unlock()
		fail(w, 409, errors.New("该 DDNS 任务正在更新中，请稍后再试"))
		return
	}
	a.ddnsRunning[id] = true
	a.ddnsMu.Unlock()
	defer func() {
		a.ddnsMu.Lock()
		delete(a.ddnsRunning, id)
		a.ddnsMu.Unlock()
	}()
	// The network round trip runs outside ddnsMu so scheduling, export and
	// other records are not blocked for up to 35 seconds.
	ip, e := a.executeDDNSRecord(r.Context(), id, true)
	if e != nil {
		fail(w, 502, e)
		return
	}
	var completed int64
	completedCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	if err := a.db.QueryRowContext(completedCtx, `SELECT last_run_at FROM ddns_records WHERE id=?`, id).Scan(&completed); err != nil {
		fail(w, 500, errors.New("更新已执行，但无法读取完成时间，请刷新任务状态"))
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "ip": ip, "last_run_at": completed})
}

func (a *App) beginDDNSRun(id int64) bool {
	a.ddnsMu.Lock()
	defer a.ddnsMu.Unlock()
	if a.ddnsRunning[id] {
		return false
	}
	a.ddnsRunning[id] = true
	return true
}

func (a *App) endDDNSRun(id int64) {
	a.ddnsMu.Lock()
	delete(a.ddnsRunning, id)
	a.ddnsMu.Unlock()
}

func (a *App) ddnsWorker() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			rows, err := a.db.QueryContext(a.ctx, `SELECT id FROM ddns_records WHERE enabled=1 AND next_run_at<=? ORDER BY next_run_at LIMIT 20`, time.Now().Unix())
			if err == nil {
				ids := []int64{}
				for rows.Next() {
					var id int64
					if rows.Scan(&id) == nil {
						ids = append(ids, id)
					}
				}
				rows.Close()
				for _, id := range ids {
					if a.ctx.Err() != nil {
						return
					}
					if !a.beginDDNSRun(id) {
						continue
					}
					var due int
					// A manual update or schedule edit may have happened while waiting.
					err := a.db.QueryRowContext(a.ctx, `SELECT COUNT(*) FROM ddns_records WHERE id=? AND enabled=1 AND next_run_at<=?`, id, time.Now().Unix()).Scan(&due)
					if err == nil && due > 0 {
						if _, e := a.updateDDNSRecordNow(a.ctx, id); e != nil {
							slog.Warn("DDNS update failed", "record_id", id, "error", e)
						}
					}
					a.endDDNSRun(id)
				}
			}
		}
	}
}

func publicIP(ctx context.Context, v6 bool) (netip.Addr, error) {
	urls := []string{"https://api.ipify.org", "https://checkip.amazonaws.com", "https://ifconfig.me/ip"}
	if v6 {
		urls = []string{"https://api6.ipify.org", "https://v6.ident.me", "https://ifconfig.me/ip"}
	}
	client := http.Client{Timeout: 7 * time.Second}
	var errs []string
	for _, endpoint := range urls {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if e != nil {
			continue
		}
		resp, e := client.Do(req)
		if e != nil {
			errs = append(errs, e.Error())
			continue
		}
		data, e := io.ReadAll(io.LimitReader(resp.Body, 128))
		resp.Body.Close()
		if e != nil || resp.StatusCode != 200 {
			errs = append(errs, endpoint+" returned an error")
			continue
		}
		ip, e := netip.ParseAddr(strings.TrimSpace(string(data)))
		if e == nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && ip.Is6() == v6 {
			return ip, nil
		}
		errs = append(errs, endpoint+" returned an invalid address for requested IP family")
	}
	return netip.Addr{}, fmt.Errorf("public IP lookup failed: %s", strings.Join(errs, "; "))
}
func providerFor(ctx context.Context, load func(context.Context, int64) (string, map[string]string, error), id int64) (ddnsProvider, error) {
	kind, c, err := load(ctx, id)
	if err != nil {
		return nil, err
	}
	switch kind {
	case "cloudflare":
		return &cloudflare.Provider{APIToken: c["api_token"]}, nil
	case "tencentcloud":
		return &tencentcloud.Provider{SecretId: c["secret_id"], SecretKey: c["secret_key"], Region: "ap-guangzhou"}, nil
	case "alidns":
		return &alidns.Provider{CredentialInfo: alidns.CredentialInfo{AccessKeyID: c["access_key_id"], AccessKeySecret: c["access_key_secret"], RegionID: c["region_id"]}}, nil
	default:
		return nil, errors.New("unsupported DNS provider")
	}
}
func (a *App) updateDDNSRecordNow(ctx context.Context, id int64) (string, error) {
	return a.executeDDNSRecord(ctx, id, false)
}

func (a *App) executeDDNSRecord(ctx context.Context, id int64, manual bool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	var v DDNSRecord
	var enabled int
	err := a.db.QueryRowContext(ctx, `SELECT id,name,comment,dns_provider_id,zone,record_name,record_type,cname_target,ip_source,interface_name,interface_ip,ttl_seconds,interval_minutes,enabled,last_ip FROM ddns_records WHERE id=?`, id).Scan(&v.ID, &v.Name, &v.Comment, &v.DNSProviderID, &v.Zone, &v.RecordName, &v.RecordType, &v.CNAMETarget, &v.IPSource, &v.InterfaceName, &v.InterfaceIP, &v.TTLSeconds, &v.IntervalMinutes, &enabled, &v.LastIP)
	if err != nil {
		return "", errors.New("DDNS record not found")
	}
	if enabled != 1 && !manual {
		return "", errors.New("DDNS record is disabled")
	}
	status := "failed"
	message := ""
	observed := ""
	if v.RecordType == "CNAME" {
		observed = v.CNAMETarget
	} else {
		var ip netip.Addr
		if v.IPSource == "interface" {
			ip, err = interfaceIPSelected(v.InterfaceName, v.RecordType == "AAAA", v.InterfaceIP)
		} else {
			ip, err = publicIP(ctx, v.RecordType == "AAAA")
		}
		if err == nil {
			observed = ip.String()
		}
	}
	if err != nil {
		message = err.Error()
	} else {
		provider, pErr := providerFor(ctx, a.loadDNSProvider, v.DNSProviderID)
		if pErr != nil {
			err = pErr
			message = err.Error()
		} else {
			records, gErr := provider.GetRecords(ctx, v.Zone+".")
			if gErr != nil {
				err = gErr
				message = fmt.Sprintf("DNS provider lookup failed: %v", gErr)
			} else {
				matches := []libdns.Record{}
				conflictingType := ""
				for _, rec := range records {
					rr := rec.RR()
					if strings.EqualFold(rr.Name, v.RecordName) {
						if rr.Type == v.RecordType {
							matches = append(matches, rec)
						} else if v.RecordType == "CNAME" || rr.Type == "CNAME" {
							conflictingType = rr.Type
						}
					}
				}
				if conflictingType != "" {
					err = fmt.Errorf("CNAME cannot coexist with the existing %s record at this name; remove the conflicting record first", conflictingType)
					message = err.Error()
				} else if len(matches) > 1 {
					err = errors.New("multiple records exist for this name and type; refusing to replace an RRset with multiple values")
					message = err.Error()
				} else if len(matches) == 1 && sameDNSValue(v.RecordType, matches[0].RR().Data, observed) {
					status = "unchanged"
				} else {
					var target libdns.Record
					if v.RecordType == "CNAME" {
						target = libdns.CNAME{Name: v.RecordName, Target: observed, TTL: time.Duration(v.TTLSeconds) * time.Second}
					} else {
						ip, _ := netip.ParseAddr(observed)
						target = libdns.Address{Name: v.RecordName, IP: ip, TTL: time.Duration(v.TTLSeconds) * time.Second}
					}
					if _, isAliDNS := provider.(*alidns.Provider); isAliDNS && len(matches) == 1 {
						if existing, ok := matches[0].(alidns.DomainRecord); ok {
							existing.Value = observed
							existing.TTL = uint32(v.TTLSeconds)
							target = existing
						} else {
							err = errors.New("AliDNS returned an unsupported record shape")
							message = err.Error()
						}
					}
					if err == nil {
						_, err = provider.SetRecords(ctx, v.Zone+".", []libdns.Record{target})
						if err != nil {
							message = fmt.Sprintf("DNS provider rejected the record update: %v", err)
						} else {
							status = "updated"
						}
					}
				}
			}
		}
	}
	now := time.Now().Unix()
	// Record the completed attempt even if the network deadline or browser request expired.
	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer persistCancel()
	tx, dbErr := a.db.BeginTx(persistCtx, nil)
	if dbErr != nil {
		return observed, dbErr
	}
	if _, dbErr = tx.ExecContext(persistCtx, `INSERT INTO ddns_runs(ddns_record_id,observed_ip,status,error_text) VALUES(?,?,?,?)`, id, observed, status, message); dbErr != nil {
		_ = tx.Rollback()
		return observed, dbErr
	}
	if _, dbErr = tx.ExecContext(persistCtx, `DELETE FROM ddns_runs WHERE ddns_record_id=? AND id NOT IN (SELECT id FROM ddns_runs WHERE ddns_record_id=? ORDER BY id DESC LIMIT 200)`, id, id); dbErr != nil {
		_ = tx.Rollback()
		return observed, dbErr
	}
	if _, dbErr = tx.ExecContext(persistCtx, `UPDATE ddns_records SET last_ip=CASE WHEN ? IN ('updated','unchanged') THEN ? ELSE last_ip END,last_run_at=?,next_run_at=?,last_error=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, status, observed, now, now+int64(v.IntervalMinutes*60), message, id); dbErr != nil {
		_ = tx.Rollback()
		return observed, dbErr
	}
	if dbErr = tx.Commit(); dbErr != nil {
		return observed, dbErr
	}
	if err != nil {
		if message == "" {
			message = "DDNS update failed"
		}
		return observed, errors.New(message)
	}
	return observed, nil
}

func (a *App) toggleDDNSEnabled(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, errors.New("invalid DDNS record id"))
		return
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input) != nil || input.Enabled == nil {
		fail(w, 400, errors.New("enabled is required"))
		return
	}
	a.ddnsMu.Lock()
	defer a.ddnsMu.Unlock()
	result, err := a.db.ExecContext(r.Context(), `UPDATE ddns_records SET enabled=?,next_run_at=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, boolInt(*input.Enabled), time.Now().Unix(), id)
	if err != nil {
		fail(w, 500, errors.New("could not update DDNS schedule"))
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		fail(w, 404, errors.New("DDNS record not found"))
		return
	}
	writeJSON(w, 200, map[string]any{"enabled": *input.Enabled})
}
