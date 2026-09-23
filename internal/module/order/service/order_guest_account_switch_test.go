package orderservice

// order_guest_account_switch_test.go — 开号开关的语义（docs/02-W-admin-order-create.md §4）。
//
// 这一条判据的全部价值在于「默认档」：后台代客建单默认**不开号、不发初始密码邮件**，
// 而前台 checkout 的「下单即开户」必须一个字都不变。两者只能靠**显式请求字段**
// （dto.CreateOrderReq.ProvisionGuestAccount 的三态）区分 —— 判据如下：
//
//   - 显式 false → 不开号（后台建单页不勾选时提交的就是它）；
//   - 显式 true  → 开号 + 发信；
//   - 未表态(nil) → 既有的 checkout 行为（下单即开户）；
//   - **CreatedVia 无论取什么值都不影响结果** —— 它是客户端可传字段，
//     拿它当安全语义等于把「是否给客户开户」的默认值交给调用方选。

import (
	"context"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	usercontract "go_wp/internal/module/user/contract"
)

// fakeGuestAccountProvisioner 只数调用次数，不做任何真实开号。
type fakeGuestAccountProvisioner struct {
	calls int
}

func (f *fakeGuestAccountProvisioner) EnsureGuestAccount(_ context.Context,
	_ *usercontract.GuestAccountInput) (*usercontract.GuestAccountResult, error) {
	f.calls++
	return &usercontract.GuestAccountResult{UserID: 9, Username: "guest9", Created: true, PasswordMailed: true}, nil
}

func boolPtr(v bool) *bool { return &v }

// TestEnsureGuestAccountFollowsExplicitSwitch 三态开关：唯一依据是显式字段。
func TestEnsureGuestAccountFollowsExplicitSwitch(t *testing.T) {
	cases := []struct {
		name       string
		switcher   *bool
		wantCalls  int
		wantLinked bool
	}{
		{name: "显式 false（后台建单默认档）", switcher: boolPtr(false), wantCalls: 0, wantLinked: false},
		{name: "显式 true（勾选 opt-in）", switcher: boolPtr(true), wantCalls: 1, wantLinked: true},
		{name: "未表态 nil（既有前台 checkout）", switcher: nil, wantCalls: 1, wantLinked: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			guest := &fakeGuestAccountProvisioner{}
			s := &Service{guest: guest}
			id, mailed := s.ensureGuestAccount(context.Background(), &orderdto.CreateOrderReq{
				CustomerEmail:         "customer@example.com",
				ProvisionGuestAccount: tc.switcher,
			})
			if guest.calls != tc.wantCalls {
				t.Fatalf("开号端口被调用 %d 次，期望 %d 次", guest.calls, tc.wantCalls)
			}
			if tc.wantLinked {
				if id == nil || *id != 9 {
					t.Fatalf("应当关联开出来的账号，实际 %v", id)
				}
				if !mailed {
					t.Error("新建账号且密码寄出时应回执已发信（页面据此提示客户查收）")
				}
				return
			}
			if id != nil {
				t.Fatalf("不开号时 user_id 必须留空，实际 %d", *id)
			}
			if mailed {
				t.Error("不开号时不该回执「已发初始密码」")
			}
		})
	}
}

// TestEnsureGuestAccountIgnoresCreatedVia CreatedVia **不是**开关。
//
// 反面判据：显式 false 时，把 CreatedVia 写成任意值（含 checkout）都不该开号 ——
// 一旦有人把判定改成「看 CreatedVia」，这条立刻变红。
func TestEnsureGuestAccountIgnoresCreatedVia(t *testing.T) {
	for _, via := range []string{"", "checkout", "admin", "api", "mystery"} {
		guest := &fakeGuestAccountProvisioner{}
		s := &Service{guest: guest}
		id, _ := s.ensureGuestAccount(context.Background(), &orderdto.CreateOrderReq{
			CustomerEmail:         "customer@example.com",
			CreatedVia:            via,
			ProvisionGuestAccount: boolPtr(false),
		})
		if guest.calls != 0 || id != nil {
			t.Errorf("CreatedVia=%q 时不该开号（它只是入口标记，不是开关）：calls=%d id=%v", via, guest.calls, id)
		}
	}

	// 另一侧：显式 true 时同样不看 CreatedVia（走的还是开号）。
	for _, via := range []string{"", "checkout", "admin"} {
		guest := &fakeGuestAccountProvisioner{}
		s := &Service{guest: guest}
		id, _ := s.ensureGuestAccount(context.Background(), &orderdto.CreateOrderReq{
			CustomerEmail:         "customer@example.com",
			CreatedVia:            via,
			ProvisionGuestAccount: boolPtr(true),
		})
		if guest.calls != 1 || id == nil {
			t.Errorf("CreatedVia=%q 且显式勾选时应开号：calls=%d id=%v", via, guest.calls, id)
		}
	}
}

// TestEnsureGuestAccountShortCircuits 既有的两条短路保持不变。
func TestEnsureGuestAccountShortCircuits(t *testing.T) {
	guest := &fakeGuestAccountProvisioner{}
	s := &Service{guest: guest}

	// 已有 user_id（访客已登录）：不重复开号，且显式 true 也不覆盖既有身份。
	existing := uint64(42)
	id, mailed := s.ensureGuestAccount(context.Background(), &orderdto.CreateOrderReq{
		UserID: &existing, CustomerEmail: "customer@example.com", ProvisionGuestAccount: boolPtr(true),
	})
	if guest.calls != 0 || id == nil || *id != existing || mailed {
		t.Fatalf("已有 user_id 时应原样返回且不调开号端口：calls=%d id=%v mailed=%v", guest.calls, id, mailed)
	}

	// 未装配用户模块（guest == nil）：下单照常，只是不自动开号。
	s = &Service{}
	id, mailed = s.ensureGuestAccount(context.Background(), &orderdto.CreateOrderReq{
		CustomerEmail: "customer@example.com", ProvisionGuestAccount: boolPtr(true),
	})
	if id != nil || mailed {
		t.Fatalf("未接用户模块时不该关联账号：id=%v mailed=%v", id, mailed)
	}
}
