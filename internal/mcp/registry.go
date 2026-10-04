package mcp

// registry.go — 工具注册表：装配期由各模块注册，运行期只读。
//
// 一份注册表服务两类消费者：进程内的 AI 助手与外部 /mcp 端点。
// 所以它的形状必须与 MCP 的 tools/list 足够近 —— 转换只该剩「改字段名」这一步，
// 而不是「外部客户端看不到某类工具」这种要靠读两处装配代码才能发现的分叉。

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// Registry 工具注册表。
//
// 加锁而不是「装配完就没人写」的约定：并发读是常态（每个会话都可能列举工具），
// 而「没人再注册」是个不成文前提 —— 一旦将来加个运行期注册（插件、热加载），
// 它就是一张会偶发崩的死锁门票。
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
	// names 已排序：tools/list 的顺序必须稳定 —— 工具集一变，provider 侧的前缀就整段作废
	//（docs/16 §3），而「每次列举顺序不同」会让这件事变成随机发生的。
	names []string
}

// NewRegistry 建一个空注册表。
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register 注册一个工具。
//
// 重名**报错而不覆盖**：两个来源声明同名工具时，覆盖的失败模式取决于装配顺序 ——
// 排查时先得搞清楚「谁后注册」，而这件事在源码里看不出来。
func (r *Registry) Register(t Tool) error {
	if t.name == "" {
		return fmt.Errorf("mcp: 工具名不能为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.tools[t.name]; dup {
		return fmt.Errorf("mcp: 工具 %q 已被注册", t.name)
	}
	r.tools[t.name] = t
	at := sort.SearchStrings(r.names, t.name)
	r.names = append(r.names, "")
	copy(r.names[at+1:], r.names[at:])
	r.names[at] = t.name
	return nil
}

// RegisterAll 批量注册（装配期用）：任一失败即整体失败。
//
// 不做「跳过坏的、继续注册其余的」：工具集停在半装配状态时，模型看到的能力集合
// 取决于哪个模块先装配 —— 那是排查不出来的不确定性。装配期 fail-fast 更好。
func (r *Registry) RegisterAll(defs ...Tool) error {
	for _, t := range defs {
		if err := r.Register(t); err != nil {
			return err
		}
	}
	return nil
}

// List 返回全部工具（按名称升序）。
func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.names))
	for _, name := range r.names {
		out = append(out, r.tools[name])
	}
	return out
}

// Lookup 按名取工具。
func (r *Registry) Lookup(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Len 工具数量。
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.names)
}

// Invoke 按名调用工具（参数校验在执行体内，见 Tool.Invoke）。
func (r *Registry) Invoke(ctx context.Context, name string, args json.RawMessage) (Result, error) {
	t, ok := r.Lookup(name)
	if !ok {
		return Result{}, &UnknownToolError{Name: name}
	}
	return t.Invoke(ctx, args)
}
