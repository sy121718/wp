// Package container — Jet 渲染路径辅助导出（Phase 0 样板）。
//
// 与 Render 方法并行的新路径：props 解码 / CSS 生成 / 属性串与形状分隔线计算
// 保留在 Go，HTML 拼装交给 container.jet 模板（含子节点递归 include）。
// Render 方法保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package container

import (
	"html"
	"strings"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出容器样式编译（复用 Render 内部的 compileCSS，CSS 字节与旧路径一致）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View container 渲染视图数据（供 container.jet 模板使用）。
type View struct {
	// Tag 原生语义标签（div/section/article/aside/nav/header/footer/main）。
	Tag string
	// Attrs 前导空格 + 属性串（组父联动 / 自定义属性 / 抽屉协议，均已转义）。
	Attrs string
	// ShapeTop 顶部形状分隔线内容片段（path，空则无；<svg> 骨架由 container.jet 渲染）。
	ShapeTop string
	// ShapeBottom 底部形状分隔线内容片段（path，空则无；<svg> 骨架由 container.jet 渲染）。
	ShapeBottom string
	// BgSlides 背景轮播图（多张；渲染为绝对定位背景层，纯 CSS 交叉淡入）。
	BgSlides []string
	// FeatureAttrs 本次渲染真实输出的**属性名**（审计 PERF-014）：抽屉协议属性与作者填的
	// 自定义属性（data-*）。产物组装层据此决定注入哪些脚本 —— 作者在容器上写
	// data-modal-open="f1" 就该带上 modal.js，而这条线索只有拼属性串的这里知道。
	FeatureAttrs []string
}

// BuildView 生成容器渲染视图：属性串 + 形状分隔线（与 Render 输出结构一致）。
func BuildView(node *core.Node, p *Props) View {
	var attrs strings.Builder
	// 组父联动标记（03-A：子组件可经 [data-sky-group] 联动）。
	if p.StyleEx.GroupParent {
		attrs.WriteString(` data-sky-group="true"`)
	}
	// 自定义属性键值对（白名单 key + 安全 value）。
	for _, kv := range p.StyleEx.Attributes {
		attrs.WriteString(" ")
		attrs.WriteString(kv.Key)
		attrs.WriteString(`="`)
		attrs.WriteString(html.EscapeString(kv.Value))
		attrs.WriteString(`"`)
	}
	// 抽屉协议（:target 显隐，零 JS）。
	if p.Position.Type == "drawer" {
		attrs.WriteString(` id="sky-drawer-`)
		attrs.WriteString(node.ID)
		attrs.WriteString(`" data-drawer-side="`)
		attrs.WriteString(p.Position.DrawerSide)
		attrs.WriteString(`"`)
		if p.Position.DrawerOverlay {
			attrs.WriteString(` data-drawer-overlay="true"`)
		}
	}

	v := View{Tag: p.Tag, Attrs: attrs.String(), BgSlides: p.Visual.BgSlides}
	// 运行时特征（审计 PERF-014）：属性名在这里顺手记一份（属性串是拼出来的，
	// 事后从字符串反查会把属性值里的同名字样误当成属性）。
	if p.StyleEx.GroupParent {
		v.FeatureAttrs = append(v.FeatureAttrs, "data-sky-group")
	}
	v.FeatureAttrs = append(v.FeatureAttrs, featureAttrKeys(p.StyleEx.Attributes)...)
	if p.Position.Type == "drawer" {
		v.FeatureAttrs = append(v.FeatureAttrs, "data-drawer-side")
		if p.Position.DrawerOverlay {
			v.FeatureAttrs = append(v.FeatureAttrs, "data-drawer-overlay")
		}
	}
	if p.StyleEx.ShapeDivider != "" {
		svg := shapeDividers[p.StyleEx.ShapeDivider]
		if p.StyleEx.ShapeDividerPosition == "top" {
			v.ShapeTop = svg
		} else {
			v.ShapeBottom = svg
		}
	}
	return v
}

// featureAttrKeys 从自定义属性白名单里挑出会影响脚本注入的属性名（data-* / hx-*）。
//
// 白名单（attrKeyRe）只放行 data-* / aria-* / role / title / tabindex，其中只有 data- 前缀
// 参与产物组装层的判定 —— 把 aria-* 之类也登记进去反而会让「登记 ⊆ 实际输出」这条
// 交叉验证失真（tokenize 不收集它们）。
func featureAttrKeys(kvs []AttributeKV) []string {
	var out []string
	for _, kv := range kvs {
		key := strings.ToLower(strings.TrimSpace(kv.Key))
		if strings.HasPrefix(key, "data-") || strings.HasPrefix(key, "hx-") {
			out = append(out, key)
		}
	}
	return out
}

// DeclareFeatures 实现 core.ViewFeatureDeclarer（审计 PERF-014）。
func (v View) DeclareFeatures() (attrs, classes []string) {
	return v.FeatureAttrs, nil
}
