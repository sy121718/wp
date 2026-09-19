package pagecontract

// page_preview_problem.go — 预览编译失败里「作者可操作」的那一类，用**类型**标记（而不是错误文本）。
//
// 背景：工作台画布的核心价值是「作者在画布上直接看到哪里坏了、怎么修」——例如
// 「手风琴至少需要一个折叠项（把组件拖入内部）」「常见问题至少需要一条」。这些提示由
// builder.ValidatePageTolerant 产出，而组件校验器返回的是**裸 fmt.Errorf**（不带类型）。
// page/service 过去用 `%w: %v` 把 compile 错误转成字符串，类型在链里就丢了，于是消费侧
// （workbench 的 422 出口）只有两条路：嗅探错误文本，或者把一切都压成一句泛化的
// 「预览编译失败」——两者都不能接受（前者判据不稳，后者把可操作信息丢给作者去猜）。
//
// 这个类型把「这条错误可以给作者看」变成**类型事实**：
//
//	产生处（page/service 的校验分支）—— 用 NewPreviewProblem 标记；
//	消费侧（workbench 的 422 出口）  —— 用 errors.As 判别，命中就把 Msg 透出到响应体。
//
// 反向同样重要：**不能给作者看的内部错误**（装配缺失、组件模板加载失败、驱动原文）
// 一律**不带**这个标记 —— 消费侧对它们只给归口文案 + 结构化日志，原文只进日志。
//
// 为什么放在 contract 而不是 page/service：消费侧（workbench）按模块边界不能 import
// page/service（见 internal/module/CLAUDE.md 的表隔离约定），而它必须能判别这个类型。
type PreviewProblem struct {
	// Msg 可展示的问题原文（组件校验器产出的、面向作者的提示，含节点定位）。
	Msg string
	// cause 由产生处传入（page/service 的 errCompileFailed 哨兵）：
	// 保证 errors.Is(err, errCompileFailed) 在包装之后**仍然成立** ——
	// 构建期与预览期既有的判别不能因为加了标记而变化。
	cause error
}

// NewPreviewProblem 构造一条可展示问题。
//
// msg 必须是**作者可操作**的提示（来自文档校验，不是驱动/装配原文）；
// cause 传调用方自己的编译失败哨兵（通常 errCompileFailed）。
func NewPreviewProblem(msg string, cause error) *PreviewProblem {
	return &PreviewProblem{Msg: msg, cause: cause}
}

// Error 实现 error：文本即可展示提示本身（不额外加前缀 —— 前缀「预览编译失败」
// 由消费侧按当前语言拼接，这里不做展示层的事）。
func (p *PreviewProblem) Error() string {
	if p == nil {
		return ""
	}
	return p.Msg
}

// Unwrap 返回 cause：让 errors.Is / errors.As 能继续往下穿（既有判别不受影响）。
func (p *PreviewProblem) Unwrap() error {
	if p == nil {
		return nil
	}
	return p.cause
}
