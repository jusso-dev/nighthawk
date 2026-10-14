package policy

import (
	"os"
	"sync"
	"time"
)

// Reloader re-reads policy files. A bad file keeps the last good engine.
type Reloader struct {
	mu       sync.RWMutex
	paths    []string
	eng      *Engine
	lastErr  error
	Interval time.Duration
	OnBad    func(error)
}

// NewReloader loads paths once.
func NewReloader(paths []string) (*Reloader, error) {
	r := &Reloader{paths: paths, Interval: 2 * time.Second}
	eng, err := loadFiles(paths)
	if err != nil {
		return nil, err
	}
	r.eng = eng
	return r, nil
}

func loadFiles(paths []string) (*Engine, error) {
	if len(paths) == 0 {
		return DefaultEngine()
	}
	var merged *Engine
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		eng, err := LoadYAML(p, b)
		if err != nil {
			return nil, err
		}
		if merged == nil {
			merged = eng
			continue
		}
		merged.rules = append(merged.rules, eng.rules...)
		merged.def = eng.def
	}
	return merged, nil
}

// Engine returns the current good engine.
func (r *Reloader) Engine() *Engine {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.eng
}

// Poll reloads once.
func (r *Reloader) Poll() {
	eng, err := loadFiles(r.paths)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		r.lastErr = err
		if r.OnBad != nil {
			r.OnBad(err)
		}
		return
	}
	r.eng = eng
	r.lastErr = nil
}

// LastErr is the most recent load error.
func (r *Reloader) LastErr() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastErr
}
