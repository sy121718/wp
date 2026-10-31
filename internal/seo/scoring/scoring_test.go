package scoring

import (
	"strings"
	"testing"
)

func idealInput() *Input {
	title := "Disposable Vape Buying Guide 2026: Puff Count, Battery & Value"
	body := "disposable vape 是目前最受欢迎的一次性电子烟形态。" + strings.Repeat("这一段用于凑足正文字数并保持句子简短。", 60)
	return &Input{
		Title:             title,
		MetaDescription:   "Compare disposable vape puff counts, battery types and cost per puff. Shop authentic devices with fast US shipping. Learn more today.",
		URL:               "https://example.com/disposable-vape-guide",
		FocusKeyword:      "disposable vape",
		SecondaryKeywords: []string{"puff count", "battery", "cost per puff"},
		Intent:            IntentInformational,
		Headings: []Heading{
			{Level: 1, Text: "Disposable Vape Buying Guide"},
			{Level: 2, Text: "Puff count explained"},
			{Level: 3, Text: "How to read puff numbers"},
			{Level: 2, Text: "Battery and charging"},
		},
		BodyText:      body,
		WordCount:     2000,
		InternalLinks: 8,
		ExternalLinks: 2,
		AnchorTexts:   []string{"puff count guide", "battery comparison"},
		Images: []Image{
			{Src: "/storage/hero.webp", Alt: "disposable vape device", SizeKB: 120, Kind: "hero"},
			{Src: "/storage/content.webp", Alt: "puff count chart", SizeKB: 90, Kind: "content"},
		},
		HasCanonical: true,
		HasSchema:    true,
		IsHTTPS:      true,
		Locale:       "zh",
	}
}

func TestScoreIdeal(t *testing.T) {
	res := Score(idealInput(), nil)
	if res.Total < 85 {
		t.Fatalf("理想输入总分偏低: %d（等级 %s）", res.Total, res.Grade)
	}
	if len(res.Sections) != 8 {
		t.Fatalf("维度数应为 8，实际 %d", len(res.Sections))
	}
	for _, sec := range res.Sections {
		if sec.Max == 0 {
			t.Fatalf("维度 %s 无检查项", sec.Key)
		}
		if sec.Color == "" {
			t.Fatalf("维度 %s 缺少色标", sec.Key)
		}
	}
}

func TestScoreEmpty(t *testing.T) {
	res := Score(&Input{}, nil)
	if res.Total != 0 {
		t.Fatalf("空输入应 0 分，实际 %d", res.Total)
	}
	if res.Grade != "F" {
		t.Fatalf("空输入等级应为 F，实际 %s", res.Grade)
	}
	for _, sec := range res.Sections {
		if sec.Color != "red-blocking" {
			t.Fatalf("维度 %s 空输入应为 red-blocking，实际 %s", sec.Key, sec.Color)
		}
	}
}

func TestTitleLengthBoundaries(t *testing.T) {
	cases := []struct {
		title string
		want  int
	}{
		{"", 0},
		{strings.Repeat("a", 19), 2},
		{strings.Repeat("a", 30), 2},
		{strings.Repeat("a", 55), 5},
		{strings.Repeat("a", 60), 5},
		{strings.Repeat("a", 62), 3},
		{strings.Repeat("a", 70), 0},

		{strings.Repeat("测", 30), 5},
		{strings.Repeat("测", 35), 0},
	}
	for _, c := range cases {
		got, _ := chkTitleLength(&Input{Title: c.title})
		if got != c.want {
			t.Errorf("标题 %q: 期望 %d 分，实际 %d", c.title, c.want, got)
		}
	}
}

func TestKeywordDensityBoundaries(t *testing.T) {

	cases := []struct {
		hits     int
		wantZero bool
	}{
		{0, true},
		{2, false},
		{12, true},
	}
	for _, c := range cases {
		body := strings.Repeat("填充正文占位内容", 250)
		in := &Input{FocusKeyword: "测试文本", BodyText: body + strings.Repeat("测试文本", c.hits), WordCount: 1500, Locale: "zh"}
		got, actual := chkKeywordDensity(in)
		if c.wantZero && got != 0 {
			t.Errorf("命中 %d 次（%s）: 期望 0 分，实际 %d", c.hits, actual, got)
		}
		if !c.wantZero && got == 0 {
			t.Errorf("命中 %d 次（%s）: 期望非 0 分", c.hits, actual)
		}
	}
}

