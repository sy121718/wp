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
	"testing"
	"time"

	usermodel "go_wp/internal/module/user/model"
	userservice "go_wp/internal/module/user/service"
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
