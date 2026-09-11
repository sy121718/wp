package unit

// compile_test.go — 组件渲染切换 Jet 路径后的统一编译入口。
//
// builder.Compile 现在要求注入组件模板 Set（builder 不依赖 internal/templates），
// 本文件提供共享 Set 与 compile helper，所有单测经它编译，避免逐处注入。

import (
	"os"
	"path/filepath"
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
// realSource 读取内联进产物的前端源码（真文件，不造假）。
// 这些文件在 internal/templates/static/ 下：运行时经 /static 给后台，构建期注入给产物。
func realSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("../../../../internal/templates/static/js", name))
	if err != nil {
		t.Fatalf("读取前端源码 %s 失败: %v", name, err)
	}
	return string(b)
}

func compile(t *testing.T, p *builder.Page, opts ...builder.CompileOption) (*builder.CompiledPage, error) {
	t.Helper()
	opts = append([]builder.CompileOption{
		builder.WithComponentSet(componentSet(t)),
		// 与生产装配对齐：客户端增强 + 原始控件基座都注入（源码读真文件，
		// 否则测出来的产物和线上不是一回事，断言也就失去了意义）。
		builder.WithEnhanceSource(realSource(t, "enhance.js")),
		// 控件样式与脚本同进同出：只注入脚本会让产物里的控件没有外观。
		builder.WithUIStyle(templates.UICSS()),
		builder.WithUISources(map[string]string{
			"_util.js":  realSource(t, "ui/_util.js"),
			"select.js": realSource(t, "ui/select.js"),
			"index.js":  realSource(t, "ui/index.js"),
		}),
	}, opts...)
	return builder.Compile(p, opts...)
}
