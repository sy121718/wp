package unit

// page_receipt_captured_version_test.go —— 回执「捕获输入代次」的消费侧（真实 PG）。
//
// 发布链在**捕获段**读到的 pages.input_version 必须原样进回执，收敛例程在实体锁内
// 拿它和当前代次比对，从而区分：
//
//	版本相等          → 这次激活编译的就是最新一代输入 → 补完成（写账本、结案）；
//	版本不等          → 线上跑的是过期字节 → 不入账，入队重建或保留 pending；
//	未知（0，旧回执）  → 退回「链接身份 + 数据库归属」两判据，不当成有效。
//
// 下面钉住三件事：
//	① 发布主链把捕获代次如实写进 receipt_data（不写当前版本冒充）；
//	② publication 的读取投影把它如实交回给 page（两跳之间不许补值、不许归零）；
//	③ 收端的分支判据：0 不否决，非 0 且不等才否决 —— 防止「拿 0 当有效版本」或
//	   「拿 0 当不相等」两种反向错误。
//
// 本文件在 publication 契约补出 BeginPublishReceiptReq.CapturedInputVersion 与
// PendingReceiptResp.CapturedInputVersion 之后才有可执行的前提；在此之前 ① 会因为
// beginPublishReceipt 没有交字段而失败 —— 那个失败就是「缺参在哪一跳」的证据。

import (
	"context"
	"testing"

	json "go_wp/pkg/jsonx"

	pubcontract "go_wp/internal/module/publication/contract"

	"gorm.io/gorm"
)

// capturedInputVersionOfPage 读页面当前输入代次（收敛要与它比对）。
func capturedInputVersionOfPage(t *testing.T, db *gorm.DB, pageID string) int64 {
	t.Helper()
	var v int64
	if err := db.Raw("SELECT input_version FROM pages WHERE id = ?", pageID).Scan(&v).Error; err != nil {
		t.Fatalf("读取 pages.input_version 失败: %v", err)
	}
	return v
}

// beginReceiptDataOf 读该路径上最新一条 switch_active 回执的 receipt_data 原文。
func beginReceiptDataOf(t *testing.T, db *gorm.DB, path string) []byte {
	t.Helper()
	var data []byte
	statement := "SELECT receipt_data FROM publication_receipts WHERE path = ? AND action = 'switch_active' ORDER BY id DESC LIMIT 1"
	if err := db.Raw(statement, path).Scan(&data).Error; err != nil {
		t.Fatalf("读取回执失败: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("路径 %s 上没有 switch_active 回执", path)
	}
	return data
}

// receiptCapturedVersion 从 receipt_data 里取捕获代次；缺键返回 0（未知）。
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

// capturedVersionVerdict 收端判据的**唯一**定义（与 recoverSwitchActiveReceipt 的 G2 同口径）：
// 返回 true 表示「这条回执的激活已过期」。
//
// 0 是未知哨兵而不是合法版本：实体版本从 1 起推进（迁移 699 的 DEFAULT 0 只用于历史行），
// 0 永远不会等于一个真实代次 —— 拿 0 去比会得到「永远过期」，把旧回执全判死；
// 把它当「与当前相同」又会让过期产物被补完成。两种都不对，只能「未知 = 不否决」。
func capturedVersionVerdict(captured, current int64) bool {
	return captured > 0 && current != captured
}

// TestPublishMainlineCarriesCapturedInputVersion ① 主链把捕获代次如实写进回执。
//
// 认领点：internal/module/page/service/page_publish.go 的 beginPublishReceipt 只交
// ProjectID/Path/PageID/From/To/Lang/Action/OldPath/Redirect —— publishReceiptInput.CapturedInputVersion
// 一路带到了这里，却没有交进 BeginPublishReceiptReq。这一跳缺了，收敛永远读到 0。
func TestPublishMainlineCarriesCapturedInputVersion(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	page := createPage(t, svc, projectID, "/receipt-captured", emptyRevDoc)
	// 输入代次推一代：捕获段读到的必须是**非 0** 的真实代次，否则本用例证不出搬运。
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

// TestCapturedVersionVerdictNeverTreatsUnknownAsValid ③ 收端分支判据的边界。
//
// 纯逻辑用例（不碰 DB）：钉住「0 不当有效、也不当不等」，以及「非 0 且不等 → 过期」。
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

// TestPublicationReceiptProjectsCapturedVersion ② 读取投影把它交回给消费者。
//
// 由真实 publication service 登记、真实 service 读回：证明 dto → receipt_data → dto
// 全程不丢、不补值。缺口一旦回流（任一侧删掉字段），这里先失败。
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
