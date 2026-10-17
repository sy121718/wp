package blockhttp

// block_save_content_return_url_test.go — 工作台保存块内容（POST /admin/blocks/save-content）
// 的 returnUrl 白名单在**真实 HTTP 出口**上的行为（task-80）。
//
// 为什么不能只测 pkg/safepath：白名单函数自己有单测，但「块保存这条链路有没有真的接住它」
// 是另一件事。少了这一环，把 block_page.go 里的 `safepath.LocalReturnPath(raw)` 换回 `raw`
// 不会有任何测试变红 —— 开放重定向就这么活了。所以这里断言调用方真正看得到的东西：
// 303 的 Location 值（或压根没有 Location），以及非法值一个字符都不许出现在响应里。
//
// 判据（pkg/safepath.LocalReturnPath）：以 / 开头、不含 `//` 与 `\`、不含控制字符，
// 且不超过 512 字节（MaxReturnPathBytes）才接受；被拒的落 /admin/blocks 兜底；
// 没传 returnUrl（或传空串）不跳，走 200 JSON 成功回执 —— 保存后留在工作台。

import (
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	"go_wp/pkg/safepath"

	"github.com/gin-gonic/gin"
)

// blockListPath 被拒 returnUrl 的兜底落点（与 handler 里的常量字面量同源）。
const blockListPath = "/admin/blocks"

// newBlockSaveContentHTTPRouter 装配与线上同路径、同 handler 的真实 HTTP 出口。
//
// 中间件链（Session / CSRF / Casbin）由装配层的 /admin 组统一挂好，不属于本用例的被测面：
// 这里测的是「handler 拿到 returnUrl 之后跳到哪去」，所以只在真实路径上挂真实 handler。
func newBlockSaveContentHTTPRouter(t *testing.T, svc blockcontract.BlockService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/admin/blocks/save-content", NewBlockPageHandle(svc, nil).SaveBlockContent)
	return router
}

