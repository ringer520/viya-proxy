package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"viya/internal/secretstore"
)

type DNSProvider struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	ProviderType   string `json:"provider_type"`
	HasCredentials bool   `json:"has_credentials"`
}

type dnsProviderInput struct {
	Name        string            `json:"name"`
	Provider    string            `json:"provider_type"`
	Credentials map[string]string `json:"credentials"`
}

var providerFields = map[string][]string{
	"cloudflare":   {"api_token"},
	"tencentcloud": {"secret_id", "secret_key"},
	"alidns":       {"access_key_id", "access_key_secret"},
}

func (a *App) listDNSProviders(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,name,provider_type,length(credentials_ciphertext)>0 FROM dns_providers ORDER BY name`)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not read DNS providers"))
		return
	}
	defer rows.Close()
	providers := []DNSProvider{}
	for rows.Next() {
		var p DNSProvider
		if err := rows.Scan(&p.ID, &p.Name, &p.ProviderType, &p.HasCredentials); err != nil {
			fail(w, http.StatusInternalServerError, errors.New("could not read DNS providers"))
			return
		}
		providers = append(providers, p)
	}
	if err := rows.Err(); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not read DNS providers"))
		return
	}
	writeJSON(w, http.StatusOK, providers)
}

func (a *App) createDNSProvider(w http.ResponseWriter, r *http.Request) {
	var input dnsProviderInput
	if err := decodeDNSProvider(w, r, &input); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := validateDNSProvider(&input); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not save DNS provider"))
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `INSERT INTO dns_providers(name,provider_type,credentials_ciphertext,credentials_nonce) VALUES(?,?,x'',x'')`, input.Name, input.Provider)
	if err != nil {
		fail(w, http.StatusConflict, errors.New("DNS provider could not be saved"))
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not save DNS provider"))
		return
	}
	if err := a.storeProviderCredentials(r, tx, id, input.Credentials); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not encrypt DNS provider credentials"))
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not save DNS provider"))
		return
	}
	writeJSON(w, http.StatusCreated, DNSProvider{ID: id, Name: input.Name, ProviderType: input.Provider, HasCredentials: true})
}

func (a *App) updateDNSProvider(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, http.StatusBadRequest, errors.New("invalid DNS provider id"))
		return
	}
	var input dnsProviderInput
	if err := decodeDNSProvider(w, r, &input); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := validateDNSProvider(&input); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	var currentType string
	if err := a.db.QueryRowContext(r.Context(), `SELECT provider_type FROM dns_providers WHERE id=?`, id).Scan(&currentType); err != nil {
		fail(w, http.StatusNotFound, errors.New("DNS provider not found"))
		return
	}
	if currentType != input.Provider {
		var references int
		if err := a.db.QueryRowContext(r.Context(), `SELECT (SELECT COUNT(*) FROM proxy_hosts WHERE dns_provider_id=?)+(SELECT COUNT(*) FROM managed_certificates WHERE dns_provider_id=?)+(SELECT COUNT(*) FROM ddns_records WHERE dns_provider_id=?)`, id, id, id).Scan(&references); err != nil {
			fail(w, 500, errors.New("could not check DNS provider references"))
			return
		}
		if references > 0 {
			fail(w, http.StatusConflict, errors.New("provider type cannot be changed while proxy hosts or DDNS records use it"))
			return
		}
	}
	var proxyHostReferences int
	if err := a.db.QueryRowContext(r.Context(), `SELECT (SELECT COUNT(*) FROM proxy_hosts WHERE dns_provider_id=?)+(SELECT COUNT(*) FROM managed_certificates WHERE dns_provider_id=?)`, id, id).Scan(&proxyHostReferences); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not check proxy host references"))
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not update DNS provider"))
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE dns_providers SET name=?,provider_type=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, input.Name, input.Provider, id)
	if err != nil {
		fail(w, http.StatusConflict, errors.New("DNS provider could not be updated"))
		return
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		fail(w, http.StatusNotFound, errors.New("DNS provider not found"))
		return
	}
	if err := a.storeProviderCredentials(r, tx, id, input.Credentials); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not encrypt DNS provider credentials"))
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not update DNS provider"))
		return
	}
	if proxyHostReferences > 0 {
		if err := a.apply(r.Context()); err != nil {
			fail(w, http.StatusBadGateway, errors.New("DNS credentials were saved, but Caddy could not reload them; retry applying the configuration"))
			return
		}
	}
	writeJSON(w, http.StatusOK, DNSProvider{ID: id, Name: input.Name, ProviderType: input.Provider, HasCredentials: true})
}

func (a *App) deleteDNSProvider(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, http.StatusBadRequest, errors.New("invalid DNS provider id"))
		return
	}
	var references int
	if err := a.db.QueryRowContext(r.Context(), `SELECT (SELECT COUNT(*) FROM proxy_hosts WHERE dns_provider_id=?)+(SELECT COUNT(*) FROM managed_certificates WHERE dns_provider_id=?)+(SELECT COUNT(*) FROM ddns_records WHERE dns_provider_id=?)`, id, id, id).Scan(&references); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not check DNS provider references"))
		return
	}
	if references > 0 {
		fail(w, http.StatusConflict, errors.New("DNS provider is used by one or more proxy hosts or DDNS records"))
		return
	}
	result, err := a.db.ExecContext(r.Context(), `DELETE FROM dns_providers WHERE id=?`, id)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not delete DNS provider"))
		return
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		fail(w, http.StatusNotFound, errors.New("DNS provider not found"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) loadDNSProvider(ctx context.Context, id int64) (string, map[string]string, error) {
	var providerType string
	var ciphertext, nonce []byte
	var keyVersion int
	if err := a.db.QueryRowContext(ctx, `SELECT provider_type,credentials_ciphertext,credentials_nonce,key_version FROM dns_providers WHERE id=?`, id).Scan(&providerType, &ciphertext, &nonce, &keyVersion); err != nil {
		return "", nil, errors.New("DNS provider credentials are unavailable")
	}
	if keyVersion != 1 {
		return "", nil, errors.New("unsupported DNS provider credential key version")
	}
	plaintext, err := secretstore.Decrypt(a.masterKey, nonce, ciphertext, []byte(fmt.Sprintf("viya:dns-provider:%d:v1", id)))
	if err != nil {
		return "", nil, errors.New("DNS provider credentials could not be decrypted")
	}
	defer func() {
		for i := range plaintext {
			plaintext[i] = 0
		}
	}()
	var credentials map[string]string
	if err := json.Unmarshal(plaintext, &credentials); err != nil {
		return "", nil, errors.New("DNS provider credentials are invalid")
	}
	return providerType, credentials, nil
}

func decodeDNSProvider(w http.ResponseWriter, r *http.Request, input *dnsProviderInput) error {
	defer r.Body.Close()
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(input); err != nil {
		return errors.New("invalid DNS provider request")
	}
	return nil
}

func validateDNSProvider(input *dnsProviderInput) error {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 100 {
		return errors.New("DNS provider name must be between 1 and 100 characters")
	}
	required, ok := providerFields[input.Provider]
	if !ok {
		return errors.New("unsupported DNS provider type")
	}
	if len(input.Credentials) > 8 {
		return errors.New("too many credential fields")
	}
	allowed := make(map[string]bool, len(required)+1)
	for _, key := range required {
		allowed[key] = true
	}
	if input.Provider == "alidns" {
		allowed["region_id"] = true
	}
	for key, value := range input.Credentials {
		if !allowed[key] || strings.TrimSpace(value) == "" || len(value) > 4096 {
			return fmt.Errorf("invalid credential field %q", key)
		}
	}
	for _, key := range required {
		if strings.TrimSpace(input.Credentials[key]) == "" {
			return fmt.Errorf("credential field %q is required", key)
		}
	}
	return nil
}

func (a *App) storeProviderCredentials(r *http.Request, tx *sql.Tx, id int64, credentials map[string]string) error {
	plaintext, err := json.Marshal(credentials)
	if err != nil {
		return err
	}
	aad := []byte(fmt.Sprintf("viya:dns-provider:%d:v1", id))
	nonce, ciphertext, err := secretstore.Encrypt(a.masterKey, plaintext, aad)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(r.Context(), `UPDATE dns_providers SET credentials_ciphertext=?,credentials_nonce=?,key_version=1 WHERE id=?`, ciphertext, nonce, id)
	return err
}
