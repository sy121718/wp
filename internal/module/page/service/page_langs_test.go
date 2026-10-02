package pageservice

// page_langs_test.go — 页面级语言排除的纯逻辑判据（迁移 491）。
//
// 这里钉的是**冻结口径**（排除在冻结时生效，而不是发布循环里逐个跳过）：
// 判据写错不会报错，只会让产物里的 Manifest.SiteLangs 与互指集合含一批
// 「本站永远不会有产物」的语言 —— 错的产物且看起来正常。

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
		{"ja", true}, // 存储值带空白也认得（比较前 trim）
		{"en-AU", false},
		{"", false},      // 空语言不算排除（它是「未接入语言」，走默认语言回退）
		{"ZH-CN", false}, // 语言码是存储值：大小写敏感
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

	// 排除非默认语言：默认语言原样保留。
	got := dropExcludedLangs(in, []string{"zh-CN"})
	if len(got.SiteLangs) != 2 || got.SiteLangs[0] != "en-AU" || got.SiteLangs[1] != "ja" {
		t.Fatalf("排除后语言表不符：%v", got.SiteLangs)
	}
	if got.DefaultLang != "en-AU" {
		t.Fatalf("默认语言不该变：%q", got.DefaultLang)
	}

	// 没有排除：输入原样返回（连切片都不新建）。
	if got := dropExcludedLangs(in, nil); len(got.SiteLangs) != 3 {
		t.Fatalf("无排除时不该改动语言表：%v", got.SiteLangs)
	}

	// 排除默认语言（T3 会拒绝，但存量数据可能有）：按「默认语言在前」取剩余首项，
	// 绝不产出空默认语言（空默认语言会让所有互指都不是 x-default）。
	got = dropExcludedLangs(in, []string{"en-AU"})
	if got.DefaultLang != "zh-CN" {
		t.Fatalf("默认语言被排除时应回退到剩余首项，实际 %q", got.DefaultLang)
	}

	// 全被排除：保留原集合，把「空语言表」交给上游既有判据处理。
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
