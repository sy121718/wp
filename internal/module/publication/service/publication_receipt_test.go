package pubservice_test

// publication_receipt_test.go —— 回执附加事实「捕获输入代次」的持久与读取链。
//
// 为什么是**外部测试包**（package pubservice_test）：本包要 import public/test/support，
// 而 support 经 test_bootstrap.go → internal/routers → publication/inbound/http →
// publication/service 回到本包 —— 同包测试文件（package pubservice）会形成 test import
// cycle。外部测试包只依赖本包的**导出** API，不产生这个环，也不需要为测试新增任何生产出口。
//
// 这条链是 page 收敛（G2 版本判据）的唯一数据来源：
//
//	BeginPublishReceiptReq.CapturedInputVersion
//	  → publishReceiptData.CapturedInputVersion（receipt_data jsonb，omitempty）
//	  → pendingReceiptRespOf（缺键 / 旧回执 = 0 未知）
//	  → PendingReceiptResp.CapturedInputVersion（消费者据此否决过期激活）
//
// 这里只测这四跳的**事实搬运**，不测「过期要不要否决」（那是消费者 owner 的判据）：
// 搬运里任何一跳做「补齐」（例如拿登记时刻的当前版本填 0）都会让消费者拿到一个
// 它亲眼没见过的版本，从而把过期产物判成最新。所以下面每条断言都必须证明
// 「值原样进、原样出，没有值就是 0」。

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	json "go_wp/pkg/jsonx"

	pubdto "go_wp/internal/module/publication/dto"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"go_wp/public/test/support"
	"gorm.io/gorm"
)

const receiptTestProjectID = "11111111-1111-1111-1111-111111111111"
const receiptTestPageID = "22222222-2222-2222-2222-222222222222"
const receiptTestPresentationID = "66666666-6666-6666-6666-666666666666"

func newReceiptService(t *testing.T) (*gorm.DB, *pubservice.Service) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	return db, pubservice.NewService(pubmodel.NewPublicationModel(db))
}

// receiptDataOf 读回执的 receipt_data 原文（直接看落库事实，不经过任何投影）。
func receiptDataOf(t *testing.T, db *gorm.DB, id int64) map[string]any {
	t.Helper()
	var raw []byte
	if err := db.Raw("SELECT receipt_data FROM publication_receipts WHERE id = ?", id).Scan(&raw).Error; err != nil {
		t.Fatalf("读取 receipt_data 失败: %v", err)
	}
	var out map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("解析 receipt_data 失败: %v (原文 %s)", err, string(raw))
		}
	}
	return out
}

// TestBeginPublishReceiptPersistsCapturedInputVersion 非 0 的捕获代次必须原样落进 receipt_data，
// 并且能从 ListPendingReceipts 原样读回（两条读取路径共用 pendingReceiptRespOf）。
func TestBeginPublishReceiptPersistsCapturedInputVersion(t *testing.T) {
	db, svc := newReceiptService(t)
	ctx := context.Background()

	id, err := svc.BeginPublishReceipt(ctx, &pubdto.BeginPublishReceiptReq{
		ProjectID: receiptTestProjectID, Path: "/captured-version", PageID: receiptTestPageID,
		ToArtifactID:         "33333333-3333-3333-3333-333333333333",
		Lang:                 "zh-CN",
		Action:               pubdto.ReceiptActionSwitchActive,
		CapturedInputVersion: 7,
	})
	if err != nil {
		t.Fatalf("登记回执失败: %v", err)
	}
	if perr := parseReceiptID(t, id); perr != nil {
		t.Fatalf("回执 id 非法: %v", perr)
	}
	receiptID, _ := strconv.ParseInt(strings.TrimSpace(id), 10, 64)

	// 一跳：落库原文。omitempty 只在 0 时省键，非 0 必须出现真实数字。
	data := receiptDataOf(t, db, receiptID)
	got, ok := data["capturedInputVersion"]
	if !ok {
		t.Fatalf("receipt_data 缺 capturedInputVersion 键: %v", data)
	}
	if n, isNum := got.(float64); !isNum || int64(n) != 7 {
		t.Fatalf("receipt_data.capturedInputVersion 应为 7，实际 %v(%T)", got, got)
	}

	// 二跳：读取投影原样读回。
	list, lerr := svc.ListPendingReceipts(ctx)
	if lerr != nil {
		t.Fatalf("列出未结案回执失败: %v", lerr)
	}
	item := findPending(t, list, "/captured-version")
	if item.CapturedInputVersion != 7 {
		t.Fatalf("读取投影应原样返回 7，实际 %d", item.CapturedInputVersion)
	}
}

