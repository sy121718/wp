package pipeline

import (
	"errors"
	"go_wp/pkg/publish"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPathLockReentrantShardDoesNotDeadlock(t *testing.T) {
	locker := &publish.ProcessPathLocker{}
	sameShard := twoPathsInSameShard(t)

	done := make(chan error, 1)
	go func() {
		done <- locker.WithPaths(sameShard, func() error { return nil })
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("同分片取锁返回错误: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("同分片多路径自锁：%v", sameShard)
	}
}

func TestPathLockDifferentShardsRunConcurrently(t *testing.T) {
	locker := &publish.ProcessPathLocker{}
	left, right := twoPathsInDifferentShards(t)

	inLeft := make(chan struct{})
	releaseLeft := make(chan struct{})
	leftDone := make(chan error, 1)
	go func() {
		leftDone <- locker.WithPaths([]string{left}, func() error {
			close(inLeft)
			<-releaseLeft
			return nil
		})
	}()
	<-inLeft

	inRight := make(chan struct{})
	rightDone := make(chan error, 1)
	go func() {
		rightDone <- locker.WithPaths([]string{right}, func() error {
			close(inRight)
			return nil
		})
	}()
	select {
	case <-inRight:
	case <-time.After(2 * time.Second):
		t.Fatalf("不同分片被串行化（疑似全局锁）：%s / %s", left, right)
	}
	close(releaseLeft)
	if err := <-leftDone; err != nil {
		t.Fatalf("左路返回错误: %v", err)
	}
	if err := <-rightDone; err != nil {
		t.Fatalf("右路返回错误: %v", err)
	}
}

func TestPathLockSerializesSamePath(t *testing.T) {
	locker := &publish.ProcessPathLocker{}
	path := "/lock/same"
	var inside atomic.Int32
	var maxInside atomic.Int32
	var wg sync.WaitGroup
	var order atomic.Int32
	var firstSaw atomic.Int32
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := locker.WithPaths([]string{path}, func() error {
				n := inside.Add(1)
				for {
					old := maxInside.Load()
					if n <= old || maxInside.CompareAndSwap(old, n) {
						break
					}
				}
				ord := order.Add(1)
				if ord == 1 {
					firstSaw.Store(int32(i))
				}
				time.Sleep(2 * time.Millisecond)
				inside.Add(-1)
				return nil
			})
			if err != nil {
				t.Errorf("取锁失败: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if got := maxInside.Load(); got != 1 {
		t.Fatalf("同一路径同时进入临界区的 goroutine 数 = %d，期望 1", got)
	}
}

func TestPathLockFailsClosedWithoutLocker(t *testing.T) {
	var typedNil *publish.ProcessPathLocker
	ran := false
	err := typedNil.WithPaths([]string{"/lock/nil"}, func() error { ran = true; return nil })
	if !errors.Is(err, publish.ErrPathLockerUnavailable) {
		t.Fatalf("typed nil 锁应 fail closed，实际 err = %v", err)
	}
	if ran {
		t.Fatal("typed nil 锁下 fn 不得执行（不允许无互斥改访问面）")
	}

	var nilLocker publish.PublicationPathLocker
	if err := publish.WithPathLock(nilLocker, []string{"/lock/nil"}, func() error { ran = true; return nil }); !errors.Is(err, publish.ErrPathLockerUnavailable) {
		t.Fatalf("nil 接口应被前置检查 fail closed，实际 err = %v", err)
	}
	if ran {
		t.Fatal("nil 接口下 fn 不得执行")
	}
}

func TestDefaultPathLockerIsShared(t *testing.T) {
	if publish.DefaultPathLocker() == nil {
		t.Fatal("默认路径锁为空")
	}
	if publish.DefaultPathLocker() != publish.DefaultPathLocker() {
		t.Fatal("默认路径锁不是同一实例：各服务各自持锁等于没加锁")
	}
}

func twoPathsInSameShard(t *testing.T) []string {
	t.Helper()
	byShard := map[int]string{}
	for i := 0; i < 512; i++ {
		p := "/shard/" + itoaForTest(i)
		idx := publish.ShardIndexOf(publish.PathLockKey(p))
		if prev, ok := byShard[idx]; ok {
			return []string{prev, p}
		}
		byShard[idx] = p
	}
	t.Fatal("512 条路径内找不到同一分片（分片函数可能等于路径本身）")
	return nil
}

func twoPathsInDifferentShards(t *testing.T) (string, string) {
	t.Helper()
	seen := map[int]string{}
	var reserved int
	for i := 0; i < 512; i++ {
		p := "/wide/" + itoaForTest(i)
		idx := publish.ShardIndexOf(publish.PathLockKey(p))
		if prev, ok := seen[idx]; ok && prev != p {
			_ = prev
			continue
		}
		if reserved == 0 {
			reserved = idx
			seen[idx] = p
			continue
		}
		return seen[reserved], p
	}
	t.Fatal("找不到两条不同分片的路径")
	return "", ""
}

func itoaForTest(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
