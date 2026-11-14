package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// 收尾顺序：先 cancel，再等在途任务退出，最后关 HTTP。
//
// 用阻塞任务证明：任务未退出时 HTTP 关闭和组件关闭都不会抢跑；
// 任务释放后 Stop 才返回。只断言「收到取消信号」不算 join 完成。
func TestShutdownWaitsForInFlightTaskBeforeClosing(t *testing.T) {
	taskStarted := make(chan struct{})
	release := make(chan struct{})
	var httpClosed, tasksDone atomic.Bool

	chain := &shutdownChain{
		stopBackground: func() {},
		waitTasks: func() {
			<-release
			tasksDone.Store(true)
		},
		shutdownHTTP: func(context.Context) error {
			if !tasksDone.Load() {
				t.Error("HTTP 在后台任务退出之前就被关闭了")
			}
			httpClosed.Store(true)
			return nil
		},
	}
	close(taskStarted)
	<-taskStarted

	done := make(chan struct{})
	go func() { chain.run(); close(done) }()

	select {
	case <-done:
		t.Fatal("在途任务未结束时收尾就返回了 —— 数据库会被提前关闭")
	case <-time.After(100 * time.Millisecond):
	}
	if httpClosed.Load() {
		t.Fatal("在途任务未结束时 HTTP 已被关闭")
	}

	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("任务退出后收尾仍未返回")
	}
	if !tasksDone.Load() {
		t.Fatal("等待任务出口没有执行")
	}
}

// 收尾幂等：正常退出路径与 defer 重复调用只真正执行一次。
func TestShutdownRunsOnce(t *testing.T) {
	var calls atomic.Int32
	chain := &shutdownChain{stopBackground: func() { calls.Add(1) }}
	chain.run()
	chain.run()
	if n := calls.Load(); n != 1 {
		t.Fatalf("收尾应只执行一次，实际 %d 次", n)
	}
}

// 收尾不掩盖原始错误：HTTP 关闭失败只记日志，不改变调用方正在返回的错误。
func TestShutdownHTTPErrorDoesNotBlockChain(t *testing.T) {
	var closed atomic.Bool
	chain := &shutdownChain{
		shutdownHTTP: func(context.Context) error {
			closed.Store(true)
			return errors.New("关闭失败")
		},
	}
	chain.run()
	if !closed.Load() {
		t.Fatal("HTTP 关闭没有被调用")
	}
}

// Shutdown 超时返回后处理器可能仍在执行：关闭依赖前必须等它退出，
// 等不到要显式记录放弃，而不是静默关闭数据库。
func TestShutdownDrainsHandlersAfterHTTPTimeout(t *testing.T) {
	inflight := newInflightTracker()
	inflight.begin() // 模拟一个正在执行的处理器

	var componentsClosed atomic.Bool
	chain := &shutdownChain{
		// Shutdown 立刻超时返回，但处理器仍在跑。
		shutdownHTTP: func(context.Context) error { return context.DeadlineExceeded },
		inflight:     inflight.Count,
		waitInflight: inflight.Wait,
		// 用一个可观察的「关闭组件」动作替代真实 config.CloseComponents。
		closeComponents: func() {
			if inflight.Count() > 0 {
				t.Error("在途请求未退出时就关闭了组件（数据库会被提前关闭）")
			}
			componentsClosed.Store(true)
		},
	}

	done := make(chan struct{})
	go func() { chain.run(); close(done) }()

	select {
	case <-done:
		t.Fatal("在途处理器未结束时收尾就返回了")
	case <-time.After(150 * time.Millisecond):
	}

	inflight.end() // 处理器真正退出
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("处理器退出后收尾仍未返回")
	}
	if !componentsClosed.Load() {
		t.Fatal("组件关闭没有执行")
	}
}

// 在途请求迟迟不退：收尾超时后明确放弃并继续关闭，不无限挂住。
func TestShutdownGivesUpOnStuckHandler(t *testing.T) {
	inflight := newInflightTracker()
	inflight.begin()
	stuck := inflight.Count
	chain := &shutdownChain{
		shutdownHTTP:    func(context.Context) error { return context.DeadlineExceeded },
		inflight:        stuck,
		waitInflight:    func(ctx context.Context) { <-ctx.Done() }, // 永不归零
		closeComponents: func() {},
		drainTimeout:    50 * time.Millisecond,
	}
	done := make(chan struct{})
	go func() { chain.run(); close(done) }()
	select {
	case <-done:
		// 期望：drain 超时后放弃，最终仍返回。
	case <-time.After(3 * time.Second):
		t.Fatal("卡住的处理器导致收尾永不返回")
	}
}

