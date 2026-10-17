package blockhttp

// block_delete_usage_detail_test.go — 单条删除（JSON）被拒时必须带上**引用明细**。
//
// 缺陷形态（task-21 P2）：Delete 的失败出口走的是通用 blockErrorMessage → err.Error()，
// 而删除被拒的 err 是 contract.BlockInUseError，它拼出来的是内部形态
// 「block.err.inUse: page_document /pricing (main)」—— 引用类别是**未翻译的枚举值**，
// 括号与分隔符也是另一套写法。结果同一次拒绝，批量接口逐条回的是本地化明细、
// 单条删除回的是内部形态，用户在两个入口看到两套说法。
//
// 这里钉三件事：
//  1. 409 的 message 里有本地化的类别名与定位（/pricing）；
//  2. **没有**内部枚举原文（page_document）—— 否则「翻译过」只是碰巧；
//  3. 无明细的普通业务错误完全不受影响（仍走原出口）。

import (
	"errors"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	blockcontract "go_wp/internal/module/block/contract"
	blockenums "go_wp/internal/module/block/enums"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/i18n"
)

// deleteResp 调一次单条删除端点，返回状态码与解析后的响应。
func deleteResp(t *testing.T, svc blockcontract.BlockService, body string) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/api/block/delete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	(&Handle{svc: svc}).Delete(c)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(w.Body.String()), &parsed); err != nil {
		t.Fatalf("响应不是合法 JSON：status=%d body=%q err=%v", w.Code, w.Body.String(), err)
	}
	return w.Code, parsed
}

func TestDeleteKeepsUsageDetailInJSON(t *testing.T) {
	// 注入生产 seed 的词条（否则断言只能看见 key，看不出「用户到底看到什么」）。
	i18n.InjectForTest(map[string]map[string]string{
		blockenums.ErrBlockInUse:             {"zh-CN": "该区块仍被页面引用，不能删除。"},
		blockenums.MsgBlockUsagePageDocument: {"zh-CN": "页面文档"},
		blockenums.UsageMore:                 {"zh-CN": " 等 {count} 处"},
	}, nil)

	svc := &bulkDeleteStubService{
		fail: map[string]error{
			"b1": blockcontract.NewBlockInUseError([]blockcontract.BlockUsage{
				{Kind: blockcontract.UsageKindPageDocument, EntityID: "page-42", Label: "/pricing", Detail: "main"},
			}),
		},
	}
	status, parsed := deleteResp(t, svc, `{"id":"b1"}`)
	if status != http.StatusConflict {
		t.Fatalf("被引用应回 409，实际 %d（body=%v）", status, parsed)
	}
	msg, _ := parsed["message"].(string)
	if !strings.Contains(msg, "/pricing") {
		t.Errorf("删除被拒的提示必须能定位到具体引用（/pricing），实际 %q", msg)
	}
	if !strings.Contains(msg, "页面文档") {
		t.Errorf("引用类别应是当前语言的文案（页面文档），实际 %q", msg)
	}
	if strings.Contains(msg, "page_document") {
		t.Errorf("提示里出现了内部枚举原文（page_document）—— 说明走的是 err.Error() 而不是本地化明细：%q", msg)
	}
	if strings.Contains(msg, "block.err.inUse") {
		t.Errorf("提示里出现了裸 key，前端会原样显示：%q", msg)
	}
}

// 无明细的普通业务错误：仍走通用出口（本用例守住「改了这一处没把别的路径带偏」）。
//
// 这里**刻意不注入词条**：i18n.InjectForTest 是进程级缓存（pkg/i18n/cache.go:99 只做 Update，
// 没有复位入口），注入 `ErrBlockNotFound` 会污染同包其它测试对「全局块不存在」的断言
// （block_save_content_err_test.go:151 的受控集合断言就会红）。没有词条时 translate 的回退是
// key 本身，所以断言用枚举常量 —— 本用例真正要守的是「**不拼接**引用明细后缀」。
func TestDeleteBusinessErrorWithoutUsageKeepsPlainMessage(t *testing.T) {
	svc := &bulkDeleteStubService{fail: map[string]error{"b1": blockcontract.ErrNotFound}}
	status, parsed := deleteResp(t, svc, `{"id":"b1"}`)
	if status != http.StatusNotFound {
		t.Fatalf("不存在应回 404，实际 %d（body=%v）", status, parsed)
	}
	msg, _ := parsed["message"].(string)
	if msg != blockenums.ErrBlockNotFound {
		t.Fatalf("无引用明细时不应拼接别的后缀（无词条时的回退就是 key 本身），实际 %q", msg)
	}
	if strings.Contains(msg, "：") || strings.Contains(msg, " /") {
		t.Fatalf("无明细的提示里出现了定位后缀：%q", msg)
	}
}

// 未知系统错误：不把驱动原文透给客户端（与批量路径同一条判据）。
func TestDeleteInternalErrorDoesNotLeakDriverText(t *testing.T) {
	svc := &bulkDeleteStubService{
		fail: map[string]error{"b1": errors.New(`pq: relation "blocks" does not exist`)},
	}
	status, parsed := deleteResp(t, svc, `{"id":"b1"}`)
	if status != http.StatusInternalServerError {
		t.Fatalf("未知错误应回 500，实际 %d", status)
	}
	if msg, _ := parsed["message"].(string); strings.Contains(msg, "pq:") || strings.Contains(msg, "relation") {
		t.Fatalf("500 响应泄露了驱动原文：%q", msg)
	}
}
