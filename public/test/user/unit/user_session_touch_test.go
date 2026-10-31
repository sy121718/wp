package unit

import (
	"context"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	userdto "go_wp/internal/module/user/dto"
	usermodel "go_wp/internal/module/user/model"
	userservice "go_wp/internal/module/user/service"
	"go_wp/pkg/crypto"
	"go_wp/public/test/support"
)

func TestTouchSessionDoesNotReviveRevokedSession(t *testing.T) {
	if err := support.SetupRedisForTest(t); err != nil {
		t.Skipf("Redis 不可用，跳过：%v", err)
	}
	ctx := context.Background()

	token := "revoke-touch-" + time.Now().Format("150405.000000000")
	sess := &userservice.UserAuthSession{UserID: 4242, Username: "probe", Token: token}

	if err := userservice.SetSessionForTest(ctx, sess, 10*time.Minute); err != nil {
		t.Fatalf("写入会话失败: %v", err)
	}

	db := support.NewMigratedPGTestDB(t)
	svc := userservice.NewService(usermodel.NewUserModel(db), nil, nil, nil, "测试站")
	if err := svc.RevokeSession(ctx, sess.UserID, userservice.SessionHashForTest(token)); err != nil {
		t.Fatalf("撤销会话失败: %v", err)
	}

	svc.TouchSession(ctx, sess)

	ttl, err := userservice.SessionTTLForTest(ctx, token)
	if err != nil {
		t.Fatalf("读 TTL 失败: %v", err)
	}

	if ttl != -2 {

		t.Fatalf("撤销后的 Touch 把会话写回来了（TTL=%d，-1ns 表示永不过期）—— "+
			"这是可与撤销并发触发的会话复活", int64(ttl))
	}
}

func TestResetPasswordRevokesSessions(t *testing.T) {
	if err := support.SetupRedisForTest(t); err != nil {
		t.Skipf("Redis 不可用，跳过：%v", err)
	}
	db := support.NewMigratedPGTestDB(t)
	ctx := context.Background()
	svc := userservice.NewService(usermodel.NewUserModel(db), nil, nil, nil, "测试站")

	const email = "reset-revoke@example.test"
	const oldPass = "OldPassw0rd!"
	const rawKey = "RESETKEY123"
	hash, err := bcrypt.GenerateFromPassword([]byte(oldPass), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}
	var userID uint64
	if err := db.Raw(`INSERT INTO users (username, password, email, status, activation_key, create_time, update_time)
		VALUES (?, ?, ?, ?, ?, now(), now()) RETURNING id`,
		"resetrevoke", string(hash), email, usermodel.UserStatusActive,
		cryptoHash(rawKey)).Scan(&userID).Error; err != nil {
		t.Fatalf("造账号失败: %v", err)
	}

	loginRes, lerr := svc.Login(ctx, &userdto.LoginReq{Account: email, Password: oldPass},
		userservice.SessionMeta{IP: "10.0.0.9", UserAgent: "attacker"})
	if lerr != nil {
		t.Fatalf("登录失败: %v", lerr)
	}
	token := loginRes.Token

	if err := svc.ResetPassword(ctx, &userdto.ResetPasswordReq{
		Email: email, Key: rawKey, NewPassword: "BrandNewPassw0rd!",
	}); err != nil {
		t.Fatalf("重置密码失败: %v", err)
	}

	ttl, terr := userservice.SessionTTLForTest(ctx, token)
	if terr != nil {
		t.Fatalf("读 TTL 失败: %v", terr)
	}
	if ttl != -2 {
		t.Fatalf("重置密码后旧会话仍然有效（TTL=%d）—— 盗号者会继续留在账号里", int64(ttl))
	}
}

func cryptoHash(raw string) string {
	return crypto.Sha256(strings.ToUpper(strings.TrimSpace(raw)))
}

func TestDisableCustomerRevokesSessions(t *testing.T) {
	if err := support.SetupRedisForTest(t); err != nil {
		t.Skipf("Redis 不可用，跳过：%v", err)
	}
	db := support.NewMigratedPGTestDB(t)
	ctx := context.Background()
	svc := userservice.NewService(usermodel.NewUserModel(db), nil, nil, nil, "测试站")

	const email = "disable-revoke@example.test"
	const pass = "DisableMe1!"
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}
	var userID uint64
	if err := db.Raw(`INSERT INTO users (username, password, email, status, create_time, update_time)
		VALUES (?, ?, ?, ?, now(), now()) RETURNING id`,
		"disablerevoke", string(hash), email, usermodel.UserStatusActive).Scan(&userID).Error; err != nil {
		t.Fatalf("造账号失败: %v", err)
	}

	loginRes, lerr := svc.Login(ctx, &userdto.LoginReq{Account: email, Password: pass},
		userservice.SessionMeta{IP: "10.0.0.8", UserAgent: "browser"})
	if lerr != nil {
		t.Fatalf("登录失败: %v", lerr)
	}

	if _, derr := svc.SetCustomerStatus(ctx, &userdto.CustomerStatusReq{
		CustomerID: userID, Status: usermodel.UserStatusDisabled,
	}); derr != nil {
		t.Fatalf("停用账号失败: %v", derr)
	}

	ttl, terr := userservice.SessionTTLForTest(ctx, loginRes.Token)
	if terr != nil {
		t.Fatalf("读 TTL 失败: %v", terr)
	}
	if ttl != -2 {
		t.Fatalf("停用后旧会话仍然有效（TTL=%d）—— 后台显示已停用，人却还在用", int64(ttl))
	}
}
