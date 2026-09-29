package productservice

import (
	"context"
	"errors"
	"testing"

	commentcontract "go_wp/internal/module/comment/contract"
	productcontract "go_wp/internal/module/product/contract"
	productenums "go_wp/internal/module/product/enums"
)

// fakePurchaseChecker 购买事实端口（productcontract.PurchaseChecker）的测试替身。
//
// 记 calls 是刻意的：有几条断言的价值就在「**没有**去问订单」——
// 非商品实体与未注入端口这两种情况下，问一次都是多余的往返。
type fakePurchaseChecker struct {
	purchased bool
	err       error
	calls     int
}

func (f *fakePurchaseChecker) HasPurchasedProduct(ctx context.Context, projectID string, userID uint64, productID string) (bool, error) {
	f.calls++
	return f.purchased, f.err
}

// TestAllowCommentPurchasePolicy 「商品评论必须买过」的分支与失败模式。
//
// 为什么这条值得钉住：它决定访客能不能提交评论，而**判定反了不会报错** ——
// 表现只是「有人被莫名拒绝」或「有人绕过了规则」，两端都不留痕迹。
//
// 尤其要钉住的是失败模式的选择：**判定失败时放行**，不是拒绝。
// 它是本实现对 commentcontract.EntityPolicy 注释里那条判据的落地
//（产品策略不是安全边界），改它等于改变「数据库抖动时访客看到什么」。
func TestAllowCommentPurchasePolicy(t *testing.T) {
	const (
		articleType = "article" // content 模块的实体类型：本模块不该对它立规矩
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
		s := NewService(nil, nil) // 刻意不注入：规则未启用
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
		// key 与中文兜底必须一起给：comment 侧拿不到本模块的词条表，
		// 只给 key 会在词条缺失时显示裸 key，只给中文则英文界面恒中文。
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
