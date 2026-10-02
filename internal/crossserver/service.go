// Package crossserver implements the config center for game-server
// coordination config (跨服协调配置): hosting, versioning, pull-on-start,
// and notify-then-pull updates.
//
// Scope boundary (docs/config-center.md §3): Atlas hosts *coordination*
// config only — cross-server topology, participation groups, feature
// switches, match domains, plus server tags/profiles. Game numeric and
// business config belong to the game's own config pipeline.
//
// Update semantics are notify-then-pull: a change publishes a signal
// carrying the new version/hash (never the body) and receivers pull
// GET /v1/crossserver/config themselves. Three notification modes are
// supported and declared per server at register time:
//
//	subscribe — the server subscribes to the config topic on the message
//	            bus and receives the signal there (survives reconnects: it
//	            re-subscribes and immediately re-pulls);
//	callback  — the server registers a callback URL and Atlas POSTs the
//	            signal to it (retried with backoff; sustained failure is
//	            logged as a degradation so operators can fall back);
//	poll      — the server polls version/ETag itself (the third fallback,
//	            also the effective behavior when nothing is declared).
package crossserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cuihairu/atlas/internal/event"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// Service hosts the cross-server config document and fans out change
// signals. It never returns the config body through the bus.
type Service struct {
	cfgStore store.CrossServerConfigStore
	servers  store.ServerStore
	events   event.EventAdapter
	logger   *slog.Logger
	client   *http.Client

	// Callback dispatch policy.
	timeout    time.Duration
	attempts   int
	baseBackof time.Duration
	maxBackoff time.Duration
	parallel   int
	publicURL  string
}

// CallbackResult summarizes one callback fan-out round.
type CallbackResult struct {
	Targets   int      `json:"targets"`
	Delivered int      `json:"delivered"`
	Failed    int      `json:"failed"`
	Errors    []string `json:"errors,omitempty"`
}

// NotifyResult reports how a change was signalled. It is returned by Save
// for operator visibility: a failed fan-out never rolls back a persisted
// config, it only degrades to poll until the receiver catches up.
type NotifyResult struct {
	Bus        string         `json:"bus"`
	BusError   string         `json:"bus_error,omitempty"`
	Targets    []string       `json:"targets"`
	Idempotent bool           `json:"idempotent"`
	Callbacks  CallbackResult `json:"callbacks"`
}

// SaveResult is the outcome of a config save.
type SaveResult struct {
	Config *model.CrossServerConfig `json:"config"`
	Notify NotifyResult             `json:"notify"`
}

// New creates a config center service. events may be nil (no bus: only
// callback and poll modes are available).
func New(cfgStore store.CrossServerConfigStore, servers store.ServerStore, events event.EventAdapter, logger *slog.Logger) *Service {
	return &Service{
		cfgStore:   cfgStore,
		servers:    servers,
		events:     events,
		logger:     logger,
		client:     &http.Client{Timeout: 5 * time.Second},
		timeout:    3 * time.Second,
		attempts:   3,
		baseBackof: 200 * time.Millisecond,
		maxBackoff: 5 * time.Second,
		parallel:   8,
	}
}

// WithCallbackPolicy overrides the callback dispatch policy (timeout,
// attempts, base backoff). Attempts <= 0 disables retries.
func (s *Service) WithCallbackPolicy(timeout time.Duration, attempts int, baseBackoff time.Duration) *Service {
	if timeout > 0 {
		s.timeout = timeout
		s.client = &http.Client{Timeout: timeout}
	}
	if attempts >= 0 {
		s.attempts = attempts
	}
	if baseBackoff > 0 {
		s.baseBackof = baseBackoff
	}
	return s
}

// PublicConfigPath is where receivers pull the config. It is part of the
// public API surface and is echoed in callback notifications.
const PublicConfigPath = "/v1/crossserver/config"

// WithPublicURL sets the externally reachable base URL of the public API
// (e.g. "http://127.0.0.1:8080"). Callback notifications echo
// base+PublicConfigPath so a receiver can pull without a second constant.
func (s *Service) WithPublicURL(base string) *Service {
	s.publicURL = strings.TrimRight(base, "/")
	return s
}

