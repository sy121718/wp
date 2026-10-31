package pipeline

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 本文件守三条**不变量**（不是行为细节）：
//  1. 同一分片的多个路径只取一次锁 —— 按路径去重排不出稳定序，会让同一片自锁；
//  2. 不同分片的路径可并行 —— 退化成一个全局锁会把所有发布串行化；
//  3. 锁覆盖「复核 + 副作用」整段 —— A 在锁内阻塞时 B 必须进不来，A 的处置在 B 成功
//     之后重放时必须能看见 B 的结果（而不是凭自己锁外的旧读数动手）。

// TestPathLockReentrantShardDoesNotDeadlock 同一分片的两个路径不得自锁。
//
// 构造方式：暴力找两条落在同一分片的路径（FNV 分片 64 片，几十个路径内必现），
// 然后同时取这两条。若实现按「路径去重」排序后逐个 Lock，第二次 Lock 同一片会
// 永久阻塞 —— 这里用超时把它变成可读的失败而不是挂死整轮测试。
func TestPathLockReentrantShardDoesNotDeadlock(t *testing.T) {
	locker := &ProcessPathLocker{}
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

// TestPathLockDifferentShardsRunConcurrently 不同分片必须能并行。
//
// 判据是「两路同时进入临界区」，不是「没超时」：把第一路按住不放，第二路若还能
// 进来就说明没有退化成全局锁。
func TestPathLockDifferentShardsRunConcurrently(t *testing.T) {
	locker := &ProcessPathLocker{}
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

// TestPathLockSerializesSamePath 同一路径上的两个临界区必须严格串行。
//
// 交错形状就是现场的那种：A 先进锁（模拟「读现场 → 判断」），B 排队；A 释放后 B
// 进来并把链接换成自己的；A 若在锁外又动手，就会覆盖 B。这里只验证锁本身串行，
// 业务侧的「锁内复核」由 page 域的归属用例覆盖。
func TestPathLockSerializesSamePath(t *testing.T) {
	locker := &ProcessPathLocker{}
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

// TestPathLockFailsClosedWithoutLocker nil 锁必须拒绝执行而不是无锁跑。
//
// 两条路径都要证：
//  1. **typed nil**（(*ProcessPathLocker)(nil)）作为具体实现时，方法可直接调用，必须 fail closed；
//  2. nil **接口**不能靠「调用它的方法」来验证 —— 那一步本身就是 SIGSEGV，与 fail closed
//     毫无关系。接口入口的 fail closed 由真实服务（Service.SetPublicationPathLocker(nil)）
//     承担，见 public/test/page/unit/page_path_lock_test.go 的锁定用例。
func TestPathLockFailsClosedWithoutLocker(t *testing.T) {
	var typedNil *ProcessPathLocker
	ran := false
	err := typedNil.WithPaths([]string{"/lock/nil"}, func() error { ran = true; return nil })
	if !errors.Is(err, ErrPathLockerUnavailable) {
		t.Fatalf("typed nil 锁应 fail closed，实际 err = %v", err)
	}
	if ran {
		t.Fatal("typed nil 锁下 fn 不得执行（不允许无互斥改访问面）")
	}

	// 接口值为 nil 时的判定必须走**调用方**的前置检查（withPaths / WithPathLock），
	// 而不是接口方法：nil 接口的方法调用在任何实现之前就 panic。
	var nilLocker PublicationPathLocker
	if err := WithPathLock(nilLocker, []string{"/lock/nil"}, func() error { ran = true; return nil }); !errors.Is(err, ErrPathLockerUnavailable) {
		t.Fatalf("nil 接口应被前置检查 fail closed，实际 err = %v", err)
	}
	if ran {
		t.Fatal("nil 接口下 fn 不得执行")
	}
}

// TestDefaultPathLockerIsShared 默认实例必须全进程唯一。
func TestDefaultPathLockerIsShared(t *testing.T) {
	if DefaultPathLocker() == nil {
		t.Fatal("默认路径锁为空")
	}
	if DefaultPathLocker() != DefaultPathLocker() {
		t.Fatal("默认路径锁不是同一实例：各服务各自持锁等于没加锁")
	}
}

// twoPathsInSameShard 找两条归一后落在同一分片的路径。
func twoPathsInSameShard(t *testing.T) []string {
	t.Helper()
	byShard := map[int]string{}
	for i := 0; i < 512; i++ {
		p := "/shard/" + itoaForTest(i)
		idx := shardIndexOf(pathLockKey(p))
		if prev, ok := byShard[idx]; ok {
			return []string{prev, p}
		}
		byShard[idx] = p
	}
	t.Fatal("512 条路径内找不到同一分片（分片函数可能等于路径本身）")
	return nil
}

// twoPathsInDifferentShards 找两条落在不同分片的路径。
func twoPathsInDifferentShards(t *testing.T) (string, string) {
	t.Helper()
	seen := map[int]string{}
	var reserved int
	for i := 0; i < 512; i++ {
		p := "/wide/" + itoaForTest(i)
		idx := shardIndexOf(pathLockKey(p))
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

// itoaForTest 避免为了拼测试路径引入 strconv 依赖链（只用于构造路径片段）。
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
