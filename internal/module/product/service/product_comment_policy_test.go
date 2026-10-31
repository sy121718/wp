package productservice

import (
	"context"
	"errors"
	"testing"

	commentcontract "go_wp/internal/module/comment/contract"
	productcontract "go_wp/internal/module/product/contract"
	productenums "go_wp/internal/module/product/enums"
)

type fakePurchaseChecker struct {
	purchased bool
	err       error
	calls     int
}

func (f *fakePurchaseChecker) HasPurchasedProduct(ctx context.Context, projectID string, userID uint64, productID string) (bool, error) {
	f.calls++
	return f.purchased, f.err
}

func TestAllowCommentPurchasePolicy(t *testing.T) {
	const (
		articleType = "article"
		productID   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	)
	reqOf := func(entityType string) *commentcontract.PolicyReq {
		return &commentcontract.PolicyReq{
			ProjectID:  "11111111-1111-4111-8111-111111111111",
			EntityType: entityType,
			EntityID:   productID,
			UserID:     1001,
		}
	}

	t.Run("请求为空一律放行", func(t *testing.T) {
		s := NewService(nil, nil)
		s.SetPurchaseChecker(&fakePurchaseChecker{purchased: false})
		denial, err := s.AllowComment(context.Background(), nil)
		if err != nil || denial != nil {
			t.Fatalf("期望放行，实际 denial=%+v err=%v", denial, err)
		}
	})

	t.Run("非商品实体不问订单", func(t *testing.T) {
		s := NewService(nil, nil)
		fake := &fakePurchaseChecker{purchased: false}
		s.SetPurchaseChecker(fake)
		denial, err := s.AllowComment(context.Background(), reqOf(articleType))
		if err != nil || denial != nil {
			t.Fatalf("文章评论不该被商品规则拦住，实际 denial=%+v err=%v", denial, err)
		}
		if fake.calls != 0 {
			t.Fatalf("非商品实体不该去问订单，实际调用 %d 次", fake.calls)
		}
	})

	t.Run("端口未注入即放行", func(t *testing.T) {
		s := NewService(nil, nil)
		denial, err := s.AllowComment(context.Background(), reqOf(productcontract.EntityTypeProduct))
		if err != nil || denial != nil {
			t.Fatalf("端口未注入应放行（规则未启用），实际 denial=%+v err=%v", denial, err)
		}
	})

	t.Run("买过则放行", func(t *testing.T) {
		s := NewService(nil, nil)
		s.SetPurchaseChecker(&fakePurchaseChecker{purchased: true})
		denial, err := s.AllowComment(context.Background(), reqOf(productcontract.EntityTypeProduct))
		if err != nil || denial != nil {
			t.Fatalf("买过应放行，实际 denial=%+v err=%v", denial, err)
		}
	})

	t.Run("没买过则拒绝且带可翻译文案", func(t *testing.T) {
		s := NewService(nil, nil)
		s.SetPurchaseChecker(&fakePurchaseChecker{purchased: false})
		denial, err := s.AllowComment(context.Background(), reqOf(productcontract.EntityTypeProduct))
		if err != nil {
			t.Fatalf("拒绝不是故障，不该返回 error：%v", err)
		}
		if denial == nil {
			t.Fatal("没买过必须被拒绝，实际放行")
		}

		if denial.Message != productenums.ErrCommentPurchaseRequired {
			t.Fatalf("Message = %q，期望 %q", denial.Message, productenums.ErrCommentPurchaseRequired)
		}
		if denial.Fallback == "" {
			t.Fatal("Fallback 为空：词条缺失时页面会回落成归口文案，这条规则的可行动性就丢了")
		}
	})

	t.Run("判定失败放行而不是拒绝", func(t *testing.T) {
		s := NewService(nil, nil)
		s.SetPurchaseChecker(&fakePurchaseChecker{err: errors.New("查询故障")})
		denial, err := s.AllowComment(context.Background(), reqOf(productcontract.EntityTypeProduct))
		if err != nil {
			t.Fatalf("端口故障不该把 error 抛给调用方（应由本实现归口）：%v", err)
		}
		if denial != nil {
			t.Fatalf("判定失败应放行（评论仍进审核队列兜底），实际拒绝：%+v", denial)
		}
	})
}
