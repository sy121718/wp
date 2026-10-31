package unit

// page_receipt_identity_verdict_test.go —— 「账本成功身份」判定的**三态**语义（FromReceiptID）。
//
// 反例（主审实证）：正常第二次发布崩在「FS 已切、DB 未提交」时，账本上留着的仍是
// **前一次**的成功身份。旧判据 `curSeq != ownSeq → superseded` 会把这条合法的前向
// 补齐误杀成「被别人取代」—— 线上新产物已经在服务，回执却被结案成失败。
//
// 因此判定必须分两种前态：账本 == 我 / 账本 == 我出发时的身份 / 其它。本文件钉住
// 这条判定，并证明 FromReceiptID 的三态（"0" / "" / 十进制）不会被混同。
//
// 判定本身走生产同一实现（pageservice.ReceiptIdentityVerdictForTest）；这里不重写公式 ——
// 重写出来的公式再像，也不能证明生产跑的是它。

import (
	"context"
	"strconv"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	pagepageservice "go_wp/internal/module/page/service"

	"gorm.io/gorm"
)

// saveDraftReqOf 构造保守草稿保存请求（路径与文档都不变，只推进草稿代次）。
// saveDraftReqOf 只更换文档、不改路径：DraftPath 传该页**当前**草稿路径。
//
// DraftPath 是必填的保存目标路径（SaveDraft → validateDraft → normalizePagePath，
// page_document.go:253 / :373-376）：留空会走 pipeline.NormalizeURL，被
// pathkit.NormalizeRoutePath 的「路径必须以 / 开头」判成 ErrInvalidPath。
// 传当前值同时保证「路径未变」—— 保存不会触发路由迁移，与其他断言的前提一致。
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
	// ExpectedVersion 必须带**当前**草稿代次：SaveDraft 用
	// req.ExpectedVersion != page.DraftVersion 判乐观锁（page_document.go:249-268），
	// 零值在已保存过草稿的页上必然撞 ErrDraftVersionConflict。
	return pagedto.SaveDraftReq{ID: pageID, ExpectedVersion: row.DraftVersion, DraftPath: row.DraftPath, DraftDocument: []byte(doc)}
}

// formatInt64 十进制（回执 id 与 FromReceiptID 同口径）。
func formatInt64(v int64) string { return strconv.FormatInt(v, 10) }

// publishContextDoc 一个不含任何动态依赖的最小文档（发布链不因依赖解析而分叉）。
const publishContextDoc = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

// markPublicationLedger 直写/改写信账本的 (active_path, artifact_id, receipt_id)，
// 用来模拟「前一次成功」留下的现场。
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

// capturedFrom 走页面服务的捕获端实现（不是用例里重写的版本）。
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

// TestCapturedFromReceiptIDSeparatesZeroFromUnknown "0"（确认没有前次成功）与
// ""（未知）必须分开：把读失败说成「没有前次成功」，恢复时就会把真实的前任身份
// 当成不存在，于是它接管别人的成功状态而毫无察觉。
func TestCapturedFromReceiptIDSeparatesZeroFromUnknown(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	page := createPage(t, svc, projectID, "/capture-identity", publishContextDoc)

	// 本语言从未发布 → 确认「没有前次成功」，返回 "0" 而不是 ""。
	if got := capturedFrom(t, svc, projectID, page.ID, "zh-CN"); got != "0" {
		t.Fatalf("从未发布时应返回 \"0\"（确认无前次成功），实际 %q", got)
	}

	// 历史行（receipt_id 为 NULL）同样是「没有成功身份」，仍是 "0"。
	markPublicationLedger(t, db, page.ID, "zh-CN", "/capture-identity", "", "hash-hist", nil)
	if got := capturedFrom(t, svc, projectID, page.ID, "zh-CN"); got != "0" {
		t.Fatalf("历史 NULL 身份应返回 \"0\"，实际 %q", got)
	}

	// 已有成功身份 → 十进制原样。
	want := int64(717171)
	markPublicationLedger(t, db, page.ID, "zh-CN", "/capture-identity", "", "hash-hist", &want)
	if got := capturedFrom(t, svc, projectID, page.ID, "zh-CN"); got != "717171" {
		t.Fatalf("已有成功身份应原样返回十进制 id，实际 %q", got)
	}

	// 空工程 → 受控降级为「未知」，不 panic、不冒充 "0"。
	if got := capturedFrom(t, svc, "", page.ID, "zh-CN"); got != "" {
		t.Fatalf("作用域缺失时应返回 \"\"（未知），实际 %q", got)
	}
}

// TestReceiptIdentityVerdictBranches 判定分支：合法恢复不得被误杀，真实取代必须被拒。
func TestReceiptIdentityVerdictBranches(t *testing.T) {
	cases := []struct {
		name   string
		from   string
		own    int64
		cur    int64
		expect string
	}{
		// —— 账本就是我自己：已兑现过，幂等补齐。
		{"账本是我自己", "0", 55, 55, "accepted"},
		// —— 账本仍是我出发时的那个人：正常第二次发布的崩溃窗口，必须放行。
		{"账本等于出发身份", "42", 55, 42, "accepted"},
		{"出发身份为0且账本仍为0", "0", 55, 0, "accepted"},
		// —— 账本出现了一个新的成功身份：真实取代。
		{"账本被新身份兑现", "42", 55, 77, "superseded"},
		{"出发身份为0但账本已有身份", "0", 55, 77, "superseded"},
		// —— 未知（历史回执）：只在出现非零异身份时判取代。
		{"未知且账本为空", "", 55, 0, "accepted"},
		{"未知且账本是我自己", "", 55, 55, "accepted"},
		{"未知且账本是别人", "", 55, 77, "superseded"},
		// —— 非法 from（历史脏数据）按未知处理，不让恢复整体失败。
		{"非法from按未知", "receipt-abc", 55, 0, "accepted"},
		{"非法from且账本是别人", "receipt-abc", 55, 77, "superseded"},
		{"负数from按未知", "-9", 55, 0, "accepted"},
		// —— 自己没有数值身份（回执 id 不可用）：只能退回「账本有别人」。
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

// TestPublishCapturesPreviousSuccessIdentity 主链第二次发布的回执里，
// FromReceiptID 必须是**第一次**的成功身份 —— 不是空、也不是本次自己。
//
// 判据直读登记事实（publication_receipts.receipt_data->>'fromReceiptId'），
// 不读内存态也不重算：这条事实是崩溃恢复唯一能拿到的线索，落不进去就等于没有。
func TestPublishCapturesPreviousSuccessIdentity(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/from-identity", publishContextDoc)
	buildAndPublish(t, svc, page.ID)

	first := ledgerReceiptIDByPath(t, db, "/from-identity")
	if first <= 0 {
		t.Fatal("第一次发布后账本应记着成功身份")
	}

	// 再发布一次：第二次 attempt 登记时捕获到的「出发身份」= 第一次的回执 id。
	draftReq := saveDraftReqOf(t, db, page.ID, publishContextDoc)
	if _, err := svc.SaveDraft(ctx, &draftReq); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	buildAndPublish(t, svc, page.ID)

	// 取该页最新的两条 switch_active 回执的登记事实（按 id 升序：第一条是首次发布）。
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

// pageReceiptIDFromData 读该路径最新一条 switch_active 回执的 id（账本身份的来源）。
func pageReceiptIDFromData(t *testing.T, db *gorm.DB, path string) int64 {
	t.Helper()
	return ledgerReceiptIDByPath(t, db, path)
}

// ledgerReceiptIDByPath 账本上该路径当前的成功身份（0 = NULL / 无行）。
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
