package commentservice

// comment_service_test.go — 模块内就近单测：状态机、形状校验、限流与归口文案。
//
// 为什么这些用例放在模块内（不走数据库、不走装配）：
//
//   - 它们判错时会**静默出错** ——「状态机的合法取值多了一个」（审核动作能把评论改回待审）、
//     「实体 id 的形状放宽了」（注入类输入进得来）、「限流没生效」（脚本可以灌库），
//     三者的共同点是：页面一切正常、日志干净、只有攻击者或运营在下一次才发现；
//   - 它们几乎每次改动都会被碰到（新增一种状态、调整限流额度、改长度上限），
//     扔进 public/test 就要先起库，于是最该被覆盖的改动反而最少被跑到。
//
// 用到的 model 一律不触库：这些用例刻意只走「校验在前、写库在后」的那几条路径
// （Submit 的顺序是 登录 → 限流 → 目标 → 正文 → 回复 → 规则 → 落库），
// 所以 NewModel(nil) 足够；真正走库的行为由 feature 链路测试覆盖。

import (
	"context"
	"errors"
	"strings"
	"testing"

	commentcontract "go_wp/internal/module/comment/contract"
	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	commentmodel "go_wp/internal/module/comment/model"
)

// testEntityTypes 两个已注册的实体类型（与装配层注册的 article / product 同形）。
func testEntityTypes() []commentcontract.EntityType {
	return []commentcontract.EntityType{
		{Type: "article", Label: commentcontract.LabelPair{Key: "admin.article.list.heading", Fallback: "文章"}},
		{Type: "product", Label: commentcontract.LabelPair{Key: "admin.product_translations.entityType.product", Fallback: "商品"}},
	}
}

// newTestService 构造一个不触库的 service（每次调用都拿到全新的限流器）。
func newTestService() *Service {
	return NewService(commentmodel.NewModel(nil), testEntityTypes())
}

const testProjectID = "11111111-1111-4111-8111-111111111111"

// —— 正文形状 ——

func TestNormalizeBody(t *testing.T) {
	cases := []struct {
		name string
		in   string
		key  string // 期望的错误 key（空 = 期望成功）
		want string
	}{
		{"空串", "", commentenums.ErrBodyRequired, ""},
		{"只有空白", "   \n\t ", commentenums.ErrBodyRequired, ""},
		{"正常文本被 trim", "  说点什么  ", "", "说点什么"},
		{"超长（按字符数）", strings.Repeat("字", commentenums.MaxBodyLen+1), commentenums.ErrBodyTooLong, ""},
		{"恰好上限通过", strings.Repeat("字", commentenums.MaxBodyLen), "", strings.Repeat("字", commentenums.MaxBodyLen)},
		// 控制字符被清掉，但换行 / 制表符保留（多段评论是正常内容）。
		{"控制字符清洗", "第一行\n第二行\x00\x07第三行", "", "第一行\n第二行第三行"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeBody(tc.in)
			if tc.key != "" {
				if err == nil || err.Error() != tc.key {
					t.Fatalf("期望错误 %q，实际 %v", tc.key, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("不该报错：%v", err)
			}
			if got != tc.want {
				t.Fatalf("清洗结果不符\n期望 %q\n实际 %q", tc.want, got)
			}
		})
	}
}

func TestIsSafeEntityID(t *testing.T) {
	ok := []string{"a1b2c3d4-1111-4222-8333-444455556666", "1024", "my_post-1", strings.Repeat("a", commentenums.MaxEntityIDLen)}
	bad := []string{"", "   ", "../../etc/passwd", "a b", "a/b", "<script>", strings.Repeat("a", commentenums.MaxEntityIDLen+1)}
	for _, id := range ok {
		if !isSafeEntityID(id) {
			t.Errorf("应通过：%q", id)
		}
	}
	for _, id := range bad {
		if isSafeEntityID(id) {
			t.Errorf("应被拒：%q", id)
		}
	}
}

// —— 实体类型白名单（验收断言 ③ 的单测侧）——

func TestEntityTypeWhitelistIsFailClosed(t *testing.T) {
	svc := newTestService()
	if !svc.IsRegisteredEntityType("article") || !svc.IsRegisteredEntityType("product") {
		t.Fatal("已注册的类型应通过")
	}
	for _, bad := range []string{"", "order", "ARTICLE", "unknown"} {
		if svc.IsRegisteredEntityType(bad) {
			t.Errorf("未注册的类型必须被拒（fail-closed）：%q", bad)
		}
	}
	// 首尾空白是**刻意容忍**的：类型来自 URL / 表单参数，尾部多一个空格不该让
	// 「这篇文章的评论」变成「类型未注册」——validateTarget 会先 TrimSpace 再判。
	if !svc.IsRegisteredEntityType("article ") {
		t.Error("首尾空白应当被容忍（TrimSpace 后判定）")
	}
	// 大小写敏感是刻意的：类型标识是**数据**（存进 comments.entity_type），
	// 容忍大小写会让同一类实体在库里出现两种写法，列表按类型筛选立刻分叉。
	if svc.IsRegisteredEntityType("Article") {
		t.Error("类型标识必须大小写敏感")
	}
}

func TestValidateTargetRejectsUnknownType(t *testing.T) {
	svc := newTestService()
	if err := svc.validateTarget(testProjectID, "article", "post-1"); err != nil {
		t.Fatalf("合法三元组不该报错：%v", err)
	}
	for _, tc := range []struct {
		name, project, entityType, entityID, wantKey string
	}{
		{"工程缺失", "", "article", "post-1", commentenums.ErrProjectRequired},
		{"工程不是 uuid", "not-a-uuid", "article", "post-1", commentenums.ErrProjectRequired},
		{"类型缺失", testProjectID, "", "post-1", commentenums.ErrEntityTypeUnknown},
		{"类型未注册", testProjectID, "order", "post-1", commentenums.ErrEntityTypeUnknown},
		{"实体 id 缺失", testProjectID, "article", "", commentenums.ErrEntityIDInvalid},
		{"实体 id 形状非法", testProjectID, "article", "../x", commentenums.ErrEntityIDInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.validateTarget(tc.project, tc.entityType, tc.entityID)
			if err == nil || err.Error() != tc.wantKey {
				t.Fatalf("期望 %q，实际 %v", tc.wantKey, err)
			}
		})
	}
}

