package main

import (
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type CertificateSource struct {
	Kind          string `json:"kind"`
	Domain        string `json:"domain,omitempty"`
	CertificateID int64  `json:"certificate_id,omitempty"`
	Label         string `json:"label"`
	Status        string `json:"status"`
	NotAfter      string `json:"not_after,omitempty"`
}
type AutomaticCertificate struct {
	Domain string            `json:"domain"`
	Source CertificateSource `json:"source"`
	Sites  []string          `json:"sites"`
}

func (a *App) certificateSources(ctx context.Context, hosts []Host) (map[int64]CertificateSource, error) {
	managed, err := a.loadManagedCertificates(ctx)
	if err != nil {
		return nil, err
	}
	custom := map[int64][]string{}
	rows, err := a.db.QueryContext(ctx, `SELECT id,dns_names_json FROM custom_certificates`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var names string
		if err = rows.Scan(&id, &names); err != nil {
			rows.Close()
			return nil, err
		}
		var decoded []string
		_ = json.Unmarshal([]byte(names), &decoded)
		custom[id] = decoded
	}
	rows.Close()
	result := map[int64]CertificateSource{}
	for _, host := range hosts {
		source := CertificateSource{Kind: "individual", Domain: host.Domain, Label: "单独自动申请", Status: "pending"}
		switch host.TLSMode {
		case "none":
			source = CertificateSource{Kind: "http", Label: "仅 HTTP", Status: "disabled"}
		case "custom":
			source = CertificateSource{Kind: "custom", Domain: host.Domain, Label: "指定上传证书", Status: "configured"}
			if host.CustomCertificateID != nil {
				source.CertificateID = *host.CustomCertificateID
				if a.db.QueryRowContext(ctx, `SELECT not_after FROM custom_certificates WHERE id=?`, *host.CustomCertificateID).Scan(&source.NotAfter) == nil {
					if expiry, e := time.Parse(time.RFC3339, source.NotAfter); e == nil {
						source.Status = "issued"
						if !expiry.After(time.Now()) {
							source.Status = "expired"
						}
					}
				}
				for _, name := range custom[*host.CustomCertificateID] {
					if strings.HasPrefix(name, "*.") && certificateMatchesDomain([]string{name}, host.Domain) {
						source.Domain = name
						source.Label = "复用上传泛域名"
						break
					}
				}
			}
		default:
			found := false
			for _, cert := range managed {
				if cert.Domain == host.Domain {
					source = CertificateSource{Kind: "managed", Domain: cert.Domain, CertificateID: cert.ID, Label: "SSL 已托管", Status: a.getCertificateProgress(cert.Domain).Status}
					found = true
					break
				}
			}
			if !found {
				for _, cert := range managed {
					if strings.HasPrefix(cert.Domain, "*.") && certificateMatchesDomain([]string{cert.Domain}, host.Domain) {
						source = CertificateSource{Kind: "wildcard", Domain: cert.Domain, CertificateID: cert.ID, Label: "复用泛域名", Status: a.getCertificateProgress(cert.Domain).Status}
						found = true
						break
					}
				}
			}
			if !found && hostUsesInternalCA(host) {
				source.Kind = "internal"
				source.Label = "单独内部证书"
			}
			if !found {
				source.Status = a.getCertificateProgress(host.Domain).Status
			}
		}
		if !host.Enabled && source.Status == "pending" {
			source.Status = "disabled"
		}
		result[host.ID] = source
	}
	// Read only public leaf metadata. Keys never enter a list response.
	root := caddyCertificateRoot(a)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil || d.IsDir() || !strings.HasSuffix(path, ".crt") {
			return nil
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return nil
		}
		block, _ := pem.Decode(raw)
		if block == nil {
			return nil
		}
		cert, e := x509.ParseCertificate(block.Bytes)
		if e != nil {
			return nil
		}
		for id, source := range result {
			if source.Kind != "custom" && source.Domain != "" && certificateMatchesDomain(cert.DNSNames, source.Domain) {
				if source.NotAfter != "" {
					existing, e := time.Parse(time.RFC3339, source.NotAfter)
					if e == nil && !cert.NotAfter.After(existing) {
						continue
					}
				}
				source.NotAfter = cert.NotAfter.UTC().Format("2006-01-02T15:04:05Z")
				source.Status = "issued"
				if cert.NotAfter.Before(time.Now()) {
					source.Status = "expired"
				}
				result[id] = source
			}
		}
		return nil
	})
	return result, nil
}
func caddyCertificateRoot(a *App) string {
	return filepath.Join(env("XDG_DATA_HOME", filepath.Join(env("HOME", a.dataDir), ".local", "share")), "caddy", "certificates")
}

