// Package image — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 render 函数并行的新路径：图片 URL 直出 + 点击动作（链接/灯箱）+ 图注包裹
// 的分支判定保留在 Go，HTML 拼装交给 image.jet 模板。
// render 函数保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package image

import (
	"fmt"
	"path"
	"strings"

	"go_wp/internal/builder/core"
	"go_wp/pkg/upload"
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
	// IsEager 立即加载（否则懒加载）。按「组件三态 → 主题默认」解析后写入。
	IsEager bool
	// Skeleton 懒加载时显示骨架屏（主题开启骨架且本图懒加载时为真）。
	Skeleton bool
	// Srcset 响应式图片候选（构建期按媒体变体存在性生成；空则不输出 srcset）。
	Srcset string
	// Sizes 浏览器选图提示（配合 Srcset）。
	Sizes string
	// FetchHigh / FetchLow 资源提示（构建期由 fetchPriority 解析，均为 false 时不输出属性）。
	FetchHigh bool
	FetchLow  bool

	// --- 点击动作分支 ---
	// IsLightbox 灯箱分支（零 JS :target 浮层）。
	IsLightbox bool
	// CloseIcon 灯箱关闭按钮图标（基座图标库 close）。
	// 以前模板里是字符 '×'，与图标库的描边风格不一致。
	CloseIcon string
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
func BuildView(node *core.Node, p *Props, class, customID string, content core.ContentResolver, defaults core.ImageDefaults, probe func(string) []int, siteLink func(string) string) (View, error) {
	// 图片地址：CMS 绑定优先，否则手填 Src（媒体库/外链统一 URL）。
	//
	// 出口归一成**完整链接**：产物里的地址直接给访客用，相对路径在「站点与 CMS
	// 不同域」的部署下会指回 CMS 自己。存量 Page Document 里存的是入库当时的相对路径
	// （未配 upload.base_url 时的形态），所以这一层必须归一 —— 只靠写入口覆盖不到历史数据。
	src := upload.StorageURL(p.Src)
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
	// **绑定解析之后也要归一**（顺序不能挪到上面）：
	// 早先只在入口对 p.Src 调了一次 StorageURL，绑定（article.featuredImage 这类）
	// 解析出来的值直接赋给 src —— 于是同一条媒体，手填的产出绝对地址、绑定的产出
	// /storage/... 相对地址。产物里的**所有**资源地址都必须是绝对路径：
	// 站点与 CMS 不同域时相对地址会把图片请求打回 CMS（甚至直接 404）。
	src = upload.StorageURL(src)
	// 协议白名单校验：拒绝 javascript:/data:/vbscript: 等危险协议（降级为空 src，不阻断编译）。
	if !core.IsSafeURL(src) {
		src = ""
	}

	// 加载策略：组件三态（空=默认/on/off，兼容旧 lazy/eager）→ 主题「图片管理」默认。
	attrs := core.ResolveImageLoading(p.Loading, defaults)
	fp := core.ResolveFetchPriority(p.FetchPriority)

	v := View{
		Src:       src,
		Alt:       resolveImageAlt(p, content),
		Title:     p.Title,
		Class:     class,
		IsEager:   attrs.IsEager,
		Skeleton:  attrs.Skeleton,
		FetchHigh: fp.High,
		FetchLow:  fp.Low,
		NodeID:    node.ID,
		Caption:   p.Caption,
	}

	// 响应式图片：媒体变体存在时输出 srcset（构建期探测，访客零查询）。
	// 变体命名约定 <stem>_<type>.jpg：1280→medium、320→thumb（见 media 模块）。
	if probe != nil {
		if widths := probe(src); len(widths) > 0 {
			parts := make([]string, 0, len(widths))
			for _, w := range widths {
				if u := variantURL(src, w); u != "" {
					parts = append(parts, fmt.Sprintf("%s %dw", u, w))
				}
			}
			// 只列已存在的变体：原图仍在 src 里，浏览器可自行回退，
			// 无需猜测原图宽度（猜错会让 srcset 的 w 描述符失真）。
			if len(parts) > 0 {
				v.Srcset = strings.Join(parts, ", ")
				v.Sizes = "(max-width: 640px) 100vw, 50vw"
			}
		}
	}

	// 点击动作分支（与旧 render 内联逻辑一致）。
	switch {
	case p.ClickAction == "lightbox":
		v.IsLightbox = true
	case p.Link != "" || p.ClickAction == "link":
		v.IsLink = true
		// 站内链接本地化（审计 I18N-015）。
		v.Link = core.SiteLinkOrSame(siteLink, p.Link)
		v.TargetBlank = p.LinkTarget == "blank"
		v.RelNofollow = p.LinkRel == "nofollow"
		v.LinkID = customID
	default:
		// 无链接：customID 织入 <img>（旧路径在 <img 后插入 id 属性）。
		v.ImgID = customID
	}

	if v.IsLightbox {
		if svg, ok := core.IconSVGClass("close", "sky-lightbox-close-icon"); ok {
			v.CloseIcon = svg
		}
	}
	return v, nil
}

// variantURL 按宽度返回媒体变体 URL，与 media 模块命名约定 <stem>_<type>.jpg 对齐：
//
//	≤320  → _thumb（320×320 Fit）
//	≤1280 → _medium（1280×1280 Fit）
//	其余  → 空串（无对应变体，跳过该候选）
//
// 变体统一编码为 JPEG，故后缀固定 .jpg（源图后缀只用于截断 stem）。
// 带查询串的 URL 不参与（变体按对象键生成，查询串会让路径失配）。
// resolveImageAlt 图片替代文本：手填 Alt 优先，否则走 AltBinding 绑定字段。
//
// 两条都不命中时返回空串：**不替作者编一个描述** —— 编出来的 alt 比空 alt 更糟
// （搜索引擎会把它当真实内容，读屏用户会被念一段没人写过的句子）。
// 绑定解析失败同样回落空串：alt 是增强项，不该让整页编译失败。
func resolveImageAlt(p *Props, content core.ContentResolver) string {
	if p == nil {
		return ""
	}
	if alt := strings.TrimSpace(p.Alt); alt != "" {
		return alt
	}
	if p.AltBinding == nil || strings.TrimSpace(p.AltBinding.Field) == "" || content == nil {
		return ""
	}
	v, err := content.ResolveString(p.AltBinding.Field)
	if err != nil {
		return ""
	}
	if s := strings.TrimSpace(v); s != "" {
		return s
	}
	return strings.TrimSpace(p.AltBinding.Fallback)
}

func variantURL(src string, width int) string {
	if src == "" || strings.Contains(src, "?") {
		return ""
	}
	ext := path.Ext(src)
	if ext == "" {
		return ""
	}
	stem := strings.TrimSuffix(src, ext)
	switch {
	case width <= 320:
		return stem + "_thumb.jpg"
	case width <= 1280:
		return stem + "_medium.jpg"
	default:
		return ""
	}
}
