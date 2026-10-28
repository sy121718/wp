// Package currencydict 把 sysconfig 的货币字典适配成订单需要的窄读口。
//
// 适配谁：订单建单要校验 req.Currency（迁移 633 的汇率快照只在「订单币 ≠ 基准币」时
// 才成立，非法币种会让快照静默留 NULL），建单页还要给币种下拉供数。
//
// 为什么独立成 outbound 子包而不是在 service 里写几行：
//
//   - **形状翻译**：sysconfig 的 DictReader.ListDictOptions 返回带 Label / Symbol 的
//     展示项，订单只要一个「启用代码集合」。让订单认识 DictOption 等于让它认识字典的
//     展示层，字典改一次下拉文案就要动订单。
//   - **方向**：实现方（本包）依赖调用方（ordercontract.CurrencyLister）契约，
//     与 orderstock 同一手法 —— 订单不 import sysconfig 的 model / dto。
//
// 缓存：货币字典是迁移 seed 的静态数据（几十行），按进程缓存一份即可；
// 读取失败**不进缓存**（下次重试），这样「启动时库还没就绪」不会把空集合钉死一整个进程。
package currencydict

import (
	"context"
	"strings"
	"sync"

	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/pkg/logger"
)

// dictTypeCurrency 字典类型（与 sysconfig 的 model.DictTypeCurrency 同值）。
//
// 这里写字面量而不是 import sysconfig 的 model：本包只依赖对方的 contract，
// 为了一个字符串常量把 model 拉进来会破模块边界（module_boundary_test 会红）。
const dictTypeCurrency = "currency"

// Adapter 币种代码来源（装配期构造一次，运行期并发读）。
type Adapter struct {
	dict sysconfigcontract.DictReader

	mu     sync.Mutex
	cached []string
}

// New 构造；dict 为 sysconfig 的只读窄口。
func New(dict sysconfigcontract.DictReader) *Adapter { return &Adapter{dict: dict} }

// ListCurrencyCodes 当前启用的币种代码（大写、去重、保持字典顺序）。
//
// 未接线（dict 为 nil）或读取失败时返回 nil + nil：调用方据此**跳过校验**，
// 而不是把所有建单都拒掉 —— 字典读不到是装配 / 基础设施问题，
// 不该表现成「客户下不了单」。
func (a *Adapter) ListCurrencyCodes(ctx context.Context) (codes []string, err error) {
	if a == nil || a.dict == nil {
		return nil, nil
	}
	a.mu.Lock()
	cached := a.cached
	a.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	opts, lerr := a.dict.ListDictOptions(ctx, dictTypeCurrency)
	if lerr != nil {
		// 不缓存：下一次建单重试。真因在 sysconfig 侧的日志里。
		logger.Scene("order").Error(lerr, "币种字典读取失败，本次建单跳过币种校验")
		return nil, lerr
	}
	out := make([]string, 0, len(opts))
	seen := make(map[string]bool, len(opts))
	for _, o := range opts {
		code := strings.ToUpper(strings.TrimSpace(o.Code))
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		out = append(out, code)
	}
	a.mu.Lock()
	a.cached = out
	a.mu.Unlock()
	return out, nil
}