// —— 提交的硬门槛与限流（验收断言 ④）——

func TestSubmitRejectsAnonymous(t *testing.T) {
	svc := newTestService()
	_, err := svc.Submit(context.Background(), &commentdto.SubmitReq{
		ProjectID: testProjectID, EntityType: "article", EntityID: "post-1", Body: "你好",
	})
	if err == nil || err.Error() != commentenums.ErrLoginRequired {
		t.Fatalf("匿名提交必须被拒（产品口径：匿名不可评），实际 %v", err)
	}
}

func TestSubmitRejectsUnregisteredEntityType(t *testing.T) {
	svc := newTestService()
	_, err := svc.Submit(context.Background(), &commentdto.SubmitReq{
		ProjectID: testProjectID, EntityType: "order", EntityID: "o-1", UserID: 7, Body: "你好",
	})
	if err == nil || err.Error() != commentenums.ErrEntityTypeUnknown {
		t.Fatalf("未注册的实体类型必须被拒，实际 %v", err)
	}
}

func TestSubmitRateLimitByIdentity(t *testing.T) {
	svc := newTestService()
	// 同一身份连续提交：前 submitPerIdentity 次通过校验，第 N+1 次被限流拦下。
	for i := 0; i < submitPerIdentity; i++ {
		err := svc.checkSubmitRate(9, "ip-hash-1")
		if err != nil {
			t.Fatalf("第 %d 次不该被限流：%v", i+1, err)
		}
	}
	err := svc.checkSubmitRate(9, "ip-hash-1")
	if err == nil || err.Error() != commentenums.ErrRateLimited {
		t.Fatalf("第 %d 次必须被身份限流拦下，实际 %v", submitPerIdentity+1, err)
	}
	// 另一个身份不受影响（限流按身份隔离，不是全站一个桶）。
	if err := svc.checkSubmitRate(10, "ip-hash-1"); err != nil {
		t.Fatalf("另一个身份不该被牵连：%v", err)
	}
}

func TestSubmitRateLimitBySource(t *testing.T) {
	svc := newTestService()
	// 同一来源、不同身份：注册是免费的，换账号成本为零 —— 这正是来源维度存在的理由。
	allowed := 0
	for userID := uint64(1); userID <= uint64(submitPerSource)+3; userID++ {
		err := svc.checkSubmitRate(userID, "same-ip")
		if err == nil {
			allowed++
			continue
		}
		if err.Error() != commentenums.ErrRateLimited {
			t.Fatalf("第 %d 个身份的错误类型不对：%v", userID, err)
		}
	}
	if allowed != submitPerSource {
		t.Fatalf("同一来源在窗口内应恰好放行 %d 次，实际 %d", submitPerSource, allowed)
	}
	// 拿不到 IP 哈希时跳过来源维度（仍按身份判）—— 不因为一个取不到的字段把入口全开。
	svc2 := newTestService()
	if err := svc2.checkSubmitRate(1, ""); err != nil {
		t.Fatalf("空 IP 哈希不该影响身份维度：%v", err)
	}
}

// —— 审核动作的状态机 ——

func TestReviewRejectsNonReviewableStatus(t *testing.T) {
	svc := newTestService()
	for _, status := range []string{commentenums.StatusPending, commentenums.StatusSpam, "", "APPROVED"} {
		_, err := svc.Review(context.Background(), &commentdto.ReviewReq{
			ProjectID: testProjectID, IDs: []int64{1}, Status: status, ReviewerID: 3,
		})
		if err == nil {
			t.Fatalf("审核动作不该接受状态 %q", status)
		}
	}
}