// Get returns the current config, or an ErrNotFound-wrapped error when
// nothing has ever been published. This is the strict pull behind
// GET /v1/crossserver/config: a booting game server fails fast and loudly
// instead of silently running without coordination config.
func (s *Service) Get(ctx context.Context) (*model.CrossServerConfig, error) {
	cfg, err := s.cfgStore.GetCrossServerConfig(ctx)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("cross-server config: %w", store.ErrNotFound)
		}
		return nil, err
	}
	cfg.Spec = model.NormalizeCrossServerSpec(cfg.Spec)
	return cfg, nil
}

// Snapshot returns the current config, substituting the version-0 empty
// snapshot when nothing was ever published. A never-configured center is
// not an outage: the registration response uses this so a booting server
// still learns the baseline version (0) to converge from.
func (s *Service) Snapshot(ctx context.Context) (*model.CrossServerConfig, error) {
	cfg, err := s.cfgStore.GetCrossServerConfig(ctx)
	if err != nil {
		if isNotFound(err) {
			return model.EmptyCrossServerConfig(), nil
		}
		return nil, err
	}
	cfg.Spec = model.NormalizeCrossServerSpec(cfg.Spec)
	return cfg, nil
}

// Since is the conditional pull used by polling receivers: it reports
// whether anything changed since knownVersion/knownHash. A knownVersion of
// 0 means "I have nothing" and always reports changed.
func (s *Service) Since(ctx context.Context, knownVersion int, knownHash string) (*model.CrossServerConfig, bool, error) {
	cfg, err := s.Snapshot(ctx)
	if err != nil {
		return nil, false, err
	}
	if knownVersion > 0 && cfg.Version <= knownVersion {
		return cfg, false, nil
	}
	if knownHash != "" && cfg.Hash == knownHash {
		return cfg, false, nil
	}
	return cfg, true, nil
}

// UpdateRequest is a full-document publish. Previous is the snapshot the
// caller based its edit on (nil when nothing was published yet); the diff
// basis is always re-read from the store, so a stale Previous can never
// produce a wrong target list — it is only a hint.
type UpdateRequest struct {
	Spec     model.CrossServerSpec
	Previous *model.CrossServerConfig
}

// Update publishes spec and returns the stored snapshot.
func (s *Service) Update(ctx context.Context, req UpdateRequest) (*model.CrossServerConfig, error) {
	res, err := s.Save(ctx, req.Spec)
	if err != nil {
		return nil, err
	}
	return res.Config, nil
}

// Save validates, hashes and persists spec as the new config, then signals
// receivers. Content identical to the stored config is idempotent: the
// version does not move and nobody is notified.
func (s *Service) Save(ctx context.Context, spec model.CrossServerSpec) (*SaveResult, error) {
	spec = model.NormalizeCrossServerSpec(spec)
	if err := model.ValidateCrossServerSpec(spec); err != nil {
		return nil, fmt.Errorf("%w: %s", model.ErrInvalid, err)
	}

	prev, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}

	hash := model.HashCrossServerSpec(spec)
	if prev.Version > 0 && prev.Hash == hash {
		// Idempotent write: same content, no version bump, no signal.
		return &SaveResult{
			Config: prev,
			Notify: NotifyResult{
				Bus:        s.busName(),
				Targets:    []string{},
				Idempotent: true,
			},
		}, nil
	}

	saved, err := s.cfgStore.SaveCrossServerConfig(ctx, &model.CrossServerConfig{Hash: hash, Spec: spec})
	if err != nil {
		return nil, err
	}
	saved.Spec = model.NormalizeCrossServerSpec(saved.Spec)

	targets := diffTargets(prev.Spec, saved.Spec)
	notify := s.Notify(ctx, saved, targets)
	return &SaveResult{Config: saved, Notify: notify}, nil
}

