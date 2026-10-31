package unit

import (
	"context"
	"testing"

	json "go_wp/pkg/jsonx"

	pubcontract "go_wp/internal/module/publication/contract"

	"gorm.io/gorm"
)

func capturedInputVersionOfPage(t *testing.T, db *gorm.DB, pageID string) int64 {
	t.Helper()
	var v int64
	if err := db.Raw("SELECT input_version FROM pages WHERE id = ?", pageID).Scan(&v).Error; err != nil {
		t.Fatalf("读取 pages.input_version 失败: %v", err)
	}
	return v
}

func beginReceiptDataOf(t *testing.T, db *gorm.DB, path string) []byte {
	t.Helper()

	var text string
	statement := "SELECT receipt_data::text FROM publication_receipts WHERE path = ? AND action = 'switch_active' ORDER BY id DESC LIMIT 1"
	if err := db.Raw(statement, path).Scan(&text).Error; err != nil {
		t.Fatalf("读取回执失败: %v", err)
	}
	if text == "" {
		t.Fatalf("路径 %s 上没有 switch_active 回执", path)
	}
	return []byte(text)
}

func receiptCapturedVersion(t *testing.T, data []byte) int64 {
	t.Helper()
	var payload struct {
		CapturedInputVersion int64 `json:"capturedInputVersion"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("解析 receipt_data 失败: %v (%s)", err, string(data))
	}
	return payload.CapturedInputVersion
}

func capturedVersionVerdict(captured, current int64) bool {
	return captured > 0 && current != captured
}

func TestPublishMainlineCarriesCapturedInputVersion(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	page := createPage(t, svc, projectID, "/receipt-captured", emptyRevDoc)

	if err := db.Exec("UPDATE pages SET input_version = input_version + 3 WHERE id = ?", page.ID).Error; err != nil {
		t.Fatalf("推进输入代次失败: %v", err)
	}
	captured := capturedInputVersionOfPage(t, db, page.ID)
	if captured == 0 {
		t.Fatalf("用例前提不成立：输入代次仍为 0，无法区分「如实搬运」与「缺省 0」")
	}
	buildAndPublish(t, svc, page.ID)

	data := beginReceiptDataOf(t, db, "/receipt-captured")
	if got := receiptCapturedVersion(t, data); got != captured {
		t.Fatalf("回执应带上捕获段的输入代次 %d，实际 %d（receipt_data=%s）", captured, got, string(data))
	}
}

func TestCapturedVersionVerdictNeverTreatsUnknownAsValid(t *testing.T) {
	cases := []struct {
		name     string
		captured int64
		current  int64
		expired  bool
		why      string
	}{
		{"未知不当有效", 0, 5, false, "旧回执没有版本事实，不能据此判过期（退回链接身份 + 归属）"},
		{"未知也不当相同", 0, 0, false, "两边都是未知，同样不下过期结论"},
		{"相等则最新", 5, 5, false, "激活编译的就是当前一代输入"},
		{"落后则过期", 4, 5, true, "输入在捕获后被推进：线上跑的是过期字节"},
		{"超前也过期", 6, 5, true, "回执里的版本比当前更新只可能是状态错乱，不能补完成"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := capturedVersionVerdict(c.captured, c.current); got != c.expired {
				t.Fatalf("%s：期望 expired=%v，实际 %v", c.why, c.expired, got)
			}
		})
	}
}

func TestPublicationReceiptProjectsCapturedVersion(t *testing.T) {
	_, svc, routes, projectID := newPageServiceWithRoutes(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/receipt-roundtrip", emptyRevDoc)
	const captured = int64(11)
	if _, err := routes.BeginPublishReceipt(ctx, &pubcontract.BeginPublishReceiptReq{
		ProjectID: projectID, Path: "/receipt-roundtrip", PageID: page.ID,
		ToArtifactID:         "66666666-6666-6666-6666-666666666666",
		Lang:                 "zh-CN",
		CapturedInputVersion: captured,
	}); err != nil {
		t.Fatalf("登记回执失败: %v", err)
	}
	list, lerr := routes.ListPendingReceipts(ctx)
	if lerr != nil {
		t.Fatalf("列出未结案回执失败: %v", lerr)
	}
	for _, item := range list {
		if item.Path != "/receipt-roundtrip" {
			continue
		}
		if item.CapturedInputVersion != captured {
			t.Fatalf("读取投影应原样返回 %d，实际 %d", captured, item.CapturedInputVersion)
		}
		return
	}
	t.Fatalf("未找到路径 /receipt-roundtrip 的 pending 回执")
}
