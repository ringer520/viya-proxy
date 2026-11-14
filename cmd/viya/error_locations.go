package main

import (
	"net/http"
	"time"
)

// errorLocation is an aggregated counter: one row per site and HTTP status.
// The panel no longer stores per-request records, so nothing here identifies a
// client, a URL or a user agent.
type errorLocation struct {
	SiteID int64  `json:"site_id"`
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Status int    `json:"status"`
	Count  int64  `json:"count"`
}

// webErrorLocations reads the same hourly aggregates the traffic card uses, so
// the breakdown always adds up to the error count shown above it.
func (a *App) webErrorLocations(w http.ResponseWriter, r *http.Request) {
	window, hours := trafficWindow(r)
	cutoff := time.Now().Unix()/3600*3600 - int64(hours-1)*3600
	rows, err := a.db.QueryContext(r.Context(), `SELECT h.id,h.name,h.domain,e.status,SUM(e.count) FROM site_error_hours e JOIN proxy_hosts h ON h.id=e.site_id WHERE e.hour>=? AND e.status BETWEEN 400 AND 599 GROUP BY h.id,e.status ORDER BY 5 DESC,h.id,e.status`, cutoff)
	if err != nil {
		fail(w, 500, err)
		return
	}
	defer rows.Close()
	out := []errorLocation{}
	for rows.Next() {
		var group errorLocation
		if err = rows.Scan(&group.SiteID, &group.Name, &group.Domain, &group.Status, &group.Count); err != nil {
			fail(w, 500, err)
			return
		}
		out = append(out, group)
	}
	if err = rows.Err(); err != nil {
		fail(w, 500, err)
		return
	}
	// Requests in the same window give the panel a denominator for the share.
	var requests int64
	if err = a.db.QueryRowContext(r.Context(), `SELECT COALESCE(SUM(requests),0) FROM traffic_hours WHERE hour>=?`, cutoff).Scan(&requests); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"groups": out, "requests": requests, "window": window})
}
