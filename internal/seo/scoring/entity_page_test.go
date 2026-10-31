package scoring

import (
	"strings"
	"testing"
)

func TestProfileForWiring(t *testing.T) {
	cases := []struct {
		kind PageKind
		want string
	}{
		{KindProduct, "product"},
		{KindCategory, "landing"},
		{KindBrand, "landing"},
		{PageKind("article"), ""},
		{PageKind(""), ""},
	}
	for _, c := range cases {
		p := ProfileFor(c.kind)
		if c.want == "" {
			if p != nil {
				t.Errorf("页型 %q 应回落到默认权重，实际拿到档案 %q", c.kind, p.Type)
			}
			continue
		}
		if p == nil {
			t.Errorf("页型 %q 应拿到档案 %q，实际为 nil", c.kind, c.want)
			continue
		}
		if p.Type != c.want {
			t.Errorf("页型 %q 应拿到档案 %q，实际 %q", c.kind, c.want, p.Type)
		}
		if strings.TrimSpace(p.Reason) == "" {
			t.Errorf("档案 %q 缺少调权理由（文档要求在结果里回显理由）", c.want)
		}
	}
}

func TestScoreEntityPageEchoesProfile(t *testing.T) {
	res := ScoreEntityPage(&EntityPageInput{Kind: KindProduct, Name: "纯棉 T 恤"})
	if res.Profile == nil {
		t.Fatalf("商品页评分应回显 Profile，实际为 nil")
	}
	if res.Profile.Type != "product" {
		t.Fatalf("商品页应回显 product 档案，实际 %q", res.Profile.Type)
	}
	if res.Profile.Reason == "" {
		t.Fatalf("回显的档案缺少调权理由")
	}

	sum := 0.0
	for _, sec := range res.Sections {
		sum += sec.Weight
	}
	if sum < 0.999 || sum > 1.001 {
		t.Fatalf("维度权重之和应为 1，实际 %.4f", sum)
	}
}

func TestProductProfileWeightsMatchDoc(t *testing.T) {
	res := ScoreEntityPage(&EntityPageInput{Kind: KindProduct, Name: "纯棉 T 恤"})
	def := Score(&Input{}, nil)
	weight := func(r *Result, key string) float64 {
		for _, sec := range r.Sections {
			if sec.Key == key {
				return sec.Weight
			}
		}
		return -1
	}
	for _, key := range []string{"images", "tech"} {
		if weight(res, key) <= weight(def, key) {
			t.Errorf("商品页 %s 权重应高于默认，实际 %v vs %v", key, weight(res, key), weight(def, key))
		}
	}
	if weight(res, "content") >= weight(def, "content") {
		t.Errorf("商品页 content 权重应低于默认，实际 %v vs %v", weight(res, "content"), weight(def, "content"))
	}
}

func TestEntityPageContentLengthLowerThanArticle(t *testing.T) {
	body := strings.Repeat("夏季纯棉短袖，透气亲肤，适合日常通勤。", 40)
	prod := ScoreEntityPage(&EntityPageInput{Kind: KindProduct, Name: "纯棉 T 恤", Description: body})
	article := Score(&Input{Title: "纯棉 T 恤", BodyText: body, WordCount: len([]rune(body))}, nil)
	if contentScore(prod) <= contentScore(article) {
		t.Fatalf("商品页内容长度得分应高于信息型口径，实际 %d vs %d", contentScore(prod), contentScore(article))
	}

	cat := ScoreEntityPage(&EntityPageInput{Kind: KindCategory, Name: "T 恤", Description: body})
	if contentScore(cat) >= contentScore(prod) {
		t.Fatalf("分类页内容长度要求应高于商品页，实际 %d vs %d", contentScore(cat), contentScore(prod))
	}
}

func contentScore(r *Result) int {
	for _, sec := range r.Sections {
		if sec.Key == "content" {
			return sec.Score
		}
	}
	return -1
}

func TestEntityPageTitleFallback(t *testing.T) {
	fallback := ScoreEntityPage(&EntityPageInput{Kind: KindProduct, Name: "纯棉 T 恤"})
	if !hasCheckWithActual(fallback, "title_present", "字符") {
		t.Fatalf("未填 SEO 标题时应回落到商品名，实际 %+v", checkOf(fallback, "title_present"))
	}
	override := ScoreEntityPage(&EntityPageInput{Kind: KindProduct, Name: "纯棉 T 恤", SEOTitle: "纯棉 T 恤 透气亲肤款"})
	if checkOf(override, "title_present").Score < checkOf(fallback, "title_present").Score {
		t.Fatalf("填了 SEO 标题时得分不应低于回落值：%+v vs %+v",
			checkOf(override, "title_present"), checkOf(fallback, "title_present"))
	}
}

func TestEntityPageHeadings(t *testing.T) {
	in := entityInputOf(&EntityPageInput{
		Kind: KindProduct, Name: "纯棉 T 恤",
		SpecNames: []string{"颜色", "尺码"}, ChildNames: []string{"夏季新品"},
	})
	if n := len(h1s(in)); n != 1 {
		t.Fatalf("实体页应恰好 1 个 H1，实际 %d", n)
	}
	if len(in.Headings) != 4 {
		t.Fatalf("H1 + 3 个 H2 应为 4 个标题，实际 %d（%+v）", len(in.Headings), in.Headings)
	}
}

func TestEntityPageIntentByKind(t *testing.T) {
	if got := intentOf(&EntityPageInput{Kind: KindProduct}); got != IntentTransactional {
		t.Errorf("商品页应为交易型意图，实际 %q", got)
	}
	if got := intentOf(&EntityPageInput{Kind: KindBrand}); got != IntentCommercial {
		t.Errorf("品牌页应为商业型意图，实际 %q", got)
	}
	if got := intentOf(&EntityPageInput{Kind: KindBrand, Intent: IntentLocal}); got != IntentLocal {
		t.Errorf("显式指定的意图应被尊重，实际 %q", got)
	}
}

func checkOf(r *Result, key string) CheckResult {
	for _, sec := range r.Sections {
		for _, ck := range sec.Checks {
			if ck.Key == key {
				return ck
			}
		}
	}
	return CheckResult{}
}

func hasCheckWithActual(r *Result, key, substr string) bool {
	return strings.Contains(checkOf(r, key).Actual, substr)
}
