package lifecycle

import (
	"errors"
	"fmt"
	"sync"
)

// Component 是参与进程生命周期的组件。
type Component struct {
	Name     string
	Critical bool
	Enabled  func() bool
	Init     func() error
	Ready    func() error
	Close    func() error
}

// Runtime 按「关键组件优先、失败逆序回滚」的顺序管理组件。
type Runtime struct {
	mu        sync.Mutex
	components []Component
	preparers  []func()
	initialized []Component
	inited    bool
}

// New 构造生命周期运行时。
func New(components []Component) *Runtime {
	return &Runtime{components: components}
}

// OnPrepare 注册初始化前的准备步骤（按注册顺序执行）。
func (r *Runtime) OnPrepare(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.preparers = append(r.preparers, fn)
}

// Init 初始化全部组件；失败时逆序关闭已初始化的组件并汇总错误。
func (r *Runtime) Init() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inited {
		return nil
	}
	for _, prepare := range r.preparers {
		prepare()
	}
	initialized := make([]Component, 0, len(r.components))
	for _, critical := range []bool{true, false} {
		for _, component := range r.components {
			if component.Critical != critical {
				continue
			}
			if component.Enabled != nil && !component.Enabled() {
				continue
			}
			if component.Init != nil {
				if err := component.Init(); err != nil {
					initErr := fmt.Errorf("component init failed [%s]: %w", component.Name, err)
					if closeErr := CloseAll(initialized); closeErr != nil {
						return errors.Join(initErr, closeErr)
					}
					return initErr
				}
			}
			initialized = append(initialized, component)
		}
	}
	r.initialized = initialized
	r.inited = true
	return nil
}

// Ready 校验运行时已初始化且全部组件就绪。
func (r *Runtime) Ready() error {
	r.mu.Lock()
	ready := r.inited
	components := append([]Component(nil), r.initialized...)
	r.mu.Unlock()
	if !ready {
		return fmt.Errorf("component not ready [runtime]: runtime not initialized")
	}
	for _, component := range components {
		if component.Ready == nil {
			continue
		}
		if err := component.Ready(); err != nil {
			return fmt.Errorf("component not ready [%s]: %w", component.Name, err)
		}
	}
	return nil
}

// Close 逆序关闭已初始化的组件。
func (r *Runtime) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.inited {
		return nil
	}
	err := CloseAll(r.initialized)
	r.initialized = nil
	r.inited = false
	return err
}

// CloseAll 逆序关闭组件并汇总错误。
func CloseAll(components []Component) error {
	var closeErr error
	for i := len(components) - 1; i >= 0; i-- {
		component := components[i]
		if component.Close == nil {
			continue
		}
		if err := component.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("关闭组件 %s 失败: %w", component.Name, err))
		}
	}
	return closeErr
}
