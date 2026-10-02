package pipeline

// site_lang_unique_langs_test.go — 「LocaleView 输出的语言集合唯一」这条隐含前提。
//
// 为什么需要钉子：seo_head（internal/builder/seo_head.go 的 alternateLinks）与
// sitemap（internal/seo/sitemap.go 的 normalizeAlternates）两个消费端都假定
// LocaleView 不会给出同一个语言码两次 —— 一旦打破，页面照常渲染，只是多出一行
// 重复的 hreflang（无效标注），不会让任何构建失败。属 AGENTS.md「判断错了会静默出错」，
// 因此必须钉住；但**不在消费端加防御**（那是不可达代码），而是在产出口钉住。
//
// 唯一性的实现来源不是按语言码去重，而是 siteRouteEntriesForLangs 按 **Path** 去重：
// Path 是 (lang, logical) 的确定函数，同一语言必然同 Path，于是重复项被丢掉；
// 这比按语言码去重更强（两个语言映射同一路径时同样会被合并）。上游 Validate
// 只拦「两个不同语言码映射同一短码」，同一语言码重复（internal/pipeline/lang.go:292
// 的 EqualFold 分支）放行 —— 所以这条前提此前确实无人守。
//
// 层次选择：就近单测（包内，不碰 DB / HTTP）。判据是「错了会不会静默出错」——
// 会（多一行重复标注、构建不报错），所以必须有测试；但观测点就在 LocaleView 的
// 返回值上，feature 层没有额外信息量（page/feature 的端到端用例已覆盖互指内容本身）。

import (
	"context"
	"fmt"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/pkg/i18n"
)

// langSeqErr 语言序列判据：条数、逐项顺序、两两不重复，三者任一不满足即返回 error。
//
// 返回 error 而不是直接 Fatal 是为了让判据**自身**可被自检（见用例末尾的子测试）：
// 一个永远返回 nil 的断言器会让整条用例变成空转，那比没有测试更糟。
func langSeqErr(label string, got, want []string) error {
	if len(got) != len(want) {
		return fmt.Errorf("%s：语言应有 %d 条 %v，实际 %d 条 %v", label, len(want), want, len(got), got)
	}
	seen := map[string]bool{}
	for i, l := range got {
		if l != want[i] {
			return fmt.Errorf("%s：第 %d 条应为 %s，实际 %v", label, i+1, want[i], got)
		}
		if seen[l] {
			return fmt.Errorf("%s：语言 %s 重复出现（重复 hreflang 属无效标注）：%v", label, l, got)
		}
		seen[l] = true
	}
	return nil
}

func altLangs(alts []builder.Alternate) []string {
	out := make([]string, 0, len(alts))
	for _, a := range alts {
		out = append(out, a.Lang)
	}
	return out
}

func linkLangs(links []core.LocaleLink) []string {
	out := make([]string, 0, len(links))
	for _, l := range links {
		out = append(out, l.Lang)
	}
	return out
}

