package runtimefragment

// comment_test.go — 评论片段的端点级测试（BIZ-5）：降级与表单契约。
//
// 这一层守的是「降级必须可见」：任何一条走错分支，访客看到的是**空白**（不报错、日志干净）——
// 未登录回 401 时 htmx 默认不替换目标节点，端口未接入 / 读失败时返回 error 会让片段变 500，
// 两者同样不 swap。所以断言既看文案，也看**状态码**。
//
// 需要真实数据库的那几条断言（未审核的不出现在列表、跨实体隔离、未注册类型不落库、
// 限流生效）在 public/test/comment/feature/comment_fragment_test.go ——
// 那几条的判据在 SQL 的 WHERE 里，用替身端口测等于什么都没测；而本包**不能** import
// `public/test/support`（support → internal/routers → 本包，会形成测试期循环依赖）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	commentcontract "go_wp/internal/module/comment/contract"
	commentdto "go_wp/internal/module/comment/dto"
	usercontract "go_wp/internal/module/user/contract"
)

// commentTestProjectID 测试工程 id（合法 uuid，便于走 rls 作用域的格式校验）。
const commentTestProjectID = "22222222-2222-4222-8222-222222222222"

// stubCommentPort 评论端口替身（本文件只验降级与成功形态，不验 SQL 过滤）。
type stubCommentPort struct {
	list  *commentdto.ListResp
	err   error
	sub   *commentdto.SubmitResp
	subEr error
}

func (s *stubCommentPort) ListApproved(context.Context, *commentdto.ListReq) (*commentdto.ListResp, error) {
	return s.list, s.err
}

func (s *stubCommentPort) Submit(context.Context, *commentdto.SubmitReq) (*commentdto.SubmitResp, error) {
	return s.sub, s.subEr
}

var _ commentcontract.FragmentPort = (*stubCommentPort)(nil)

// stubCommentFacing 文案出口替身（真实现是 comment service 的 FacingText）。
type stubCommentFacing struct{ text string }

func (s *stubCommentFacing) FacingText(string, error) string { return s.text }

var _ commentcontract.FacingTexter = (*stubCommentFacing)(nil)

// withCommentPorts 临时注入端口并在测试结束时还原（包级状态，测试之间不能互相污染）。
func withCommentPorts(t *testing.T, port commentcontract.FragmentPort, facing commentcontract.FacingTexter) {
	t.Helper()
	t.Cleanup(MutateDepsForTest(func(d *Deps) {
		d.CommentPort, d.CommentFacingTexter = port, facing
		d.CommentSourceHasher = func(ip string) string { return "hash:" + ip }
	}))
}

// callCommentFragment 调一次评论片段，返回（状态码, 响应体）。
//
// csrf 非空时同时注入访客域 token（提交能力据此校验）；visitorID 非零表示已登录。
func callCommentFragment(t *testing.T, method, typeName, payload string, visitorID uint64, csrf string) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var req *http.Request
	if method == http.MethodPost {
		req = httptest.NewRequest(method, "/_fragments/"+typeName, strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, "/_fragments/"+typeName+"?"+payload, nil)
	}
	c.Request = req
	c.Params = gin.Params{{Key: "type", Value: typeName}}
	if visitorID != 0 {
		c.Set(usercontract.VisitorContextKey, visitorID)
	}
	if csrf != "" {
		c.Set(usercontract.VisitorCSRFContextKey, csrf)
	}
	FragmentEndpoint(c)
	return w.Code, w.Body.String()
}

// listQuery 拼列表片段的查询串。
func listQuery(entityType, entityID string) string {
	q := url.Values{}
	q.Set("projectId", commentTestProjectID)
	q.Set("entityType", entityType)
	q.Set("entityId", entityID)
	return q.Encode()
}

// submitForm 拼提交片段的表单。
func submitForm(entityType, entityID, body, csrf string) string {
	q := url.Values{}
	q.Set("projectId", commentTestProjectID)
	q.Set("entityType", entityType)
	q.Set("entityId", entityID)
	q.Set("body", body)
	q.Set("csrf_token", csrf)
	return q.Encode()
}

