package httpapi

import (
	"sync"
	"time"
)

// loginThrottle counts failed sign-ins in a sliding window, in memory.
// Keys combine the account with the client address, plus a wider budget per
// account and per address, so one attacker cannot lock a real user out from
// every place and distributed guessing is still slowed down.
type loginThrottle struct {
	mu      sync.Mutex
	window  time.Duration
	entries map[string]*loginFailures
	now     func() time.Time
}

type loginFailures struct {
	count int
	reset time.Time
}

const (
	maxLoginKeys          = 50000
	failuresPerAccountIP  = 5
	failuresPerAccount    = 50
	failuresPerClientAddr = 30
)

func newLoginThrottle(window time.Duration) *loginThrottle {
	return &loginThrottle{window: window, entries: make(map[string]*loginFailures), now: time.Now}
}

func throttleKeys(account, address string) [3]string {
	return [3]string{"pair:" + account + "\x00" + address, "account:" + account, "address:" + address}
}

var throttleLimits = [3]int{failuresPerAccountIP, failuresPerAccount, failuresPerClientAddr}

// Blocked reports how long the caller must wait, or zero when it may try.
func (t *loginThrottle) Blocked(account, address string) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	var wait time.Duration
	for i, key := range throttleKeys(account, address) {
		entry := t.entries[key]
		if entry == nil {
			continue
		}
		if !now.Before(entry.reset) {
			delete(t.entries, key)
			continue
		}
		if entry.count >= throttleLimits[i] && entry.reset.Sub(now) > wait {
			wait = entry.reset.Sub(now)
		}
	}
	return wait
}

func (t *loginThrottle) Fail(account, address string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if len(t.entries) >= maxLoginKeys {
		for key, entry := range t.entries {
			if !now.Before(entry.reset) {
				delete(t.entries, key)
			}
		}
	}
	for _, key := range throttleKeys(account, address) {
		entry := t.entries[key]
		if entry == nil || !now.Before(entry.reset) {
			if len(t.entries) >= maxLoginKeys {
				continue // Memory stays bounded; the global limiter still applies.
			}
			entry = &loginFailures{reset: now.Add(t.window)}
			t.entries[key] = entry
		}
		entry.count++
	}
}

// Succeed clears the failures of this account from this address only.
func (t *loginThrottle) Succeed(account, address string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, throttleKeys(account, address)[0])
}
