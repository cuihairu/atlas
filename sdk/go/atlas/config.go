package atlas

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"
)

// Cross-server coordination config (config center). The SDK owns the
// receiver half of the contract:
//
//   - pull once at startup and fail fast when it cannot be read;
//   - apply notify-then-pull updates (a signal carries the new version,
//     never the body — the receiver pulls it);
//   - keep running on the previous config when a pull fails, retrying
//     with backoff.
//
// Wire types mirror internal/model.CrossServerSpec; keep them in sync.

// Notify modes a server declares at register time. See
// docs/config-center.md §2.
const (
	// NotifyModeSubscribe receives the change signal on a long-lived
	// message-bus subscription (recommended for fleets).
	NotifyModeSubscribe = "subscribe"
	// NotifyModeCallback has Atlas POST the signal to a URL the server
	// exposes.
	NotifyModeCallback = "callback"
	// NotifyModePoll polls version/ETag. It is also the fallback when
	// neither a subscription nor a callback is available.
	NotifyModePoll = "poll"
)

// CrossServerCluster is one cluster in the cross-server topology.
type CrossServerCluster struct {
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Region  string   `json:"region,omitempty"`
	Status  string   `json:"status,omitempty"` // active (default) | disabled
	Servers []string `json:"servers"`
}

// CrossServerGroup is a participation group (an operator-defined cohort
// of servers, e.g. one cross-server event season).
type CrossServerGroup struct {
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Servers []string `json:"servers"`
}

// CrossServerMatchDomain is one match pool plus its game-defined params.
type CrossServerMatchDomain struct {
	ID      string            `json:"id"`
	Name    string            `json:"name,omitempty"`
	Servers []string          `json:"servers"`
	Params  map[string]string `json:"params,omitempty"`
}

// CrossServerSpec is the whole coordination document.
type CrossServerSpec struct {
	Topology struct {
		Clusters []CrossServerCluster `json:"clusters"`
	} `json:"topology"`
	Groups       []CrossServerGroup       `json:"groups"`
	Features     map[string]bool          `json:"features"`
	MatchDomains []CrossServerMatchDomain `json:"match_domains"`
}

