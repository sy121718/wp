package routers

import (
	"context"
	"sync"
	"time"

	"go_wp/pkg/logger"
)

type pendingReceiptBacklog interface {
	PendingReceiptBacklog(ctx context.Context) (pending int64, oldestAge time.Duration, lastConvergeAt time.Time, err error)
}

type pendingReceiptRegistry struct {
	mu      sync.Mutex
	sources map[string]pendingReceiptBacklog
}

func (r *pendingReceiptRegistry) register(name string, src pendingReceiptBacklog) {
	if src == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sources == nil {
		r.sources = make(map[string]pendingReceiptBacklog, 2)
	}
	r.sources[name] = src
}

func (r *pendingReceiptRegistry) snapshot(ctx context.Context) map[string]any {
	r.mu.Lock()
	sources := make(map[string]pendingReceiptBacklog, len(r.sources))
	for name, src := range r.sources {
		sources[name] = src
	}
	r.mu.Unlock()

	out := make(map[string]any, len(sources))
	for name, src := range sources {
		entry := map[string]any{"observed": false}
		pending, oldestAge, lastConvergeAt, err := src.PendingReceiptBacklog(ctx)
		if err != nil {
			logger.Scene("readyz").With("source", name).With("err", err).
				Warn("待收敛回执观测失败（/readyz 只标记该来源不可观测，不影响就绪判定）")
			out[name] = entry
			continue
		}
		entry["observed"] = true
		entry["pending"] = pending
		entry["oldestAgeSeconds"] = int64(oldestAge / time.Second)
		if lastConvergeAt.IsZero() {
			entry["lastConvergeAt"] = nil
		} else {
			entry["lastConvergeAt"] = lastConvergeAt.Format(time.RFC3339)
		}
		out[name] = entry
	}
	return out
}