func TestHeadingsChecks(t *testing.T) {
	one := &Input{Headings: []Heading{{1, "a"}, {2, "b"}, {3, "c"}}}
	if s, _ := chkSingleH1(one); s != 4 {
		t.Errorf("单个 H1 应 4 分，实际 %d", s)
	}
	if s, _ := chkHierarchy(one); s != 3 {
		t.Errorf("层级正常应 3 分，实际 %d", s)
	}
	two := &Input{Headings: []Heading{{1, "a"}, {1, "b"}}}
	if s, _ := chkSingleH1(two); s != 1 {
		t.Errorf("两个 H1 应 1 分，实际 %d", s)
	}
	jump := &Input{Headings: []Heading{{1, "a"}, {4, "b"}}}
	if s, _ := chkHierarchy(jump); s != 1 {
		t.Errorf("跳级应 1 分，实际 %d", s)
	}
}

func TestInternalLinksBoundaries(t *testing.T) {
	cases := []struct {
		words, links, want int
	}{
		{300, 0, 0},
		{300, 3, 6},
		{300, 10, 2},
		{2000, 8, 6},
		{2000, 6, 4},
		{2000, 1, 2},
	}
	for _, c := range cases {
		in := &Input{WordCount: c.words, InternalLinks: c.links}
		got, actual := chkInternalLinks(in)
		if got != c.want {
			t.Errorf("%d 字 / %d 条内链: 期望 %d 分，实际 %d（%s）", c.words, c.links, c.want, got, actual)
		}
	}
}

func TestImagesChecks(t *testing.T) {
	in := &Input{Images: []Image{
		{Src: "/a.webp", Alt: "a", SizeKB: 10, Kind: "content"},
		{Src: "/b.webp", Alt: "", SizeKB: 10, Kind: "content"},
	}}
	if s, _ := chkAltCoverage(in); s != 2 {
		t.Errorf("一半缺 alt 应 2 分，实际 %d", s)
	}
	if s, _ := chkImageFormat(in); s != 2 {
		t.Errorf("全 webp 应 2 分，实际 %d", s)
	}
}

func TestProfiles(t *testing.T) {
	in := idealInput()
	def := Score(in, nil)
	prod := Score(in, ProductProfile())
	if prod.Sections[6].Weight == def.Sections[6].Weight {
		t.Fatalf("商品页应调整图片权重")
	}
}

func TestScoreDeterministic(t *testing.T) {
	a := Score(idealInput(), nil)
	b := Score(idealInput(), nil)
	if a.Total != b.Total || len(a.Sections) != len(b.Sections) {
		t.Fatalf("两次评分结果不一致")
	}
	for i := range a.Sections {
		if a.Sections[i].Score != b.Sections[i].Score {
			t.Fatalf("维度 %s 两次得分不一致", a.Sections[i].Key)
		}
	}
}

func TestComparisonTableCheck(t *testing.T) {
	scoreOf := func(in *Input) (score, max int, skipped bool) {
		res := Score(in, nil)
		for _, sec := range res.Sections {
			if sec.Key != "content" {
				continue
			}
			for _, ck := range sec.Checks {
				if ck.Key == "comparison_table" {
					return ck.Score, ck.Max, ck.Skipped
				}
			}
		}
		t.Fatal("内容质量维度里找不到 comparison_table 检查项")
		return
	}

	buying := *idealInput()
	buying.Intent = IntentCommercial
	buying.HasComparisonTable = true
	if score, max, skipped := scoreOf(&buying); skipped || score != max || max != 5 {
		t.Errorf("比较型 + 有表应为满分 5：score=%d max=%d skipped=%v", score, max, skipped)
	}

	buying.HasComparisonTable = false
	if score, _, skipped := scoreOf(&buying); skipped || score != 0 {
		t.Errorf("比较型 + 无表应为 0 分（不是跳过）：score=%d skipped=%v", score, skipped)
	}

	info := *idealInput()
	info.Intent = IntentInformational
	info.HasComparisonTable = false
	if _, _, skipped := scoreOf(&info); !skipped {
		t.Error("信息型不要求对比表：应跳过（不计入维度分母），而不是记 0 分")
	}
}
