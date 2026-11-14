package routers

import (
	"context"
	"sync"

	"github.com/gin-gonic/gin"

	mw "go_wp/pkg/middleware"
)

// Assembly 是一次路由装配的对外句柄。
//
// 后台任务的停止函数归**这个实例**持有，不落包级全局：多实例（测试装配、
// 审计工具）各自停止自己的任务，互不污染；进程收尾只停自己那次装配启动的东西。
type Assembly struct {
	registry *mw.Registry
	gate     *startupGate

	stopOnce sync.Once
	stops    []func()
}

// Stop 取消并等待本次装配启动的全部后台任务退出。
//
// 调用顺序：先 cancel 装配的 bgCtx，再调 Stop，最后关闭数据库。
// 重复调用是幂等等待，不重复执行停止函数。
func (r *Assembly) Stop() {
	if r == nil {
		return
	}
	r.stopOnce.Do(func() {
		// 逆序停止：后启动的先停，缩短任务之间的依赖窗口。
		for i := len(r.stops) - 1; i >= 0; i-- {
			if r.stops[i] != nil {
				r.stops[i]()
			}
		}
	})
}

// Registry 返回本次装配的权限登记索引。
func (r *Assembly) Registry() *mw.Registry {
	if r == nil {
		return nil
	}
	return r.registry
}

// StartAssembly 装配路由并返回句柄；句柄的 Stop 必须在关闭数据库之前调用。
//
// 与 SetupRoutesWithRegistry 走同一条装配链（newAssembly + runAssembly），
// 区别只在于把实例句柄交回调用方。
func StartAssembly(ctx context.Context, router *gin.Engine, ready func() error) *Assembly {
	if router == nil {
		return nil
	}
	a := newAssembly(ctx, router)
	runAssembly(a, ready)
	return a.handle()
}