// TestBeginPublishReceiptUnknownVersionStaysZero 调用方不提供版本事实（0）时：
// 落库不出现该键，读取投影为 0。0 的含义是「未知」，消费者不得当「有效」。
func TestBeginPublishReceiptUnknownVersionStaysZero(t *testing.T) {
	db, svc := newReceiptService(t)
	ctx := context.Background()

	id, err := svc.BeginPublishReceipt(ctx, &pubdto.BeginPublishReceiptReq{
		ProjectID: receiptTestProjectID, Path: "/unknown-version", PageID: receiptTestPageID,
		ToArtifactID: "44444444-4444-4444-4444-444444444444",
		Lang:         "zh-CN",
	})
	if err != nil {
		t.Fatalf("登记回执失败: %v", err)
	}
	if perr := parseReceiptID(t, id); perr != nil {
		t.Fatalf("回执 id 非法: %v", perr)
	}
	receiptID, _ := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
	if _, ok := receiptDataOf(t, db, receiptID)["capturedInputVersion"]; ok {
		t.Fatalf("0 表示未知，omitempty 下不应写出该键")
	}
	list, lerr := svc.ListPendingReceipts(ctx)
	if lerr != nil {
		t.Fatalf("列出未结案回执失败: %v", lerr)
	}
	if item := findPending(t, list, "/unknown-version"); item.CapturedInputVersion != 0 {
		t.Fatalf("未知版本应为 0，实际 %d", item.CapturedInputVersion)
	}
}

// TestLegacyReceiptWithoutVersionReadsAsUnknown 旧回执（receipt_data 里根本没有这个键，
// 或是别处手工写的 json）走真实表 + 真实列表读回来必须是 0 —— 兼容旧行只允许「当作未知」。
//
// 这里刻意手工 INSERT 而不是走 BeginPublishReceipt：要造的是"历史行"，即本次改动之前
// 落库的形态（receipt_data 里没有 capturedInputVersion）。用真实列 + 真实读路径，
// 才能证明省略键时不会在投影里被补成任何值。
func TestLegacyReceiptWithoutVersionReadsAsUnknown(t *testing.T) {
	cases := []struct {
		name string
		data string
		path string
	}{
		{"无该键的旧行", `{"lang":"zh-CN","projectId":"` + receiptTestProjectID + `","oldPath":"/old"}`, "/legacy-no-key"},
		{"空对象", `{}`, "/legacy-empty"},
		{"只带重定向与旧路径", `{"redirect":true,"oldPath":"/old","projectId":"` + receiptTestProjectID + `"}`, "/legacy-redirect"},
		{"解析不了的坏 json", `{"capturedInputVersion":`, "/legacy-broken"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db, svc := newReceiptService(t)
			ctx := context.Background()
			if err := db.Exec(`INSERT INTO publication_receipts
				(source_type, source_id, action, path, receipt_state, receipt_data)
				VALUES (?, ?, ?, ?, 'pending', ?::jsonb)`,
				pubdto.ReceiptSourcePresentation, receiptTestPresentationID,
				pubdto.ReceiptActionSwitchActive, c.path, c.data).Error; err != nil {
				t.Fatalf("插入历史回执失败: %v", err)
			}
			list, err := svc.ListPendingReceipts(ctx)
			if err != nil {
				t.Fatalf("列出未结案回执失败: %v", err)
			}
			item := findPending(t, list, c.path)
			if item.CapturedInputVersion != 0 {
				t.Fatalf("旧回执缺该事实时必须为 0（未知），实际 %d", item.CapturedInputVersion)
			}
			// 旧事实不能被这一次改动弄丢：路径来自表列，语言与旧路径来自 jsonb。
			if item.OldPath != "/old" && c.name == "无该键的旧行" {
				t.Fatalf("旧路径透出异常: %q", item.OldPath)
			}
			if c.name == "无该键的旧行" && item.Lang != "zh-CN" {
				t.Fatalf("语言透出异常: %q", item.Lang)
			}
		})
	}
}

