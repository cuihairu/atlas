// Alerting for the health monitor (TODO v0.1.15): when the fraction of
// auto-managed servers in suspect/offline status crosses a configured
// threshold, emit a structured log alert and optionally POST a JSON webhook.
//
// Alerts are latched: the "firing" notification is emitted once when the
// ratio crosses the threshold, and a "recovered" notification once when it
// drops back below — no repeated spam on every sweep.
package health

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// AlertConfig controls fleet-level health alerting.
type AlertConfig struct {
	// SuspectRatio fires the "suspect_ratio" alert when
	// suspect / auto-managed >= this value. 0 disables the alert.
	SuspectRatio float64

	// OfflineRatio fires the "offline_ratio" alert when
	// offline / auto-managed >= this value. 0 disables the alert.
	OfflineRatio float64

	// WebhookURL optionally receives a JSON POST when an alert fires or
	// recovers. Empty disables webhook delivery.
	WebhookURL string

	// WebhookTimeout bounds each webhook POST. 0 means 5s.
	WebhookTimeout time.Duration
}

// Alerter tracks fleet health ratios across sweeps and emits latched alerts.
// It is safe for use from the monitor's single sweep goroutine.
type Alerter struct {
	cfg    AlertConfig
	logger *slog.Logger
	client *http.Client
	now    func() time.Time
	firing map[string]bool
}

// NewAlerter creates an alerter from the given config.
func NewAlerter(cfg AlertConfig, logger *slog.Logger) *Alerter {
	timeout := cfg.WebhookTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Alerter{
		cfg:    cfg,
		logger: logger,
		client: &http.Client{Timeout: timeout},
		now:    time.Now,
		firing: make(map[string]bool),
	}
}

// Evaluate compares the post-sweep status counts against the configured
// thresholds. A nil alerter is a no-op.
func (a *Alerter) Evaluate(ctx context.Context, total, suspect, offline int) {
	if a == nil {
		return
	}
	a.evaluate(ctx, "suspect_ratio", a.cfg.SuspectRatio, suspect, total)
	a.evaluate(ctx, "offline_ratio", a.cfg.OfflineRatio, offline, total)
}

func (a *Alerter) evaluate(ctx context.Context, name string, threshold float64, count, total int) {
	if threshold <= 0 {
		return
	}

	ratio := 0.0
	if total > 0 {
		ratio = float64(count) / float64(total)
	}
	isFiring := total > 0 && ratio >= threshold
	wasFiring := a.firing[name]

	switch {
	case isFiring && !wasFiring:
		a.firing[name] = true
		a.emit(ctx, name, count, total, ratio, threshold, true)
	case !isFiring && wasFiring:
		delete(a.firing, name)
		a.emit(ctx, name, count, total, ratio, threshold, false)
	}
}

func (a *Alerter) emit(ctx context.Context, name string, count, total int, ratio, threshold float64, firing bool) {
	state := "recovered"
	if firing {
		state = "firing"
	}
	a.logger.Warn("health alert",
		"alert", name,
		"state", state,
		"count", count,
		"auto_managed_total", total,
		"ratio", fmt.Sprintf("%.3f", ratio),
		"threshold", fmt.Sprintf("%.3f", threshold),
	)

	if a.cfg.WebhookURL == "" {
		return
	}
	a.postWebhook(ctx, webhookPayload{
		Alert:     name,
		State:     state,
		Count:     count,
		Total:     total,
		Ratio:     ratio,
		Threshold: threshold,
		FiredAt:   a.now().UTC().Format(time.RFC3339),
	})
}

type webhookPayload struct {
	Alert     string  `json:"alert"`
	State     string  `json:"state"`
	Count     int     `json:"count"`
	Total     int     `json:"auto_managed_total"`
	Ratio     float64 `json:"ratio"`
	Threshold float64 `json:"threshold"`
	FiredAt   string  `json:"fired_at"`
}

func (a *Alerter) postWebhook(ctx context.Context, payload webhookPayload) {
	body, err := json.Marshal(payload)
	if err != nil {
		a.logger.Error("health alert webhook encode failed", "error", err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		a.logger.Error("health alert webhook request invalid", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "atlas-health-alerts")

	resp, err := a.client.Do(req)
	if err != nil {
		a.logger.Error("health alert webhook delivery failed", "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		a.logger.Error("health alert webhook rejected", "status", resp.StatusCode)
	}
}
