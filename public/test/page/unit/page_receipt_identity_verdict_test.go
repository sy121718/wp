package unit

import (
	"context"
	"strconv"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	pagepageservice "go_wp/internal/module/page/service"

	"gorm.io/gorm"
)

func saveDraftReqOf(t *testing.T, db *gorm.DB, pageID, doc string) pagedto.SaveDraftReq {
	t.Helper()
	var row struct {
		DraftPath    string
		DraftVersion int64
	}
	if err := db.Raw(`SELECT draft_path, draft_version FROM pages WHERE id = ?`, pageID).Scan(&row).Error; err != nil {
		t.Fatalf("读取页面草稿路径失败: %v", err)
	}
	if row.DraftPath == "" {
		t.Fatalf("前置不成立：页面 %s 没有草稿路径", pageID)
	}

	return pagedto.SaveDraftReq{ID: pageID, ExpectedVersion: row.DraftVersion, DraftPath: row.DraftPath, DraftDocument: []byte(doc)}
}

func formatInt64(v int64) string { return strconv.FormatInt(v, 10) }

const publishContextDoc = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

func markPublicationLedger(t *testing.T, db *gorm.DB, pageID, lang, path, artifactID, hash string, receiptID *int64) {
	t.Helper()
	statement := `INSERT INTO page_publications (page_id, lang, active_path, artifact_id, artifact_hash, published_at, update_time, receipt_id)
		VALUES (?, ?, ?, NULLIF(?, '')::uuid, ?, now(), now(), ?)
		ON CONFLICT (page_id, lang) DO UPDATE SET active_path = EXCLUDED.active_path,
			artifact_id = EXCLUDED.artifact_id, artifact_hash = EXCLUDED.artifact_hash,
			receipt_id = EXCLUDED.receipt_id, update_time = now()`
	if err := db.Exec(statement, pageID, lang, path, artifactID, hash, receiptID).Error; err != nil {
		t.Fatalf("写账本行失败: %v", err)
	}
}

func capturedFrom(t *testing.T, svc any, projectID, pageID, lang string) string {
	t.Helper()
	capturer, ok := svc.(interface {
		CaptureFromReceiptIDForTest(context.Context, string, string, string) string
	})
	if !ok {
		t.Fatal("页面服务未提供捕获身份导出口")
	}
	return capturer.CaptureFromReceiptIDForTest(context.Background(), projectID, pageID, lang)
}

func TestCapturedFromReceiptIDSeparatesZeroFromUnknown(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	page := createPage(t, svc, projectID, "/capture-identity", publishContextDoc)

	if got := capturedFrom(t, svc, projectID, page.ID, "zh-CN"); got != "0" {
		t.Fatalf("从未发布时应返回 \"0\"（确认无前次成功），实际 %q", got)
	}

	markPublicationLedger(t, db, page.ID, "zh-CN", "/capture-identity", "", "hash-hist", nil)
	if got := capturedFrom(t, svc, projectID, page.ID, "zh-CN"); got != "0" {
		t.Fatalf("历史 NULL 身份应返回 \"0\"，实际 %q", got)
	}

	want := int64(717171)
	markPublicationLedger(t, db, page.ID, "zh-CN", "/capture-identity", "", "hash-hist", &want)
	if got := capturedFrom(t, svc, projectID, page.ID, "zh-CN"); got != "717171" {
		t.Fatalf("已有成功身份应原样返回十进制 id，实际 %q", got)
	}

	if got := capturedFrom(t, svc, "", page.ID, "zh-CN"); got != "" {
		t.Fatalf("作用域缺失时应返回 \"\"（未知），实际 %q", got)
	}
}

func TestReceiptIdentityVerdictBranches(t *testing.T) {
	cases := []struct {
		name   string
		from   string
		own    int64
		cur    int64
		expect string
	}{

		{"账本是我自己", "0", 55, 55, "accepted"},

		{"账本等于出发身份", "42", 55, 42, "accepted"},
		{"出发身份为0且账本仍为0", "0", 55, 0, "accepted"},

		{"账本被新身份兑现", "42", 55, 77, "superseded"},
		{"出发身份为0但账本已有身份", "0", 55, 77, "superseded"},

		{"未知且账本为空", "", 55, 0, "accepted"},
		{"未知且账本是我自己", "", 55, 55, "accepted"},
		{"未知且账本是别人", "", 55, 77, "superseded"},

		{"非法from按未知", "receipt-abc", 55, 0, "accepted"},
		{"非法from且账本是别人", "receipt-abc", 55, 77, "superseded"},
		{"负数from按未知", "-9", 55, 0, "accepted"},

		{"own缺失且账本有身份", "0", 0, 77, "superseded"},
		{"own缺失且账本为空", "0", 0, 0, "accepted"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pagepageservice.ReceiptIdentityVerdictForTest(c.from, c.own, c.cur); got != c.expect {
				t.Fatalf("from=%q own=%d cur=%d 判为 %q，期望 %q", c.from, c.own, c.cur, got, c.expect)
			}
		})
	}
}

func TestPublishCapturesPreviousSuccessIdentity(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/from-identity", publishContextDoc)
	buildAndPublish(t, svc, page.ID)

	first := ledgerReceiptIDByPath(t, db, "/from-identity")
	if first <= 0 {
		t.Fatal("第一次发布后账本应记着成功身份")
	}

	draftReq := saveDraftReqOf(t, db, page.ID, publishContextDoc)
	if _, err := svc.SaveDraft(ctx, &draftReq); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	buildAndPublish(t, svc, page.ID)

	var rows []struct {
		ID      int64   `gorm:"column:id"`
		FromSeq *string `gorm:"column:from_receipt_id"`
		Receipt string  `gorm:"column:receipt_state"`
	}
	statement := `SELECT id, receipt_data->>'fromReceiptId' AS from_receipt_id, receipt_state
		FROM publication_receipts WHERE source_type = 'page' AND source_id = ? AND action = 'switch_active'
		ORDER BY id ASC`
	if err := db.Raw(statement, page.ID).Scan(&rows).Error; err != nil {
		t.Fatalf("读取回执登记事实失败: %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("应有至少两条 switch_active 回执（两次发布各一条），实际 %d", len(rows))
	}
	second := rows[len(rows)-1]
	if second.FromSeq == nil {
		t.Fatal("第二次登记的 FromReceiptID 缺失：崩溃恢复就无从判断「我接替了谁」")
	}
	if got, want := *second.FromSeq, formatInt64(first); got != want {
		t.Fatalf("第二条回执的 FromReceiptID 应为第一次的成功身份 %q，实际 %q", want, got)
	}
	if got := second.Receipt; got != "committed" {
		t.Fatalf("第二次发布应已结案（committed），实际 %q", got)
	}
}

func pageReceiptIDFromData(t *testing.T, db *gorm.DB, path string) int64 {
	t.Helper()
	return ledgerReceiptIDByPath(t, db, path)
}

func ledgerReceiptIDByPath(t *testing.T, db *gorm.DB, path string) int64 {
	t.Helper()
	var v *int64
	if err := db.Raw("SELECT receipt_id FROM page_publications WHERE active_path = ?", path).
		Scan(&v).Error; err != nil {
		t.Fatalf("读账本身份失败: %v", err)
	}
	if v == nil {
		return 0
	}
	return *v
}