// postSaveContent 用真实 HTTP 请求打保存端点，返回原始响应。
// returnURL 为 nil 表示**整个字段缺席**（与传空串是两种输入，handler 对它们行为相同）。
func postSaveContent(t *testing.T, router *gin.Engine, returnURL *string) *httptest.ResponseRecorder {
	t.Helper()
	payload := map[string]any{"id": "block-1", "document": json.RawMessage(`{}`)}
	if returnURL != nil {
		// 用 json.Marshal 而不是手拼：被拒的样例里有控制字符与 HTML 形态的值，
		// 手拼字符串很容易连**请求体本身**都不是合法 JSON（那就变成测绑定失败，而不是测白名单）。
		payload["returnUrl"] = *returnURL
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("构造请求体失败：%v", err)
	}
	body := string(raw)
	req := httptest.NewRequest(http.MethodPost, "/admin/blocks/save-content", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// TestBlockSaveContentReturnURLWhitelist 白名单在保存链路上的真实落点。
func TestBlockSaveContentReturnURLWhitelist(t *testing.T) {
	// 512 字节是**闭区间**：正好等于上限仍接受，多一字节拒绝。
	atLimit := "/" + strings.Repeat("a", safepath.MaxReturnPathBytes-1)
	overLimit := "/" + strings.Repeat("a", safepath.MaxReturnPathBytes)

	cases := []struct {
		name         string
		returnURL    *string
		wantStatus   int
		wantLocation string
	}{
		{"站内相对路径保留（含查询串）", strPtr("/admin/blocks?page=2&kind=header"), http.StatusSeeOther, "/admin/blocks?page=2&kind=header"},
		{"前后空白先 trim 再判定", strPtr("  /admin/blocks?page=3  "), http.StatusSeeOther, "/admin/blocks?page=3"},
		{"恰好 512 字节仍保留", strPtr(atLimit), http.StatusSeeOther, atLimit},
		{"协议相对 URL 拒绝", strPtr("//evil.example.com/x"), http.StatusSeeOther, blockListPath},
		{"绝对 URL 拒绝", strPtr("https://evil.example.com/x"), http.StatusSeeOther, blockListPath},
		{"反斜杠（浏览器按 / 解析）拒绝", strPtr(`/\evil.example.com`), http.StatusSeeOther, blockListPath},
		{"含换行拒绝", strPtr("/admin/blocks\nevil"), http.StatusSeeOther, blockListPath},
		{"含制表符拒绝", strPtr("/admin/blocks\tevil"), http.StatusSeeOther, blockListPath},
		{"含垂直制表符拒绝", strPtr("/admin/blocks\vevil"), http.StatusSeeOther, blockListPath},
		{"超过 512 字节拒绝", strPtr(overLimit), http.StatusSeeOther, blockListPath},
		{"缺前导斜杠拒绝", strPtr("admin/blocks"), http.StatusSeeOther, blockListPath},
		{"空串等价于没传（不跳，200 JSON）", strPtr(""), http.StatusOK, ""},
		{"字段缺席（不跳，200 JSON）", nil, http.StatusOK, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &stubBlockService{detail: &blockdto.BlockResp{ID: "block-1", Name: "页眉", Document: json.RawMessage(`{}`)}}
			router := newBlockSaveContentHTTPRouter(t, svc)
			rec := postSaveContent(t, router, tc.returnURL)

			if rec.Code != tc.wantStatus {
				t.Fatalf("状态码应为 %d，实际 %d（响应体：%s）", tc.wantStatus, rec.Code, rec.Body.String())
			}
			loc := rec.Header().Get("Location")
			if loc != tc.wantLocation {
				t.Errorf("Location 应为 %q，实际 %q", tc.wantLocation, loc)
			}
			// 不跳的那两档必须是 JSON 成功回执（前端 saveDraft 对响应做 r.json()）：
			// 若这里变成 303/纯文本，保存成功会静默失败。
			if tc.wantLocation == "" {
				if loc != "" {
					t.Errorf("不该有 Location，实际 %q", loc)
				}
				var parsed struct {
					Code int `json:"code"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
					t.Fatalf("不跳时响应体应是合法 JSON，实际 %s（%v）", rec.Body.String(), err)
				}
				// 本仓约定：成功响应的 code 就是 HTTP 状态码（response.SuccessWithMessage）。
				if parsed.Code != http.StatusOK {
					t.Errorf("成功回执 code 应为 %d，实际 %d", http.StatusOK, parsed.Code)
				}
			}
			// 非法值一个片段都不许穿透到响应（既不在 Location，也不在正文里）：
			// Location 是浏览器直接消费的，正文是排障时会被贴进工单的那份。
			if tc.wantLocation == blockListPath && strings.Contains(rec.Body.String()+loc, "evil") {
				t.Errorf("被拒的 returnUrl 穿透到了响应：Location=%q 正文=%s", loc, rec.Body.String())
			}
		})
	}
}

// TestBlockSaveContentReturnURLRejectionIsNotReflected 被拒值不许被「回显式」处理。
//
// 单独一条：上面那张表的判据是「Location 等于兜底」，本用例补**方向相反的错法** ——
// 有些实现会把非法值原样塞进 Location（403/303 都常见），也有实现会把它写进正文当提示。
// 这里用一个带 HTML/头注入形态的值，确认响应里连片段都没有。
func TestBlockSaveContentReturnURLRejectionIsNotReflected(t *testing.T) {
	const hostile = "//evil.example.com/\"><script>alert(1)</script>"
	router := newBlockSaveContentHTTPRouter(t, &stubBlockService{
		detail: &blockdto.BlockResp{ID: "block-1", Name: "页眉", Document: json.RawMessage(`{}`)},
	})
	rec := postSaveContent(t, router, strPtr(hostile))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("状态码应为 303，实际 %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != blockListPath {
		t.Errorf("Location 应落兜底 %q，实际 %q", blockListPath, loc)
	}
	body := rec.Body.String()
	for _, leak := range []string{"evil.example.com", "script", "alert"} {
		if strings.Contains(body, leak) {
			t.Errorf("响应体不应含被拒 returnUrl 的片段 %q：%s", leak, body)
		}
	}
}

func strPtr(s string) *string { return &s }
