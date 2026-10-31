package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type DNSZoneOption struct {
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
}

type DNSZoneList struct {
	Supported bool            `json:"supported"`
	Zones     []DNSZoneOption `json:"zones"`
	Message   string          `json:"message,omitempty"`
}

type cloudflareZonePage struct {
	Success    bool            `json:"success"`
	Result     []DNSZoneOption `json:"result"`
	ResultInfo struct {
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

func (a *App) listDNSZones(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, http.StatusBadRequest, errors.New("invalid DNS provider id"))
		return
	}
	kind, credentials, err := a.loadDNSProvider(r.Context(), id)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if kind != "cloudflare" {
		writeJSON(w, http.StatusOK, DNSZoneList{Supported: false, Zones: []DNSZoneOption{}, Message: "该服务商暂不支持自动列出 Zone，请手动输入。"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	zones, status, err := cloudflareZones(ctx, credentials["api_token"])
	if err != nil {
		message := "读取 Cloudflare Zone 失败，请检查网络和 DNS 凭据。"
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			message = "Cloudflare 拒绝读取 Zone；请确认凭据具备 Zone:Read 权限。"
		} else if status > 0 {
			message = fmt.Sprintf("Cloudflare Zone 查询失败（HTTP %d）。", status)
		}
		fail(w, http.StatusBadGateway, errors.New(message))
		return
	}
	writeJSON(w, http.StatusOK, DNSZoneList{Supported: true, Zones: zones})
}

func cloudflareZones(ctx context.Context, token string) ([]DNSZoneOption, int, error) {
	if token == "" {
		return nil, 0, errors.New("Cloudflare API token is empty")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	zones := []DNSZoneOption{}
	for page := 1; page <= 100; page++ {
		query := url.Values{"page": {strconv.Itoa(page)}, "per_page": {"100"}}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.cloudflare.com/client/v4/zones?"+query.Encode(), nil)
		if err != nil {
			return nil, 0, err
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Accept", "application/json")
		response, err := client.Do(request)
		if err != nil {
			return nil, 0, err
		}
		var result cloudflareZonePage
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&result)
		response.Body.Close()
		if decodeErr != nil {
			return nil, response.StatusCode, decodeErr
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 || !result.Success {
			return nil, response.StatusCode, errors.New("Cloudflare did not return a successful zone list")
		}
		zones = append(zones, result.Result...)
		if page >= result.ResultInfo.TotalPages || len(result.Result) == 0 {
			return zones, 0, nil
		}
	}
	return zones, 0, nil
}
