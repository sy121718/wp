package orderhttp

// order_create_page_form_test.go — 建单页的表单解析判据（纯逻辑，不碰数据库、不渲染）。
//
// 重点是「提交的明细行数超过解析上限」这一支：它**不能静默截断** —— 被截掉的行会凭空
// 消失，而页面上一切正常、订单也照样建成了，事后只能靠对账发现（项目规则明令禁止
// 静默丢弃一行）。所以判据是两条：① 超限被标出来；② 它落到用户看得见的字段错误上。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// postFormContext 造一个带 urlencoded 表单的 gin context（与真实请求同一条解析路径）。
func postFormContext(t *testing.T, form url.Values) *gin.Context {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/admin/orders/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return c
}

// TestOrderCreateItemRowsParseInOrder 正常路径：variantId / quantity 按提交顺序逐下标配对。
func TestOrderCreateItemRowsParseInOrder(t *testing.T) {
	form := url.Values{}
	form.Add("variantId", "v1")
	form.Add("quantity", "2")
	form.Add("variantId", "v2")
	form.Add("quantity", "3")

	rows, tooMany := orderCreateItemRowsFromPost(postFormContext(t, form))
	if tooMany {
		t.Fatal("两行明细不该被判超限")
	}
	if len(rows) != 2 {
		t.Fatalf("应重建出 2 行，实际 %d 行：%+v", len(rows), rows)
	}
	if rows[0].VariantID != "v1" || rows[0].Quantity != "2" ||
		rows[1].VariantID != "v2" || rows[1].Quantity != "3" {
		t.Fatalf("明细行必须按提交顺序配对（错位就是「数量跑到别的商品上」），实际 %+v", rows)
	}
}

// TestOrderCreateItemRowsRejectsOverlongList 超限：明确拒绝并给出字段错误，**不截断**。
func TestOrderCreateItemRowsRejectsOverlongList(t *testing.T) {
	form := url.Values{}
	for i := 0; i <= orderCreateParseLimit; i++ {
		form.Add("variantId", "v")
		form.Add("quantity", "1")
	}
	c := postFormContext(t, form)

	rows, tooMany := orderCreateItemRowsFromPost(c)
	if !tooMany {
		t.Fatalf("超过解析上限（%d）必须被标出来，而不是截断后照常建单", orderCreateParseLimit)
	}
	if len(rows) != 1 {
		t.Fatalf("拒绝时只留一行空行供回填，实际 %d 行", len(rows))
	}

	// 超限必须变成一道**用户看得见**的字段错误（模板渲染成标红 + 行内文案），
	// 而不是静默通过、也不是让 service 去猜。
	_, fieldErrs := orderCreateReqFromForm(c, orderCreateForm{ItemsTooMany: true})
	if fieldErrs["items"] == "" {
		t.Error("超限应当落到 items 字段错误上（否则用户只看到「标红字段需要修正」却不知道自己超了）")
	}
}

// TestOrderCreateItemRowsEmptyListKeepsOneRow 一行都没提交：给一行空行，页面仍可填。
func TestOrderCreateItemRowsEmptyListKeepsOneRow(t *testing.T) {
	rows, tooMany := orderCreateItemRowsFromPost(postFormContext(t, url.Values{}))
	if tooMany {
		t.Fatal("空提交不是超限")
	}
	if len(rows) != 1 {
		t.Fatalf("空提交应给一行空行，实际 %d 行", len(rows))
	}
}
