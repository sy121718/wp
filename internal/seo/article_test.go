package seo

// article_test.go — 文章评分提取的口径测试（SEO-10）。
//
// 钉住三件容易悄悄错的事：
//   - SEO 字段的回落顺序（seoTitle 优先于 title）；
//   - 富文本标签不进字数（否则一篇 800 字的文章会被算成 3000 字，长度项虚假达标）；
//   - 内链 / 外链 / 标题结构的提取口径（mailto 不算外链、h1..h6 全收）。

import (
	"strings"
	"testing"

	"go_wp/internal/seo/scoring"
)

func TestArticlePlainTextStripsTagsAndEntities(t *testing.T) {
	body := "<h2>标题</h2><p>正文 &amp; 更多</p>"
	got := articlePlainText(body)
	if strings.Contains(got, "<") || strings.Contains(got, "&amp;") {
		t.Fatalf("标签或实体未还原，得到 %q", got)
	}
	if !strings.Contains(got, "正文 & 更多") {
		t.Fatalf("实体未还原为字面字符，得到 %q", got)
	}
}

func TestArticleHeadingsKeepsLevelsInOrder(t *testing.T) {
	body := "<h3>三</h3><h2>二</h2><h2>二之二</h2>"
	hs := articleHeadings(body)
	if len(hs) != 3 {
		t.Fatalf("标题数应为 3，得到 %d", len(hs))
	}
	want := []int{3, 2, 2}
	for i, h := range hs {
		if h.Level != want[i] {
			t.Errorf("第 %d 个标题级别应为 %d，得到 %d", i, want[i], h.Level)
		}
	}
	if hs[1].Text != "二" {
		t.Errorf("标题文本提取错误：%q", hs[1].Text)
	}
}

func TestArticleLinksCounts(t *testing.T) {
	body := `<p>
		<a href="/about">关于我们</a>
		<a href="https://example.com/x">外部站点</a>
		<a href="//cdn.example.com">协议相对外链</a>
		<a href="mailto:hi@example.com">写信</a>
		<a href="tel:+8613800000000">打电话</a>
		<a href="#section">锚点</a>
	</p>`
	internal, external, anchors := articleLinks(body)
	if internal != 2 {
		t.Errorf("内链应为 2（/about 与 #section），得到 %d", internal)
	}
	if external != 2 {
		t.Errorf("外链应为 2，得到 %d", external)
	}
	// mailto / tel 不计入任何一类，但锚文本照收（它们是真实可点击文字）。
	if len(anchors) != 6 {
		t.Errorf("锚文本应为 6（含邮件与电话），得到 %d", len(anchors))
	}
}

func TestArticleImages(t *testing.T) {
	body := `<p><img src="/a.jpg" alt="配图一"><img src="/b.jpg"></p>`
	imgs := articleImages(body, "/cover.jpg")
	if len(imgs) != 3 {
		t.Fatalf("图片应为 3（正文 2 + 封面 1），得到 %d", len(imgs))
	}
	if imgs[0].Kind != "content" || imgs[2].Kind != "hero" {
		t.Errorf("图片类型错误：%q / %q", imgs[0].Kind, imgs[2].Kind)
	}
	if imgs[0].Alt != "配图一" {
		t.Errorf("alt 未提取：%q", imgs[0].Alt)
	}
}

// TestScoreArticleSEOTitleWins 钉住回落顺序：SEO 字段填了就用它。
func TestScoreArticleSEOTitleWins(t *testing.T) {
	base := map[string]any{
		"title":          "很短",
		"body":           "<p>" + strings.Repeat("正文内容 ", 200) + "</p>",
		"excerpt":        "摘要",
		"focusKeyword":   "关键词",
		"seoTitle":       "一个长度合适、且把关键词放在前半段的 SEO 标题文本",
		"seoDescription": "一段长度合适、带行动号召的描述文本，用于结果页点击率。",
	}
	res := ScoreArticle(base, "/blog/x", "zh-CN")
	if res == nil {
		t.Fatal("评分结果为空")
	}
	// 用 seoTitle 时必须命中「标题含关键词且在前半段」这一项。
	if !hasFullScore(res, "title_keyword_first_half") {
		t.Errorf("SEO 标题未被采纳（title_keyword_first_half 未满分）；分数 %d", res.Total)
	}
}

// TestScoreArticleBodyTagsDoNotInflateWordCount 钉住「标签不算字数」。
func TestScoreArticleBodyTagsDoNotInflateWordCount(t *testing.T) {
	body := "<p>" + strings.Repeat("字", 1200) + "</p>"
	withTags := map[string]any{"title": "标题", "body": body}
	res := ScoreArticle(withTags, "/blog/x", "zh-CN")
	if res == nil {
		t.Fatal("评分结果为空")
	}
	// 内容长度项在 1200 字时应达标；若标签被算进字数会虚高但仍然是满分，
	// 所以这里反过来钉：把标签撑得极大，字数项不应因此被判「过长」。
	padded := map[string]any{"title": "标题", "body": strings.Repeat("<div>", 400) + body}
	res2 := ScoreArticle(padded, "/blog/x", "zh-CN")
	if res2.Total != res.Total {
		t.Errorf("标签影响了评分：%d → %d（标签不应参与字数与内容判断）", res.Total, res2.Total)
	}
}

// TestScoreArticleEmptyData 空数据不 panic、给出可解释的低分。
func TestScoreArticleEmptyData(t *testing.T) {
	for name, data := range map[string]map[string]any{"nil": nil, "empty": {}} {
		res := ScoreArticle(data, "", "zh-CN")
		if res == nil {
			t.Fatalf("%s：评分结果为空", name)
		}
		if res.Total < 0 || res.Total > 100 {
			t.Errorf("%s：总分越界 %d", name, res.Total)
		}
	}
}

// TestScoreArticleKeywordLift 有主关键词的整体分数应高于没有的（其余字段相同）。
func TestScoreArticleKeywordLift(t *testing.T) {
	body := "<h2>长尾词布局</h2><p>" + strings.Repeat("长尾词布局是内容运营的基本功 ", 60) + "</p>"
	without := map[string]any{"title": "长尾词布局", "body": body, "excerpt": "摘要"}
	with := map[string]any{"title": "长尾词布局", "body": body, "excerpt": "摘要", "focusKeyword": "长尾词布局"}
	a, b := ScoreArticle(without, "/blog/x", "zh-CN"), ScoreArticle(with, "/blog/x", "zh-CN")
	if b.Total <= a.Total {
		t.Errorf("设置主关键词后分数应上升：无关键词 %d，有关键词 %d", a.Total, b.Total)
	}
}

// hasFullScore 指定检查项是否满分（按 Key 找，不按顺序）。
func hasFullScore(res *scoring.Result, key string) bool {
	for _, sec := range res.Sections {
		for _, ck := range sec.Checks {
			if ck.Key == key {
				return ck.Score == ck.Max
			}
		}
	}
	return false
}
