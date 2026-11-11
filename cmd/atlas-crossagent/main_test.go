package main

import (
	"sync"
	"testing"
)

type countingCloser struct {
	mu     sync.Mutex
	closed int
}

func (c *countingCloser) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed++
	return nil
}

func (c *countingCloser) closeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// TestBusStateConcurrentDropReplaceClose exercises the /ctl/bus-off,
// /ctl/bus-reset, and shutdown races under the race detector: every
// transition must be serialized, and no concurrent close/replacement may
// produce a data race or a double-close panic.
func TestBusStateConcurrentDropReplaceClose(t *testing.T) {
	state := &busState{}
	bus1 := &countingCloser{}
	rdb1 := &countingCloser{}
	state.replace(bus1, rdb1)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			state.drop()
		}()
		go func() {
			defer wg.Done()
			state.replace(&countingCloser{}, &countingCloser{})
		}()
		go func() {
			defer wg.Done()
			state.close()
		}()
	}
	wg.Wait()
	state.close()
}
