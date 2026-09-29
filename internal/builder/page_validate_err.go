package builder

// page_validate_err.go — 页面文档校验错误的**结构化**形态（Error() 文本逐字不变）。
//
// 背景：ValidatePage / ValidatePageTolerant 的错误文本是给日志与单测看的中文；而它在
// page 模块的写侧会被拼进业务错误的**补充说明**（page/service/page_draft.go:
// `fmt.Errorf("%w: %v", ErrInvalidDocument, builderErr)`），一路走到后台页面与 JSON 响应 ——
// 英文界面下那半句必须是英文。要让写侧能产出可翻译的明细，它得拿到**参数**
//（第几个顶级节点、深度多少、上限多少），而不是一段中文。
//
// 判据为什么不能用错误文本：嗅探文本是最脆的一类判据（改一个字、加一个空格就静默失效，
// 而且失效方向是「明细消失」而不是报错）。所以这里把校验错误做成**类型**：
//
//	· ErrPageDocumentEmpty —— 文档为空（sentinel，无参数）；
//	· PageSettingsError   —— 页面设置非法（内层原文仍在文本里）；
//	· NodeDepthError      —— 组件树深度超限（带 index / depth / max）；
//	· NodeInvalidError    —— 顶级节点配置非法（带 index）。
//
// **Error() 文本逐字不变**：这些文本被日志、builder/depth_test.go 的单测，以及
// contenttemplate / workbench / blueprint / page 的既有行为依赖（它们把错误压成自己的
// 哨兵或布尔，但 depth_test 与 page_assemble 的「复算文本逐字相同」注释依赖文本稳定）。
// 类型只是**追加**信息，不改变文本。

import (
	"errors"
	"fmt"
)

// ErrPageDocumentEmpty 页面文档为空（nil 文档 / 空指针）。此前是 inline errors.New，
// 提成 sentinel 只为让消费侧能 errors.Is 判定，文本与原来一致。
var ErrPageDocumentEmpty = errors.New("页面文档为空")

// PageSettingsError 页面设置校验失败（validateSettings 给的原因放在 Err 里）。
type PageSettingsError struct {
	Err error
}

// Error 文本与改造前的 `fmt.Errorf("页面设置: %w", err)` 逐字一致。
func (e *PageSettingsError) Error() string {
	if e == nil || e.Err == nil {
		return "页面设置: "
	}
	return "页面设置: " + e.Err.Error()
}

// Unwrap 保持改造前 `%w` 的错误链。
func (e *PageSettingsError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NodeDepthError 组件树深度超过 MaxNodeDepth。
//
// Index 是顶级节点下标（与文本里的「顶级节点 N」同一个 N，0 起）。
type NodeDepthError struct {
	Index int
	Depth int
	Max   int
}

// Error 文本与改造前逐字一致（depth_test.go 断言「组件树深度」「嵌套失控」两句都在）。
func (e *NodeDepthError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("顶级节点 %d: 组件树深度 %d 超过上限 %d（嵌套失控，请简化结构）", e.Index, e.Depth, e.Max)
}

// NodeInvalidError 顶级节点配置非法（未知组件 / 非法 props / 重复 ID 等）。
type NodeInvalidError struct {
	Index int
	Err   error
}

// Error 文本与改造前 `fmt.Errorf("顶级节点 %d: %w", i, verr)` 逐字一致。
func (e *NodeInvalidError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("顶级节点 %d: %v", e.Index, e.Err)
}

// Unwrap 保持改造前 `%w` 的错误链（如 core.ErrIncompleteNode 的判别）。
func (e *NodeInvalidError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
