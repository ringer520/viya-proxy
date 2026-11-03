package main

import (
	"context"
	"encoding/json"
	"fmt"
	gnet "github.com/shirou/gopsutil/v4/net"
	"net"
	"net/http"
	"strconv"
	"time"
)

type forwardRuntime struct {
	State     string `json:"state"`
	Message   string `json:"message"`
	CheckedAt int64  `json:"checked_at"`
}

func (a *App) forwardRuntimeStates(ctx context.Context, rules []Layer4Rule) {
	body, err := a.livePolicyConfig(ctx)
	var config struct {
		Apps struct {
			Layer4 struct {
				Servers map[string]json.RawMessage `json:"servers"`
			} `json:"layer4"`
		} `json:"apps"`
	}
	if err == nil {
		err = json.Unmarshal(body, &config)
	}
	var sockets []gnet.ConnectionStat
	var socketErr error
	pid := int32(a.caddyPID.Load())
	if pid > 0 {
		sockets, socketErr = gnet.ConnectionsPidWithContext(ctx, "all", pid)
	} else {
		socketErr = fmt.Errorf("engine PID unavailable")
	}
	for i := range rules {
		v := &rules[i]
		state := forwardRuntime{"unknown", "无法读取引擎配置", time.Now().Unix()}
		if err == nil {
			state = forwardRuntime{"disabled", "规则已停用", time.Now().Unix()}
			found := 0
			bound := 0
			matches := true
			for _, protocol := range l4Protocols(v.Protocol) {
				raw, ok := config.Apps.Layer4.Servers[fmt.Sprintf("viya_%s_%d", protocol, v.ID)]
				if !ok {
					matches = false
					continue
				}
				found++
				var server struct {
					Listen []string `json:"listen"`
					Routes []struct {
						Handle []struct {
							Upstreams []struct {
								Dial []string `json:"dial"`
							} `json:"upstreams"`
						} `json:"handle"`
					} `json:"routes"`
				}
				if json.Unmarshal(raw, &server) != nil {
					matches = false
					continue
				}
				expected := protocol + "/" + net.JoinHostPort(v.ListenAddress, strconv.Itoa(v.ListenPort))
				if len(server.Listen) != 1 || server.Listen[0] != expected {
					matches = false
				}
				dial := ""
				if len(server.Routes) > 0 && len(server.Routes[0].Handle) > 0 && len(server.Routes[0].Handle[0].Upstreams) > 0 && len(server.Routes[0].Handle[0].Upstreams[0].Dial) > 0 {
					dial = server.Routes[0].Handle[0].Upstreams[0].Dial[0]
				}
				if dial != l4Dial(protocol, v.Upstream) {
					matches = false
				}
				for _, socket := range sockets {
					socketProtocol := "tcp"
					if socket.Type == 2 {
						socketProtocol = "udp"
					}
					ip := net.ParseIP(socket.Laddr.IP)
					requested := net.ParseIP(v.ListenAddress)
					if socketProtocol == protocol && socket.Laddr.Port == uint32(v.ListenPort) && ip != nil && (ip.Equal(requested) || ip.IsUnspecified() && requested.IsUnspecified()) && (protocol == "udp" || socket.Status == "LISTEN") {
						bound++
						break
					}
				}
			}
			if !v.Enabled && found > 0 {
				state.State = "still-active"
				state.Message = "停用尚未生效，旧监听仍在引擎中"
			} else if v.Enabled {
				state.State = "pending"
				state.Message = "配置尚未加载，请检查配置历史"
				if matches && found == len(l4Protocols(v.Protocol)) {
					state.State = "loaded"
					state.Message = "引擎已加载，套接字尚未核实"
					if socketErr == nil {
						if bound == found {
							state.State = "listening"
							state.Message = "已核实 Caddy 正在监听；上游连通性需单独判断"
						} else {
							state.State = "unbound"
							state.Message = "已加载但未发现对应监听，请刷新核实"
						}
					}
				}
			}
		}
		v.Runtime = &state
	}
}
func (a *App) checkForwardSocket(ctx context.Context, id int64, v Layer4Rule) error {
	if !v.Enabled {
		return nil
	}
	var old Layer4Rule
	var enabled int
	_ = a.db.QueryRowContext(ctx, `SELECT listen_address,listen_port,protocol,enabled FROM layer4_rules WHERE id=?`, id).Scan(&old.ListenAddress, &old.ListenPort, &old.Protocol, &enabled)
	for _, protocol := range l4Protocols(v.Protocol) {
		if a.caddyPID.Load() > 0 && enabled == 1 && old.ListenAddress == v.ListenAddress && old.ListenPort == v.ListenPort && (old.Protocol == protocol || old.Protocol == "both") {
			continue
		}
		address := net.JoinHostPort(v.ListenAddress, strconv.Itoa(v.ListenPort))
		if protocol == "tcp" {
			listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
			if err != nil {
				return fmt.Errorf("TCP 监听 %s 不可用：端口被占用、地址不在本机或权限不足", address)
			}
			listener.Close()
		} else {
			listener, err := (&net.ListenConfig{}).ListenPacket(ctx, "udp", address)
			if err != nil {
				return fmt.Errorf("UDP 监听 %s 不可用：端口被占用、地址不在本机或权限不足", address)
			}
			listener.Close()
		}
	}
	return nil
}
func (a *App) checkForwardPort(w http.ResponseWriter, r *http.Request) {
	var v Layer4Rule
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&v) != nil {
		fail(w, 400, fmt.Errorf("无效转发格式"))
		return
	}
	if err := validateLayer4Rule(&v); err != nil {
		fail(w, 400, err)
		return
	}
	if err := a.checkLayer4Conflict(r, v.ID, v); err != nil {
		fail(w, 409, err)
		return
	}
	if err := a.checkForwardSocket(r.Context(), v.ID, v); err != nil {
		fail(w, 409, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "当前检测通过；保存时会再次检查，端口不会被预先保留"})
}
