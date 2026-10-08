// Package driver 定义模板驱动器契约：把「模板名 + 变量」渲染成字节。
//
// 视图引擎（pkg/view）只面向本接口，具体引擎（Jet v6 等）在 driver/ 子包实现 ——
// 与 pkg 的 facade + provider/driver 组织方式一致。
//
// 本包**不认识** gin、不认识请求上下文、不读 config：变量一律由调用方传入。
package driver

// Driver 模板驱动器。
type Driver interface {
	// Render 渲染模板为字节。name 为模板名（不含扩展名时按驱动默认扩展名尝试）。
	Render(name string, vars map[string]any) ([]byte, error)
	// Exists 模板是否存在（启动期校验与条件渲染用）。
	Exists(name string) bool
}
