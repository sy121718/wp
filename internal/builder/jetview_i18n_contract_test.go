package builder

// jetview_i18n_contract_test.go — 「手写 nodeView 的 builder 必须回填 i18n」的门禁。
//
// 为什么需要它：视图的固定文案（筛选栏、分页「第 N 页」、购物车提示…）由各组件包的
// ApplyI18n 回填，而回填**不是自动的** —— 它依赖每个 builder 自己调 applyI18n。
// 漏掉的表现极具欺骗性：页面照常渲染、控件照常显示（BuildView 里落了中文兜底 Labels），
// **只有带计数的成品文案是空的**，而且切语言对它完全无效。
//
// 实际踩过两次：productListViewOf（分页页码文本为空）、新增 core.articleList 时的
// articleListViewOf。两次都靠人肉发现 —— 产物不报错、测试全绿、只有盯着页面才看得出来。
//
// 判据：函数体里出现 &nodeView{ 的（= 手写装配，不走 atomViewOf 泛型）都要有 applyI18n。
// 例外只给「视图没有实现 core.I18nAware」的组件，且**过期条目会让测试失败**
// （只增不减的豁免清单等于没有门禁）。

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// manualViewBuildersWithoutI18n 手写 nodeView、但视图没有实现 core.I18nAware 的 builder。
//
// 加条目要写清楚「这个组件的 View 为什么不需要翻译」；一旦某组件补上了 ApplyI18n，
// 这条豁免就变成过期条目并让测试失败（提醒改由 applyI18n 覆盖）。
var manualViewBuildersWithoutI18n = map[string]string{
	"buttonViewOf":      "core.button 的文案来自作者填写，不是组件固定文案",
	"containerViewOf":   "core.container 无访客可见固定文案",
	"imageViewOf":       "core.image 的 alt/title 来自作者或实体字段绑定",
	"articleListViewOf": "core.articleList 的空态文案由作者填写（EmptyText）",
	"tabsViewOf":        "core.tabs 的页签标题来自作者",
	"accordionViewOf":   "core.accordion 的标题来自作者",
	"marqueeViewOf":     "core.marquee 的内容来自作者",
	"blockRefViewOf":    "core.blockref 是全局块引用，文案在被引用的块里",
	// core.checkoutForm：字段标签 / 分组标题 / 按钮文字目前只有中文（本批不做多语言表单文案，
	// 见 docs 的批次边界）。作者可在 Props 里覆盖标签与按钮文案，那些走内容翻译（Translatable）。
	// 固定文案 key 化（site.component.checkoutForm.*）留到多语言批次，届时把它移到 applyI18n 覆盖。
	"checkoutFormViewOf": "core.checkoutForm 的固定文案 key 化留待多语言批次；作者填写部分走内容翻译",
}

// viewBuilderFuncRe 匹配文件顶层的 *ViewOf 函数定义。
// 用普通字符串而不是反引号字面量：后者在源码里要写两层转义，改一次错一次。
var viewBuilderFuncRe = regexp.MustCompile("(?m)^func (\\w+ViewOf)\\(")

// TestManualViewBuildersApplyI18n 手写 nodeView 的 builder 必须有 applyI18n 调用。
func TestManualViewBuildersApplyI18n(t *testing.T) {
	srcBytes, err := os.ReadFile("jetview.go")
	if err != nil {
		t.Fatalf("读取 jetview.go 失败: %v", err)
	}
	src := string(srcBytes)
	idxs := viewBuilderFuncRe.FindAllStringSubmatchIndex(src, -1)
	if len(idxs) == 0 {
		t.Fatal("没扫到任何 *ViewOf 函数 —— 正则或文件结构变了，这道门禁已失效")
	}
	bodyOf := func(i int) string {
		end := len(src)
		if i+1 < len(idxs) {
			end = idxs[i+1][0]
		}
		return src[idxs[i][0]:end]
	}
	seen := map[string]bool{}
	var missing []string
	for i, m := range idxs {
		name := src[m[2]:m[3]]
		body := bodyOf(i)
		if !strings.Contains(body, "&nodeView{") {
			continue // 走泛型 helper（atomViewOf / decodeProps …），那里已统一回填
		}
		seen[name] = true
		if strings.Contains(body, "applyI18n(") {
			continue
		}
		if _, ok := manualViewBuildersWithoutI18n[name]; ok {
			continue
		}
		missing = append(missing, name)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("以下手写 nodeView 的 builder 缺少 applyI18n（视图文案不会被翻译、带计数的成品文案会是空串）:\n  %s\n"+
			"要么补 applyI18n(&view, ctx)，要么在 manualViewBuildersWithoutI18n 里写明理由", strings.Join(missing, "\n  "))
	}
	// 反向校验：豁免表里已经不需要豁免的条目必须删掉。
	var stale []string
	for i, m := range idxs {
		name := src[m[2]:m[3]]
		reason, ok := manualViewBuildersWithoutI18n[name]
		if !ok {
			continue
		}
		if !seen[name] {
			stale = append(stale, name+"（已不手写 nodeView）: "+reason)
			continue
		}
		if strings.Contains(bodyOf(i), "applyI18n(") {
			stale = append(stale, name+"（已补 applyI18n）: "+reason)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("manualViewBuildersWithoutI18n 有过期条目，请删除:\n  %s", strings.Join(stale, "\n  "))
	}
}
