// Package runtimefragment 运行时片段能力注册表（0-D，docs/04 §1.2）。
//
// Fragment Registry 是「静态站 + 动态能力」的桥：capability 白名单（type →
// 处理器 + 匿名/认证策略）。Page Document 只保存语义化 Fragment Type，
// 不保存 endpoint/脚本；Handler 不读 Page Document、不执行 Jet、不解释
// Binding——只实现该 capability 的固定运行时协议并返回 HTML 片段。
package runtimefragment

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Request 单次片段请求（props 已由 endpoint 白名单校验）。
type Request struct {
	// Type 能力类型（白名单 key）。
	Type string
	// Context 语义上下文（currentProduct / visitorSession / searchQuery，可选）。
	Context string
	// Params 受限查询参数（已长度/枚举校验）。
	Params map[string]string
	// UserID 已认证用户 ID（session 策略时非空）。
	UserID string
}

// Spec 一个运行时片段能力（capability 白名单条目）。
type Spec struct {
	// Type 能力类型（如 loginPanel / cartSummary）。
	Type string
	// Method HTTP method（GET 无副作用 / POST 写操作带 CSRF）。
	Method string
	// Auth 认证策略：anonymous（公开）/ session（需登录）。
	Auth string
	// Render 处理器：返回 HTML 片段（由 Registry 统一 escape/sanitize 边界，
	// 处理器内部输出的用户数据必须经 html.EscapeString）。
	Render func(ctx context.Context, r *Request) (string, error)
}

// 认证策略常量。
const (
	AuthAnonymous = "anonymous"
	AuthSession   = "session"
)

// registry 能力白名单（进程级，包 init 自注册内置 capability）。
var (
	registryMu sync.RWMutex
	registry   = map[string]Spec{}
)

// Register 注册片段能力（重复类型覆盖，便于测试替换）。
func Register(s Spec) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[s.Type] = s
}

// Lookup 按类型查能力（白名单校验的唯一入口）。
func Lookup(typeName string) (s Spec, ok bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	s, ok = registry[typeName]
	return s, ok
}

// Types 全部能力类型（字典序，确定性）。
func Types() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for t := range registry {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// validateContext 语义上下文白名单（协议：context 只能是枚举值）。
func validateContext(ctx string) error {
	switch ctx {
	case "", "currentProduct", "visitorSession", "searchQuery":
		return nil
	}
	return fmt.Errorf("非法的片段上下文: %q", ctx)
}