// deleteAutomaticCertificate removes the issued certificate files and the
// matching application record. A site that still requests automatic HTTPS for
// the domain would immediately re-issue it, so that case is refused instead of
// deleting the certificate behind the operator's back.
func (a *App) deleteAutomaticCertificate(w http.ResponseWriter, r *http.Request) {
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(r.PathValue("domain")), "."))
	if !domainRE.MatchString(domain) {
		fail(w, 400, errors.New("证书域名无效"))
		return
	}
	pattern := domain
	if strings.HasPrefix(domain, "*.") {
		pattern = "%." + strings.TrimPrefix(domain, "*.")
	}
	var using int
	if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM proxy_hosts WHERE (domain=? OR domain LIKE ?) AND tls_mode NOT IN ('none','custom')`, domain, pattern).Scan(&using); err != nil {
		fail(w, 500, err)
		return
	}
	if using > 0 {
		fail(w, 409, errors.New("仍有站点依赖该证书自动签发，请先停用站点或改用指定证书"))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	removed, err := removeStoredCertificates(caddyCertificateRoot(a), domain)
	if err != nil {
		fail(w, 500, errors.New("无法删除引擎中的证书文件"))
		return
	}
	if _, err = a.db.ExecContext(r.Context(), `DELETE FROM managed_certificates WHERE domain=?`, domain); err != nil {
		fail(w, 500, err)
		return
	}
	if err = a.applyLocked(r.Context()); err != nil {
		failSavedApplication(w)
		return
	}
	writeJSON(w, 200, map[string]any{"domain": domain, "removed": removed})
}

// removeStoredCertificates deletes the leaf certificate folders whose name
// matches the domain. Issuer folders and the local CA hierarchy are untouched.
func removeStoredCertificates(root, domain string) (int, error) {
	issuers, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	names := []string{domain}
	if strings.HasPrefix(domain, "*.") {
		names = append(names, strings.TrimPrefix(domain, "*."))
	}
	removed := 0
	for _, issuer := range issuers {
		if !issuer.IsDir() {
			continue
		}
		issuerPath := filepath.Join(root, issuer.Name())
		sites, err := os.ReadDir(issuerPath)
		if err != nil {
			continue
		}
		for _, site := range sites {
			if !site.IsDir() {
				continue
			}
			for _, name := range names {
				if !strings.EqualFold(site.Name(), name) {
					continue
				}
				if err := os.RemoveAll(filepath.Join(issuerPath, site.Name())); err != nil {
					return removed, err
				}
				removed++
				break
			}
		}
	}
	return removed, nil
}

func (a *App) certificateInventory(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,domain,tls_mode,custom_certificate_id,enabled FROM proxy_hosts ORDER BY domain`)
	if err != nil {
		fail(w, 500, err)
		return
	}
	hosts := []Host{}
	for rows.Next() {
		var h Host
		var enabled int
		if err = rows.Scan(&h.ID, &h.Domain, &h.TLSMode, &h.CustomCertificateID, &enabled); err != nil {
			rows.Close()
			fail(w, 500, err)
			return
		}
		h.Enabled = enabled == 1
		hosts = append(hosts, h)
	}
	rows.Close()
	sources, err := a.certificateSources(r.Context(), hosts)
	if err != nil {
		fail(w, 500, err)
		return
	}
	out := []AutomaticCertificate{}
	indices := map[string]int{}
	for _, h := range hosts {
		source := sources[h.ID]
		if source.Kind != "individual" && source.Kind != "internal" {
			continue
		}
		if i, ok := indices[h.Domain]; ok {
			out[i].Sites = append(out[i].Sites, h.Domain)
			continue
		}
		indices[h.Domain] = len(out)
		out = append(out, AutomaticCertificate{Domain: h.Domain, Source: source, Sites: []string{h.Domain}})
	}
	writeJSON(w, 200, out)
}

// expiringCertificate is one certificate that either already expired or will
// expire inside the requested window. Sites that share a wildcard certificate
// are grouped so the operator renews one thing, not eight.
type expiringCertificate struct {
	Domain   string   `json:"domain"`
	Kind     string   `json:"kind"`
	Label    string   `json:"label"`
	NotAfter string   `json:"not_after"`
	DaysLeft int      `json:"days_left"`
	Expired  bool     `json:"expired"`
	Sites    []string `json:"sites"`
}

// expiringCertificates answers "what is about to break?" for the overview and
// the certificate page. It reads the same inventory the SSL page shows, so the
// two views cannot disagree.
func (a *App) expiringCertificates(w http.ResponseWriter, r *http.Request) {
	days := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 && value <= 365 {
			days = value
		}
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,domain,tls_mode,custom_certificate_id FROM proxy_hosts ORDER BY domain`)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("无法获取证书清单"))
		return
	}
	hosts := []Host{}
	for rows.Next() {
		var host Host
		var certID sql.NullInt64
		if err := rows.Scan(&host.ID, &host.Domain, &host.TLSMode, &certID); err != nil {
			rows.Close()
			fail(w, http.StatusInternalServerError, errors.New("无法获取证书清单"))
			return
		}
		if certID.Valid {
			host.CustomCertificateID = &certID.Int64
		}
		hosts = append(hosts, host)
	}
	rows.Close()

	sources, err := a.certificateSources(r.Context(), hosts)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("无法获取证书清单"))
		return
	}

	now := time.Now()
	index := map[string]*expiringCertificate{}
	for _, host := range hosts {
		source, ok := sources[host.ID]
		if !ok || source.NotAfter == "" {
			continue
		}
		notAfter, err := time.Parse(time.RFC3339, source.NotAfter)
		if err != nil {
			continue
		}
		daysLeft := int(notAfter.Sub(now).Hours() / 24)
		if daysLeft > days {
			continue
		}
		key := source.Domain
		if key == "" {
			key = host.Domain
		}
		item, exists := index[key]
		if !exists {
			item = &expiringCertificate{
				Domain:   key,
				Kind:     source.Kind,
				Label:    source.Label,
				NotAfter: source.NotAfter,
				DaysLeft: daysLeft,
				Expired:  notAfter.Before(now),
			}
			index[key] = item
		}
		// Keep the earliest expiry when several sites report the same name.
		if daysLeft < item.DaysLeft {
			item.DaysLeft = daysLeft
			item.NotAfter = source.NotAfter
			item.Expired = notAfter.Before(now)
		}
		item.Sites = append(item.Sites, host.Domain)
	}

	result := make([]expiringCertificate, 0, len(index))
	for _, item := range index {
		sort.Strings(item.Sites)
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].DaysLeft == result[j].DaysLeft {
			return result[i].Domain < result[j].Domain
		}
		return result[i].DaysLeft < result[j].DaysLeft
	})
	writeJSON(w, http.StatusOK, map[string]any{"days": days, "certificates": result})
}
