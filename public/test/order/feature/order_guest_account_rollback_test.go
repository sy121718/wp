package feature

import (
	"context"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

func userIDByEmail(t *testing.T, f *orderFixture, email string) uint64 {
	t.Helper()
	var id uint64
	if err := f.db.Raw("SELECT COALESCE(MAX(id), 0) FROM users WHERE lower(email) = lower(?)", email).
		Scan(&id).Error; err != nil {
		t.Fatalf("查 users 表失败: %v", err)
	}
	return id
}

func guestMailSent(f *orderFixture) bool {
	for _, c := range f.mail.calls {
		if c != nil && c.TemplateKey == "guest_account" {
			return true
		}
	}
	return false
}

func TestOrderGuestAccountRollsBackWithOrder(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	const email = "orphan-check@example.com"

	_, vid := f.addProduct(t, "库存不足商品", 30, 1)
	req := f.createBaseReq(vid, 5)
	req.CustomerEmail = email
	req.CustomerName = "孤儿账号候选"

	if _, err := f.orders.CreateOrder(ctx, req); err == nil {
		t.Fatal("前置条件不成立：库存不足应当建单失败")
	} else if err.Error() != orderenums.ErrStockInsufficient {
		t.Fatalf("建单失败原因应为库存不足，实际 %v", err)
	}

	if id := userIDByEmail(t, f, email); id != 0 {
		t.Fatalf("整单已回滚，却留下了孤儿账号（user id=%d）—— 客户能登录、却没有任何订单", id)
	}
	if guestMailSent(f) {
		t.Fatal("整单已回滚，却把初始密码邮件发了出去（客户会拿着一个不存在的账号去登录）")
	}
}

func TestOrderGuestAccountCommitsWithOrder(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	const email = "new-buyer@example.com"

	_, vid := f.addProduct(t, "正常商品", 30, 10)
	req := f.createBaseReq(vid, 2)
	req.CustomerEmail = email
	req.CustomerName = "新买家"

	res, err := f.orders.CreateOrder(ctx, req)
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	uid := userIDByEmail(t, f, email)
	if uid == 0 {
		t.Fatal("建单成功时应当为访客自动开号")
	}
	if !res.AccountMailed {
		t.Fatal("账号建立且密码已寄出时 AccountMailed 应为 true（客户要知道去哪里取密码）")
	}
	if !guestMailSent(f) {
		t.Fatal("建单成功时应当发出初始密码邮件")
	}

	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("读订单失败: %v", err)
	}
	if detail.Head.Status != ordermodel.OrderStatusPending {
		t.Fatalf("建单后应为待付款，实际 %q", detail.Head.Status)
	}
	if detail.Head.UserID == nil || *detail.Head.UserID != uid {
		t.Fatalf("订单必须关联到刚开的账号：订单 user_id=%v，实际账号 id=%d", detail.Head.UserID, uid)
	}
}

func TestOrderGuestAccountExplicitOffSkipsProvision(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	const email = "no-account@example.com"

	_, vid := f.addProduct(t, "代客商品", 30, 10)
	req := f.createBaseReq(vid, 1)
	req.CustomerEmail = email
	off := false
	req.ProvisionGuestAccount = &off

	res, err := f.orders.CreateOrder(ctx, req)
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	if res.AccountMailed {
		t.Fatal("显式不开号时不该声称发过初始密码邮件")
	}
	if id := userIDByEmail(t, f, email); id != 0 {
		t.Fatalf("显式不开号却建了账号（user id=%d）", id)
	}
	if guestMailSent(f) {
		t.Fatal("显式不开号却发了初始密码邮件")
	}
}