// TestLocaleViewLanguageSetUnique 同一份站点设置下 LocaleView 输出的语言集合唯一：
// 输入含重复项（批次语言集合 / 启用语言清单都来自外部配置）、当前语言被页面级覆盖
// （不在清单内）时都不产生重复 Lang，默认语言与当前语言的标记也只落在那一条上。
func TestLocaleViewLanguageSetUnique(t *testing.T) {
	prevDefault := i18n.GetDefaultLang()
	i18n.SetDefaultLang("zh-CN")
	t.Cleanup(func() {
		i18n.SetDefaultLang(prevDefault)
	})

	// 批次口径（自动发布实例）：语言集合由调用方传入，重复项在这里被吸收。
	t.Run("批次口径含重复项不重复输出", func(t *testing.T) {
		alts, links := LocaleView(LocaleViewInput{
			Ctx: context.Background(), ProjectID: "p1",
			LogicalPath: "/about", Lang: "zh-CN",
			TargetLangs: []string{"zh-CN", "en-US", "zh-CN", "en-US", "zh-CN"},
		})
		if err := langSeqErr("批次口径互指", altLangs(alts), []string{"zh-CN", "en-US"}); err != nil {
			t.Fatal(err)
		}
		if err := langSeqErr("批次口径切换器", linkLangs(links), []string{"zh-CN", "en-US"}); err != nil {
			t.Fatal(err)
		}
		// 默认语言标记唯一 → head 的 x-default 只可能有一条。
		if !alts[0].Default || alts[1].Default {
			t.Fatalf("Default 只应落在默认语言那一条：%+v", alts)
		}
		if !links[0].Current || links[1].Current {
			t.Fatalf("Current 只应落在当前构建语言那一条：%+v", links)
		}
	})

	// 访问面口径（手工 Page）：启用语言清单同样可能带重复项。
	project := stubLangProject{langs: []string{"zh-CN", "en-US", "zh-CN", "zh-CN"}}
	allPublished := func(string) bool { return true }

	t.Run("访问面口径含重复项不重复输出", func(t *testing.T) {
		alts, links := LocaleView(LocaleViewInput{
			Ctx: context.Background(), Project: project, ProjectID: "p1",
			LogicalPath: "/about", Lang: "zh-CN", Published: allPublished,
		})
		if err := langSeqErr("访问面口径互指", altLangs(alts), []string{"zh-CN", "en-US"}); err != nil {
			t.Fatal(err)
		}
		if err := langSeqErr("访问面口径切换器", linkLangs(links), []string{"zh-CN", "en-US"}); err != nil {
			t.Fatal(err)
		}
		if !links[0].Current || links[1].Current {
			t.Fatalf("Current 只应落在当前语言那一条：%+v", links)
		}
	})

	// 页面级语言覆盖：当前构建语言不在站点清单内。这不改变语言集合的唯一性，
	// 也不该把 Current 标到别的语言上。
	t.Run("当前语言被页面级覆盖时集合仍唯一", func(t *testing.T) {
		alts, links := LocaleView(LocaleViewInput{
			Ctx: context.Background(), Project: project, ProjectID: "p1",
			LogicalPath: "/about", Lang: "ja-JP", Published: allPublished,
		})
		if err := langSeqErr("页面级覆盖互指", altLangs(alts), []string{"zh-CN", "en-US"}); err != nil {
			t.Fatal(err)
		}
		if err := langSeqErr("页面级覆盖切换器", linkLangs(links), []string{"zh-CN", "en-US"}); err != nil {
			t.Fatal(err)
		}
		for _, l := range links {
			if l.Current {
				t.Fatalf("覆盖语言不在语言集合内时不该有 Current 条目：%+v", links)
			}
		}
	})

	// 判据自检：三条真实存在的重复形态都必须被判据捕获，否则上面的用例可能只是空转。
	t.Run("判据自检：重复与多余条目都会被捕获", func(t *testing.T) {
		dup := []string{"zh-CN", "zh-CN", "en-US"}
		if err := langSeqErr("自检", dup, []string{"zh-CN", "en-US"}); err == nil {
			t.Fatal("判据没能捕获「条数多出 + 语言重复」")
		}
		extra := []string{"zh-CN", "en-US", "ja-JP"}
		if err := langSeqErr("自检", extra, []string{"zh-CN", "en-US"}); err == nil {
			t.Fatal("判据没能捕获多余语言")
		}
		swapped := []string{"en-US", "zh-CN"}
		if err := langSeqErr("自检", swapped, []string{"zh-CN", "en-US"}); err == nil {
			t.Fatal("判据没能捕获顺序变化")
		}
		if err := langSeqErr("自检", []string{"zh-CN", "en-US"}, []string{"zh-CN", "en-US"}); err != nil {
			t.Fatalf("判据对正确输入误报：%v", err)
		}
	})
}