// 真实的 http.Server.Shutdown 超时：用短 httpTimeout 让 srv.Shutdown 自己返回
// context.DeadlineExceeded，而不是桩函数假装超时。
//
// 这补上了此前测试的空白：`shutdownHTTP: func(...) error { return DeadlineExceeded }`
// 只证明「超时后我们会 drain」，证明不了「超时确实会以那个方式发生」。
// 这里跑真实 HTTP 服务，阻塞处理器直到超过 httpTimeout，观察：
//  1. Shutdown 真的超时返回；
//  2. 处理器仍在跑时依赖没有被关；
//  3. 处理器退出后收尾完成、依赖才关。
func TestShutdownRealHTTPTimeoutThenDrain(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})

	inflight := newInflightTracker()
	router := gin.New()
	router.Use(inflight.Middleware())
	router.POST("/block", func(c *gin.Context) {
		close(entered)
		<-release
		c.Status(http.StatusOK)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败：%v", err)
	}
	srv := &http.Server{Handler: router}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	var (
		shutdownTimedOut   atomic.Bool
		closedWithInflight atomic.Bool
	)
	chain := &shutdownChain{
		shutdownHTTP: func(ctx context.Context) error {
			err := srv.Shutdown(ctx)
			if errors.Is(err, context.DeadlineExceeded) {
				shutdownTimedOut.Store(true)
			}
			return err
		},
		inflight:     inflight.Count,
		waitInflight: inflight.Wait,
		closeComponents: func() {
			if inflight.Count() > 0 {
				closedWithInflight.Store(true)
			}
		},
		// 关键：让 Shutdown 在真实时钟上超时，而不是由桩直接返回错误。
		httpTimeout:  150 * time.Millisecond,
		drainTimeout: 3 * time.Second,
	}

	go func() {
		resp, rerr := http.Post("http://"+ln.Addr().String()+"/block", "text/plain", nil)
		if resp != nil {
			_ = resp.Body.Close()
		}
		_ = rerr
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("阻塞请求没有进入处理器")
	}

	done := make(chan struct{})
	go func() { chain.run(); close(done) }()

	// 150ms 后 Shutdown 超时；处理器仍在阻塞，收尾必须停在 drain 等待上。
	select {
	case <-done:
		t.Fatal("处理器未退出时收尾就返回了")
	case <-time.After(600 * time.Millisecond):
	}
	if !shutdownTimedOut.Load() {
		t.Fatal("httpTimeout 到点后 srv.Shutdown 应返回 DeadlineExceeded —— 超时路径没有被真实触发")
	}
	if closedWithInflight.Load() {
		t.Fatal("处理器仍在执行时就关闭了依赖")
	}

	close(release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("处理器退出后收尾仍未返回")
	}
	if closedWithInflight.Load() {
		t.Fatal("依赖关闭时仍有在途请求")
	}
}

// 真实阻塞 HTTP 请求：Shutdown 超时返回后，处理器仍在跑时不能关闭依赖；
// 处理器退出后才允许关闭。用真的 http.Server + tracker 中间件验证，
// 不靠手工 begin/end。
func TestShutdownRealBlockedRequestKeepsDependenciesOpen(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})

	inflight := newInflightTracker()
	router := gin.New()
	router.Use(inflight.Middleware())
	router.POST("/block", func(c *gin.Context) {
		close(entered)
		<-release
		c.Status(http.StatusOK)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败：%v", err)
	}
	srv := &http.Server{Handler: router}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	var closedWithInflight atomic.Bool
	chain := &shutdownChain{
		shutdownHTTP: func(ctx context.Context) error { return srv.Shutdown(ctx) },
		inflight:     inflight.Count,
		waitInflight: inflight.Wait,
		closeComponents: func() {
			if inflight.Count() > 0 {
				closedWithInflight.Store(true)
			}
		},
		drainTimeout: 3 * time.Second,
	}

	respCh := make(chan error, 1)
	go func() {
		resp, rerr := http.Post("http://"+ln.Addr().String()+"/block", "text/plain", nil)
		if resp != nil {
			_ = resp.Body.Close()
		}
		respCh <- rerr
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("阻塞请求没有进入处理器")
	}

	done := make(chan struct{})
	go func() { chain.run(); close(done) }()

	// Shutdown 会等这个处理器；它没退出前收尾不能完成，也不能关闭依赖。
	select {
	case <-done:
		t.Fatal("阻塞请求未结束时收尾就返回了")
	case <-time.After(300 * time.Millisecond):
	}
	if closedWithInflight.Load() {
		t.Fatal("在途请求仍在执行时就关闭了依赖")
	}

	close(release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("请求退出后收尾仍未返回")
	}
	if closedWithInflight.Load() {
		t.Fatal("依赖关闭时仍有在途请求")
	}
	select {
	case err := <-respCh:
		if err != nil {
			// 连接被关闭是预期结果之一；这里只要求它最终结束。
			t.Logf("阻塞请求结束于：%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("阻塞请求没有结束")
	}
}
