package unit

// compile_test.go — 组件渲染切换 Jet 路径后的统一编译入口。
//
// builder.Compile 现在要求注入组件模板 Set（builder 不依赖 internal/templates），
// 本文件提供共享 Set 与 compile helper，所有单测经它编译，避免逐处注入。

import (
	"sync"
	"testing"

	"github.com/CloudyKit/jet/v6"

	"go_wp/internal/builder"
	"go_wp/internal/templates"
)

var (
	csetOnce sync.Once
	cset     *jet.Set
	csetErr  error
)

// componentSet 返回组件模板 Set（测试进程工作目录为 public/test/builder/unit）。
func componentSet(t *testing.T) *jet.Set {
	t.Helper()
	csetOnce.Do(func() {
		cset, csetErr = templates.NewComponentSet("../../../../internal/templates/components")
	})
	if csetErr != nil {
		t.Fatalf("加载组件模板 Set 失败: %v", csetErr)
	}
	return cset
}

// compile 用组件模板 Set 编译页面（Compile 切换到 Jet 路径后必需注入 Set）。
// 返回值与 builder.Compile 一致，便于成功/失败两种断言场景复用。
func compile(t *testing.T, p *builder.Page, opts ...builder.CompileOption) (*builder.CompiledPage, error) {
	t.Helper()
	opts = append([]builder.CompileOption{builder.WithComponentSet(componentSet(t))}, opts...)
	return builder.Compile(p, opts...)
}
