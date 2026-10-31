package builder

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var manualViewBuildersWithoutI18n = map[string]string{
	"buttonViewOf":      "core.button 的文案来自作者填写，不是组件固定文案",
	"containerViewOf":   "core.container 无访客可见固定文案",
	"imageViewOf":       "core.image 的 alt/title 来自作者或实体字段绑定",
	"articleListViewOf": "core.articleList 的空态文案由作者填写（EmptyText）",
	"tabsViewOf":        "core.tabs 的页签标题来自作者",
	"accordionViewOf":   "core.accordion 的标题来自作者",
	"marqueeViewOf":     "core.marquee 的内容来自作者",
	"blockRefViewOf":    "core.blockref 是全局块引用，文案在被引用的块里",
}

var viewBuilderFuncRe = regexp.MustCompile("(?m)^func (\\w+ViewOf)\\(")

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
			continue
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
