// Package image — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 render 函数并行的新路径：图片 URL 直出 + 点击动作（链接/灯箱）+ 图注包裹
// 的分支判定保留在 Go，HTML 拼装交给 image.jet 模板。
// render 函数保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package image

import (
	"fmt"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出图片样式编译（复用 render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View image 渲染视图数据（供 image.jet 模板使用）。
type View struct {
	// Src 图片地址（协议校验后，模板输出时由 Jet 默认转义）。
	Src string
	// Alt 替代文本（模板输出时由 Jet 默认转义）。
	Alt string
	// Title 局部标题（模板输出时由 Jet 默认转义）。
	Title string
	// Class 已合并节点 class（非空才输出 class 属性，模板输出时由 Jet 默认转义）。
	Class string
	// IsEager 立即加载（否则懒加载）。
	IsEager bool
	// FetchHigh 加载优先级 high。
	FetchHigh bool

	// --- 点击动作分支 ---
	// IsLightbox 灯箱分支（零 JS :target 浮层）。
	IsLightbox bool
	// IsLink 链接分支（Link 非空或 ClickAction == link）。
	IsLink bool
	// Link 链接地址（模板输出时由 Jet 默认转义）。
	Link string
	// TargetBlank 新窗口打开。
	TargetBlank bool
	// RelNofollow 加 nofollow。
	RelNofollow bool
	// LinkID 链接分支的自定义 Element ID（加到 <a>）。
	LinkID string
	// ImgID 无链接分支的自定义 Element ID（加到 <img>）。
	ImgID string
	// NodeID 节点 ID（灯箱锚点/浮层 id 前缀，模板输出时由 Jet 默认转义）。
	NodeID string

	// Caption 图注（非空则 figure/figcaption 包裹）。
	Caption string
}

// BuildView 生成图片渲染视图：URL 直出 + 点击动作分支判定 + 图注（与 render 输出结构一致）。
// class 为已合并的节点 class（nodeView 层计算），customID 为 Advanced 自定义 Element ID。
func BuildView(node *core.Node, p *Props, class, customID string, content core.ContentResolver) (View, error) {
	// 图片地址：CMS 绑定优先，否则手填 Src（媒体库/外链统一 URL）。
	src := p.Src
	if p.Binding != nil && p.Binding.Field != "" {
		if content == nil {
			return View{}, fmt.Errorf("编译上下文缺少内容解析器，无法解析绑定 %q", p.Binding.Field)
		}
		v, err := content.ResolveString(p.Binding.Field)
		if err != nil {
			return View{}, fmt.Errorf("解析绑定 %q 失败: %w", p.Binding.Field, err)
		}
		if v != "" {
			src = v
		} else if p.Binding.Fallback != "" {
			src = p.Binding.Fallback
		}
	}
	if src == "" {
		return View{}, fmt.Errorf("图片地址为空")
	}
	// 协议白名单校验：拒绝 javascript:/data:/vbscript: 等危险协议（降级为空 src，不阻断编译）。
	if !core.IsSafeURL(src) {
		src = ""
	}

	v := View{
		Src:       src,
		Alt:       p.Alt,
		Title:     p.Title,
		Class:     class,
		IsEager:   p.Loading == "eager",
		FetchHigh: p.FetchPriority == "high",
		NodeID:    node.ID,
		Caption:   p.Caption,
	}

	// 点击动作分支（与旧 render 内联逻辑一致）。
	switch {
	case p.ClickAction == "lightbox":
		v.IsLightbox = true
	case p.Link != "" || p.ClickAction == "link":
		v.IsLink = true
		v.Link = p.Link
		v.TargetBlank = p.LinkTarget == "blank"
		v.RelNofollow = p.LinkRel == "nofollow"
		v.LinkID = customID
	default:
		// 无链接：customID 织入 <img>（旧路径在 <img 后插入 id 属性）。
		v.ImgID = customID
	}

	return v, nil
}