// TestVersionedReceiptCarriesVersionThroughRealList 非 0 值经真实落库 + 真实列表原样透出
// （不在投影里做任何解释）：消费者拿到的必须就是登记时那一跳的事实。
func TestVersionedReceiptCarriesVersionThroughRealList(t *testing.T) {
	db, svc := newReceiptService(t)
	ctx := context.Background()
	if err := db.Exec(`INSERT INTO publication_receipts
		(source_type, source_id, action, path, receipt_state, receipt_data)
		VALUES (?, ?, ?, ?, 'pending', ?::jsonb)`,
		pubdto.ReceiptSourcePresentation, receiptTestPresentationID,
		pubdto.ReceiptActionSwitchActive, "/carried-version",
		`{"lang":"zh-CN","capturedInputVersion":42}`).Error; err != nil {
		t.Fatalf("插入回执失败: %v", err)
	}
	list, err := svc.ListPendingReceipts(ctx)
	if err != nil {
		t.Fatalf("列出未结案回执失败: %v", err)
	}
	if item := findPending(t, list, "/carried-version"); item.CapturedInputVersion != 42 {
		t.Fatalf("应原样透出 42，实际 %d", item.CapturedInputVersion)
	}
}

// TestBeginPublishReceiptNegativeVersionNormalized 负值没有语义：归一到 0（未知）而不是落库。
// 让消费者拿到一个「比当前版本小」的假事实，会被 G2 判成不相等而误报过期。
func TestBeginPublishReceiptNegativeVersionNormalized(t *testing.T) {
	db, svc := newReceiptService(t)
	ctx := context.Background()
	id, err := svc.BeginPublishReceipt(ctx, &pubdto.BeginPublishReceiptReq{
		ProjectID: receiptTestProjectID, Path: "/negative-version", PageID: receiptTestPageID,
		ToArtifactID: "55555555-5555-5555-5555-555555555555",
		Lang:         "zh-CN", CapturedInputVersion: -3,
	})
	if err != nil {
		t.Fatalf("登记回执失败: %v", err)
	}
	if perr := parseReceiptID(t, id); perr != nil {
		t.Fatalf("回执 id 非法: %v", perr)
	}
	receiptID, _ := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
	if _, ok := receiptDataOf(t, db, receiptID)["capturedInputVersion"]; ok {
		t.Fatalf("负值应归一到 0（未知）而不落库")
	}
}

// parseReceiptID 校验 service 返回的回执 id 是可解析的十进制主键（不是 attempt 身份）。
func parseReceiptID(t *testing.T, id string) error {
	t.Helper()
	if _, err := strconv.ParseInt(strings.TrimSpace(id), 10, 64); err != nil {
		return err
	}
	return nil
}

// findPending 在真实 ListPendingReceipts 结果里按路径定位一条回执。
//
// 只查不造：测试要证明的是"表里那一行经真实读路径投影成什么"，
// 任何自带构造行的查表函数都会把落库/读回那两跳绕过去。
func findPending(t *testing.T, list []pubdto.PendingReceiptResp, path string) pubdto.PendingReceiptResp {
	t.Helper()
	for _, item := range list {
		if item.Path == path {
			return item
		}
	}
	t.Fatalf("真实列表里没有路径 %s 的回执（共 %d 条）", path, len(list))
	return pubdto.PendingReceiptResp{}
}

