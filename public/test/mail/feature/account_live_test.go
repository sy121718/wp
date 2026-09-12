package feature

// account_live_test.go — 用真实 SMTP 端到端验证「账号 → 解密 → 发送」整条链路。
//
// 只在设置了环境变量时运行：凭据不落盘、不进代码、不进提交。
// 与单元性质不同 —— 它证明的是「这套配置真的能发出邮件」，而不是「代码看起来对」。

import (
	"context"
	"os"
	"strconv"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
)

func TestLiveAccountTestSend(t *testing.T) {
	host := os.Getenv("MAIL_LIVE_HOST")
	user := os.Getenv("MAIL_LIVE_USER")
	pass := os.Getenv("MAIL_LIVE_PASS")
	to := os.Getenv("MAIL_LIVE_TO")
	if host == "" || user == "" || pass == "" || to == "" {
		t.Skip("未设置 MAIL_LIVE_* 环境变量，跳过真实发送验证")
	}
	port, _ := strconv.Atoi(os.Getenv("MAIL_LIVE_PORT"))
	if port == 0 {
		port = 587
	}
	portStr := os.Getenv("MAIL_LIVE_ENC")
	if portStr == "" {
		portStr = "starttls"
	}

	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	item, err := f.svc.CreateAccount(ctx, &maildto.SaveAccountReq{
		Name: "实测账号", Purpose: "transactional", FromName: "系统通知",
		FromEmail: user, Host: host, Port: port, Username: user, Password: pass,
		Encryption: portStr, RatePerHour: 0,
	})
	if err != nil {
		t.Fatalf("建账号失败: %v", err)
	}

	res, err := f.svc.TestSend(ctx, &maildto.TestSendReq{AccountID: item.ID, ToEmail: to})
	if err != nil {
		t.Fatalf("TestSend 调用失败: %v", err)
	}
	if !res.OK {
		t.Fatalf("真实发送失败 kind=%s err=%s", res.ErrorKind, res.Error)
	}
	t.Logf("真实发送成功 → %s（账号 id=%d）", res.To, item.ID)
}
