package commentservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	commentcontract "go_wp/internal/module/comment/contract"
	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	commentmodel "go_wp/internal/module/comment/model"
	"go_wp/pkg/facing"
)

func testEntityTypes() []commentcontract.EntityType {
	return []commentcontract.EntityType{
		{Type: "article", Label: commentcontract.LabelPair{Key: "admin.article.list.heading", Fallback: "文章"}},
		{Type: "product", Label: commentcontract.LabelPair{Key: "admin.product_translations.entityType.product", Fallback: "商品"}},
	}
}

func newTestService() *Service {
	return NewService(commentmodel.NewModel(nil), testEntityTypes())
}

const testProjectID = "11111111-1111-4111-8111-111111111111"

func TestNormalizeBody(t *testing.T) {
	cases := []struct {
		name string
		in   string
		key  string
		want string
	}{
		{"空串", "", commentenums.ErrBodyRequired, ""},
		{"只有空白", "   \n\t ", commentenums.ErrBodyRequired, ""},
		{"正常文本被 trim", "  说点什么  ", "", "说点什么"},
		{"超长（按字符数）", strings.Repeat("字", commentenums.MaxBodyLen+1), commentenums.ErrBodyTooLong, ""},
		{"恰好上限通过", strings.Repeat("字", commentenums.MaxBodyLen), "", strings.Repeat("字", commentenums.MaxBodyLen)},

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

	if !svc.IsRegisteredEntityType("article ") {
		t.Error("首尾空白应当被容忍（TrimSpace 后判定）")
	}

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

	if err := svc.checkSubmitRate(10, "ip-hash-1"); err != nil {
		t.Fatalf("另一个身份不该被牵连：%v", err)
	}
}

func TestSubmitRateLimitBySource(t *testing.T) {
	svc := newTestService()

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

	svc2 := newTestService()
	if err := svc2.checkSubmitRate(1, ""); err != nil {
		t.Fatalf("空 IP 哈希不该影响身份维度：%v", err)
	}
}

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

func TestPolicyDeniedCarriesFacingFact(t *testing.T) {
	fact, ok := facing.FactOf(&PolicyDeniedError{
		Key: "product.err.commentPurchaseRequired", Fallback: "购买过这件商品才能评论。",
	})
	if !ok || !fact.Business {
		t.Fatal("消费方拒绝必须是业务事实")
	}
	if fact.Key != "product.err.commentPurchaseRequired" || fact.Fallback != "购买过这件商品才能评论。" {
		t.Fatalf("事实应原样带出 key 与兜底，实际 %+v", fact)
	}

	if _, ok := facing.FactOf(errors.New(`pq: relation "comments" does not exist`)); ok {
		t.Fatal("内部错误不该给出业务事实")
	}
}

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

	if got := svc.EntityTypeLabels(nil); got[0].Label != "文章" {
		t.Fatalf("无取词函数时应回落兜底，实际 %+v", got[0])
	}
}
