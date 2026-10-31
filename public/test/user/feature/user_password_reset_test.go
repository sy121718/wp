package feature

import (
	"context"
	"testing"

	"golang.org/x/crypto/bcrypt"

	userdto "go_wp/internal/module/user/dto"
	usermodel "go_wp/internal/module/user/model"
	"go_wp/pkg/cache"
	"go_wp/public/test/support"
)

func activeUser(t *testing.T, f *userFixture, username, email, password string) {
	t.Helper()
	ctx := context.Background()
	res, err := f.svc.Register(ctx, &userdto.RegisterReq{Username: username, Email: email, Password: password})
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	var code string
	for _, c := range f.mail.calls {
		if c.TemplateKey == "register_verify" && c.To == email {
			code, _ = c.Vars["code"].(string)
		}
	}
	if code == "" {
		t.Fatalf("没拿到注册验证码: %+v", f.mail.calls)
	}
	if _, err = f.svc.ActivateEmail(ctx, &userdto.ActivateEmailReq{Key: code}); err != nil {
		t.Fatalf("激活失败: %v", err)
	}
	_ = res
}

func TestPasswordResetFlow(t *testing.T) {
	if err := support.SetupRedisForTest(t); err != nil {
		t.Skipf("本地 Redis 不可用，跳过密码重置会话撤销: %v", err)
	}
	f := newUserFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	activeUser(t, f, "dave", "dave@example.com", "old-password-1")
	f.mail.calls = nil

	if err := f.svc.RequestPasswordReset(ctx, &userdto.PasswordResetReqRequest{Email: "dave@example.com"}); err != nil {
		t.Fatalf("申请重置失败: %v", err)
	}
	if len(f.mail.calls) != 1 || f.mail.calls[0].TemplateKey != "password_reset" {
		t.Fatalf("应发出密码重置邮件，实际 %+v", f.mail.calls)
	}
	code, _ := f.mail.calls[0].Vars["code"].(string)
	if len(code) != 8 {
		t.Fatalf("重置码应为 8 位，实际 %q", code)
	}

	if err := f.svc.ResetPassword(ctx, &userdto.ResetPasswordReq{
		Email: "dave@example.com", Key: "WRONGKEY", NewPassword: "new-password-1",
	}); err == nil {
		t.Fatal("错误的码应被拒绝")
	}

	if err := f.svc.ResetPassword(ctx, &userdto.ResetPasswordReq{
		Email: "DAVE@example.com", Key: code, NewPassword: "new-password-1",
	}); err != nil {
		t.Fatalf("重置失败: %v", err)
	}
	row, err := f.m.GetByEmail(ctx, "dave@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(row.Password), []byte("new-password-1")) != nil {
		t.Fatal("新密码没有生效")
	}
	if bcrypt.CompareHashAndPassword([]byte(row.Password), []byte("old-password-1")) == nil {
		t.Fatal("旧密码竟然还能用")
	}
	if row.ActivationKey != nil {
		t.Fatal("改密后应清掉重置凭据")
	}
	if row.LoginFailureCount != 0 || row.LockedUntilTime != nil {
		t.Fatal("改密应顺带清掉登录失败锁定")
	}

	if err := f.svc.ResetPassword(ctx, &userdto.ResetPasswordReq{
		Email: "dave@example.com", Key: code, NewPassword: "another-pass-1",
	}); err == nil {
		t.Fatal("已用过的重置码不该还能用")
	}
}

func TestPasswordResetDoesNotLeakAccountExistence(t *testing.T) {
	f := newUserFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	err := f.svc.RequestPasswordReset(ctx, &userdto.PasswordResetReqRequest{Email: "nobody@example.com"})
	if err != nil {
		t.Fatalf("不存在的邮箱不该报错（否则接口变成账号枚举器）: %v", err)
	}
	if len(f.mail.calls) != 0 {
		t.Fatal("不存在的邮箱不该发信")
	}
}

func TestRegisterCodeCannotResetPassword(t *testing.T) {
	f := newUserFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	if _, err := f.svc.Register(ctx, &userdto.RegisterReq{
		Username: "erin", Email: "erin@example.com", Password: "old-password-1",
	}); err != nil {
		t.Fatal(err)
	}
	var regCode string
	for _, c := range f.mail.calls {
		if c.TemplateKey == "register_verify" {
			regCode, _ = c.Vars["code"].(string)
		}
	}
	if regCode == "" {
		t.Fatal("没拿到注册码")
	}

	if err := f.svc.ResetPassword(ctx, &userdto.ResetPasswordReq{
		Email: "erin@example.com", Key: regCode, NewPassword: "new-password-1",
	}); err == nil {
		t.Fatal("未激活账号的码不该能改密码（共用 activation_key 不等于可以串用）")
	}
	row, _ := f.m.GetByEmail(ctx, "erin@example.com")
	if row.Status != usermodel.UserStatusPending {
		t.Fatal("账号状态不该被改动")
	}
}

func TestPasswordResetRetriesAfterSessionStoreFailure(t *testing.T) {
	if err := support.SetupRedisForTest(t); err != nil {
		t.Skipf("Redis不可用: %v", err)
	}
	f := newUserFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	activeUser(t, f, "retrycode", "retry-code@example.invalid", "old-password-123")
	f.mail.calls = nil
	if err := f.svc.RequestPasswordReset(ctx, &userdto.PasswordResetReqRequest{Email: "retry-code@example.invalid"}); err != nil {
		t.Fatal(err)
	}
	code, _ := f.mail.calls[0].Vars["code"].(string)
	before, err := f.m.GetByEmail(ctx, "retry-code@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	req := &userdto.ResetPasswordReq{Email: "retry-code@example.invalid", Key: code, NewPassword: "new-password-123"}
	if err := f.svc.ResetPassword(ctx, req); err == nil {
		t.Fatal("会话存储失败时不应报告改密成功")
	}
	after, err := f.m.GetByEmail(ctx, req.Email)
	if err != nil {
		t.Fatal(err)
	}
	if before.Password != after.Password || before.ActivationKey == nil || after.ActivationKey == nil || *before.ActivationKey != *after.ActivationKey {
		t.Fatal("撤销失败已消费验证码或部分改密")
	}
	if err := support.SetupRedisForTest(t); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ResetPassword(ctx, req); err != nil {
		t.Fatalf("恢复会话存储后同一个验证码应可重试: %v", err)
	}
}
