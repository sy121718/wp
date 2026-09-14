package orderservice

// order_create_draft_test.go — 建单的纯计算部分：入参校验、金额分摊、优惠码判定。
//
// 这些用例是 CQ-023（把 278 行的 CreateOrder 拆开）的**直接收益**：拆分前它们只能
// 通过整条建单链路间接验证（要起数据库、要造商品与库存），现在可以只喂数据断言结论。
// 金额与折扣是审计里两个资损缺陷的藏身处（无券时采信客户端折扣、每人限次在事务外判定），
// 拆出来之后这两处第一次有了不依赖数据库的回归测试。

import (
	"testing"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

// TestValidateCreateOrderReq 入参校验的错误必须可区分。
//
// 校验从 CreateOrder 里抽出来之后是纯函数：不碰数据库、不构造订单，
// 所以边界可以逐条钉死 —— 少了这一点，「哪种输入返回哪句话」只能靠读代码确认。
func TestValidateCreateOrderReq(t *testing.T) {
	valid := &orderdto.CreateOrderReq{
		ProjectID:     "p-1",
		CustomerEmail: "buyer@example.com",
		Items:         []orderdto.OrderItemReq{{VariantID: "v-1", Quantity: 1}},
	}
	cases := []struct {
		name string
		req  *orderdto.CreateOrderReq
		want string // 期望的错误文案前缀（空 = 应通过）
	}{
		{name: "nil 请求", req: nil, want: orderenums.ErrInvalidParam},
		{name: "缺工程", req: &orderdto.CreateOrderReq{CustomerEmail: "a@b.com", Items: valid.Items}, want: orderenums.ErrProjectRequired},
		{name: "缺邮箱", req: &orderdto.CreateOrderReq{ProjectID: "p-1", Items: valid.Items}, want: orderenums.ErrCustomerEmailRequired},
		{name: "邮箱格式不对", req: &orderdto.CreateOrderReq{ProjectID: "p-1", CustomerEmail: "ab", Items: valid.Items}, want: orderenums.ErrCustomerEmailInvalid},
		{name: "无商品项", req: &orderdto.CreateOrderReq{ProjectID: "p-1", CustomerEmail: "a@b.com"}, want: orderenums.ErrItemsRequired},
		{name: "工程 id 只有空白", req: &orderdto.CreateOrderReq{ProjectID: "   ", CustomerEmail: "a@b.com", Items: valid.Items}, want: orderenums.ErrProjectRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCreateOrderReq(tc.req)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("应当通过，实际: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("应当返回 %s，实际通过", tc.want)
			}
			if err.Error() != tc.want {
				t.Fatalf("错误文案应为 %q，实际 %q", tc.want, err.Error())
			}
		})
	}
	if err := validateCreateOrderReq(valid); err != nil {
		t.Fatalf("合法请求应通过，实际: %v", err)
	}

	// 商品项上限：超限必须拒绝（无上限的输入会让一次请求锁住任意多的库存行）。
	over := &orderdto.CreateOrderReq{ProjectID: "p-1", CustomerEmail: "a@b.com"}
	for i := 0; i <= maxOrderItems; i++ {
		over.Items = append(over.Items, orderdto.OrderItemReq{VariantID: "v", Quantity: 1})
	}
	if err := validateCreateOrderReq(over); err == nil || err.Error() != orderenums.ErrItemLimitExceeded {
		t.Fatalf("超过 %d 个商品项应被拒，实际: %v", maxOrderItems, err)
	}
}

// TestCouponDiscount 折扣计算：两种口径 + 上限收紧。
func TestCouponDiscount(t *testing.T) {
	cases := []struct {
		name     string
		coupon   *ordermodel.CouponEntity
		subtotal int64
		want     int64
	}{
		{name: "百分折扣取整（向下）", coupon: &ordermodel.CouponEntity{DiscountType: ordermodel.CouponTypePercent, DiscountValue: 20}, subtotal: 10000, want: 2000},
		{name: "百分折扣的除不尽部分归商家", coupon: &ordermodel.CouponEntity{DiscountType: ordermodel.CouponTypePercent, DiscountValue: 33}, subtotal: 1001, want: 330},
		{name: "固定金额", coupon: &ordermodel.CouponEntity{DiscountType: ordermodel.CouponTypeFixed, DiscountValue: 1500}, subtotal: 10000, want: 1500},
		{name: "固定金额超过小计则收到小计", coupon: &ordermodel.CouponEntity{DiscountType: ordermodel.CouponTypeFixed, DiscountValue: 99999}, subtotal: 10000, want: 10000},
		{name: "满额百分折扣 = 0 元单只能到 0 元", coupon: &ordermodel.CouponEntity{DiscountType: ordermodel.CouponTypePercent, DiscountValue: 100}, subtotal: 5000, want: 5000},
		{name: "小计为 0", coupon: &ordermodel.CouponEntity{DiscountType: ordermodel.CouponTypePercent, DiscountValue: 50}, subtotal: 0, want: 0},
		{name: "负小计不产生折扣", coupon: &ordermodel.CouponEntity{DiscountType: ordermodel.CouponTypePercent, DiscountValue: 50}, subtotal: -100, want: 0},
		{name: "未知口径一律 0", coupon: &ordermodel.CouponEntity{DiscountType: "mystery", DiscountValue: 50}, subtotal: 10000, want: 0},
		{name: "空券", coupon: nil, subtotal: 10000, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := couponDiscount(tc.coupon, tc.subtotal); got != tc.want {
				t.Fatalf("折扣应为 %d 分，实际 %d", tc.want, got)
			}
		})
	}
}