// ── 下线回执端口（Mark/Load）的四类负测 ──────────────────────────────────────────
//
// 这四条覆盖"静默放行"的四种形态：非事务句柄、零行（跨 sourceID / 跨工程 / 已结案）、
// 缺工程事实、非法 owner。每一种放行的后果都一样：把一次没成立的下线当成该撤链接。

// insertDeactivationReceipt 手工插一条 deactivate 回执（pending），返回其 id。
//
// 手工 INSERT 而不是走 BeginPublishReceipt：这里要造的是**任意形态**的持久事实
// （缺 projectId、跨 sourceID），登记入口的归一化会把它们改掉，就测不到读路径。
func insertDeactivationReceipt(t *testing.T, db *gorm.DB, sourceType, sourceID, projectID, path string,
	withProject bool) int64 {
	t.Helper()
	data := map[string]any{"oldPaths": []string{path}, "pathArtifacts": map[string]any{}}
	if withProject {
		data["projectId"] = projectID
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("编码 receipt_data 失败: %v", err)
	}
	var id int64
	if err := db.Raw(`INSERT INTO publication_receipts
		(source_type, source_id, action, path, receipt_state, receipt_data)
		VALUES (?, ?, ?, ?, 'pending', ?::jsonb) RETURNING id`,
		sourceType, sourceID, pubdto.ReceiptActionDeactivate, path, string(encoded)).Scan(&id).Error; err != nil {
		t.Fatalf("插入下线回执失败: %v", err)
	}
	return id
}

// TestMarkDeactivationRejectsNonTransactionHandle 自动提交的 db 句柄必须被拒绝。
//
// "同事务"是这套协议的硬约束：传一个普通 *gorm.DB 也能"成功"的话，进程在
// 「标记已写、业务未提交」之间崩掉，恢复就会把一次没发生的删除当成该撤链接。
func TestMarkDeactivationRejectsNonTransactionHandle(t *testing.T) {
	db, svc := newReceiptService(t)
	ctx := context.Background()
	id := insertDeactivationReceipt(t, db, pubdto.ReceiptSourcePage, receiptTestPageID,
		receiptTestProjectID, "/non-tx", true)
	owner := pubdto.ReceiptOwner{SourceType: pubdto.ReceiptSourcePage, SourceID: receiptTestPageID}
	if err := svc.MarkDeactivationCommittedTx(ctx, db, owner, receiptTestProjectID,
		strconv.FormatInt(id, 10), nil); err == nil {
		t.Fatalf("非事务句柄必须被拒绝")
	}
	if _, ok := receiptDataOf(t, db, id)["businessCommitted"]; ok {
		t.Fatalf("被拒绝的标记不得落库任何事实")
	}
}

// TestLoadDeactivationRejectsNonTransactionHandle 读路径同样只接受真事务句柄。
func TestLoadDeactivationRejectsNonTransactionHandle(t *testing.T) {
	db, svc := newReceiptService(t)
	ctx := context.Background()
	id := insertDeactivationReceipt(t, db, pubdto.ReceiptSourcePage, receiptTestPageID,
		receiptTestProjectID, "/non-tx-load", true)
	owner := pubdto.ReceiptOwner{SourceType: pubdto.ReceiptSourcePage, SourceID: receiptTestPageID}
	if _, err := svc.LoadDeactivationReceiptTx(ctx, db, owner, receiptTestProjectID,
		strconv.FormatInt(id, 10)); err == nil {
		t.Fatalf("非事务句柄必须被拒绝（读取同样不能被自动提交句柄满足）")
	}
}

