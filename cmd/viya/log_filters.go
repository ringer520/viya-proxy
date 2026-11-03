package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type logQuery struct {
	Domain, Text, Level, Status string
	From, To                    int64
	Limit                       int
}

func parseLogQuery(r *http.Request) (logQuery, error) {
	q := r.URL.Query()
	v := logQuery{Domain: strings.ToLower(q.Get("domain")), Text: strings.ToLower(q.Get("q")), Level: strings.ToUpper(q.Get("level")), Status: q.Get("status"), Limit: 200}
	if len(v.Text) > 200 || len(v.Domain) > 253 {
		return v, fmt.Errorf("筛选条件过长")
	}
	for key, dest := range map[string]*int64{"from": &v.From, "to": &v.To} {
		if raw := q.Get(key); raw != "" {
			value, e := strconv.ParseInt(raw, 10, 64)
			if e != nil || value < 0 {
				return v, fmt.Errorf("时间范围无效")
			}
			*dest = value
		}
	}
	if v.To > 0 && v.From > v.To {
		return v, fmt.Errorf("结束时间不能早于开始时间")
	}
	if raw := q.Get("limit"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e == nil && n >= 1 && n <= 1000 {
			v.Limit = n
		}
	}
	if v.Level != "" && v.Level != "ALL" && v.Level != "DEBUG" && v.Level != "INFO" && v.Level != "WARN" && v.Level != "ERROR" {
		return v, fmt.Errorf("日志级别无效")
	}
	switch v.Status {
	case "", "all", "errors", "blocked", "2xx", "3xx", "4xx", "5xx":
	default:
		n, e := strconv.Atoi(v.Status)
		if e != nil || n < 100 || n > 599 {
			return v, fmt.Errorf("状态码无效")
		}
	}
	return v, nil
}
func (q logQuery) matches(event map[string]any) bool {
	timestamp, _ := event["ts"].(float64)
	if timestamp == 0 {
		if raw, ok := event["time"].(string); ok {
			if t, e := time.Parse(time.RFC3339Nano, raw); e == nil {
				timestamp = float64(t.Unix())
			}
		}
	}
	if q.From > 0 && timestamp < float64(q.From) || q.To > 0 && timestamp > float64(q.To) {
		return false
	}
	request, _ := event["request"].(map[string]any)
	host, _ := request["host"].(string)
	if q.Domain != "" && hostname(host) != q.Domain {
		return false
	}
	if q.Level != "" && q.Level != "ALL" {
		level, _ := event["level"].(string)
		if strings.ToUpper(level) != q.Level {
			return false
		}
	}
	if q.Status != "" && q.Status != "all" {
		raw, _ := event["status"].(float64)
		status := int(raw)
		switch q.Status {
		case "errors":
			if status < 400 {
				return false
			}
		case "blocked":
			if status != 403 && status != 429 {
				return false
			}
		case "2xx", "3xx", "4xx", "5xx":
			if status/100 != int(q.Status[0]-'0') {
				return false
			}
		default:
			n, _ := strconv.Atoi(q.Status)
			if status != n {
				return false
			}
		}
	}
	if q.Text != "" {
		raw, _ := json.Marshal(event)
		if !strings.Contains(strings.ToLower(string(raw)), q.Text) {
			return false
		}
	}
	return true
}