// Notify signals a change on the bus and by callback. It never returns an
// error: the config is already persisted, and receivers fall back to
// polling when a signal is lost.
func (s *Service) Notify(ctx context.Context, cfg *model.CrossServerConfig, targets []string) NotifyResult {
	if targets == nil {
		targets = []string{model.TargetAll}
	}
	res := NotifyResult{Bus: s.busName(), Targets: targets}

	if s.events != nil {
		e := &event.Event{
			Type:          event.EventConfigUpdated,
			ConfigVersion: cfg.Version,
			ConfigHash:    cfg.Hash,
			ConfigTargets: targets,
			Timestamp:     time.Now(),
		}
		if err := s.events.Publish(ctx, e); err != nil {
			// No in-process subscriber is the normal case for the http
			// adapter: the signal simply has no local receiver.
			res.BusError = err.Error()
			if s.logger != nil {
				s.logger.Warn("config update signal not delivered on bus",
					"adapter", s.events.Name(), "version", cfg.Version, "error", err)
			}
		} else if s.logger != nil {
			s.logger.Info("config update signaled on bus",
				"adapter", s.events.Name(), "version", cfg.Version,
				"hash", cfg.Hash, "targets", targets)
		}
	}

	res.Callbacks = s.dispatchCallbacks(ctx, cfg, targets)
	return res
}

func (s *Service) busName() string {
	if s.events == nil {
		return "none"
	}
	return s.events.Name()
}

// dispatchCallbacks POSTs the change signal to every online server that
// declared notify_mode=callback. Delivery is retried with exponential
// backoff; exhausted targets are reported so the operator sees the
// degradation instead of a silently missed update.
func (s *Service) dispatchCallbacks(ctx context.Context, cfg *model.CrossServerConfig, targets []string) CallbackResult {
	var res CallbackResult
	if s.servers == nil {
		return res
	}
	list, err := s.servers.ListServers(ctx, store.ServerFilter{Limit: 1000})
	if err != nil {
		res.Errors = append(res.Errors, "list servers: "+err.Error())
		return res
	}

	var todo []callbackTarget
	for _, srv := range list {
		if srv.NotifyMode != model.NotifyModeCallback || srv.NotifyCallbackURL == "" {
			continue
		}
		// Subscription lifetime follows registration (docs/config-center.md
		// §4): a deregistered server is offline and stops receiving.
		if !srv.Status.AcceptsTraffic() {
			continue
		}
		todo = append(todo, callbackTarget{id: srv.ID, url: srv.NotifyCallbackURL})
	}
	res.Targets = len(todo)
	if len(todo) == 0 {
		return res
	}

	payload := callbackPayload{
		Type:           string(event.EventConfigUpdated),
		Version:        cfg.Version,
		Hash:           cfg.Hash,
		Targets:        targets,
		CrossServerURL: s.publicURL + PublicConfigPath,
		UpdatedAt:      cfg.UpdatedAt,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		res.Errors = append(res.Errors, "encode payload: "+err.Error())
		return res
	}

	sem := make(chan struct{}, s.parallel)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, t := range todo {
		wg.Add(1)
		go func(t callbackTarget) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := s.postWithRetry(ctx, t, body); err != nil {
				mu.Lock()
				res.Failed++
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", t.id, err))
				mu.Unlock()
				if s.logger != nil {
					s.logger.Warn("config update callback failed — receiver should fall back to polling",
						"server_id", t.id, "version", cfg.Version, "attempts", s.attempts, "error", err)
				}
				return
			}
			mu.Lock()
			res.Delivered++
			mu.Unlock()
		}(t)
	}
	wg.Wait()
	sort.Strings(res.Errors)
	if s.logger != nil {
		s.logger.Info("config update callbacks dispatched",
			"version", cfg.Version, "targets", res.Targets,
			"delivered", res.Delivered, "failed", res.Failed)
	}
	return res
}

// postWithRetry delivers the signal, retrying with exponential backoff.
// The response is a signal, not a data transfer: any 2xx means "I heard
// it, I will pull" — the receiver then fetches the config itself.
func (s *Service) postWithRetry(ctx context.Context, t callbackTarget, body []byte) error {
	attempts := s.attempts
	if attempts < 1 {
		attempts = 1
	}
	backoff := s.baseBackof
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			if backoff *= 2; backoff > s.maxBackoff {
				backoff = s.maxBackoff
			}
		}
		if err := s.postOnce(ctx, t, body); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("after %d attempts: %w", attempts, lastErr)
}

