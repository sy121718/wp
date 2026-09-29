package commentfeature

// comment_fragment_test.go — 评论片段的链路测试（BIZ-5）：从**片段端点**进入，
// 走真实 service + 真实 PostgreSQL。
//
// 为什么这几条断言必须在这里（而不是模块内单测）：
//
//   - 「未审核的评论不出现在列表里」的判据在 SQL 的 WHERE 里（model.ListTopLevelTx），
//     用替身端口测它等于什么都没测 —— 而那正是本模块最要紧的产品口径（先审后发）；
//   - 「跨实体隔离」同理：它由 (project_id, entity_type, entity_id) 三元组承载；
//   - 「未注册类型不落库」要真库才能证明拒绝发生在写库之前；
//   - 「限流真的生效」走的是端点 → service 的同一条路径。
//
// 为什么不在 internal/module/runtimefragment 包内写：那个包**不能** import
// `public/test/support`（support → internal/routers → runtimefragment，测试期循环依赖）。
// PG 不可用时 support 会 t.Skip（与其它 feature 测试同口径）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	commentcontract "go_wp/internal/module/comment/contract"
	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	commentmodel "go_wp/internal/module/comment/model"
	commentservice "go_wp/internal/module/comment/service"
	"go_wp/internal/module/runtimefragment"
	usercontract "go_wp/internal/module/user/contract"
	"go_wp/public/test/support"
)

// testProjectID 测试工程 id（合法 uuid；comments 未建到 projects 的外键，不必种工程行）。
const testProjectID = "33333333-3333-4333-8333-333333333333"

// commentTypes 两个已注册的实体类型（与装配层注册的 article / product 同形）。
func commentTypes() []commentcontract.EntityType {
	return []commentcontract.EntityType{
		{Type: "article", Label: commentcontract.LabelPair{Key: "admin.article.list.heading", Fallback: "文章"}},
		{Type: "product", Label: commentcontract.LabelPair{Key: "admin.product_translations.entityType.product", Fallback: "商品"}},
	}
}

// setupCommentService 建一个真实库上的评论 service，并把它接到片段端口上。
func setupCommentService(t *testing.T) (*commentservice.Service, *gorm.DB) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	svc := commentservice.NewService(commentmodel.NewModel(db), commentTypes())
	runtimefragment.SetCommentPort(svc)
	runtimefragment.SetCommentFacingTexter(svc)
	// 哈希函数由装配层注入（算法与盐留在评论模块）；测试里给一个可辨认的实现。
	runtimefragment.SetCommentSourceHasher(func(ip string) string { return "hash:" + ip })
	t.Cleanup(func() {
		runtimefragment.SetCommentPort(nil)
		runtimefragment.SetCommentFacingTexter(nil)
		runtimefragment.SetCommentSourceHasher(nil)
	})
	return svc, db
}

// callFragment 调一次片段端点，返回（状态码, 响应体）。
func callFragment(t *testing.T, method, typeName, payload string, visitorID uint64, csrf string) (int, string) {
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
	runtimefragment.FragmentEndpoint(c)
	return w.Code, w.Body.String()
}

// listQuery 拼列表片段查询串。
func listQuery(entityType, entityID string) string {
	q := url.Values{}
	q.Set("projectId", testProjectID)
	q.Set("entityType", entityType)
	q.Set("entityId", entityID)
	return q.Encode()
}

// submitForm 拼提交片段表单。
func submitForm(entityType, entityID, body, csrf string) string {
	q := url.Values{}
	q.Set("projectId", testProjectID)
	q.Set("entityType", entityType)
	q.Set("entityId", entityID)
	q.Set("body", body)
	q.Set("csrf_token", csrf)
	return q.Encode()
}

// submitAndApprove 走真实写路径造一条**已通过**的评论，返回它的 id。
func submitAndApprove(t *testing.T, svc *commentservice.Service, entityType, entityID, body string, userID uint64) int64 {
	t.Helper()
	res, err := svc.Submit(context.Background(), &commentdto.SubmitReq{
		ProjectID: testProjectID, EntityType: entityType, EntityID: entityID,
		UserID: userID, Body: body,
	})
	if err != nil {
		t.Fatalf("提交失败：%v", err)
	}
	if res.Status != commentenums.StatusPending {
		t.Fatalf("新评论必须落 pending（先审后发），实际 %q", res.Status)
	}
	if _, err := svc.Review(context.Background(), &commentdto.ReviewReq{
		ProjectID: testProjectID, IDs: []int64{res.ID},
		Status: commentenums.StatusApproved, ReviewerID: 9,
	}); err != nil {
		t.Fatalf("审核失败：%v", err)
	}
	return res.ID
}

// TestFragmentHidesUnapprovedComment ① 未审核的评论不出现在 commentList（先审后发）。
func TestFragmentHidesUnapprovedComment(t *testing.T) {
	svc, _ := setupCommentService(t)

	approvedBody := "这条已经审核通过了"
	submitAndApprove(t, svc, "article", "post-1", approvedBody, 41)
	pendingBody := "这条还在等待审核"
	if _, err := svc.Submit(context.Background(), &commentdto.SubmitReq{
		ProjectID: testProjectID, EntityType: "article", EntityID: "post-1", UserID: 42, Body: pendingBody,
	}); err != nil {
		t.Fatalf("提交失败：%v", err)
	}

	code, body := callFragment(t, http.MethodGet, "commentList", listQuery("article", "post-1"), 0, "")
	if code != http.StatusOK {
		t.Fatalf("列表片段应 200，实际 %d：%s", code, body)
	}
	if !strings.Contains(body, approvedBody) {
		t.Fatalf("已通过的评论应当出现在列表里：%s", body)
	}
	if strings.Contains(body, pendingBody) {
		t.Fatalf("未审核的评论**不该**出现在列表里（先审后发被破坏）：%s", body)
	}
}

