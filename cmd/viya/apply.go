package main

import (
	"context"
	"log/slog"
	"time"
)

func (a *App) apply(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.applyLocked(ctx)
}

// scheduleApply coalesces bursty configuration changes, notably automatic IP
// bans, into at most one Caddy validate + load per window. Without it every
// banned request could fork a Caddy subprocess.
func (a *App) scheduleApply() {
	select {
	case a.applyKick <- struct{}{}:
	default:
	}
}

func (a *App) applyWorker() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-a.applyKick:
			timer := time.NewTimer(500 * time.Millisecond)
			select {
			case <-a.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			// Drop pending kicks; the apply below already covers them.
			select {
			case <-a.applyKick:
			default:
			}
			ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
			if err := a.apply(ctx); err != nil && a.ctx.Err() == nil {
				// Bans stay in the database; the next change or restart retries.
				slog.Warn("deferred Caddy apply failed", "error", err)
			}
			cancel()
		}
	}
}
