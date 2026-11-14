package main

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

// inflightTracker 记录「已经进入处理器、尚未返回」的请求数。
//
// 为什么不能用 http.Server.ConnState 判断：连接在 handler 执行时可能已进入
// closing 状态，而 handler 仍在跑（Shutdown 超时的典型表现）。这里以处理器
// 进入/退出为准，close 数据库前据此判断是否仍有真实工作在进行。
type inflightTracker struct {
	active atomic.Int64

	mu     sync.Mutex
	zeroCh chan struct{}
	hasCh  chan struct{}
}

func newInflightTracker() *inflightTracker {
	return &inflightTracker{}
}

// Middleware 是最外层中间件：进入时 +1，返回（含 panic 展开）时 -1。
func (t *inflightTracker) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		t.begin()
		defer t.end()
		c.Next()
	}
}

func (t *inflightTracker) begin() {
	t.mu.Lock()
	t.active.Add(1)
	t.mu.Unlock()
}

func (t *inflightTracker) end() {
	t.mu.Lock()
	n := t.active.Add(-1)
	if n == 0 && t.hasCh != nil {
		close(t.hasCh)
		t.hasCh = nil
	}
	t.mu.Unlock()
}

// Count 返回当前在途处理器数。
func (t *inflightTracker) Count() int {
	if t == nil {
		return 0
	}
	return int(t.active.Load())
}

// Wait 阻塞到在途归零或 ctx 结束；返回时仍可能大于 0（调用方负责记录放弃）。
func (t *inflightTracker) Wait(ctx context.Context) {
	if t == nil {
		return
	}
	for {
		if t.Count() == 0 {
			return
		}
		t.mu.Lock()
		if t.Count() == 0 {
			t.mu.Unlock()
			return
		}
		if t.hasCh == nil {
			t.hasCh = make(chan struct{})
		}
		ch := t.hasCh
		t.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return
		}
	}
}
