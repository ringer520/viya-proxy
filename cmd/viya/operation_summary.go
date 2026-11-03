package main

import (
	"context"
	"net/http"
	"strings"
)

type operationContextKey struct{}
type operationContext struct{ Summary, Actor string }

func requestOperation(r *http.Request, actor string) context.Context {
	summary := "应用配置"
	names := []struct{ Path, Name string }{{"/proxy-hosts", "Web 站点"}, {"/layer4-rules", "端口转发"}, {"/ip-bans", "访问限制"}, {"/dns-providers", "DNS 凭据"}, {"/managed-certificates", "证书申请"}, {"/custom-certificates", "上传证书"}, {"/web-listeners", "Web 监听"}, {"/security-defaults", "安全策略"}, {"/logs/settings", "日志设置"}, {"/config/import", "导入业务配置"}, {"/config/rollback", "恢复配置点"}}
	for _, item := range names {
		if strings.Contains(r.URL.Path, item.Path) {
			action := "修改"
			if r.Method == "POST" {
				action = "添加"
			}
			if r.Method == "DELETE" {
				action = "删除"
			}
			summary = action + item.Name
			if id := r.PathValue("id"); id != "" {
				summary += " #" + id
			}
			break
		}
	}
	if strings.HasSuffix(r.URL.Path, "/enabled") {
		summary = "调整启停状态 · " + summary
	}
	return context.WithValue(r.Context(), operationContextKey{}, operationContext{summary, actor})
}
