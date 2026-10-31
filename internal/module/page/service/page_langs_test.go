package pageservice

import (
	"testing"

	pagemodel "go_wp/internal/module/page/model"
	"go_wp/internal/pipeline"
)

func TestPageExcludesLang(t *testing.T) {
	page := &pagemodel.PageEntity{ExcludedLangs: pagemodel.StringArray{"zh-CN", " ja "}}
	cases := []struct {
		lang string
		want bool
	}{
		{"zh-CN", true},
		{"ja", true},
		{"en-AU", false},
		{"", false},
		{"ZH-CN", false},
	}
	for _, c := range cases {
		if got := pageExcludesLang(page, c.lang); got != c.want {
			t.Errorf("pageExcludesLang(%q) = %v，期望 %v", c.lang, got, c.want)
		}
	}
	if pageExcludesLang(nil, "zh-CN") {
		t.Error("nil 页面不该判定为已排除（调用方未加载页面时不能静默跳过）")
	}
}

func TestDropExcludedLangs(t *testing.T) {
	in := pipeline.SiteLangInputs{SiteLangs: []string{"en-AU", "zh-CN", "ja"}, DefaultLang: "en-AU"}

	got := dropExcludedLangs(in, []string{"zh-CN"})
	if len(got.SiteLangs) != 2 || got.SiteLangs[0] != "en-AU" || got.SiteLangs[1] != "ja" {
		t.Fatalf("排除后语言表不符：%v", got.SiteLangs)
	}
	if got.DefaultLang != "en-AU" {
		t.Fatalf("默认语言不该变：%q", got.DefaultLang)
	}

	if got := dropExcludedLangs(in, nil); len(got.SiteLangs) != 3 {
		t.Fatalf("无排除时不该改动语言表：%v", got.SiteLangs)
	}

	got = dropExcludedLangs(in, []string{"en-AU"})
	if got.DefaultLang != "zh-CN" {
		t.Fatalf("默认语言被排除时应回退到剩余首项，实际 %q", got.DefaultLang)
	}

	got = dropExcludedLangs(in, []string{"en-AU", "zh-CN", "ja"})
	if len(got.SiteLangs) != 3 {
		t.Fatalf("全排除时不该造出空语言表（失败点会被推到更远）：%v", got.SiteLangs)
	}
}

func TestPlanHasExcludedLang(t *testing.T) {
	plan := pipeline.PlanOfSiteLangInputs(pipeline.SiteLangInputs{
		SiteLangs: []string{"en-AU", "zh-CN"}, DefaultLang: "en-AU",
	})
	if !planHasExcludedLang(plan, []string{"zh-CN"}) {
		t.Fatal("计划含被排除语言时应判定为需要重新冻结")
	}
	if planHasExcludedLang(plan, []string{"ja"}) {
		t.Fatal("计划不含该语言时不该触发重冻")
	}
	if planHasExcludedLang(plan, nil) {
		t.Fatal("没有排除项时不该触发重冻")
	}
}
