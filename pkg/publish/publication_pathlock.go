package publish

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

type PublicationPathLocker interface {
	WithPaths(paths []string, fn func() error) error
}

var defaultPathLocker = &ProcessPathLocker{}

func NewProcessPathLocker() *ProcessPathLocker { return &ProcessPathLocker{} }

func WithPathLock(locker PublicationPathLocker, paths []string, fn func() error) error {
	if fn == nil {
		return nil
	}
	if locker == nil {
		return ErrPathLockerUnavailable
	}
	return locker.WithPaths(paths, fn)
}

func DefaultPathLocker() PublicationPathLocker { return defaultPathLocker }

var ErrPathLockerUnavailable = errors.New("访问面路径锁不可用：未注入且无进程默认实例")

const pathLockShardCount = 64

type ProcessPathLocker struct {
	shards [pathLockShardCount]sync.Mutex
}

var _ PublicationPathLocker = (*ProcessPathLocker)(nil)

func (l *ProcessPathLocker) WithPaths(paths []string, fn func() error) error {
	if fn == nil {
		return nil
	}
	if l == nil {

		return ErrPathLockerUnavailable
	}
	shards := l.shardIndexes(paths)
	for _, i := range shards {
		l.shards[i].Lock()
	}
	defer func() {
		for i := len(shards) - 1; i >= 0; i-- {
			l.shards[shards[i]].Unlock()
		}
	}()
	return fn()
}

func (l *ProcessPathLocker) shardIndexes(paths []string) []int {
	seen := make(map[int]bool, len(paths))
	out := make([]int, 0, len(paths))
	for _, p := range paths {
		key := pathLockKey(p)
		if key == "" {
			continue
		}
		i := shardIndexOf(key)
		if seen[i] {
			continue
		}
		seen[i] = true
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

func pathLockKey(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if norm, err := NormalizeURL(p); err == nil {
		return norm
	}
	return p
}

func shardIndexOf(key string) int {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(key); i++ {
		h ^= uint64(key[i])
		h *= prime64
	}
	return int(h % pathLockShardCount)
}

// ShardIndexOf 返回键所属的分片序号（供应用侧与测试核对分布）。
func ShardIndexOf(key string) int { return shardIndexOf(key) }

// PathLockKey 归一化路径锁键。
func PathLockKey(path string) string { return pathLockKey(path) }
