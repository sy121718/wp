package feature

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"go_wp/pkg/auth"
	"go_wp/pkg/captcha"
	"go_wp/pkg/i18n"
	"go_wp/public/test/support"

	"github.com/spf13/viper"
)

// 本文件只守一条事实：账号被锁定时，登录接口回 423，不是 400。
//
// 为什么单独一条用例：423 是错误声明里给出的状态（service 侧
// facing.WithStatus(..., http.StatusLocked)），不是出口按「业务错误默认 400」
// 猜出来的。这条链路一旦被谁改回裸 fmt.Errorf，状态就会悄悄退回 400，而
// 文案仍然正确 —— 只断言文案的用例抓不住这个回归。
//
// 断言口径：
//   - 状态码精确 423；
//   - message 是「前缀 + 可解析的剩余时长 + 后缀」的成品句，参数真的填进去了；
//   - message 里没有裸业务 key（ErrAccountLocked 不得上屏）；
//   - 请求走真实 HTTP 登录链路（binding → service → 统一 JSON 出口），
//     不绕过 service 直接造错误。
func TestAdminLoginLockedAccountReturns423(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)

	cfg := viper.New()
	cfg.Set("auth.session_secret", "test-secret-admin-login-locked-status")
	if err := auth.Init(cfg); err != nil {
		t.Fatalf("初始化会话存储失败: %v", err)
	}
	t.Cleanup(func() { _ = auth.Close() })

	// 词库固定成已知成品句，避免依赖仓库里的 seed 文案。
	// %s 是剩余时长参数，由 service 拼进 key 的 | 段，出口逐段取词后填进去。
	const (
		zhMessageKey = "ErrAccountLocked"
		zhTemplate   = "TEST-LOCKED-ZH-还需 %s"
	)
	i18n.InjectForTest(map[string]map[string]string{
		zhMessageKey: {"zh-CN": zhTemplate},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	engine, m := newLoginEngine(t, db)
	const (
		username = "locked_status_feature_user"
		password = "right-password"
	)
	seeded := seedAdmin(t, m, username, password)
	ctx := context.Background()

	// 固定锁定窗口为未来 30 分钟：不靠「连续失败 5 次」触发，
	// 那条路径已有 TestAdminLoginFiveFailuresLocksTemporarilyNotBan 覆盖。
	// 这里要验的是「已锁定的账号来登录时出口给什么状态」。
	lockedUntil := time.Now().Add(30 * time.Minute)
	// 直接按表名写：这里只需要造出「已锁定」的状态，不经过 service，
	// 所以用真实的 sys_admin 表而不是绕开生产链去调 AdminLogin 的锁定分支。
	if err := m.DB(ctx).Table("sys_admin").
		Where("id = ?", seeded.ID).
		Update("locked_until_time", lockedUntil).Error; err != nil {
		t.Fatalf("写入锁定时间失败: %v", err)
	}

	captchaID, code := captcha.Get().Generate()
	raw := fmt.Sprintf(`{"username":%q,"password":%q,"captcha_id":%q,"captcha":%q}`,
		username, password, captchaID, code)
	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method:  http.MethodPost,
		Path:    "/api/admin/login",
		RawBody: []byte(raw),
		Headers: map[string]string{
			"Content-Type":    "application/json",
			"Accept-Language": "zh-CN",
		},
	})
	if err != nil {
		t.Fatalf("登录请求失败: %v", err)
	}

	if recorder.Code != http.StatusLocked {
		t.Fatalf("锁定账号登录应回 423 Locked，实际 %d（body=%s）", recorder.Code, recorder.Body.String())
	}
	if ct := recorder.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("锁定失败也必须回 JSON，实际 Content-Type=%q", ct)
	}

	std, err := support.ParseStandardResponse(recorder)
	if err != nil {
		t.Fatalf("解析标准响应失败: %v", err)
	}

	// 参数精确：前缀与后缀之外必须是一段可解析的时长，且落在 29~30 分钟内。
	const zhPrefix = "TEST-LOCKED-ZH-还需 "
	if !strings.HasPrefix(std.Message, zhPrefix) {
		t.Fatalf("文案前缀不符，实际 %q，期望以 %q 开头", std.Message, zhPrefix)
	}
	remainingText := strings.TrimSpace(strings.TrimPrefix(std.Message, zhPrefix))
	remaining, perr := time.ParseDuration(remainingText)
	if perr != nil {
		t.Fatalf("文案里的剩余时长不是合法时长（说明 %s 参数没被填进去）：%q", "%s", std.Message)
	}
	if remaining <= 0 || remaining > 30*time.Minute {
		t.Fatalf("剩余时长应在 (0, 30m] 内，实际 %v（message=%q）", remaining, std.Message)
	}

	// 裸 key 不得上屏：成品句里出现 ErrAccountLocked 就说明取词失败退了 key。
	for _, leaked := range []string{"ErrAccountLocked", "admin.err", "%s", "%!"} {
		if strings.Contains(std.Message, leaked) {
			t.Fatalf("锁定文案里出现了不该出现的片段 %q：%q", leaked, std.Message)
		}
	}

	// 账号状态没有被这次登录改动。
	entity, err := m.GetByID(ctx, seeded.ID)
	if err != nil || entity == nil {
		t.Fatalf("复查管理员失败: %v", err)
	}
	if !entity.IsLocked() {
		t.Fatal("被拒的登录不该解除锁定")
	}
}
