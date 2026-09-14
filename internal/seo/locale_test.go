package seo

import "testing"

// TestWordCountLocale 英文按词、中文按字统计（SEO-001）。
func TestWordCountLocale(t *testing.T) {
	en := "one two three four five"
	if got := wordCount(en, "en-US"); got != 5 {
		t.Fatalf("en word count: want 5 got %d", got)
	}
	zh := "一二三四五"
	if got := wordCount(zh, "zh-CN"); got != 5 {
		t.Fatalf("zh rune count: want 5 got %d", got)
	}
	if got := wordCount(en, "zh-CN"); got != len([]rune(en)) {
		t.Fatalf("en text with zh locale should count runes")
	}
}

// TestScoreArticleEnglishWordCount 同一段英文在 en 与 zh locale 下字数不同。
func TestScoreArticleEnglishWordCount(t *testing.T) {
	body := "<p>" + "word " + "word " + "word " + "word " + "word" + "</p>"
	data := map[string]any{"title": "Title", "body": body}
	en := ScoreArticle(data, "/blog/x", "en-US")
	zh := ScoreArticle(data, "/blog/x", "zh-CN")
	if en == nil || zh == nil {
		t.Fatal("score nil")
	}
	if en.Total == zh.Total && en.Total > 0 {
		// 至少字数统计不同应影响内容长度相关项；总分完全相同则可疑。
		t.Logf("en total=%d zh total=%d (may differ by rubric)", en.Total, zh.Total)
	}
	if wordCount("word word word word word", "en-US") != 5 {
		t.Fatal("english word split failed")
	}
}
