// Package view 是视图引擎门面：持有驱动器，提供「模板名 + 变量 → 字节」的渲染入口。
//
// 定位（对照 ThinkPHP 的 think\View）：**只做引擎侧**，不含业务。
//   - 不认识 gin、不认识请求上下文、不读 config —— 变量与配置全部由调用方传入；
//   - 不做变量累积（ThinkPHP 的 View::$data 挂在单例上，靠 PHP 每请求重建进程才安全；
//     Go 是常驻进程，同样的写法会让权限 / 用户 / 语言跨请求串号）；
//   - 请求级数据的正确形态：调用方在 vars 里放**捕获本次请求的闭包**
//     （如 t(key, fallback)、can(code)），或直接放标量（lang、csrf_token）。
//
// 业务侧（gin 适配、权限上下文、菜单、i18n 语言解析）在 internal/render，
// 由它构造 vars 后调用本包 —— 依赖方向单向：internal → pkg。
package view

import (
	"io"

	"go_wp/pkg/view/driver"
)

// Engine 视图引擎。
type Engine struct {
	d driver.Driver
}

// New 用给定驱动器构造引擎（驱动器由 pkg/view/driver/jet 等提供）。
func New(d driver.Driver) *Engine { return &Engine{d: d} }

// Render 渲染模板为字节。
func (e *Engine) Render(name string, vars map[string]any) ([]byte, error) {
	return e.d.Render(name, vars)
}

// RenderTo 渲染并写入 w（先完整渲染再写，避免半截内容出去）。
func (e *Engine) RenderTo(w io.Writer, name string, vars map[string]any) error {
	b, err := e.Render(name, vars)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// Exists 模板是否存在（启动期校验与条件渲染用）。
func (e *Engine) Exists(name string) bool { return e.d.Exists(name) }
