package unit

import (
	"context"
	json "go_wp/pkg/jsonx"
	"testing"

	pubdto "go_wp/internal/module/publication/dto"
	pubmodel "go_wp/internal/module/publication/model"
)

func TestPublicationReceiptFlow(t *testing.T) {
	svc := newUnitService(t)
	_, err := svc.Activate(context.Background(), &pubdto.ActivateReq{
		ProjectID: projectID, Path: "/flow", PageID: pageID, ArtifactID: artifactUUID,
	})
	if err != nil {
		t.Fatalf("激活失败: %v", err)
	}
	if n := countReceipts(t, svc, "receipt_state = ?", pubmodel.ReceiptPending); n != 0 {
		t.Fatalf("成功后不应有 pending: %d", n)
	}
	if n := countReceipts(t, svc, "receipt_state = ?", pubmodel.ReceiptCommitted); n != 1 {
		t.Fatalf("应有 1 条 committed: %d", n)
	}
}

func TestPublicationReceiptActionFallback(t *testing.T) {
	svc := newUnitService(t)
	_, err := svc.Activate(context.Background(), &pubdto.ActivateReq{
		ProjectID: projectID, Path: "/fallback", PageID: pageID, ArtifactID: artifactUUID,
	})
	if err != nil {
		t.Fatalf("激活失败: %v", err)
	}
	var receipts []pubmodel.ReceiptEntity
	if err := svc.Model().ReceiptDB(context.Background()).Find(&receipts).Error; err != nil {
		t.Fatalf("读取回执失败: %v", err)
	}
	if len(receipts) != 1 || receipts[0].Action != "activate" {
		t.Fatalf("action 应回退为 activate: %+v", receipts)
	}
	if receipts[0].SourceType != "page" || receipts[0].SourceID != pageID || receipts[0].Path != "/fallback" {
		t.Fatalf("回执字段不完整: %+v", receipts[0])
	}
}

func TestPublicationReceiptDataValid(t *testing.T) {
	svc := newUnitService(t)
	_, err := svc.Activate(context.Background(), &pubdto.ActivateReq{
		ProjectID: projectID, Path: "/json", PageID: pageID, ArtifactID: artifactUUID,
	})
	if err != nil {
		t.Fatalf("激活失败: %v", err)
	}
	var receipts []pubmodel.ReceiptEntity
	if err := svc.Model().ReceiptDB(context.Background()).Find(&receipts).Error; err != nil {
		t.Fatalf("读取回执失败: %v", err)
	}
	var payload struct {
		To string `json:"to"`
	}
	if err := json.Unmarshal(receipts[0].ReceiptData, &payload); err != nil {
		t.Fatalf("ReceiptData 应为合法 JSON: %v (raw=%s)", err, string(receipts[0].ReceiptData))
	}
	if payload.To != artifactUUID {
		t.Fatalf("ReceiptData.to 应为目标 artifact: %+v", payload)
	}
}

func TestPublicationReceiptRedirectDataValid(t *testing.T) {
	svc := newUnitService(t)
	_, err := svc.Redirect(context.Background(), &pubdto.RedirectReq{
		ProjectID: projectID, OldPath: "/rd", PageID: pageID, ArtifactID: artifactUUID,
	})
	if err != nil {
		t.Fatalf("重定向失败: %v", err)
	}
	var receipts []pubmodel.ReceiptEntity
	if err := svc.Model().ReceiptDB(context.Background()).Find(&receipts).Error; err != nil {
		t.Fatalf("读取回执失败: %v", err)
	}
	var payload struct {
		Redirect string `json:"redirect"`
	}
	if err := json.Unmarshal(receipts[0].ReceiptData, &payload); err != nil {
		t.Fatalf("redirect ReceiptData 应为合法 JSON: %v (raw=%s)", err, string(receipts[0].ReceiptData))
	}
	if payload.Redirect != artifactUUID {
		t.Fatalf("ReceiptData.redirect 应为目标 artifact: %+v", payload)
	}
}