// TestMarkDeactivationZeroRowsIsControlled 跨 sourceID / 跨工程都必须是受控 0 行，
// 而不是"更新了 0 行但返回 nil"。
func TestMarkDeactivationZeroRowsIsControlled(t *testing.T) {
	db, svc := newReceiptService(t)
	ctx := context.Background()
	id := insertDeactivationReceipt(t, db, pubdto.ReceiptSourcePage, receiptTestPageID,
		receiptTestProjectID, "/zero-rows", true)

	crossOwner := pubdto.ReceiptOwner{SourceType: pubdto.ReceiptSourcePage, SourceID: receiptTestPresentationID}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return svc.MarkDeactivationCommittedTx(ctx, tx, crossOwner, receiptTestProjectID,
			strconv.FormatInt(id, 10), nil)
	}); err == nil {
		t.Fatalf("sourceID 不匹配必须受控失败")
	}

	crossProject := pubdto.ReceiptOwner{SourceType: pubdto.ReceiptSourcePage, SourceID: receiptTestPageID}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return svc.MarkDeactivationCommittedTx(ctx, tx, crossProject, "99999999-9999-9999-9999-999999999999",
			strconv.FormatInt(id, 10), nil)
	}); err == nil {
		t.Fatalf("工程不匹配必须受控失败")
	}
	if _, ok := receiptDataOf(t, db, id)["businessCommitted"]; ok {
		t.Fatalf("失败的两笔标记都不得落库")
	}
}

// TestLoadDeactivationRequiresProjectFact 缺 projectId 的回执不得被"工程校验为真"放行。
//
// 这是主审点出的具体缺陷形态：事后比较里 (空串 != expected) 只要有一处分叉就变成放行，
// 所以谓词必须写进 SQL —— 缺事实等同于不匹配。
func TestLoadDeactivationRequiresProjectFact(t *testing.T) {
	db, svc := newReceiptService(t)
	ctx := context.Background()
	id := insertDeactivationReceipt(t, db, pubdto.ReceiptSourcePage, receiptTestPageID,
		receiptTestProjectID, "/no-project", false)
	owner := pubdto.ReceiptOwner{SourceType: pubdto.ReceiptSourcePage, SourceID: receiptTestPageID}

	var loaded *pubmodel.ReceiptEntity
	lerr := db.Transaction(func(tx *gorm.DB) error {
		row, err := svc.LoadDeactivationReceiptTx(ctx, tx, owner, receiptTestProjectID, strconv.FormatInt(id, 10))
		loaded = row
		return err
	})
	if lerr == nil {
		t.Fatalf("缺工程事实的回执必须读不到（实际读到 %+v）", loaded)
	}
	if !errors.Is(lerr, gorm.ErrRecordNotFound) {
		t.Fatalf("应是 not found，实际 %v", lerr)
	}
	// 反向：补上工程事实后必须能读到（证明拒绝的是"缺事实"，不是"永远拒绝"）。
	if err := db.Exec(`UPDATE publication_receipts SET receipt_data = receipt_data || ?::jsonb WHERE id = ?`,
		`{"projectId":"`+receiptTestProjectID+`"}`, id).Error; err != nil {
		t.Fatalf("补工程事实失败: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := svc.LoadDeactivationReceiptTx(ctx, tx, owner, receiptTestProjectID, strconv.FormatInt(id, 10))
		return err
	}); err != nil {
		t.Fatalf("补齐工程事实后应能读到: %v", err)
	}
}

// TestLoadDeactivationRejectsZeroOwner 空 owner 不得退化成"任意来源都匹配"。
func TestLoadDeactivationRejectsZeroOwner(t *testing.T) {
	db, svc := newReceiptService(t)
	ctx := context.Background()
	id := insertDeactivationReceipt(t, db, pubdto.ReceiptSourcePage, receiptTestPageID,
		receiptTestProjectID, "/zero-owner", true)
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := svc.LoadDeactivationReceiptTx(ctx, tx, pubdto.ReceiptOwner{}, receiptTestProjectID,
			strconv.FormatInt(id, 10))
		return err
	}); err == nil {
		t.Fatalf("空 owner 必须被拒绝")
	}
}
