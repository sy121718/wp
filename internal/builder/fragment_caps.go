package builder

// fragment_caps.go — 从产物 HTML 里识别「页面引用了哪些运行时片段能力」（审计 UIK-005）。
//
// 判定特征：页面里出现指向 /_fragments/<capability> 的 htmx 请求属性值
// （hx-get / hx-post / hx-put / hx-patch / hx-delete —— 前缀判定，新指令不必回来改表）。
//
// 为什么按**值**判定，而不是按「页面上放了哪个组件」：
//   样式该跟着消费方走。作者手写 <button hx-post="/_fragments/cartAdd"> 的页面与
//   放了 core.addToCart 的页面需要同一份片段样式；反过来，放了 core.cartIcon 却从不
//   打开浮层的页面不该白带（UIK-005 的原始症状正是前者 —— 自定义入口拿不到样式）。
//
// 为什么只扫 hx-*：htmx 是片段消费的唯一入口（组件在输出 hx-post 的同时输出原生
// action 降级路径，同一个 URL，覆盖 hx-* 就覆盖了组件两条路径）；而这次扫描只在页面
// **确实存在** hx-* 属性时才会发生（见 featureScan），纯内容页零成本 ——
// 与 PERF-014「不再对整页跑 tokenizer」的收敛方向一致。
//
// 一致性：登记路径（CompiledPage.Features）与 tokenize 路径（collectHTMLScan）产出的
// 片段能力集合都来自**同一份 HTML** 的同一段解析逻辑，所以两条路径的结果逐字节一致
// （ui_feature_crosscheck_test.go 的 A 断言直接覆盖这一点）。

import (
	"strings"

	"golang.org/x/net/html"
)

// fragmentPathPrefix 运行时片段端点的路径前缀（与 runtimefragment 的路由一致）。
const fragmentPathPrefix = "/_fragments/"

// fragmentCapsFromHTML 收集 HTML 里被引用的片段能力（小写能力名集合）。
//
// 只读 hx-* 属性的值：属性名收集归collectHTMLScan 的 attrs，这里只关心值里的片段路径。
func fragmentCapsFromHTML(content string) htmlFeatures {
	out := htmlFeatures{}
	z := html.NewTokenizer(strings.NewReader(content))
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			return out // 内存字符串读到 EOF；script / style 内的原始文本不当成标签
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		_, more := z.TagName()
		for more {
			var key, val []byte
			key, val, more = z.TagAttr()
			if !strings.HasPrefix(strings.ToLower(string(key)), "hx-") {
				continue
			}
			for _, cap := range fragmentCapsIn(string(val)) {
				out[cap] = struct{}{}
			}
		}
	}
}

// fragmentCapsIn 从一段属性值里取出全部 /_fragments/<capability> 能力名（小写）。
//
// 值里可能是相对路径、带查询串（?projectId=…）、带绝对前缀（https://site/_fragments/x）——
// 只要出现该前缀就取紧随其后的能力名，直到遇到非能力名字符为止。
func fragmentCapsIn(value string) []string {
	var out []string
	rest := value
	for {
		idx := strings.Index(rest, fragmentPathPrefix)
		if idx < 0 {
			return out
		}
		rest = rest[idx+len(fragmentPathPrefix):]
		end := 0
		for end < len(rest) && isFragmentCapByte(rest[end]) {
			end++
		}
		if end == 0 {
			// "/_fragments/" 后面直接是分隔符（如模板里的空值）—— 继续往后找，不产生能力名。
			continue
		}
		out = append(out, strings.ToLower(rest[:end]))
		rest = rest[end:]
	}
}

// isFragmentCapByte 能力名的合法字符（与 runtimefragment 的路由参数一致：字母数字与下划线）。
func isFragmentCapByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9', b == '_':
		return true
	}
	return false
}

// hasHXAttr 页面是否出现了 htmx 指令属性（决定要不要付一次片段能力扫描）。
func hasHXAttr(attrs htmlFeatures) bool {
	for name := range attrs {
		if strings.HasPrefix(name, "hx-") {
			return true
		}
	}
	return false
}
