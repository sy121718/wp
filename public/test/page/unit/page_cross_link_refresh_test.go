package unit

// page_cross_link_refresh_test.go — 逐语言发布后的互指闭环（审计 I18N-01 续）。
//
// 缺陷现象（常规流程「先构建两种语言，再逐个发布」）：
//
//	发布 zh 时 en 还没上线 → zh 产物不含指向 en 的互指；
//	随后发布 en 时 zh 已上线 → en 产物含指向 zh 的互指；
//	而 zh 那一份**没有任何人回头重建** → 线上最终是**单向互指**。
//
// 这正是审计 I18N-01 验收的第二条（发布顺序不改变同一计划的字节 / 每条 hreflang
// 目标在同一激活批次可达），也是 page_seo_patrol 点名要抓的「互指单向」。
//
// 修复：激活成功后对同页其余**已发布**语言做一次互指刷新（同一份冻结计划重编译，
// hash 变了才重新激活，未变则一个字节都不写）。

import (
	"context"
	"strings"
	"testing"
	"time"

	pagedto "go_wp/internal/module/page/dto"

	"gorm.io/gorm"
)

// activeHashOfLang 读取某语言当前**已激活**产物的 hash（page_publications 为真源）。
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

// activeUpdatedAtOf 读取某语言激活记录的写入时刻（判定「有没有被重新激活」）。
func activeUpdatedAtOf(t *testing.T, db *gorm.DB, pageID, lang string) time.Time {
	t.Helper()
	var at time.Time
	if err := db.Raw("SELECT update_time FROM page_publications WHERE page_id = ? AND lang = ?", pageID, lang).
		Scan(&at).Error; err != nil {
		t.Fatalf("读取激活记录时刻失败: %v", err)
	}
	return at
}

// assertBidirectionalHreflang 断言两份**已激活**产物互相声明同一套互指。
func assertBidirectionalHreflang(t *testing.T, db *gorm.DB, pageID string, langs ...string) {
	t.Helper()
	wants := []string{
		`hreflang="zh-CN" href="/about"`,
		`hreflang="en-US" href="/en/about"`,
		`hreflang="x-default" href="/about"`,
	}
	// 先把各语言的互指全部读出来再断言：失败信息里要能一眼看出**哪一份缺哪一条**
	//（单向互指的表现恰恰是「一份全、一份空」，只报其中一份等于把现场丢掉一半）。
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
	// 同一份冻结计划 → 两份产物的互指集合与顺序逐字一致（x-default 同一条）。
	for i := 1; i < len(langs); i++ {
		if strings.Join(got[langs[i]], "\n") != strings.Join(got[langs[0]], "\n") {
			t.Fatalf("两份产物的互指不一致：%s", summary())
		}
	}
}

// TestPageSequentialPublishKeepsBidirectionalHreflang 逐语言发布（zh 先、en 后）后
// 两份已激活产物互相声明。
func TestPageSequentialPublishKeepsBidirectionalHreflang(t *testing.T) {
	run := func(t *testing.T, first, second string) {
		db, svc, projects, projectID := newPageService(t)
		withDefaultPlain(t)
		saveSiteLocales(t, projects, projectID, "zh-CN", "en-US")
		page := createPage(t, svc, projectID, "/about", headingDocument)

		// 先构建两种语言，再逐个发布（这正是留下单向互指的常规流程）。
		buildAndPublishLang(t, svc, page.ID, first)
		buildAndPublishLang(t, svc, page.ID, second)

		assertBidirectionalHreflang(t, db, page.ID, "zh-CN", "en-US")
		t.Logf("发布顺序 %s → %s：zh=%s en=%s",
			first, second, activeHashOfLang(t, db, page.ID, "zh-CN"), activeHashOfLang(t, db, page.ID, "en-US"))
	}

	t.Run("zh先en后", func(t *testing.T) { run(t, "zh-CN", "en-US") })
	t.Run("en先zh后", func(t *testing.T) { run(t, "en-US", "zh-CN") })
}

// TestPageRepublishDoesNotRekickUnchangedPeers 重复发布同一语言不产生无谓重建：
// 互指刷新在 hash 未变时一个字节都不写（不重新激活、不刷新激活时刻）。
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
	// 拉开一个可分辨的时间差：若 en 被重新激活，update_time 必然前进。
	time.Sleep(20 * time.Millisecond)

	// 再发布一次 zh（它的字节没有变化，en 的互指也没有变化）。
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
