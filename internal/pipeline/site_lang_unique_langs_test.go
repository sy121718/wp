package pipeline

import (
	"context"
	"fmt"
	"testing"

	"go_wp/internal/builder"
	"go_wp/pkg/builder/core"
	"go_wp/pkg/i18n"
)

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

func TestLocaleViewLanguageSetUnique(t *testing.T) {
	prevDefault := i18n.GetDefaultLang()
	i18n.SetDefaultLang("zh-CN")
	t.Cleanup(func() {
		i18n.SetDefaultLang(prevDefault)
	})

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

		if !alts[0].Default || alts[1].Default {
			t.Fatalf("Default 只应落在默认语言那一条：%+v", alts)
		}
		if !links[0].Current || links[1].Current {
			t.Fatalf("Current 只应落在当前构建语言那一条：%+v", links)
		}
	})

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
