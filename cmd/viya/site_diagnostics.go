package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type diagnosticStep struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Detail string `json:"detail"`
	Advice string `json:"advice,omitempty"`
}

func (a *App) diagnoseSite(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		fail(w, 400, errors.New("invalid site id"))
		return
	}
	var h Host
	var enabled int
	e = a.db.QueryRowContext(r.Context(), `SELECT id,domain,listener_id,upstream,type,root_path,path,tls_mode,enabled,custom_certificate_id FROM proxy_hosts WHERE id=?`, id).Scan(&h.ID, &h.Domain, &h.ListenerID, &h.Upstream, &h.Type, &h.RootPath, &h.Path, &h.TLSMode, &enabled, &h.CustomCertificateID)
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			fail(w, 404, errors.New("站点不存在"))
		} else {
			fail(w, 500, e)
		}
		return
	}
	h.Enabled = enabled == 1
	ctx, cancel := context.WithTimeout(r.Context(), 14*time.Second)
	defer cancel()
	steps := []diagnosticStep{}
	var served map[string]any
	add := func(name, state, detail, advice string) {
		steps = append(steps, diagnosticStep{name, state, detail, advice})
	}
	if strings.HasPrefix(h.Domain, "*.") {
		add("DNS 解析", "info", "泛域名规则需要具体子域名才能验证访问链路", "请选择一个真实子域名检查公网解析")
	} else {
		dnsCtx, stop := context.WithTimeout(ctx, 2*time.Second)
		ips, err := net.DefaultResolver.LookupIPAddr(dnsCtx, h.Domain)
		stop()
		if err != nil {
			add("DNS 解析", "fail", "服务器未能解析该域名", "检查 A / AAAA / CNAME 和 DNS 服务器")
		} else {
			values := []string{}
			for _, ip := range ips {
				values = append(values, ip.IP.String())
			}
			add("DNS 解析", "pass", strings.Join(values, ", "), "结果来自服务器的 DNS 解析，不代表公网各地解析一致")
		}
	}
	listeners, e := a.webListeners(ctx)
	if e != nil {
		fail(w, 500, e)
		return
	}
	var chosen *WebListener
	for i := range listeners {
		if listeners[i].ID == h.ListenerID {
			chosen = &listeners[i]
			break
		}
	}
	if chosen == nil {
		add("本机监听", "fail", "站点没有有效监听器", "重新选择监听器")
	} else {
		address := net.JoinHostPort("127.0.0.1", strconv.Itoa(chosen.Port))
		conn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", address)
		if err != nil {
			add("本机监听", "fail", address+" 无法连接", "检查引擎状态、端口冲突和配置应用结果")
		} else {
			conn.Close()
			add("本机监听", "pass", chosen.Protocol+" "+address, "本机可连接不等于公网防火墙与路由已放行")
		}
		if chosen.Protocol == "https" && h.Enabled && !strings.HasPrefix(h.Domain, "*.") {
			dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 3 * time.Second}, Config: &tls.Config{ServerName: h.Domain, InsecureSkipVerify: true}}
			c, err := dialer.DialContext(ctx, "tcp", address)
			if err != nil {
				add("TLS 证书", "fail", "TLS 握手失败", "检查证书签发进度、证书覆盖范围和监听协议")
			} else {
				certs := c.(*tls.Conn).ConnectionState().PeerCertificates
				c.Close()
				if len(certs) > 0 {
					cert := certs[0]
					fingerprint := sha256.Sum256(cert.Raw)
					served = map[string]any{"dns_names": cert.DNSNames, "not_after": cert.NotAfter.UTC().Format(time.RFC3339), "issuer": cert.Issuer.CommonName, "fingerprint": hex.EncodeToString(fingerprint[:]), "hostname_valid": cert.VerifyHostname(h.Domain) == nil, "valid_now": time.Now().After(cert.NotBefore) && time.Now().Before(cert.NotAfter)}
					pool := x509.NewCertPool()
					for _, issuer := range certs[1:] {
						pool.AddCert(issuer)
					}
					_, verifyErr := cert.Verify(x509.VerifyOptions{DNSName: h.Domain, Intermediates: pool})
					state := "pass"
					advice := ""
					if verifyErr != nil {
						state = "warn"
						advice = "证书域名、有效期或信任链验证未通过；内部 CA 需客户端自行信任"
					}
					add("TLS 证书", state, fmt.Sprintf("%s · 到期 %s", cert.Subject.CommonName, cert.NotAfter.Format("2006-01-02")), advice)
				}
			}
		} else {
			add("TLS 证书", "info", "HTTP、停用或泛域名规则不执行本机 TLS 检查", "")
		}
		if !h.Enabled {
			add("本机路由", "warn", "站点已停用", "启用站点后才能验证路由")
		} else if !strings.HasPrefix(h.Domain, "*.") {
			transport := &http.Transport{Proxy: nil, DialContext: func(c context.Context, network, addr string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(c, network, address)
			}, TLSClientConfig: &tls.Config{InsecureSkipVerify: true, ServerName: h.Domain}}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			req, requestErr := http.NewRequestWithContext(ctx, "HEAD", chosen.Protocol+"://"+h.Domain+h.Path+"/", nil)
			if requestErr != nil {
				add("本机路由", "fail", "访问路径无法生成有效请求", "检查路径中的百分号转义")
			} else {
				resp, err := client.Do(req)
				if err != nil {
					add("本机路由", "fail", "没有收到 HTTP 响应", "检查上游、访问策略和配置历史")
				} else {
					resp.Body.Close()
					state := "pass"
					if resp.StatusCode >= 400 {
						state = "warn"
					}
					add("本机路由", state, resp.Status, "401/403 可能是访问保护；404 也可能来自上游。此检查使用本机入口和站点 Host")
				}
			}
		}
	}
	if h.Type == "static" {
		info, err := os.Stat(h.RootPath)
		if err != nil || !info.IsDir() {
			add("静态目录", "fail", "目录不存在或不可读取", "检查托管目录与权限")
		} else {
			add("静态目录", "pass", h.RootPath, "")
		}
	} else if h.Type == "redirect" {
		add("重定向目标", "info", "重定向规则不需要连接上游", "")
	} else {
		u, err := parseUpstream(h.Upstream)
		if err != nil {
			add("上游连接", "fail", "上游地址无效", "")
		} else {
			conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", u.Host)
			if err != nil {
				add("上游连接", "fail", u.Host+" 无法连接", "检查目标地址、端口、防火墙和服务是否启动")
			} else {
				conn.Close()
				add("上游连接", "pass", u.Host+" TCP 可连接", "TCP 可连接不代表业务接口正常")
			}
		}
	}
	source, err := a.certificateSources(ctx, []Host{h})
	if err != nil {
		add("证书关联", "warn", "暂时无法读取证书来源", "")
	}
	writeJSON(w, 200, map[string]any{"site_id": id, "checked_at": time.Now().UTC().Format(time.RFC3339), "scope": "server-local", "steps": steps, "certificate": source[id], "served_certificate": served})
}
