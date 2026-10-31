package feature

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
