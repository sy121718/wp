// product_bulk_http_contract_test.go — 批量端点的对外契约（入口只做编排的证据）。
//
// 三条判据都落在**响应**上，而不是「控制器里有没有某行代码」：
//
//	① 超过上限 → 400，文案带「上限 / 本次条数」两个数字（归一在 service，且不静默截断）；
//	② 没勾选 → 400（不是「删除 0 个」的成功回执）；
//	③ 夹着不可处理的 id → 409，data.items 里带 id / name / reasonKey / message。
//
// 第 ③ 条的 message 由**出口**翻译（service 只交 key + 兜底）：断言它非空且不是裸 key ——
// 这正是「控制器不再拼文案」之后最容易退化的地方。
package feature

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
)

func TestProductBulkHTTPContract(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		return
	}
	// fixture 的 engine 只挂了 bulk-pricing（列表抽屉那条）；批量删除在这里补挂，
	// 断言的是同一个 handler 与同一个出口。
	engine.POST("/api/product/bulk-delete", producthttp.NewProductHandle(f.svc).BulkDelete)

	t.Run("超过上限：400 且文案带两个数字", func(t *testing.T) {
		ids := make([]string, 201)
		for i := range ids {
			ids[i] = uuid.NewString()
		}
		rec := postJSON(t, engine, "/api/product/bulk-delete", map[string]any{
			"projectId": f.projectID,
			"ids":       ids,
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("超限应 400，实际 %d：%s", rec.Code, rec.Body.String())
		}
		env := decodeJSONEnvelope(t, rec)
		// 两个数字缺一不可：只说「参数不合法」时操作者不知道该拆成几批。
		if !strings.Contains(env.Message, "200") || !strings.Contains(env.Message, "201") {
			t.Fatalf("文案应带上限与本次条数，实际 %q", env.Message)
		}
	})

	t.Run("没勾选：400 而不是删除 0 个的成功", func(t *testing.T) {
		rec := postJSON(t, engine, "/api/product/bulk-delete", map[string]any{
			"projectId": f.projectID,
			"ids":       []string{},
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("空勾选应 400，实际 %d：%s", rec.Code, rec.Body.String())
		}
		if msg := decodeJSONEnvelope(t, rec).Message; strings.TrimSpace(msg) == "" {
			t.Fatal("空勾选应给一句可读文案")
		}
	})

	t.Run("夹着不存在的 id：409 + data.items 逐条定位", func(t *testing.T) {
		price := 12.0
		p, err := f.svc.Create(t.Context(), &productdto.CreateReq{
			ProjectID: f.projectID, Name: "批量契约商品", Slug: "bulk-http-contract", DefaultPrice: &price,
		})
		if err != nil {
			t.Fatalf("建商品失败: %v", err)
		}
		rec := postJSON(t, engine, "/api/product/bulk-delete", map[string]any{
			"projectId": f.projectID,
			"ids":       []string{p.ID, "11111111-1111-4111-8111-111111111111"},
		})
		if rec.Code != http.StatusConflict {
			t.Fatalf("整批被拒应 409，实际 %d：%s", rec.Code, rec.Body.String())
		}
		env := decodeJSONEnvelope(t, rec)
		items, _ := env.Data["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("data.items 应只有 1 条（不存在的那个），实际 %v", items)
		}
		row, _ := items[0].(map[string]any)
		if id, _ := row["id"].(string); id != "11111111-1111-4111-8111-111111111111" {
			t.Fatalf("条目应指向不存在的 id，实际 %v", row["id"])
		}
		if key, _ := row["reasonKey"].(string); key != "ErrNotFound" {
			t.Fatalf("reasonKey 应为 ErrNotFound，实际 %v", row["reasonKey"])
		}
		msg, _ := row["message"].(string)
		if strings.TrimSpace(msg) == "" || strings.Contains(msg, "ErrNotFound") {
			t.Fatalf("message 应是当前语言的文案而不是裸 key，实际 %q", msg)
		}
		// 整批被拒 = 可删的那个也没删。
		if _, gerr := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: p.ID}); gerr != nil {
			t.Fatalf("整批被拒时商品不该被删：%v", gerr)
		}
	})
}
