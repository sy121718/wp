package orderservice

import (
	"context"
	json "go_wp/pkg/jsonx"
	"testing"

	"gorm.io/gorm"

	orderdto "go_wp/internal/module/order/dto"
	usercontract "go_wp/internal/module/user/contract"
)

type fakeGuestAccountProvisioner struct {
	calls int

	mails int
}

func (f *fakeGuestAccountProvisioner) EnsureGuestAccount(_ context.Context,
	_ *usercontract.GuestAccountInput) (*usercontract.GuestAccountResult, error) {
	f.calls++
	return &usercontract.GuestAccountResult{UserID: 9, Username: "guest9", Created: true, PasswordMailed: true}, nil
}

func (f *fakeGuestAccountProvisioner) EnsureGuestAccountTx(_ context.Context, _ *gorm.DB,
	_ *usercontract.GuestAccountInput) (*usercontract.GuestAccountResult, error) {
	f.calls++
	return &usercontract.GuestAccountResult{
		UserID: 9, Username: "guest9", Created: true,
		PendingMail: &usercontract.GuestAccountMail{Email: "customer@example.com", Username: "guest9", Password: "pw"},
	}, nil
}

func (f *fakeGuestAccountProvisioner) SendGuestAccountMail(_ context.Context, mail *usercontract.GuestAccountMail) error {
	if mail != nil {
		f.mails++
	}
	return nil
}

func (f *fakeGuestAccountProvisioner) LookupGuestAccount(_ context.Context, _ string) (uint64, bool, error) {
	return 0, false, nil
}

func boolPtr(v bool) *bool { return &v }

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
			id, mail, _ := s.provisionGuestAccountTx(context.Background(), nil, &orderdto.CreateOrderReq{
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
				if mail == nil {
					t.Error("新建账号且密码寄出时应回执已发信（页面据此提示客户查收）")
				}
				return
			}
			if id != nil {
				t.Fatalf("不开号时 user_id 必须留空，实际 %d", *id)
			}
			if mail != nil {
				t.Error("不开号时不该回执「已发初始密码」")
			}
		})
	}
}

func TestEnsureGuestAccountIgnoresJSONCreatedVia(t *testing.T) {
	for _, tc := range []struct {
		name      string
		provision bool
		wantCalls int
	}{
		{name: "明确不开", provision: false, wantCalls: 0},
		{name: "明确开", provision: true, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var req orderdto.CreateOrderReq
			payload := `{"customerEmail":"customer@example.com","createdVia":"admin","provisionGuestAccount":` +
				map[bool]string{true: "true", false: "false"}[tc.provision] + `}`
			if err := json.Unmarshal([]byte(payload), &req); err != nil {
				t.Fatal(err)
			}
			guest := &fakeGuestAccountProvisioner{}
			s := &Service{guest: guest}
			id, _, _ := s.provisionGuestAccountTx(context.Background(), nil, &req)
			if guest.calls != tc.wantCalls || (id != nil) != tc.provision {
				t.Fatalf("伪造来源时开户次数=%d id=%v，期望次数=%d", guest.calls, id, tc.wantCalls)
			}
		})
	}
}

func TestEnsureGuestAccountShortCircuits(t *testing.T) {
	guest := &fakeGuestAccountProvisioner{}
	s := &Service{guest: guest}

	existing := uint64(42)
	id, mail, _ := s.provisionGuestAccountTx(context.Background(), nil, &orderdto.CreateOrderReq{
		UserID: &existing, CustomerEmail: "customer@example.com", ProvisionGuestAccount: boolPtr(true),
	})
	if guest.calls != 0 || id == nil || *id != existing || mail != nil {
		t.Fatalf("已有 user_id 时应原样返回且不调开号端口：calls=%d id=%v mail=%v", guest.calls, id, mail)
	}

	s = &Service{}
	id, mail, _ = s.provisionGuestAccountTx(context.Background(), nil, &orderdto.CreateOrderReq{
		CustomerEmail: "customer@example.com", ProvisionGuestAccount: boolPtr(true),
	})
	if id != nil || mail != nil {
		t.Fatalf("未接用户模块时不该关联账号：id=%v mail=%v", id, mail)
	}
}
