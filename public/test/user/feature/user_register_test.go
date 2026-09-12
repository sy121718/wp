package feature

// user_register_test.go — 注册链路与邮件接入（issue #36 / #37 的接入点）。
//
// 这是「邮件模块真的被业务用上」的证据：注册发出验证邮件、邮件里的码能激活账号。
//
// 用假的 MailSender 而不是真 SMTP：这里要验证的是**用户模块传给邮件模块什么**
//（模板 key / 变量 / 收件人），真发信属于 mail 模块自己的测试范围。

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"

	maildto "go_wp/internal/module/mail/dto"
	userdto "go_wp/internal/module/user/dto"
	usermodel "go_wp/internal/module/user/model"
	userservice "go_wp/internal/module/user/service"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// fakeMail 记录被调用的邮件请求。
type fakeMail struct {
	calls []*maildto.SendTemplateReq
	fail  bool
}

func (f *fakeMail) SendTemplate(_ context.Context, req *maildto.SendTemplateReq) (*maildto.SendResult, error) {
	if f.fail {
		return nil, errMailDown
	}
	f.calls = append(f.calls, req)
	return &maildto.SendResult{Queued: true, To: req.To}, nil
}

var errMailDown = &mailUnavailable{}

type mailUnavailable struct{}

func (m *mailUnavailable) Error() string { return "邮件服务不可用（测试替身）" }

type userFixture struct {
	svc   *userservice.Service
	m     *usermodel.UserModel
	sm    *usermodel.UserSessionModel
	pm    *usermodel.UserProfileModel
	prefm *usermodel.UserPreferenceModel
	mail  *fakeMail
}

func newUserFixture(t *testing.T) *userFixture {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行生产迁移建表失败: %v", err)
	}
	m := usermodel.NewUserModel(db)
	sm := usermodel.NewUserSessionModel(db)
	pm := usermodel.NewUserProfileModel(db)
	prefm := usermodel.NewUserPreferenceModel(db)
	fm := &fakeMail{}
	return &userFixture{
		svc:   userservice.NewService(m, sm, pm, prefm, fm, "测试站"),
		m:     m,
		sm:    sm,
		pm:    pm,
		prefm: prefm,
		mail:  fm,
	}
}

// TestRegisterSendsVerifyMailThenActivates 注册发验证邮件，码能激活，且接口不回传码。
func TestRegisterSendsVerifyMailThenActivates(t *testing.T) {
	f := newUserFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	res, err := f.svc.Register(ctx, &userdto.RegisterReq{
		Username: "alice", Email: "alice@example.com", Password: "correct-horse",
		Nickname: "爱丽丝", RegisterIP: "203.0.113.9",
	})
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	if res.Status != usermodel.UserStatusPending {
		t.Fatalf("注册后应为待激活，实际 %d", res.Status)
	}
	if !res.MailQueued {
		t.Fatal("应已受理验证邮件")
	}

	// 邮件模块收到的内容：注册验证模板 + 收件人 + 必要变量。
	if len(f.mail.calls) != 1 {
		t.Fatalf("应发出 1 封邮件，实际 %d", len(f.mail.calls))
	}
	call := f.mail.calls[0]
	if call.TemplateKey != "register_verify" {
		t.Fatalf("模板 key 应为 register_verify，实际 %q", call.TemplateKey)
	}
	if call.To != "alice@example.com" {
		t.Fatalf("收件人错误: %q", call.To)
	}
	code, _ := call.Vars["code"].(string)
	if len(code) != 8 {
		t.Fatalf("验证码应为 8 位，实际 %q", code)
	}
	if call.Vars["name"] != "爱丽丝" {
		t.Fatalf("模板变量 name 应优先用昵称，实际 %v", call.Vars["name"])
	}

	// 激活码存的是 hash：库里不能出现明文码。
	row, err := f.m.GetByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if row.ActivationKey == nil || *row.ActivationKey == code {
		t.Fatal("激活码被明文存库了")
	}
	if strings.Contains(*row.ActivationKey, code) {
		t.Fatal("库里出现明文激活码")
	}
	// 返回值里也不能有码（否则任何调用方都能直接激活）。
	if strings.Contains(res.Username+res.Email, code) {
		t.Fatal("注册响应不该包含激活码")
	}

	// 用邮件里的码激活。
	act, err := f.svc.ActivateEmail(ctx, &userdto.ActivateEmailReq{Key: strings.ToLower(code)})
	if err != nil {
		t.Fatalf("激活失败（码应大小写不敏感）: %v", err)
	}
	if act.Username != "alice" {
		t.Fatalf("激活返回的用户不对: %+v", act)
	}
	row, _ = f.m.GetByID(ctx, act.UserID)
	if row.Status != usermodel.UserStatusActive {
		t.Fatalf("激活后状态应为正常，实际 %d", row.Status)
	}
	if row.ActivationKey != nil {
		t.Fatal("激活后应清掉激活凭据（否则同一个码能重复用）")
	}
	// 激活后还发了欢迎邮件。
	if len(f.mail.calls) != 2 || f.mail.calls[1].TemplateKey != "welcome" {
		t.Fatalf("激活后应发欢迎邮件，实际 %+v", f.mail.calls)
	}

	// 同一个码不能再用。
	if _, err = f.svc.ActivateEmail(ctx, &userdto.ActivateEmailReq{Key: code}); err == nil {
		t.Fatal("已用过的激活码不该还能激活")
	}
}

// TestRegisterRejectsDuplicateAndWeakInput 查重与弱输入。
func TestRegisterRejectsDuplicateAndWeakInput(t *testing.T) {
	f := newUserFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	base := func() *userdto.RegisterReq {
		return &userdto.RegisterReq{Username: "bob", Email: "bob@example.com", Password: "correct-horse"}
	}
	if _, err := f.svc.Register(ctx, base()); err != nil {
		t.Fatalf("首次注册应成功: %v", err)
	}
	// 用户名冲突
	r := base()
	r.Email = "other@example.com"
	if _, err := f.svc.Register(ctx, r); err == nil {
		t.Fatal("重复用户名应被拒绝")
	}
	// 邮箱冲突
	r = base()
	r.Username = "bob2"
	if _, err := f.svc.Register(ctx, r); err == nil {
		t.Fatal("重复邮箱应被拒绝")
	}
	// 弱输入
	for _, bad := range []*userdto.RegisterReq{
		{Username: "ab", Email: "x@y.com", Password: "correct-horse"},
		{Username: "abcd", Email: "not-an-email", Password: "correct-horse"},
		{Username: "abcd", Email: "x@y.com", Password: "short"},
	} {
		if _, err := f.svc.Register(ctx, bad); err == nil {
			t.Fatalf("非法输入应被拒绝: %+v", bad)
		}
	}
}

// TestRegisterSucceedsEvenIfMailFails 邮件发不出去时注册仍然成功（可重发），返回值如实反映。
func TestRegisterSucceedsEvenIfMailFails(t *testing.T) {
	f := newUserFixture(t)
	if f == nil {
		return
	}
	f.mail.fail = true
	res, err := f.svc.Register(context.Background(), &userdto.RegisterReq{
		Username: "carol", Email: "carol@example.com", Password: "correct-horse",
	})
	if err != nil {
		t.Fatalf("邮件故障不该让注册失败（回滚会把 SMTP 抽风变成注册不了）: %v", err)
	}
	if res.MailQueued {
		t.Fatal("邮件失败时 MailQueued 应为 false")
	}
}

var _ = gorm.ErrRecordNotFound