func TestReviewRequiresSelection(t *testing.T) {
	svc := newTestService()
	_, err := svc.Review(context.Background(), &commentdto.ReviewReq{
		ProjectID: testProjectID, IDs: nil, Status: commentenums.StatusApproved, ReviewerID: 3,
	})
	if err == nil || err.Error() != commentenums.ErrNothingSelected {
		t.Fatalf("空选择必须被拒，实际 %v", err)
	}
}

func TestReviewRejectsOversizedBatch(t *testing.T) {
	svc := newTestService()
	ids := make([]int64, commentenums.MaxReviewIDs+1)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	_, err := svc.Review(context.Background(), &commentdto.ReviewReq{
		ProjectID: testProjectID, IDs: ids, Status: commentenums.StatusApproved, ReviewerID: 3,
	})
	if err == nil || err.Error() != commentenums.ErrInvalidParam {
		t.Fatalf("超过批量上限必须被拒，实际 %v", err)
	}
}

// —— 归口文案（三件套的读侧）——

// TestFacingTextTranslatesPolicyDenial 消费方给的拒绝文案按**请求语言**取词，且绝不输出空串。
//
// 这是「差异化规则的拒绝」这条链路的唯一出口：它必须能把 product 的词条
// （`product.err.commentPurchaseRequired` 这类）翻成当前语言 —— 只给中文成品的话，
// 英文站点会看到一句中文；而缺兜底时输出空串会让页面上出现一块空白。
func TestFacingTextTranslatesPolicyDenial(t *testing.T) {
	svc := newTestService()
	// key + 中文兜底：i18n 未初始化（测试环境）时命中不了词条 → 出消费方给的兜底原文。
	got := svc.FacingText("en-US", &PolicyDeniedError{
		Key: "product.err.commentPurchaseRequired", Fallback: "购买过这件商品才能评论。",
	})
	if strings.TrimSpace(got) == "" {
		t.Fatal("消费方给了兜底时不能输出空串")
	}
	if !strings.Contains(got, "购买过这件商品") {
		t.Fatalf("未命中词条应回落到消费方给的中文兜底，实际 %q", got)
	}
	// 只有 key、没有兜底：回落本模块的归口业务文案（不是空串，也不是「系统内部错误」）。
	got = svc.FacingText("en-US", &PolicyDeniedError{Key: "product.err.whatever"})
	if strings.TrimSpace(got) == "" {
		t.Fatal("缺兜底时不能输出空串")
	}
	if strings.Contains(got, "product.err") {
		t.Fatalf("缺兜底时不该把消费方的裸 key 铺到页面上，实际 %q", got)
	}
	// 只有兜底（消费方直接给成品文案）：原样输出。
	if got := svc.FacingText("zh-CN", &PolicyDeniedError{Fallback: "买过才能评。"}); got != "买过才能评。" {
		t.Fatalf("只有兜底时应原样输出，实际 %q", got)
	}
}

func TestFacingTextNeverLeaksInternalError(t *testing.T) {
	svc := newTestService()
	if got := svc.FacingText("zh-CN", nil); got != "" {
		t.Fatalf("nil 应返回空串（调用方不必先判空），实际 %q", got)
	}
	internal := errors.New(`pq: relation "comments" does not exist`)
	got := svc.FacingText("zh-CN", internal)
	if got == "" || strings.Contains(got, "comments") {
		t.Fatalf("未命中的错误必须归口且不含库原文，实际 %q", got)
	}
	// 命中白名单的业务错误按 key 返回（调用方再取词）。
	if got := svc.FacingText("zh-CN", errors.New(commentenums.ErrRateLimited)); got == "" {
		t.Fatal("命中的业务错误必须给出文案")
	}
}

// —— 实体类型展示名（由拥有者声明的 label）——

func TestEntityTypeLabelsUseOwnerDeclaredLabel(t *testing.T) {
	svc := newTestService()
	tr := func(key, fallback string) string {
		if key == "admin.article.list.heading" {
			return "译文·文章"
		}
		return fallback
	}
	labels := svc.EntityTypeLabels(tr)
	if len(labels) != 2 {
		t.Fatalf("应有两个已注册类型，实际 %d", len(labels))
	}
	if labels[0].Type != "article" || labels[0].Label != "译文·文章" {
		t.Fatalf("第一个类型应由拥有者的词条取词，实际 %+v", labels[0])
	}
	// tr 为 nil 时回落中文兜底（而不是裸 key）。
	if got := svc.EntityTypeLabels(nil); got[0].Label != "文章" {
		t.Fatalf("无取词函数时应回落兜底，实际 %+v", got[0])
	}
}