// TestCouponRuleCheck 券自身的可用性判定（不含每人限次 —— 那条要查核销表）。
func TestCouponRuleCheck(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.Local)
	before := now.Add(-time.Hour)
	after := now.Add(time.Hour)
	enabled := ordermodel.CouponStatusEnabled

	cases := []struct {
		name     string
		coupon   *ordermodel.CouponEntity
		subtotal int64
		want     string
	}{
		{name: "正常可用", coupon: &ordermodel.CouponEntity{Status: enabled}, subtotal: 100, want: ""},
		{name: "空券", coupon: nil, subtotal: 100, want: orderenums.ErrCouponNotFound},
		{name: "已停用", coupon: &ordermodel.CouponEntity{Status: ordermodel.CouponStatusDisabled}, subtotal: 100, want: orderenums.ErrCouponDisabled},
		{name: "未到生效时间", coupon: &ordermodel.CouponEntity{Status: enabled, StartsAt: &after}, subtotal: 100, want: orderenums.ErrCouponNotStarted},
		{name: "已过失效时间", coupon: &ordermodel.CouponEntity{Status: enabled, EndsAt: &before}, subtotal: 100, want: orderenums.ErrCouponExpired},
		{name: "时间窗内", coupon: &ordermodel.CouponEntity{Status: enabled, StartsAt: &before, EndsAt: &after}, subtotal: 100, want: ""},
		{name: "总次数用尽", coupon: &ordermodel.CouponEntity{Status: enabled, MaxUses: 5, UsedCount: 5}, subtotal: 100, want: orderenums.ErrCouponExhausted},
		{name: "不限次数不受 used_count 影响", coupon: &ordermodel.CouponEntity{Status: enabled, MaxUses: 0, UsedCount: 99}, subtotal: 100, want: ""},
		{name: "未达门槛", coupon: &ordermodel.CouponEntity{Status: enabled, MinSubtotal: 200}, subtotal: 199, want: orderenums.ErrCouponMinSubtotal},
		{name: "刚好达到门槛", coupon: &ordermodel.CouponEntity{Status: enabled, MinSubtotal: 200}, subtotal: 200, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := couponRuleCheck(tc.coupon, tc.subtotal, now); got != tc.want {
				t.Fatalf("判定应为 %q，实际 %q", tc.want, got)
			}
		})
	}
}

// TestNormalizeCouponCode 券码归一化只发生在服务层一处（大写 + 去空白）。
func TestNormalizeCouponCode(t *testing.T) {
	if got := normalizeCouponCode("  save20 "); got != "SAVE20" {
		t.Fatalf("券码应归一化为大写去空白，实际 %q", got)
	}
	if got := normalizeCouponCode(""); got != "" {
		t.Fatalf("空券码应保持空串（表示未使用券），实际 %q", got)
	}
}

// TestAllocateLineDiscountsEdgeCases 分摊的边界：舍入差、全额折扣、无折扣、空列表。
//
// 基本比例分摊在 order_money_test.go 里已有；这里补的是「和必须精确等于折扣额」
// 的边界 —— 它是退款的依据（退款按分摊后的 LineTotal 算），差一分钱会在多笔部分
// 退货里累积成资损，所以最后一行吸收舍入差这件事必须有测试盯着。
func TestAllocateLineDiscountsEdgeCases(t *testing.T) {
	newItems := func(subtotals ...int64) []*ordermodel.OrderItemEntity {
		items := make([]*ordermodel.OrderItemEntity, 0, len(subtotals))
		for _, s := range subtotals {
			items = append(items, &ordermodel.OrderItemEntity{LineSubtotal: s, LineTotal: s})
		}
		return items
	}
	sum := func(items []*ordermodel.OrderItemEntity) int64 {
		var total int64
		for _, it := range items {
			total += it.LineDiscount
		}
		return total
	}

	t.Run("无折扣时各行回到原价", func(t *testing.T) {
		items := newItems(1000, 2000)
		allocateLineDiscounts(items, 3000, 0)
		for _, it := range items {
			if it.LineDiscount != 0 || it.LineTotal != it.LineSubtotal {
				t.Fatalf("无折扣不应产生分摊: %+v", it)
			}
		}
	})

	t.Run("按比例分摊且总和相等", func(t *testing.T) {
		items := newItems(1000, 2000, 7000)
		allocateLineDiscounts(items, 10000, 3333)
		if got := sum(items); got != 3333 {
			t.Fatalf("分摊之和应等于折扣额 3333，实际 %d", got)
		}
		if items[2].LineDiscount != 3333-items[0].LineDiscount-items[1].LineDiscount {
			t.Fatalf("最后一行应吸收舍入差，实际 %+v", items)
		}
	})

	t.Run("全额折扣时各行实付为 0", func(t *testing.T) {
		items := newItems(500, 700)
		allocateLineDiscounts(items, 1200, 1200)
		for _, it := range items {
			if it.LineTotal != 0 {
				t.Fatalf("满额折扣后行实付应为 0，实际 %d", it.LineTotal)
			}
		}
	})

	t.Run("空行列表不 panic", func(t *testing.T) {
		allocateLineDiscounts(nil, 100, 10)
	})
}
