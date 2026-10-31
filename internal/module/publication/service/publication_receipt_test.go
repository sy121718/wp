package pubservice_test

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

func receiptDataOf(t *testing.T, db *gorm.DB, id int64) map[string]any {
	t.Helper()

	var raw string
	if err := db.Raw("SELECT receipt_data::text FROM publication_receipts WHERE id = ?", id).Scan(&raw).Error; err != nil {
		t.Fatalf("读取 receipt_data 失败: %v", err)
	}
	var out map[string]any
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("解析 receipt_data 失败: %v (原文 %s)", err, string(raw))
		}
	}
	return out
}

func insertPendingReceipt(t *testing.T, db *gorm.DB, sourceType, sourceID, action, path, data string) int64 {
	t.Helper()
	var id int64
	if err := db.Raw(`INSERT INTO publication_receipts
		(source_type, source_id, action, path, receipt_state, receipt_data)
		VALUES (?, ?, ?, ?, 'pending', ?::text::jsonb) RETURNING id`,
		sourceType, sourceID, action, path, data).Scan(&id).Error; err != nil {
		t.Fatalf("插入历史回执失败: %v", err)
	}
	return id
}

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

	data := receiptDataOf(t, db, receiptID)
	got, ok := data["capturedInputVersion"]
	if !ok {
		t.Fatalf("receipt_data 缺 capturedInputVersion 键: %v", data)
	}
	if n, isNum := got.(float64); !isNum || int64(n) != 7 {
		t.Fatalf("receipt_data.capturedInputVersion 应为 7，实际 %v(%T)", got, got)
	}

	list, lerr := svc.ListPendingReceipts(ctx)
	if lerr != nil {
		t.Fatalf("列出未结案回执失败: %v", lerr)
	}
	item := findPending(t, list, "/captured-version")
	if item.CapturedInputVersion != 7 {
		t.Fatalf("读取投影应原样返回 7，实际 %d", item.CapturedInputVersion)
	}
}

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

func TestLegacyReceiptWithoutVersionReadsAsUnknown(t *testing.T) {
	cases := []struct {
		name string
		data string
		path string
	}{
		{"无该键的旧行", `{"lang":"zh-CN","projectId":"` + receiptTestProjectID + `","oldPath":"/old"}`, "/legacy-no-key"},
		{"空对象", `{}`, "/legacy-empty"},
		{"只带重定向与旧路径", `{"redirect":true,"oldPath":"/old","projectId":"` + receiptTestProjectID + `"}`, "/legacy-redirect"},

		{"字段类型读不成的旧行", `{"capturedInputVersion":"不是数字"}`, "/legacy-broken"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db, svc := newReceiptService(t)
			ctx := context.Background()
			insertPendingReceipt(t, db, pubdto.ReceiptSourcePresentation, receiptTestPresentationID,
				pubdto.ReceiptActionSwitchActive, c.path, c.data)
			list, err := svc.ListPendingReceipts(ctx)
			if err != nil {
				t.Fatalf("列出未结案回执失败: %v", err)
			}
			item := findPending(t, list, c.path)
			if item.CapturedInputVersion != 0 {
				t.Fatalf("旧回执缺该事实时必须为 0（未知），实际 %d", item.CapturedInputVersion)
			}

			if item.OldPath != "/old" && c.name == "无该键的旧行" {
				t.Fatalf("旧路径透出异常: %q", item.OldPath)
			}
			if c.name == "无该键的旧行" && item.Lang != "zh-CN" {
				t.Fatalf("语言透出异常: %q", item.Lang)
			}
		})
	}
}

func TestVersionedReceiptCarriesVersionThroughRealList(t *testing.T) {
	db, svc := newReceiptService(t)
	ctx := context.Background()
	insertPendingReceipt(t, db, pubdto.ReceiptSourcePresentation, receiptTestPresentationID,
		pubdto.ReceiptActionSwitchActive, "/carried-version",
		`{"lang":"zh-CN","capturedInputVersion":42}`)
	list, err := svc.ListPendingReceipts(ctx)
	if err != nil {
		t.Fatalf("列出未结案回执失败: %v", err)
	}
	if item := findPending(t, list, "/carried-version"); item.CapturedInputVersion != 42 {
		t.Fatalf("应原样透出 42，实际 %d", item.CapturedInputVersion)
	}
}

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

func parseReceiptID(t *testing.T, id string) error {
	t.Helper()
	if _, err := strconv.ParseInt(strings.TrimSpace(id), 10, 64); err != nil {
		return err
	}
	return nil
}

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
		VALUES (?, ?, ?, ?, 'pending', ?::text::jsonb) RETURNING id`,
		sourceType, sourceID, pubdto.ReceiptActionDeactivate, path, string(encoded)).Scan(&id).Error; err != nil {
		t.Fatalf("插入下线回执失败: %v", err)
	}
	return id
}

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
