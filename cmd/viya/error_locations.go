package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

type errorLocation struct {
	SiteID int64         `json:"site_id"`
	Name   string        `json:"name"`
	Domain string        `json:"domain"`
	Status int           `json:"status"`
	Count  int           `json:"count"`
	Events []accessEntry `json:"events"`
}

// This view deliberately uses retained events, never extrapolates a full-period count.
func (a *App) webErrorLocations(w http.ResponseWriter, r *http.Request) {
	hours := 24
	switch r.URL.Query().Get("range") {
	case "7d":
		hours = 168
	case "30d":
		hours = 720
	}
	cutoff := time.Now().Unix()/3600*3600 - int64(hours-1)*3600
	rows, err := a.db.QueryContext(r.Context(), `SELECT h.id,h.name,h.domain,e.event FROM site_access_events e JOIN proxy_hosts h ON h.id=e.site_id WHERE json_extract(e.event,'$.ts')>=? AND json_extract(e.event,'$.status') BETWEEN 400 AND 599 ORDER BY e.id DESC LIMIT 20001`, cutoff)
	if err != nil {
		fail(w, 500, err)
		return
	}
	defer rows.Close()
	groups := map[[2]int64]*errorLocation{}
	scanned := 0
	limited := false
	for rows.Next() {
		var id int64
		var name, domain, raw string
		if err = rows.Scan(&id, &name, &domain, &raw); err != nil {
			fail(w, 500, err)
			return
		}
		scanned++
		if scanned > 20000 {
			limited = true
			break
		}
		var event accessEntry
		if json.Unmarshal([]byte(raw), &event) != nil || strings.HasPrefix(event.Logger, "http.log.error") {
			continue
		}
		key := [2]int64{id, int64(event.Status)}
		group := groups[key]
		if group == nil {
			group = &errorLocation{SiteID: id, Name: name, Domain: domain, Status: event.Status, Events: []accessEntry{}}
			groups[key] = group
		}
		group.Count++
		if len(group.Events) < 30 {
			group.Events = append(group.Events, event)
		}
	}
	if err = rows.Err(); err != nil {
		fail(w, 500, err)
		return
	}
	out := []*errorLocation{}
	for _, g := range groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].SiteID != out[j].SiteID {
			return out[i].SiteID < out[j].SiteID
		}
		return out[i].Status < out[j].Status
	})
	writeJSON(w, 200, map[string]any{"groups": out, "recent_only": true, "limited": limited})
}
