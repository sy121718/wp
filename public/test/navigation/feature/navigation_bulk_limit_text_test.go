package feature

// navigation_bulk_limit_text_test.go — 导航菜单页批量删除的 id 超限路径（JSON 通道）。
//
// 背景：批量删除已从页面 POST 迁到真实 JSON 端点（POST /api/navigation/delete，批档 = {"ids":[…]},
// 见 internal/module/navigation/inbound/http/navigation_delete_write.go）。超限不再渲染整页提示，
// 而是 400 + 受控 key 的**带参协议**（`shell.err.bulkIdsTooMany|上限|本次条数`），由 pkg/response
// 的 translate 按 | 后的参数依次填 %s —— 与 block / 词条页 / 联系人页同一口径
// （页面出口那条 shell.BulkIDsFacingText 已随页面 POST 退役，词条与两个数字仍是同一个真源：
// pkg/bulkids）。
//
// 本文件守住那条收口：
//   - 响应里是**受控 key**，不是 bulkids.IDsError 的原文（原文只进日志）；
//   - 上限与本次条数两个数字都在 —— 只断言「超限了」会让「当前 M 项」丢了也绿，
//     而 M（去重后的条数）正是这批发往服务端的那个数；
//   - 不含任何内部细节指纹。
//
// 用 nil service 是刻意的：超限在**读到 service 之前**就被 pkg/bulkids 拒绝，
// 这条路径本来就不该碰数据库（碰了就 panic，而不是悄悄跑通）。

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	bulkids "go_wp/pkg/bulkids"
)

// navBulkOverLimitIDs 造一份超过 bulkids.MaxIDs 的 id 列表（多给一条即触发上限）。
func navBulkOverLimitIDs() []string {
	ids := make([]string, 0, bulkids.MaxIDs+1)
	for i := 0; i < bulkids.MaxIDs+1; i++ {
		ids = append(ids, "00000000-0000-0000-0000-"+strconv.Itoa(1000000000000+i))
	}
	return ids
}

// navBulkOverLimitJSONBody 把上面那份 id 列表编成批档请求体。
func navBulkOverLimitJSONBody() string {
	ids := navBulkOverLimitIDs()
	quoted := make([]string, 0, len(ids))
	for _, id := range ids {
		quoted = append(quoted, `"`+id+`"`)
	}
	return `{"ids":[` + strings.Join(quoted, ",") + `]}`
}

// navJSONErrMessage 取 JSON 错误响应里的 message（pkg/response.Response）。
func navJSONErrMessage(t *testing.T, body string) string {
	t.Helper()
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("响应不是 JSON：%v（%s）", err, body)
	}
	return resp.Message
}

// navBulkOverLimitRawError 反证用：把同一份 id 列表交给 pkg/bulkids，取回原始错误。
func navBulkOverLimitRawError() error {
	_, err := bulkids.Normalize(navBulkOverLimitIDs())
	return err
}

// TestNavigationsBulkDeleteOverLimitUsesControlledText 超限时给出受控提示（两个数字都在），
// 且响应里没有内部错误原文的任何片段。
func TestNavigationsBulkDeleteOverLimitUsesControlledText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// nil service：超限必须在这之前被拒绝，否则下面这条请求会 panic（而不是悄悄跑通）。
	engine.POST("/api/navigation/delete", navigationhttp.NewHandle(nil).Delete)

	// 反证：原始错误是带 sentinel 的类型，值域只有两个整数（上限 / 本次条数），
	// 且它确实知道本次条数 —— 少了这一步，「响应没泄漏」可能只是因为原文里本来就没有细节。
	rawErr := navBulkOverLimitRawError()
	if rawErr == nil || !errors.Is(rawErr, bulkids.ErrTooMany) {
		t.Fatalf("反证失败：超限应返回命中 sentinel 的错误，实际 %v", rawErr)
	}
	over := strconv.Itoa(bulkids.MaxIDs + 1)
	if !strings.Contains(rawErr.Error(), over) {
		t.Fatalf("反证失败：原文应带本次条数 %s，实际 %v", over, rawErr)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/navigation/delete",
		strings.NewReader(navBulkOverLimitJSONBody()))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	// 整批拒绝（400）而不是部分执行：超限是形状问题，删除没有撤销。
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超限应整批拒绝（400，不部分执行），实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	message := navJSONErrMessage(t, body)
	if !strings.HasPrefix(message, bulkids.MsgTooMany+"|") {
		t.Fatalf("超限应回受控 key（%s）的带参协议而不是原文，实际 %q", bulkids.MsgTooMany, message)
	}
	if !strings.Contains(message, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(message, over) {
		t.Fatalf("受控提示应带上限 %d 与本次条数 %s，实际 %q", bulkids.MaxIDs, over, message)
	}
	for _, tok := range []string{
		"SQLSTATE", "42P02", "constraint", "uuid", "invalid input syntax", "bulkids:",
	} {
		if strings.Contains(body, tok) {
			t.Errorf("响应泄漏内部细节 %q：%s", tok, body)
		}
	}
}