func (s *Service) postOnce(ctx context.Context, t callbackTarget, body []byte) error {
	reqCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Atlas-Server-Id", t.id)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Drain a bounded amount so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("callback returned %s", resp.Status)
	}
	return nil
}

// callbackTarget is one server's notification endpoint.
type callbackTarget struct{ id, url string }

// callbackPayload is the wire body of a callback notification. It carries
// the version/hash signal and the changed targets — never the config.
type callbackPayload struct {
	Type    string   `json:"type"`
	Version int      `json:"version"`
	Hash    string   `json:"hash"`
	Targets []string `json:"targets"`
	// CrossServerURL tells the receiver where to pull, so it does not have
	// to hardcode a second base URL. Set from ATLAS_PUBLIC_URL.
	CrossServerURL string    `json:"crossserver_url,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// diffTargets computes which clusters / groups / match domains changed so
// subscribers can ignore signals that do not concern them. Feature-switch
// changes are global (TargetAll).
func diffTargets(old, updated model.CrossServerSpec) []string {
	old = model.NormalizeCrossServerSpec(old)
	updated = model.NormalizeCrossServerSpec(updated)

	set := map[string]bool{}
	diffClusters(old.Topology.Clusters, updated.Topology.Clusters, set)
	diffGroups(old.Groups, updated.Groups, set)
	diffDomains(old.MatchDomains, updated.MatchDomains, set)
	if !sameFeatures(old.Features, updated.Features) {
		set[model.TargetAll] = true
	}
	if len(set) == 0 {
		// Structurally identical but the hash moved (e.g. a rename that
		// kept every id): treat as global so nobody misses it.
		return []string{model.TargetAll}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func diffClusters(old, updated []model.CrossServerCluster, set map[string]bool) {
	index := func(in []model.CrossServerCluster) map[string]model.CrossServerCluster {
		m := make(map[string]model.CrossServerCluster, len(in))
		for _, c := range in {
			m[c.ID] = c
		}
		return m
	}
	o, u := index(old), index(updated)
	for id := range u {
		if prev, ok := o[id]; !ok || !clusterEqual(prev, u[id]) {
			set[id] = true
		}
	}
	for id := range o {
		if _, ok := u[id]; !ok {
			set[id] = true
		}
	}
}

func clusterEqual(a, b model.CrossServerCluster) bool {
	return a.Name == b.Name && a.Region == b.Region && a.Status == b.Status &&
		stringsEqual(a.Servers, b.Servers)
}

func diffGroups(old, updated []model.CrossServerGroup, set map[string]bool) {
	index := func(in []model.CrossServerGroup) map[string]model.CrossServerGroup {
		m := make(map[string]model.CrossServerGroup, len(in))
		for _, g := range in {
			m[g.ID] = g
		}
		return m
	}
	o, u := index(old), index(updated)
	for id := range u {
		if prev, ok := o[id]; !ok || prev.Name != u[id].Name || !stringsEqual(prev.Servers, u[id].Servers) {
			set[id] = true
		}
	}
	for id := range o {
		if _, ok := u[id]; !ok {
			set[id] = true
		}
	}
}

func diffDomains(old, updated []model.CrossServerMatchDomain, set map[string]bool) {
	equal := func(a, b model.CrossServerMatchDomain) bool {
		if a.Name != b.Name || !stringsEqual(a.Servers, b.Servers) || len(a.Params) != len(b.Params) {
			return false
		}
		for k, v := range a.Params {
			if b.Params[k] != v {
				return false
			}
		}
		return true
	}
	index := func(in []model.CrossServerMatchDomain) map[string]model.CrossServerMatchDomain {
		m := make(map[string]model.CrossServerMatchDomain, len(in))
		for _, d := range in {
			m[d.ID] = d
		}
		return m
	}
	o, u := index(old), index(updated)
	for id := range u {
		if prev, ok := o[id]; !ok || !equal(prev, u[id]) {
			set[id] = true
		}
	}
	for id := range o {
		if _, ok := u[id]; !ok {
			set[id] = true
		}
	}
}

func sameFeatures(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func isNotFound(err error) bool {
	return err != nil && errors.Is(err, store.ErrNotFound)
}
