package pagehttp

// page_bulk_notice_test.go — 受控回执的防伪造判据（纯逻辑，不需要数据库）。
//
// 这是本机制存在的**唯一理由**：URL 里的文案不可伪造、计数有上限。
// 伪造能改的只有范围内的整数，塞不进整句话 —— 旧形态（整串匹配 + 数字归一化）恰恰相反，
// `?done=已删除 999999 个页面` 与真回执同形。

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func noticeCtx(rawQuery string) *gin.Context {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("GET", "/admin/pages?"+rawQuery, nil)
	return c
}

func TestBulkNoticeRejectsForgedKey(t *testing.T) {
	// ① 白名单外的 key：不渲染任何文案（fallback 空串）。
	got := pageBulkNoticeText(noticeCtx("doneKey=admin.pages.evil&doneN=3"), pageBulkNoticeDone)
	if got != "" {
		t.Fatalf("白名单外的 key 不该渲染出文案，实际 %q", got)
	}
	// ② 直接塞整句（旧形态的伪造手法）：同样拒绝。
	got = pageBulkNoticeText(noticeCtx("doneKey=%E5%B7%B2%E5%88%A0%E9%99%A4+999999+%E4%B8%AA"), pageBulkNoticeDone)
	if got != "" {
		t.Fatalf("伪造的整句不该被渲染，实际 %q", got)
	}
	t.Log("① 白名单外 key → 空（不渲染）；② 伪造整句 → 空（不渲染）")
}

func TestBulkNoticeEnforcesCountLimit(t *testing.T) {
	// ③ 白名单内的 key + 合法计数 → 渲染出句子（默认语言 zh-CN 的兜底）。
	got := pageBulkNoticeText(noticeCtx("doneKey=admin.pages.bulk.deleted&doneN=2"), pageBulkNoticeDone)
	if got == "" || !contains(got, "2") {
		t.Fatalf("白名单内 key + 合法计数应渲染出含计数的句子，实际 %q", got)
	}
	t.Logf("③ doneKey=…deleted&doneN=2 → %q", got)

	// ④ 计数超上限（MaxBulkIDs=200）→ 整条丢弃。
	got = pageBulkNoticeText(noticeCtx("doneKey=admin.pages.bulk.deleted&doneN=999999"), pageBulkNoticeDone)
	if got != "" {
		t.Fatalf("超上限的计数应丢弃整条回执，实际 %q", got)
	}
	t.Log("④ doneN=999999（超上限）→ 丢弃整条（不渲染「已删除 999999 个」）")

	// ⑤ 参数缺失（词条需要 {n}）→ 丢弃，不把缺失当 0。
	got = pageBulkNoticeText(noticeCtx("doneKey=admin.pages.bulk.deleted"), pageBulkNoticeDone)
	if got != "" {
		t.Fatalf("缺参数时应丢弃整条回执，实际 %q", got)
	}
	t.Log("⑤ 缺 doneN → 丢弃（不渲染「已删除 0 个」）")
}

func TestBulkNoticeWriteSideShape(t *testing.T) {
	q, ok := bulkNoticeQueryOf(true, noticeBulkPartial, map[string]int{"n": 2, "m": 1})
	if !ok {
		t.Fatal("合法计数应写入成功")
	}
	enc := q.Encode()
	if !contains(enc, "errwarnKey=admin.pages.bulk.partial") || !contains(enc, "errwarnN=2") || !contains(enc, "errwarnM=1") {
		t.Fatalf("写侧 query 形状不符：%s", enc)
	}
	t.Logf("写侧：%s", enc)
	// 未声明的参数名 → 拒绝（调用点写错的当场暴露）。
	if _, ok := bulkNoticeQueryOf(true, noticeBulkPartial, map[string]int{"x": 1}); ok {
		t.Fatal("未声明的参数名应被拒绝")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