// TestFragmentIsolatesEntities ② 同一实体类型 + 实体 id 的评论互不串（跨实体隔离）。
func TestFragmentIsolatesEntities(t *testing.T) {
	svc, _ := setupCommentService(t)
	submitAndApprove(t, svc, "article", "post-A", "A 的评论", 51)
	submitAndApprove(t, svc, "article", "post-B", "B 的评论", 51)
	// 同一个 entity id、不同类型：也必须互不可见。
	submitAndApprove(t, svc, "product", "post-A", "同 id 的商品评论", 51)

	_, bodyA := callFragment(t, http.MethodGet, "commentList", listQuery("article", "post-A"), 0, "")
	if !strings.Contains(bodyA, "A 的评论") {
		t.Fatalf("A 的评论应出现：%s", bodyA)
	}
	if strings.Contains(bodyA, "B 的评论") || strings.Contains(bodyA, "同 id 的商品评论") {
		t.Fatalf("A 的列表串进了别的实体：%s", bodyA)
	}

	_, bodyP := callFragment(t, http.MethodGet, "commentList", listQuery("product", "post-A"), 0, "")
	if !strings.Contains(bodyP, "同 id 的商品评论") {
		t.Fatalf("商品的评论应出现：%s", bodyP)
	}
	if strings.Contains(bodyP, "A 的评论") {
		t.Fatalf("类型不同但 id 相同的评论串了起来：%s", bodyP)
	}
}

// TestFragmentRejectsUnregisteredEntityType ③ 实体类型不在白名单时拒绝（不是静默接受）。
func TestFragmentRejectsUnregisteredEntityType(t *testing.T) {
	svc, db := setupCommentService(t)
	_ = svc

	code, body := callFragment(t, http.MethodPost, "commentSubmit",
		submitForm("order", "o-1", "试图挂到未注册的类型上", "csrf-1"), 61, "csrf-1")
	if code != http.StatusOK {
		t.Fatalf("被拒也要给可见文案（不是 500），实际 %d：%s", code, body)
	}
	if strings.Contains(body, "待审核") {
		t.Fatalf("未注册类型不该被接受：%s", body)
	}
	if !strings.Contains(body, "不支持") && !strings.Contains(body, "entityTypeUnknown") {
		t.Fatalf("应给出「类型不支持」这类可行动文案：%s", body)
	}
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM comments WHERE entity_type = ?", "order").Scan(&n).Error; err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	if n != 0 {
		t.Fatalf("未注册类型的提交不该落库，实际 %d 行", n)
	}
}

// TestFragmentSubmitRateLimited ④ 提交端点的限流真的生效（同一身份刷到额度用尽即被拦）。
func TestFragmentSubmitRateLimited(t *testing.T) {
	setupCommentService(t)

	blocked := false
	for i := 0; i < 12; i++ {
		code, body := callFragment(t, http.MethodPost, "commentSubmit",
			submitForm("article", "post-rate", "第 N 次提交", "csrf-1"), 71, "csrf-1")
		if code != http.StatusOK {
			t.Fatalf("限流也要给可见文案（不是 500），实际 %d：%s", code, body)
		}
		if strings.Contains(body, "待审核") {
			continue
		}
		if !strings.Contains(body, "频繁") && !strings.Contains(body, "rateLimited") {
			t.Fatalf("第 %d 次被拒的原因不是限流：%s", i+1, body)
		}
		blocked = true
		break
	}
	if !blocked {
		t.Fatal("连续提交 12 次都没被限流拦下：限流没生效")
	}
}

// TestFragmentListAndSubmitShareOneProjectScope 列表与提交落在同一个工程作用域内（RLS 前提）。
//
// 这条守的是「作用域漏设」这个静默故障：RLS 策略谓词读 app.project_id，
// 事务里没设变量时读操作**匹配 0 行且不报错**（写入被 WITH CHECK 拒绝时才报错）。
// 断言同一工程下提交立刻可被审核队列看到，即证明两条路径都在作用域内。
func TestFragmentListAndSubmitShareOneProjectScope(t *testing.T) {
	svc, _ := setupCommentService(t)
	if _, err := svc.Submit(context.Background(), &commentdto.SubmitReq{
		ProjectID: testProjectID, EntityType: "article", EntityID: "post-scope",
		UserID: 88, Body: "作用域检查",
	}); err != nil {
		t.Fatalf("提交失败：%v", err)
	}
	list, err := svc.AdminList(context.Background(), &commentdto.AdminListReq{
		ProjectID: testProjectID, PageSize: 10,
	})
	if err != nil {
		t.Fatalf("审核队列读取失败：%v", err)
	}
	if list.Total < 1 || len(list.Items) < 1 {
		t.Fatalf("刚提交的评论在审核队列里看不到（工程作用域不一致）")
	}
}
