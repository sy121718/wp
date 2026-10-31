package unit

import (
	"context"
	"os"
	"testing"

	json "go_wp/pkg/jsonx"

	pagedto "go_wp/internal/module/page/dto"
	pageservice "go_wp/internal/module/page/service"

	"gorm.io/gorm"
)

func ledgerReceiptID(t *testing.T, db *gorm.DB, pageID, lang string) int64 {
	t.Helper()
	var v *int64
	if err := db.Raw("SELECT receipt_id FROM page_publications WHERE page_id = ? AND lang = ?", pageID, lang).
		Scan(&v).Error; err != nil {
		t.Fatalf("读取 page_publications.receipt_id 失败: %v", err)
	}
	if v == nil {
		return 0
	}
	return *v
}

func latestSwitchActiveReceiptID(t *testing.T, db *gorm.DB, path string) int64 {
	t.Helper()
	var id int64
	statement := "SELECT id FROM publication_receipts WHERE path = ? AND action = 'switch_active' ORDER BY id DESC LIMIT 1"
	if err := db.Raw(statement, path).Scan(&id).Error; err != nil {
		t.Fatalf("读取回执 id 失败: %v", err)
	}
	if id == 0 {
		t.Fatalf("路径 %s 上没有 switch_active 回执", path)
	}
	return id
}

func TestPublishRecordsReceiptIdentityInLedger(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	page := createPage(t, svc, projectID, "/identity", emptyRevDoc)
	buildAndPublish(t, svc, page.ID)

	got := ledgerReceiptID(t, db, page.ID, "zh-CN")
	if got <= 0 {
		t.Fatal("成功激活后账本必须记着本次回执 id（NULL 只允许出现在历史行）")
	}
	if want := latestSwitchActiveReceiptID(t, db, "/identity"); got != want {
		t.Fatalf("账本身份 %d 应等于本次回执 %d", got, want)
	}
}

func TestRecoverSupersededByDifferentReceipt(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/identity-superseded", DraftDocument: json.RawMessage(emptyRevDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	hash := "hash-identity-1"
	writeArtifactDir(t, root, hash)
	artifactID := insertArtifactRow(t, db, page.ID, hash)
	linkActivePath(t, root, "/identity-superseded", hash)
	insertPendingReceipt(t, db, page.ID, "/identity-superseded", "zh-CN", artifactID)

	if err := db.Exec("INSERT INTO page_publications (page_id, lang, active_path, artifact_id, artifact_hash, published_at, update_time, receipt_id) VALUES (?, 'zh-CN', '/identity-superseded', ?, ?, now(), now(), 424242)",
		page.ID, artifactID, hash).Error; err != nil {
		t.Fatalf("预置账本身份失败: %v", err)
	}

	recovered, rolledBack, rerr := recoverPending(t, svc, ctx)
	if rerr != nil {
		t.Fatalf("恢复失败: %v", rerr)
	}
	if recovered != 0 {
		t.Fatalf("账本已由另一条回执兑现时不得补完成，实际补完成 %d 条", recovered)
	}
	if rolledBack != 1 {
		t.Fatalf("应结案 1 条（superseded / rolled_back），实际 %d", rolledBack)
	}
	if got := ledgerReceiptID(t, db, page.ID, "zh-CN"); got != 424242 {
		t.Fatalf("不可以覆盖别人的成功身份，实际 %d", got)
	}
}

func TestRecoverIgnoresNullLedgerIdentity(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/identity-null", DraftDocument: json.RawMessage(emptyRevDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	hash := "hash-identity-2"
	writeArtifactDir(t, root, hash)
	artifactID := insertArtifactRow(t, db, page.ID, hash)
	linkActivePath(t, root, "/identity-null", hash)
	insertPendingReceipt(t, db, page.ID, "/identity-null", "zh-CN", artifactID)

	if err := db.Exec("INSERT INTO page_publications (page_id, lang, active_path, artifact_id, artifact_hash, published_at, update_time, receipt_id) VALUES (?, 'zh-CN', '/identity-null', NULL, 'hash-old', now(), now(), NULL)",
		page.ID).Error; err != nil {
		t.Fatalf("预置历史账本行失败: %v", err)
	}

	recovered, _, rerr := recoverPending(t, svc, ctx)
	if rerr != nil {
		t.Fatalf("恢复失败: %v", rerr)
	}
	if recovered != 1 {
		t.Fatalf("历史 NULL 身份不应否决补完成，实际补完成 %d 条", recovered)
	}
	if got := ledgerReceiptID(t, db, page.ID, "zh-CN"); got <= 0 {
		t.Fatal("补完成后账本应记上本条回执的身份")
	}
	if got, want := activeArtifactID(t, db, page.ID), artifactID; got != want {
		t.Fatalf("激活产物应补齐为本次产物 %s，实际 %s", want, got)
	}
}

func TestParseReceiptIDRejectsNonNumeric(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		ok   bool
	}{
		{"十进制 id", "12345", true},
		{"空串表示没有回执设施", "", true},
		{"非数值", "receipt-abc", false},
		{"零不是回执", "0", false},
		{"负数不是回执", "-3", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, err := parseReceiptIDForTest(c.raw)
			if c.ok && err != nil {
				t.Fatalf("应接受 %q，实际报错 %v", c.raw, err)
			}
			if !c.ok && err == nil {
				t.Fatalf("应拒绝 %q，实际返回 %d", c.raw, id)
			}
		})
	}
}

func parseReceiptIDForTest(raw string) (int64, error) {
	return pageservice.ParseReceiptID(raw)
}
