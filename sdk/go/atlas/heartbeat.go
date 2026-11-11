package atlas

import (
	"context"
	"sync"
	"time"
)

// HeartbeatLoop re-sends Heartbeat on a fixed interval so Atlas's
// health monitor never suspects the server. Create one with
// Client.StartHeartbeat after Register.
//
// The loop sends once immediately (so the fleet sees the starting
// player count right away), then on every tick. Payload updates from
// the game server are race-free via Set.
type HeartbeatLoop struct {
	client   *Client
	serverID string
	interval time.Duration

	mu       sync.Mutex
	payload  HeartbeatRequest
	lastErr  error
	lastSent time.Time
	stopped  bool

	stop     chan struct{}
	done     chan struct{}
	onError  func(error)
	stopOnce sync.Once
}

// StartHeartbeat begins reporting req immediately and then every
// interval until Stop. A non-positive interval defaults to 10s.
//
//	err := cli.Register(ctx, req)
//	if err != nil { ... }
//	loop := cli.StartHeartbeat(req.ServerID, 10*time.Second, atlas.HeartbeatRequest{Players: 0})
//	defer loop.Stop()
func (c *Client) StartHeartbeat(serverID string, interval time.Duration, req HeartbeatRequest) *HeartbeatLoop {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	l := &HeartbeatLoop{
		client:   c,
		serverID: serverID,
		interval: interval,
		payload:  req,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go l.run()
	return l
}

// Set updates the payload sent on the next tick (players, load).
func (l *HeartbeatLoop) Set(players int, load float64) {
	l.mu.Lock()
	l.payload = HeartbeatRequest{Players: players, Load: load}
	l.mu.Unlock()
}

// OnError installs a callback for failed heartbeats. Failed attempts
// are retried by the transport's retry policy before surfacing here;
// the loop keeps running either way.
func (l *HeartbeatLoop) OnError(fn func(error)) {
	l.mu.Lock()
	l.onError = fn
	l.mu.Unlock()
}

// LastError reports the most recent heartbeat failure (nil if the last
// one succeeded or none has been attempted).
func (l *HeartbeatLoop) LastError() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastErr
}

// LastSent reports when a heartbeat last left the client.
func (l *HeartbeatLoop) LastSent() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastSent
}

// Stop ends the loop and waits for it to exit. Safe to call twice.
func (l *HeartbeatLoop) Stop() {
	l.stopOnce.Do(func() { close(l.stop) })
	<-l.done
}

func (l *HeartbeatLoop) run() {
	defer close(l.done)

	l.beat() // immediate first report
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
			l.beat()
		}
	}
}

func (l *HeartbeatLoop) beat() {
	l.mu.Lock()
	req, cb := l.payload, l.onError
	l.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), l.interval)
	defer cancel()

	_, err := l.client.Heartbeat(ctx, l.serverID, req)

	l.mu.Lock()
	l.lastErr = err
	if err == nil {
		l.lastSent = time.Now()
	}
	l.mu.Unlock()

	if err != nil && cb != nil {
		cb(err)
	}
}
