package lock

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type localEntry struct {
	expiresAt time.Time
}

// LocalLocker 进程内锁实现。
//
// 适用场景：
// - 单进程内的热点互斥
// - 测试环境
// - 不要求跨实例协调的轻量场景
type LocalLocker struct {
	mu    sync.Mutex
	locks map[string]localEntry
}

type localLease struct {
	locker *LocalLocker
	key    string
}

// NewLocal 创建进程内锁实现。
func NewLocal() *LocalLocker {
	return &LocalLocker{
		locks: make(map[string]localEntry),
	}
}

// Acquire 尝试获取本地锁。
func (l *LocalLocker) Acquire(_ context.Context, key string, ttl time.Duration) (Lease, error) {
	if ttl <= 0 {
		return nil, fmt.Errorf("锁 TTL 必须大于 0")
	}

	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	for itemKey, entry := range l.locks {
		if !entry.expiresAt.After(now) {
			delete(l.locks, itemKey)
		}
	}

	if _, exists := l.locks[key]; exists {
		return nil, fmt.Errorf("锁已被占用: %s", key)
	}

	l.locks[key] = localEntry{
		expiresAt: now.Add(ttl),
	}
	return &localLease{locker: l, key: key}, nil
}

func (l *localLease) Key() string {
	return l.key
}

func (l *localLease) Release(_ context.Context) error {
	l.locker.mu.Lock()
	defer l.locker.mu.Unlock()
	delete(l.locker.locks, l.key)
	return nil
}
