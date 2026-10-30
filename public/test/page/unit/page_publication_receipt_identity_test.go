package unit

// page_publication_receipt_identity_test.go —— 激活账本的**成功身份**（迁移 701 + 收敛判据）。
//
// 一次发布在切访问面之前先预登记一条回执；回执 id 就是这次尝试的身份。激活成功时
// 它必须随激活落进 page_publications.receipt_id：
//
//	· 崩溃恢复要知道「账本上这个产物是不是**我这条**回执兑现的」；
//	· 同版本、同产物的两次成功激活在 (version, artifact) 上完全一样，
//	  只有身份能区分 —— 这就是本文件要钉住的东西。
//
// 三条不变量：
//	1. 主链发布后账本记着**本次**回执 id，且该 id 确实是回执表里的那一行；
//	2. 收敛遇到「账本已被**另一条**回执兑现」时判 superseded，不覆盖、不写 DB；
//	3. 历史行（receipt_id 为 NULL）不参与身份判定，退回既有「链接 + 归属」判据。

import (
	"context"
	"os"
	"testing"

	json "go_wp/pkg/jsonx"

	pagedto "go_wp/internal/module/page/dto"
	pageservice "go_wp/internal/module/page/service"

	"gorm.io/gorm"
)

// ledgerReceiptID 读账本上某语言的成功身份；NULL → 0（历史行）。
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

// latestSwitchActiveReceiptID 取该路径上最新一条 switch_active 回执的 id。
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

// TestPublishRecordsReceiptIdentityInLedger 主链把成功身份与激活同事务落库。
//
// 回归背景：账本在加这一列之前只能记「哪个产物在线」，记不了「哪次尝试成功的」——
// 同版本同产物的两次成功在账本上无法区分，恢复会把别人的成功当成自己的。
func TestPublishRecordsReceiptIdentityInLedger(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
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

// TestRecoverSupersededByDifferentReceipt 「同产物、同版本、但账本是另一条回执兑现的」
// 必须判为已被替代：不写 DB、只结案。
//
// 为什么这条不能用「版本相等」代替：两次成功激活的产物与版本可以完全一致（同一份暂存
// 产物被两次发布判成功），此时唯一能回答「我兑现过吗」的就是账本上的回执 id。
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
	// 账本已由**另一条**回执兑现（例如同产物被更早的一次发布成功激活）。
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

// TestRecoverIgnoresNullLedgerIdentity 历史行（NULL）不参与身份判定：
// 链接是我的、账本仍指向 from 时照常补完成，并把本条回执的身份写上。
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
	// 历史行：账本存在但没有成功身份（本迁移之前落库的那一类）。
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

// TestParseReceiptIDRejectsNonNumeric 回执 id 无法解析成数值身份时**阻止激活**，
// 不得静默退化成 0（0 在账本里读作「历史行」，会把真实成功伪装成没有身份）。
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

// parseReceiptIDForTest 走生产同一实现（页面服务的 ParseReceiptID）。
func parseReceiptIDForTest(raw string) (int64, error) {
	return pageservice.ParseReceiptID(raw)
}
