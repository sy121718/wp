package unit

import (
	"context"
	"strings"
	"testing"
	"time"

	pagedto "go_wp/internal/module/page/dto"

	"gorm.io/gorm"
)

func activeHashOfLang(t *testing.T, db *gorm.DB, pageID, lang string) string {
	t.Helper()
	var hash string
	if err := db.Raw("SELECT artifact_hash FROM page_publications WHERE page_id = ? AND lang = ?", pageID, lang).
		Scan(&hash).Error; err != nil {
		t.Fatalf("读取激活产物失败: %v", err)
	}
	if hash == "" {
		t.Fatalf("激活记录缺失（page=%s lang=%s）", pageID, lang)
	}
	return hash
}

func activeUpdatedAtOf(t *testing.T, db *gorm.DB, pageID, lang string) time.Time {
	t.Helper()
	var at time.Time
	if err := db.Raw("SELECT update_time FROM page_publications WHERE page_id = ? AND lang = ?", pageID, lang).
		Scan(&at).Error; err != nil {
		t.Fatalf("读取激活记录时刻失败: %v", err)
	}
	return at
}

func assertBidirectionalHreflang(t *testing.T, db *gorm.DB, pageID string, langs ...string) {
	t.Helper()
	wants := []string{
		`hreflang="zh-CN" href="/about"`,
		`hreflang="en-US" href="/en/about"`,
		`hreflang="x-default" href="/about"`,
	}

	got := make(map[string][]string, len(langs))
	for _, lang := range langs {
		got[lang] = hreflangLinesOf(artifactIndexHTML(t, activeHashOfLang(t, db, pageID, lang)))
	}
	summary := func() string {
		parts := make([]string, 0, len(langs))
		for _, lang := range langs {
			lines := got[lang]
			if len(lines) == 0 {
				lines = []string{"(无互指)"}
			}
			parts = append(parts, lang+"="+strings.Join(lines, " "))
		}
		return strings.Join(parts, " | ")
	}
	for _, lang := range langs {
		joined := strings.Join(got[lang], "\n")
		for _, want := range wants {
			if !strings.Contains(joined, want) {
				t.Fatalf("%s 的**已激活**产物缺少 %s（互指单向）：%s", lang, want, summary())
			}
		}
	}

	for i := 1; i < len(langs); i++ {
		if strings.Join(got[langs[i]], "\n") != strings.Join(got[langs[0]], "\n") {
			t.Fatalf("两份产物的互指不一致：%s", summary())
		}
	}
}

func TestPageSequentialPublishKeepsBidirectionalHreflang(t *testing.T) {
	run := func(t *testing.T, first, second string) {
		db, svc, projects, projectID := newPageService(t)
		withDefaultPlain(t)
		saveSiteLocales(t, projects, projectID, "zh-CN", "en-US")
		page := createPage(t, svc, projectID, "/about", headingDocument)

		buildAndPublishLang(t, svc, page.ID, first)
		buildAndPublishLang(t, svc, page.ID, second)

		assertBidirectionalHreflang(t, db, page.ID, "zh-CN", "en-US")
		t.Logf("发布顺序 %s → %s：zh=%s en=%s",
			first, second, activeHashOfLang(t, db, page.ID, "zh-CN"), activeHashOfLang(t, db, page.ID, "en-US"))
	}

	t.Run("zh先en后", func(t *testing.T) { run(t, "zh-CN", "en-US") })
	t.Run("en先zh后", func(t *testing.T) { run(t, "en-US", "zh-CN") })
}

func TestPageRepublishDoesNotRekickUnchangedPeers(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	withDefaultPlain(t)
	saveSiteLocales(t, projects, projectID, "zh-CN", "en-US")
	page := createPage(t, svc, projectID, "/about", headingDocument)

	buildAndPublishLang(t, svc, page.ID, "zh-CN")
	buildAndPublishLang(t, svc, page.ID, "en-US")
	assertBidirectionalHreflang(t, db, page.ID, "zh-CN", "en-US")

	zhBefore := activeHashOfLang(t, db, page.ID, "zh-CN")
	enBefore := activeHashOfLang(t, db, page.ID, "en-US")
	enUpdatedBefore := activeUpdatedAtOf(t, db, page.ID, "en-US")

	time.Sleep(20 * time.Millisecond)

	if _, err := svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID, Lang: "zh-CN"}); err != nil {
		t.Fatalf("重复发布 zh 失败: %v", err)
	}

	if got := activeHashOfLang(t, db, page.ID, "zh-CN"); got != zhBefore {
		t.Fatalf("重复发布同一语言不应换产物：%s → %s", zhBefore, got)
	}
	if got := activeHashOfLang(t, db, page.ID, "en-US"); got != enBefore {
		t.Fatalf("未变化的语言不应被换产物：%s → %s", enBefore, got)
	}
	if got := activeUpdatedAtOf(t, db, page.ID, "en-US"); !got.Equal(enUpdatedBefore) {
		t.Fatalf("互指刷新对未变化的语言产生了无谓写入（重新激活）：update_time %s → %s",
			enUpdatedBefore.Format(time.RFC3339Nano), got.Format(time.RFC3339Nano))
	}
	assertBidirectionalHreflang(t, db, page.ID, "zh-CN", "en-US")
}