// CrossServerConfig is a versioned snapshot.
type CrossServerConfig struct {
	Version   int             `json:"version"`
	Hash      string          `json:"hash"`
	Spec      CrossServerSpec `json:"spec"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// ErrConfigNotPublished means Atlas has no coordination config yet — a
// fresh install. Booting servers must decide explicitly whether to run
// without cross-server features (ErrConfigNotFound) or refuse to start.
var ErrConfigNotPublished = errors.New("atlas: no cross-server config published yet")

// FetchCrossServerConfig pulls the current config. It fails fast: a
// transport error or an unpublished config is returned as an error
// instead of an empty document, so a booting server cannot mistake
// "I could not reach the config center" for "there is nothing configured".
func (c *Client) FetchCrossServerConfig(ctx context.Context) (*CrossServerConfig, error) {
	cfg, err := c.backend.fetchCrossServerConfig(ctx)
	if err != nil {
		var apiErr *Error
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w (%s)", ErrConfigNotPublished, apiErr.Message)
		}
		return nil, err
	}
	return cfg, nil
}

// ConfigWatcher keeps one server's view of the coordination config
// converged: it pulls on demand (startup, change signals, periodic
// fallback) and applies a snapshot only when it is strictly newer than the
// one in hand. A failed pull is retried with backoff and never discards
// the config the game is currently running on — a config center outage
// must not take a game server down.
//
// Watcher is safe for concurrent use.
type ConfigWatcher struct {
	client *Client

	// ServerID scopes bus targets; empty means "every target".
	serverID string
	// OnApply receives each newly applied snapshot.
	OnApply func(*CrossServerConfig)

	pollInterval time.Duration
	maxBackoff   time.Duration

	mu      sync.RWMutex
	current *CrossServerConfig

	signal chan struct{}

	// stats
	applied    int
	idempotent int
	skipped    int
	failures   int
}

// ConfigWatcherOptions configures NewConfigWatcher.
type ConfigWatcherOptions struct {
	// OnApply is called once per applied snapshot, on the watcher's
	// goroutine. It should swap the game's coordination config and return
	// quickly; the next pull waits for it.
	OnApply func(*CrossServerConfig)

	// PollInterval is the fallback poll period for servers that cannot
	// subscribe (or whose subscription is down). Zero disables polling,
	// leaving signal-driven updates only. Default 30s.
	PollInterval time.Duration

	// MaxBackoff caps the retry delay after repeated pull failures.
	// Default 30s.
	MaxBackoff time.Duration

	// ServerID scopes change signals to this server's own targets.
	ServerID string
}

// NewConfigWatcher creates a watcher for one game server process.
func NewConfigWatcher(c *Client, opts ConfigWatcherOptions) *ConfigWatcher {
	if opts.PollInterval == 0 {
		opts.PollInterval = 30 * time.Second
	}
	if opts.MaxBackoff == 0 {
		opts.MaxBackoff = 30 * time.Second
	}
	return &ConfigWatcher{
		client:       c,
		serverID:     opts.ServerID,
		OnApply:      opts.OnApply,
		pollInterval: opts.PollInterval,
		maxBackoff:   opts.MaxBackoff,
		signal:       make(chan struct{}, 1),
	}
}

// Current returns the snapshot in hand (nil before the first successful
// pull).
func (w *ConfigWatcher) Current() *CrossServerConfig {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.current
}

// Stats reports watcher counters: applied snapshots, pulls skipped
// because the version did not move, stale pulls refused, and failed pulls
// (each retried with backoff, never fatal).
func (w *ConfigWatcher) Stats() (applied, idempotent, skipped, failures int) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.applied, w.idempotent, w.skipped, w.failures
}

// Notify tells the watcher a change signal arrived. It is
// non-blocking and coalescing: a pending signal is not duplicated, so a
// burst of updates collapses into one pull. Wire it to the bus
// subscription (subscribe mode) or to the callback handler (callback
// mode).
func (w *ConfigWatcher) Notify() {
	select {
	case w.signal <- struct{}{}:
	default:
	}
}

// Pull fetches the config once and applies it when it is strictly newer
// than the snapshot in hand. Same-version responses are ignored
// (idempotent re-delivery), and an older version is refused so a delayed
// response cannot roll the server back. Pull failures are returned and
// leave the current config untouched.
func (w *ConfigWatcher) Pull(ctx context.Context) (applied bool, err error) {
	cfg, err := w.client.FetchCrossServerConfig(ctx)
	if err != nil {
		w.mu.Lock()
		w.failures++
		w.mu.Unlock()
		return false, err
	}

	w.mu.Lock()
	cur := w.current
	switch {
	case cur == nil:
		// First pull: adopt whatever the config center has.
	case cfg.Version < cur.Version:
		// Stale response (out-of-order retry): refuse it.
		w.skipped++
		w.mu.Unlock()
		return false, nil
	case cfg.Version == cur.Version:
		// Already running this version — idempotent re-delivery.
		w.idempotent++
		w.mu.Unlock()
		return false, nil
	}
	w.current = cfg
	w.applied++
	apply := w.OnApply
	w.mu.Unlock()

	if apply != nil {
		apply(cfg)
	}
	return true, nil
}

// Run pulls the config at startup (fail-fast, bounded retries), then keeps
// it converged until ctx is cancelled: change signals trigger an
// immediate pull, and a periodic poll covers lost signals, unreachable
// callbacks and the no-subscription fallback. A startup pull that cannot
// be satisfied within the initial window is returned as an error — the
// caller decides whether to boot without coordination. A later pull
// failure is retried with backoff and logged through OnError; the game
// keeps running on its last good config.
func (w *ConfigWatcher) Run(ctx context.Context, startupAttempts int) error {
	if startupAttempts <= 0 {
		startupAttempts = 3
	}
	backoff := w.client.opts.BaseBackoff
	if backoff <= 0 {
		backoff = 100 * time.Millisecond
	}

	var lastErr error
	for i := 0; i < startupAttempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(jitter(backoff)):
			}
			backoff = min(backoff*2, w.maxBackoff)
		}
		if _, err := w.Pull(ctx); err == nil {
			lastErr = nil
			break
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return fmt.Errorf("atlas: cross-server config unavailable after %d attempts: %w", startupAttempts, lastErr)
	}

	var ticker *time.Ticker
	var tick <-chan time.Time
	if w.pollInterval > 0 {
		ticker = time.NewTicker(w.pollInterval)
		tick = ticker.C
		defer ticker.Stop()
	}

	// Runtime failures back off so an unreachable config center does not
	// turn into a hot pull loop.
	runtimeBackoff := backoff
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick:
			w.retryPull(ctx, &runtimeBackoff)
		case <-w.signal:
			w.retryPull(ctx, &runtimeBackoff)
		}
	}
}

// jitter returns a full-jitter delay in [0, d) so a fleet pulling after a
// shared signal does not stampede the config center in lockstep.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(d)))
}

func (w *ConfigWatcher) retryPull(ctx context.Context, backoff *time.Duration) {
	if _, err := w.Pull(ctx); err != nil {
		if ctx.Err() != nil {
			return
		}
		// Keep the previous config: log and retry with backoff.
		select {
		case <-ctx.Done():
		case <-time.After(jitter(*backoff)):
		}
		*backoff = min(*backoff*2, w.maxBackoff)
		return
	}
	*backoff = w.client.opts.BaseBackoff
	if *backoff <= 0 {
		*backoff = 100 * time.Millisecond
	}
}
