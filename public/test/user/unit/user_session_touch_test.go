package unit

// user_session_touch_test.go — 会话撤销与 Touch 的竞争（审计 SEC-03）。
//
// 守的缺陷形态：TouchSession 是读-改-写（「刷新最后活跃时间」），中间有一个窗口
// 能被撤销插进来：
//
//	Touch 读到会话 → Revoke 删除会话 → Touch 把会话写回
//
// 而 Redis 的 `SET ... KEEPTTL` 在 key **不存在**时不会失败 —— 它把 key 当成
// 无过期时间写进去（实测 TTL = -1）。后果比"复活"更重：被撤销的会话变成**永不过期**。
//
// 判据：撤销之后的 Touch 必须**写不进去**，且 key 不能重新出现。

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

// TestTouchSessionDoesNotReviveRevokedSession 撤销后 Touch 不得复活会话。
func TestTouchSessionDoesNotReviveRevokedSession(t *testing.T) {
	if err := support.SetupRedisForTest(t); err != nil {
		t.Skipf("Redis 不可用，跳过：%v", err)
	}
	ctx := context.Background()

	// 会话键的唯一性由 token 决定；用时间戳避免与同库其它用例互撞。
	token := "revoke-touch-" + time.Now().Format("150405.000000000")
	sess := &userservice.UserAuthSession{UserID: 4242, Username: "probe", Token: token}

	// ① 正常会话（带 TTL），走生产同一条写入路径。
	if err := userservice.SetSessionForTest(ctx, sess, 10*time.Minute); err != nil {
		t.Fatalf("写入会话失败: %v", err)
	}

	// ② 撤销：走生产撤销路径（含归属校验与索引清理）。
	// 撤销路径只碰 Redis 索引与会话 key，不读库；这里仍给真实的 model
	// （由生产迁移建表），避免"测试用一个形状不对的服务"。
	db := support.NewMigratedPGTestDB(t)
	svc := userservice.NewService(usermodel.NewUserModel(db), nil, nil, nil, "测试站")
	if err := svc.RevokeSession(ctx, sess.UserID, userservice.SessionHashForTest(token)); err != nil {
		t.Fatalf("撤销会话失败: %v", err)
	}

	// ③ 撤销之后才到达的 Touch（模拟它在撤销前就读到了会话）。
	svc.TouchSession(ctx, sess)

	// ④ 会话必须仍然是"不存在"。
	ttl, err := userservice.SessionTTLForTest(ctx, token)
	if err != nil {
		t.Fatalf("读 TTL 失败: %v", err)
	}
	// go-redis 的 TTL 用 time.Duration 承载 Redis 的整数语义：
	//   -2ns = key 不存在，-1ns = key 存在但无过期时间。
	if ttl != -2 {
		// ttl == -1 是最坏结果：会话复活且永不过期。
		t.Fatalf("撤销后的 Touch 把会话写回来了（TTL=%d，-1ns 表示永不过期）—— "+
			"这是可与撤销并发触发的会话复活", int64(ttl))
	}
}

// TestResetPasswordRevokesSessions 重置密码后旧会话必须失效（SEC-04）。
//
// 场景就是这条判据存在的理由：账号可能已被盗，受害者走「忘记密码」重置。
// 攻击者手里的会话是 Redis 里的独立凭据，**不随密码变化失效** ——
// 不撤销等于把盗号者继续留在里面。
func TestResetPasswordRevokesSessions(t *testing.T) {
	if err := support.SetupRedisForTest(t); err != nil {
		t.Skipf("Redis 不可用，跳过：%v", err)
	}
	db := support.NewMigratedPGTestDB(t)
	ctx := context.Background()
	svc := userservice.NewService(usermodel.NewUserModel(db), nil, nil, nil, "测试站")

	// 造一个激活账号（与生产同一张表、同一套列）。
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

	// 攻击者在此之前拿到的一个有效会话 —— 走**真实登录**而不是手工写 key：
	// 会话撤销依赖用户设备索引（ZSET），手工写 key 会漏掉索引，于是
	// RevokeAllSessions 找不到它（"会话在 Redis、索引里没有"正是登录路径
	// 刻意先写索引再写会话要避免的形态）。
	loginRes, lerr := svc.Login(ctx, &userdto.LoginReq{Account: email, Password: oldPass},
		userservice.SessionMeta{IP: "10.0.0.9", UserAgent: "attacker"})
	if lerr != nil {
		t.Fatalf("登录失败: %v", lerr)
	}
	token := loginRes.Token

	// 受害者重置密码。
	if err := svc.ResetPassword(ctx, &userdto.ResetPasswordReq{
		Email: email, Key: rawKey, NewPassword: "BrandNewPassw0rd!",
	}); err != nil {
		t.Fatalf("重置密码失败: %v", err)
	}

	// 旧会话必须已失效。
	ttl, terr := userservice.SessionTTLForTest(ctx, token)
	if terr != nil {
		t.Fatalf("读 TTL 失败: %v", terr)
	}
	if ttl != -2 {
		t.Fatalf("重置密码后旧会话仍然有效（TTL=%d）—— 盗号者会继续留在账号里", int64(ttl))
	}
}

// cryptoHash 激活码的哈希（与服务端校验同源，直接调 pkg/crypto 而不是复刻算法）。
func cryptoHash(raw string) string {
	return crypto.Sha256(strings.ToUpper(strings.TrimSpace(raw)))
}

// TestDisableCustomerRevokesSessions 停用账号后旧会话必须失效（SEC-04）。
//
// 停用的语义是"这个人现在不该再进来"，而会话是 Redis 里的独立凭据、不随账号状态失效。
// 不撤销的话，后台显示"已停用"，被停用的人仍在用 —— 直到会话自然过期（记住我可能几天）。
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