// TestCommentFragmentDegradations 端口未接入 / 缺参数 / 未登录 / CSRF 不匹配一律给**可见文案**且 200。
func TestCommentFragmentDegradations(t *testing.T) {
	// 端口未注入：列表只说「评论功能暂时不可用」，不是 500。
	withCommentPorts(t, nil, nil)
	code, body := callCommentFragment(t, http.MethodGet, "commentList", listQuery("article", "post-1"), 0, "")
	if code != http.StatusOK || !strings.Contains(body, "不可用") {
		t.Fatalf("端口未注入应给可见文案 + 200，实际 %d：%s", code, body)
	}

	// 端口就位但参数缺失：指向页面作者的配置问题，不是「这站不让评论」。
	withCommentPorts(t, &stubCommentPort{}, &stubCommentFacing{text: "读不出来"})
	code, body = callCommentFragment(t, http.MethodGet, "commentList", "entityType=article", 0, "")
	if code != http.StatusOK || !strings.Contains(body, "站点工程") {
		t.Fatalf("缺工程应给「还没指定站点工程」，实际 %d：%s", code, body)
	}
	// 缺实体 id：另一句人话（配置问题里「缺哪一半」要能分辨）。
	code, body = callCommentFragment(t, http.MethodGet, "commentList",
		"projectId="+commentTestProjectID+"&entityType=article", 0, "")
	if code != http.StatusOK || !strings.Contains(body, "评论对象") {
		t.Fatalf("缺实体应给「还没指定评论对象」，实际 %d：%s", code, body)
	}

	// 未登录提交：给登录引导（**不是** 401 —— htmx 不替换 401 的目标节点）。
	code, body = callCommentFragment(t, http.MethodPost, "commentSubmit",
		submitForm("article", "post-1", "匿名试试", "csrf-1"), 0, "csrf-1")
	if code != http.StatusOK || !strings.Contains(body, "登录") {
		t.Fatalf("未登录应给登录引导 + 200，实际 %d：%s", code, body)
	}

	// 已登录但 CSRF 不匹配：拒绝（纵深防线），文案是「页面已过期」。
	code, body = callCommentFragment(t, http.MethodPost, "commentSubmit",
		submitForm("article", "post-1", "伪造的表单", "wrong-token"), 81, "csrf-1")
	if code != http.StatusOK || !strings.Contains(body, "刷新") {
		t.Fatalf("CSRF 不匹配应给「页面已过期」+200，实际 %d：%s", code, body)
	}
}

// TestCommentListRendersSubmitFormOnlyWhenLoggedIn 已登录 + 有 token 时才渲染提交表单。
func TestCommentListRendersSubmitFormOnlyWhenLoggedIn(t *testing.T) {
	withCommentPorts(t,
		&stubCommentPort{list: &commentdto.ListResp{Items: []commentdto.Item{}, Page: 1, PageSize: 20}},
		&stubCommentFacing{text: "读不出来"})

	// 未登录：列表照常（评论是公开可读的），但不给表单。
	_, guest := callCommentFragment(t, http.MethodGet, "commentList", listQuery("article", "post-form"), 0, "csrf-1")
	if strings.Contains(guest, "/_fragments/commentSubmit") {
		t.Fatalf("未登录不该渲染提交表单：%s", guest)
	}
	// 已登录：表单里必须带 CSRF 隐藏域（少了它提交永远 403，而页面看起来完全正常）。
	_, logged := callCommentFragment(t, http.MethodGet, "commentList", listQuery("article", "post-form"), 91, "csrf-1")
	if !strings.Contains(logged, "/_fragments/commentSubmit") {
		t.Fatalf("已登录应渲染提交表单：%s", logged)
	}
	if !strings.Contains(logged, `name="csrf_token" value="csrf-1"`) {
		t.Fatalf("表单必须带访客域 CSRF token：%s", logged)
	}
}

// TestCommentSubmitRendersVisibleResult 提交成功落 pending：给的是「待审核」这句人话。
func TestCommentSubmitRendersVisibleResult(t *testing.T) {
	withCommentPorts(t,
		// 状态与回执用字面量：本包的测试也不 import 评论模块的 enums
		// （跨模块可传递类型只有 contract 与不可变 dto —— 生产代码与测试同一条判据）。
		&stubCommentPort{sub: &commentdto.SubmitResp{ID: 1, Status: "pending", Message: "comment.msg.submitPending"}},
		&stubCommentFacing{text: "读不出来"})

	code, body := callCommentFragment(t, http.MethodPost, "commentSubmit",
		submitForm("article", "post-1", "说点什么", "csrf-1"), 77, "csrf-1")
	if code != http.StatusOK {
		t.Fatalf("提交应 200，实际 %d：%s", code, body)
	}
	if !strings.Contains(body, "待审核") {
		t.Fatalf("提交成功必须说清「待审核」（先审后发口径），响应：%s", body)
	}
}

// TestCommentSubmitShowsFacingTextOnBusinessError 业务错误由契约出口给文案（不是 500、不是裸 key）。
func TestCommentSubmitShowsFacingTextOnBusinessError(t *testing.T) {
	withCommentPorts(t,
		&stubCommentPort{subEr: &stubErr{msg: "comment.err.notAllowed"}},
		&stubCommentFacing{text: "这条内容需要先购买才能评论"})

	code, body := callCommentFragment(t, http.MethodPost, "commentSubmit",
		submitForm("product", "p-1", "没买过也想评", "csrf-1"), 55, "csrf-1")
	if code != http.StatusOK {
		t.Fatalf("业务拒绝也要 200 + 可见文案，实际 %d：%s", code, body)
	}
	if !strings.Contains(body, "需要先购买") {
		t.Fatalf("应展示消费方给的拒绝文案，响应：%s", body)
	}
}

// stubErr 一个最简的 error（模拟 service 上抛的业务错误）。
type stubErr struct{ msg string }

func (e *stubErr) Error() string { return e.msg }
