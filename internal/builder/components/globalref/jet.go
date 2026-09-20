// Package globalref — Jet 渲染路径辅助导出（Phase 2）。
//
// 与 Render 方法并行的新路径：props 解码 / 块解析（占位或展开）保留在 Go，
// HTML 拼装交给 globalref.jet 模板（占位 div 或递归展开块 root children）。
// Render 方法保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package globalref

import (
	"html"

	"go_wp/internal/builder/core"
)

// CompileCSS globalref 无自身样式：占位 div 与块展开路径均不生成 CSS。
// 导出仅为与其它组件签名保持一致（nodeView 层统一调用）。
func CompileCSS(id string, b *core.CSSBuckets) {}

// View globalref 渲染视图数据（供 globalref.jet 模板使用）。
type View struct {
	// IsPlaceholder 是否降级为占位（Block 未注入 / 块不存在 / 解析失败 / 空块）。
	IsPlaceholder bool
	// NodeID 节点 ID（占位 div 的 data-sky-id 属性值，模板输出时由 Jet 默认转义）。
	NodeID string
	// Reason 降级归因码（core.RefReason* 稳定枚举）。
	//
	// 写进占位属性与 Manifest 诊断，**不是**错误原文：原文随实现与存储状态变动，
	// 写进产物会破坏确定性构建；排查要看原文时去看日志。
	Reason string
	// RefID 被引用的来源 ID（全局块 ID / 结构模板的构建期虚拟引用 ID）。
	RefID string
	// Slot 结构槽位名（仅 core.layoutSlot 的占位非空，由 layoutslot.BuildView 补上）。
	Slot string
}

// BuildView 生成全局块引用渲染视图：解析块 ID（可用时返回带前缀重写的 root 节点
// 供 nodeView 层递归展开，否则返回占位视图）。返回的 roots 为 nil 表示占位。
func BuildView(node *core.Node, block core.BlockResolver) (View, []*core.Node, error) {
	props, err := decode(node)
	if err != nil {
		return View{}, nil, err
	}
	// 占位视图同时带上归因，供预览定位（哪个槽位 / 哪个节点 / 引用哪份来源 / 为什么）。
	placeholder := View{IsPlaceholder: true, NodeID: html.EscapeString(node.ID), RefID: props.BlockID}

	if block == nil {
		placeholder.Reason = core.RefReasonResolverMissing
		return placeholder, nil, nil
	}
	roots, err := block.ResolveBlockRoot(props.BlockID)
	if err != nil {
		// 解析器可用稳定归因码区分「块不存在」与「结构模板不可用」等类别（见 core.RefDegradeError）；
		// 未包装的错误统一归为「拿不到」。
		placeholder.Reason = core.RefDegradeReason(err, core.RefReasonUnavailable)
		return placeholder, nil, nil
	}
	if len(roots) == 0 {
		// 块存在、能解析，只是没有任何节点：**不是**配置错误（作者建了块还没写内容），
		// 因此与上面两类分开归因 —— 发布期只有前者会让构建失败。
		placeholder.Reason = core.RefReasonEmpty
		return placeholder, nil, nil
	}

	// 块节点 ID 统一加前缀，保证文档内唯一、sky-c-<id> CSS 类不冲突。
	prefix := node.ID + "-b-"
	out := make([]*core.Node, 0, len(roots))
	for _, r := range roots {
		out = append(out, cloneWithIDPrefix(r, prefix))
	}
	return View{}, out, nil
}
