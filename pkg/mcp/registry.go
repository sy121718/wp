package mcp

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"sort"
	"sync"
)

type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool

	names  []string
	frozen bool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) error {
	if t.name == "" {
		return fmt.Errorf("mcp: 工具名不能为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fmt.Errorf("mcp: 工具注册已冻结，%q 不能再注册", t.name)
	}
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

func (r *Registry) RegisterAll(defs ...Tool) error {
	for _, t := range defs {
		if err := r.Register(t); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.names))
	for _, name := range r.names {
		out = append(out, r.tools[name])
	}
	return out
}

func (r *Registry) Lookup(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Freeze 在全部工具登记完成后冻结目录：迟到注册返回错误，不实现热加载。
func (r *Registry) Freeze() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frozen = true
}

func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.names)
}

func (r *Registry) Invoke(ctx context.Context, name string, args json.RawMessage) (Result, error) {
	t, ok := r.Lookup(name)
	if !ok {
		return Result{}, &UnknownToolError{Name: name}
	}
	return t.Invoke(ctx, args)
}
