package main

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type CustomCertificate struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	DNSNames []string `json:"dns_names"`
	NotAfter string   `json:"not_after"`
}

func certificateMatchesDomain(names []string, domain string) bool {
	for _, name := range names {
		if strings.EqualFold(name, domain) {
			return true
		}
		if strings.HasPrefix(domain, "*.") && strings.EqualFold(name, domain) {
			return true
		}
		if strings.HasPrefix(name, "*.") && !strings.HasPrefix(domain, "*.") {
			suffix := strings.TrimPrefix(strings.ToLower(name), "*")
			if strings.HasSuffix(strings.ToLower(domain), suffix) && strings.Count(strings.TrimSuffix(strings.ToLower(domain), suffix), ".") == 0 {
				return true
			}
		}
	}
	return false
}
func (a *App) listCustomCertificates(w http.ResponseWriter, r *http.Request) {
	rows, e := a.db.QueryContext(r.Context(), `SELECT id,name,dns_names_json,not_after FROM custom_certificates ORDER BY name`)
	if e != nil {
		fail(w, 500, errors.New("could not read certificates"))
		return
	}
	defer rows.Close()
	out := []CustomCertificate{}
	for rows.Next() {
		var v CustomCertificate
		var names string
		if rows.Scan(&v.ID, &v.Name, &names, &v.NotAfter) != nil {
			fail(w, 500, errors.New("could not read certificates"))
			return
		}
		if json.Unmarshal([]byte(names), &v.DNSNames) != nil {
			fail(w, 500, errors.New("stored certificate metadata is invalid"))
			return
		}
		out = append(out, v)
	}
	writeJSON(w, 200, out)
}
func (a *App) createCustomCertificate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if e := r.ParseMultipartForm(2 << 20); e != nil {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		fail(w, 400, errors.New("certificate upload exceeds 2 MiB or is malformed"))
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || len(name) > 100 {
		fail(w, 400, errors.New("certificate name must be 1-100 characters"))
		return
	}
	readPart := func(field string, limit int64) ([]byte, error) {
		f, _, e := r.FormFile(field)
		if e != nil {
			return nil, e
		}
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, limit+1))
		if e != nil || int64(len(b)) > limit {
			return nil, errors.New("upload too large")
		}
		return b, nil
	}
	certPEM, e := readPart("certificate", 1<<20)
	if e != nil {
		fail(w, 400, errors.New("certificate PEM file is required and must be under 1 MiB"))
		return
	}
	keyPEM, e := readPart("private_key", 1<<20)
	if e != nil {
		fail(w, 400, errors.New("private key PEM file is required and must be under 1 MiB"))
		return
	}
	pair, e := tls.X509KeyPair(certPEM, keyPEM)
	if e != nil {
		fail(w, 400, errors.New("certificate and private key are invalid or do not match"))
		return
	}
	leaf, e := x509.ParseCertificate(pair.Certificate[0])
	if e != nil {
		fail(w, 400, errors.New("certificate is invalid"))
		return
	}
	if time.Now().After(leaf.NotAfter) {
		fail(w, 400, errors.New("certificate has expired"))
		return
	}
	if time.Now().Before(leaf.NotBefore) {
		fail(w, 400, errors.New("certificate is not valid yet"))
		return
	}
	if len(leaf.DNSNames) == 0 {
		fail(w, 400, errors.New("certificate must contain DNS Subject Alternative Names"))
		return
	}
	names := leaf.DNSNames
	namesJSON, _ := json.Marshal(names)
	// Write the PEM files before touching the database. If the process dies or
	// the disk fills up mid-upload, the worst that can happen is a pair of
	// orphan files — never a database row whose cert_path/key_path are empty,
	// which would make every subsequent Caddy apply fail validation.
	dir := filepath.Join(a.dataDir, "custom-certs")
	if e = os.MkdirAll(dir, 0700); e != nil {
		fail(w, 500, errors.New("could not create certificate storage"))
		return
	}
	if e = os.Chmod(dir, 0700); e != nil {
		fail(w, 500, errors.New("could not secure certificate storage"))
		return
	}
	suffix, e := randomSecret()
	if e != nil {
		fail(w, 500, errors.New("could not allocate certificate storage"))
		return
	}
	certPath := filepath.Join(dir, fmt.Sprintf("cert-%s.pem", suffix))
	keyPath := filepath.Join(dir, fmt.Sprintf("key-%s.pem", suffix))
	if e = os.WriteFile(certPath, certPEM, 0600); e == nil {
		e = os.WriteFile(keyPath, keyPEM, 0600)
	}
	if e != nil {
		_ = os.Remove(certPath)
		_ = os.Remove(keyPath)
		fail(w, 500, errors.New("could not save certificate files"))
		return
	}
	res, e := a.db.ExecContext(r.Context(), `INSERT INTO custom_certificates(name,cert_path,key_path,dns_names_json,not_after) VALUES(?,?,?,?,?)`, name, certPath, keyPath, string(namesJSON), leaf.NotAfter.UTC().Format(time.RFC3339))
	if e != nil {
		_ = os.Remove(certPath)
		_ = os.Remove(keyPath)
		slog.Error("store certificate metadata", "error", e)
		fail(w, 500, errors.New("could not store certificate metadata"))
		return
	}
	id, e := res.LastInsertId()
	if e != nil {
		_ = os.Remove(certPath)
		_ = os.Remove(keyPath)
		slog.Error("read certificate id", "error", e)
		fail(w, 500, errors.New("could not store certificate metadata"))
		return
	}
	writeJSON(w, 201, CustomCertificate{ID: id, Name: name, DNSNames: names, NotAfter: leaf.NotAfter.UTC().Format(time.RFC3339)})
}
func (a *App) deleteCustomCertificate(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		fail(w, 400, errors.New("invalid certificate id"))
		return
	}
	var used int
	if e = a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM proxy_hosts WHERE custom_certificate_id=?`, id).Scan(&used); e != nil {
		fail(w, 500, errors.New("could not check certificate references"))
		return
	}
	if used > 0 {
		fail(w, 409, errors.New("certificate is used by a proxy host"))
		return
	}
	var certPath, keyPath string
	e = a.db.QueryRowContext(r.Context(), `SELECT cert_path,key_path FROM custom_certificates WHERE id=?`, id).Scan(&certPath, &keyPath)
	if errors.Is(e, sql.ErrNoRows) {
		fail(w, 404, errors.New("certificate not found"))
		return
	}
	if e != nil {
		fail(w, 500, errors.New("could not read certificate"))
		return
	}
	if _, e = a.db.ExecContext(r.Context(), `DELETE FROM custom_certificates WHERE id=?`, id); e != nil {
		fail(w, 500, errors.New("could not delete certificate"))
		return
	}
	_ = os.Remove(certPath)
	_ = os.Remove(keyPath)
	w.WriteHeader(204)
}
