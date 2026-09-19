package routers

// readyz_receipts.go —— /readyz 的「待收敛回执」只读字段与观测源注册。
//
// 为什么放在 routers 而不是某个模块的 contract：它只服务健康检查这一条消费链。
// 一旦进 contract，每个实现方都要为「可观测」扩一次对外接口，测试替身也得跟着实现一遍 ——
// 而模块内部本来就已有收敛调度与只读观测（PendingReceiptStatus），健康检查要的只是
// **多一个视图**，不是新能力。所以这里走隐式接口（装配期断言 + 注册），模块不认识健康检查。

import (
	"context"
	"sync"
	"time"

	"go_wp/pkg/logger"
)

// pendingReceiptBacklog 是收敛例程的只读观测形状（page / presentation 各自实现）。
//
// 返回三元组而不是结构体：routers 不 import 任何模块的 service 包（那会绕过 contract
// 直接依赖实现），形状写在方法签名里，由双方各自保证。
type pendingReceiptBacklog interface {
	// PendingReceiptBacklog 返回待收敛条数、最老一条已等待的时长、本进程最近一次收敛时刻。
	PendingReceiptBacklog(ctx context.Context) (pending int64, oldestAge time.Duration, lastConvergeAt time.Time, err error)
}

// pendingReceiptRegistry 观测源注册表：装配期逐个登记，运行期由 /readyz 读取。
// 加锁是因为装配与请求处在不同 goroutine（端口在装配完成前就已可达）。
type pendingReceiptRegistry struct {
	mu      sync.Mutex
	sources map[string]pendingReceiptBacklog
}

// register 登记一个来源；同名覆盖，避免同一进程多次装配时残留上一次的实现。
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

// snapshot 汇总全部来源的积压观测，直接作为 /readyz 响应数据的一个字段。
//
// **这是可观测性，不是就绪门**：pending 无论多大都只原样报出，绝不参与状态码判定 ——
// 待收敛回执表达的是「线上已切换、数据库还差一步」，收敛例程会自己把它收掉；
// 把它做成就绪门，等于让一次发布失败把整个实例从负载均衡里摘掉（故障面被放大）。
//
// 观测失败（数据库不可用等）按 observed=false 报出：原文只进日志，不进响应
// （后台页面与健康检查都不直出内部错误，见 AGENTS.md 的错误文案三件套）。
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
