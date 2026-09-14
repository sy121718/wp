package feature

// order_guest_account_test.go — 访客下单自动开号（BIZ-1 与 issue #36 的联动）。

import (
	"context"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	orderdto "go_wp/internal/module/order/dto"
	userdto "go_wp/internal/module/user/dto"
)

// fakeMail 记录被调用的邮件请求（只为断言「发了哪封信」）。
type fakeMail struct {
	calls []*maildto.SendTemplateReq
}

func (f *fakeMail) SendTemplate(_ context.Context, req *maildto.SendTemplateReq) (*maildto.SendResult, error) {
	f.calls = append(f.calls, req)
	return &maildto.SendResult{Queued: true, To: req.To}, nil
}

// findTemplate 找某 key 的邮件。
func (f *fakeMail) findTemplate(key string) *maildto.SendTemplateReq {
	for _, c := range f.calls {
		if c.TemplateKey == key {
			return c
		}
	}
	return nil
}

// TestOrderCreateProvisionsGuestAccount：访客下单 → 自动开号 + 发初始密码 + 订单关联该账号。
func TestOrderCreateProvisionsGuestAccount(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "访客商品", 25, 5)

	res, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1))
	if err != nil {
		t.Fatalf("访客下单失败: %v", err)
	}
	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("读订单失败: %v", err)
	}
	if detail.Head.UserID == nil {
		t.Fatalf("访客下单应自动开号并关联 user_id，否则他无处可查自己的订单")
	}

	// 账号真的按邮箱建出来了，且与订单关联的是同一个。
	var uid uint64
	if err := f.db.Raw("SELECT id FROM users WHERE lower(email) = lower(?)", detail.Head.CustomerEmail).Scan(&uid).Error; err != nil {
		t.Fatalf("按邮箱查账号失败: %v", err)
	}
	if uid == 0 || uid != *detail.Head.UserID {
		t.Fatalf("订单关联的账号与邮箱查到的不是同一个: %v vs %d", detail.Head.UserID, uid)
	}

	// 初始密码邮件：四个变量都必须是真值 —— 缺任何一个模板会静默渲染成空，
	// 客户收到的就是「用户名：　密码：」这样的废邮件。
	mail := f.mail.findTemplate("guest_account")
	if mail == nil {
		t.Fatalf("应发出 guest_account 邮件，实际发了 %d 封", len(f.mail.calls))
	}
	if mail.To != detail.Head.CustomerEmail {
		t.Fatalf("收件人应为下单邮箱，实际 %q", mail.To)
	}
	for _, k := range []string{"name", "username", "password", "site_name"} {
		v, ok := mail.Vars[k]
		if !ok || v == nil || v == "" {
			t.Fatalf("初始密码邮件的 %s 变量为空: %+v", k, mail.Vars)
		}
	}
}

// TestOrderCreateNeverResetsExistingAccountPassword：安全边界。
//
// 邮箱已有账号时只关联、绝不动密码。若改成「顺手重置并发邮件」，任何人拿别人的邮箱
// 下一单就能把对方密码换成自己知道的那一串，而且整条链路从外部看完全正常、不会报错。
func TestOrderCreateNeverResetsExistingAccountPassword(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	const email = "existing@example.com"

	reg, err := f.users.Register(ctx, &userdto.RegisterReq{
		Username: "already", Email: email, Password: "original-password-123",
	})
	if err != nil {
		t.Fatalf("预置账号注册失败: %v", err)
	}
	var before string
	if err := f.db.Raw("SELECT password FROM users WHERE id = ?", reg.UserID).Scan(&before).Error; err != nil {
		t.Fatalf("读原密码哈希失败: %v", err)
	}

	_, vid := f.addProduct(t, "已有账号商品", 10, 5)
	req := f.createBaseReq(vid, 1)
	req.CustomerEmail = email
	res, err := f.orders.CreateOrder(ctx, req)
	if err != nil {
		t.Fatalf("已有账号邮箱下单应成功（只关联）: %v", err)
	}

	var after string
	if err := f.db.Raw("SELECT password FROM users WHERE id = ?", reg.UserID).Scan(&after).Error; err != nil {
		t.Fatalf("读新密码哈希失败: %v", err)
	}
	if after != before {
		t.Fatalf("已有账号的密码被下单流程改动了 —— 这是账户接管")
	}

	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("读订单失败: %v", err)
	}
	if detail.Head.UserID == nil || *detail.Head.UserID != reg.UserID {
		t.Fatalf("订单应关联既有账号 %d，实际 %v", reg.UserID, detail.Head.UserID)
	}

	for _, c := range f.mail.calls {
		if c.TemplateKey == "guest_account" && c.To == email {
			t.Fatalf("已有账号不该收到初始密码邮件 —— 那等于把别人的密码发出来")
		}
	}
}
