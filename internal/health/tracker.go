// Package health tracks arr instances disabled by rejected API keys.
package health

import "sync"

// Tracker stores process-lifetime disable reasons.
type Tracker struct {
	mu       sync.RWMutex
	disabled map[string]string
}

// New creates an empty tracker.
func New() *Tracker { return &Tracker{disabled: make(map[string]string)} }

// Enabled reports whether jobs may run for key.
func (t *Tracker) Enabled(key string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, disabled := t.disabled[key]
	return !disabled
}

// Reason returns the disable reason or an empty string.
func (t *Tracker) Reason(key string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.disabled[key]
}

// Disable records the first disable reason and reports whether it was new.
func (t *Tracker) Disable(key, reason string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.disabled[key]; exists {
		return false
	}
	t.disabled[key] = reason
	return true
}
