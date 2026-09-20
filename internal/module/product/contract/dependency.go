package productcontract

// dependency.go — 商品模块的依赖失效端口（审计 ARCH-01）。
//
// 形状与 content 模块的同名端口（content/contract + content_service.go）逐字一致：
// 商品模块只声明「我改了哪些依赖源」，不认识 pipeline.Fanout、也不认识 page /
// presentation —— 扇出编排、精确标记与自动重建全是装配层的事。
//
// 为什么是装配期注入的端口而不是在 service 里 new 一个扇出实现：
//   1. 依赖方向（不变量 7）：product → pipeline 只用于**构造 DepKey**；
//      Fanout 的实现与注册表属于发布内核，由顶层装配接线；
//   2. 未注入时行为收敛（事件照常落库、不产生失效），而不是编译期就把两个模块绑死。

import "context"

// DependencyInvalidator 依赖失效扇出的窄端口。
//
// 实现者必须保证：**永不返回错误**（接口没有返回值）。依赖失效是内容写入的后置
// 副作用，失败只记日志 —— 不能因为「标记/重建失败」让已经成功的内容保存返回失败。
type DependencyInvalidator interface {
	// Invalidate 让某个依赖源键失效（kind 见 pipeline.DepKind*，key 用
	// pipeline.DirectContentKey / ContentCollectionKey 构造，两侧必须逐字一致）。
	Invalidate(ctx context.Context, kind, key string)
}
