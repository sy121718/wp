// order_guest_account_rollback_test.go — 访客开号必须与建单同事务（BIZ-11）。
//
// 缺陷：开号（建号 + 发初始密码邮件）发生在**建单事务之外**（buildOrderDraft 阶段）。
// 触发序列：新邮箱下单一件库存不足的量大商品 → 建号 + 发信**成功** → 整单**回滚**
// → 留下一个**孤儿账号**（能登录、没有任何订单，且客户收到的密码指向一个"凭空出现"的账号）。
//
// 判据：
//  1. 建单失败（库存不足）后，`users` 里不得留下该邮箱的账号，也不得发出初始密码邮件；
//  2. 建单成功时账号照常建立、订单关联到它、初始密码邮件照发（正常路径不回退）。
package feature

import (
	"context"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

// userIDByEmail 直查字节真源：该邮箱在 users 表里的 id（0 = 不存在）。
func userIDByEmail(t *testing.T, f *orderFixture, email string) uint64 {
	t.Helper()
	var id uint64
	if err := f.db.Raw("SELECT COALESCE(MAX(id), 0) FROM users WHERE lower(email) = lower(?)", email).
		Scan(&id).Error; err != nil {
		t.Fatalf("查 users 表失败: %v", err)
	}
	return id
}

// guestMailSent 是否发出过初始密码邮件（模板 key 见 user 模块的 guestAccountTemplate）。
func guestMailSent(f *orderFixture) bool {
	for _, c := range f.mail.calls {
		if c != nil && c.TemplateKey == "guest_account" {
			return true
		}
	}
	return false
}

// TestOrderGuestAccountRollsBackWithOrder 建单失败不得留下孤儿账号，也不得发信。
func TestOrderGuestAccountRollsBackWithOrder(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	const email = "orphan-check@example.com"

	// 只备 1 件货，却下单 5 件 → 库存不足，整单回滚。
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

// TestOrderGuestAccountCommitsWithOrder 正常路径不回退：账号随订单一起提交、订单关联到它、邮件照发。
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

// TestOrderGuestAccountExplicitOffSkipsProvision 显式不开号（后台代客建单档）不得建号、不得发信。
//
// 这条是既有三态语义的防回退：把开号挪进事务不能把「显式开关」这一层判断丢掉。
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
