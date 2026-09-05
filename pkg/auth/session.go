// Package auth Session + Cookie 认证 + Redis 用户会话管理。
//
// Cookie 会话（cookie_session.go）只管认证（识别当前用户），无服务端状态。
// Redis 管理用户信息、封禁标记、在线心跳。
package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go_wp/pkg/cache"

	"github.com/redis/go-redis/v9"
)

const (
	// ContextSessionRevokedKey 标记当前请求已注销，禁止中间件续签和刷新在线状态。
	ContextSessionRevokedKey = "auth_session_revoked"

	userSessionPrefix = "user:session:"
	userBlockedPrefix = "user:blocked:"
	onlinePrefix      = "online:"

	defaultSessionTTL = 24 * time.Hour
	defaultOnlineTTL  = 5 * time.Minute

	// blockedTTL 封禁标记的固定存活时长。
	// RevokeUserSession 传 time.Now() 时 time.Until(blockedUntil) 为负值，Redis 会报
	// "invalid expire time in 'set' command" 导致强制下线写不进去（M 级缺陷）。
	// 改用固定 7 天：覆盖最长会话有效期（rememberMe=7d），key 到期自然消失，
	// 封禁语义由 IsBlocked 的 blockedAt > sessionIssuedAt 判断，与 TTL 长短解耦。
	blockedTTL = 7 * 24 * time.Hour
)

// UserSession 用户会话信息，登录成功后写入 Redis。
type UserSession struct {
	ID        uint64 `json:"id"`
	SessionID string `json:"session_id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	Avatar    string `json:"avatar"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Status    int    `json:"status"`
	IsAdmin   int    `json:"is_admin"`
	DeptID    uint64 `json:"dept_id"`
}

func sessionKey(userID uint64) string {
	return fmt.Sprintf("%s%d", userSessionPrefix, userID)
}

func blockedKey(userID uint64) string {
	return fmt.Sprintf("%s%d", userBlockedPrefix, userID)
}

func onlineKey(userID uint64) string {
	return fmt.Sprintf("%s%d", onlinePrefix, userID)
}

// SaveUserSession 将用户会话信息写入 Redis。
// ttl 传 0 时使用默认 24h。
func SaveUserSession(ctx context.Context, session *UserSession, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	return cache.SetJSON(ctx, sessionKey(session.ID), session, ttl)
}

// GetUserSession 从 Redis 获取用户会话信息。
// 不存在时返回 nil, nil。
func GetUserSession(ctx context.Context, userID uint64) (*UserSession, error) {
	session, err := cache.GetJSON[UserSession](ctx, sessionKey(userID))
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// DeleteUserSession 删除用户会话（退出登录时调用）。
func DeleteUserSession(ctx context.Context, userID uint64) error {
	client, err := cache.GetRedis()
	if err != nil {
		return err
	}
	return client.Del(ctx, sessionKey(userID), onlineKey(userID)).Err()
}

// RevokeUserSession 撤销当前用户已建立的会话，并清理会话与在线状态。
//
// 封禁时间设为当前时间：所有此前建立的 cookie 会话（issued_at < now）立即失效。
func RevokeUserSession(ctx context.Context, userID uint64) error {
	if err := BlockUser(ctx, userID, time.Now()); err != nil {
		return err
	}
	return DeleteUserSession(ctx, userID)
}

// BlockUser 封禁用户，在此时间之前建立的会话都会被拒绝。
func BlockUser(ctx context.Context, userID uint64, blockedUntil time.Time) error {
	client, err := cache.GetRedis()
	if err != nil {
		return err
	}
	// 固定 TTL：不随 blockedUntil 变化。若用 time.Until(blockedUntil)，
	// 调用方传 time.Now() 时 TTL 为负，Redis 直接报错（见 blockedTTL 注释）。
	return client.Set(ctx, blockedKey(userID), blockedUntil.Unix(), blockedTTL).Err()
}

// UnblockUser 解封用户。
func UnblockUser(ctx context.Context, userID uint64) error {
	client, err := cache.GetRedis()
	if err != nil {
		return err
	}
	return client.Del(ctx, blockedKey(userID)).Err()
}

// IsBlocked 检查用户是否被封禁。
// sessionIssuedAt 为会话建立时间戳，0 表示不检查。
//
// fail-closed：只有 redis.Nil（key 不存在）才视为未封禁返回 (false, nil)；
// 其他错误（连接失败、超时等）原样返回 err，由调用方（SessionAuthMiddleware）
// 返回 503，避免 Redis 故障时把封禁用户误放行。
func IsBlocked(ctx context.Context, userID uint64, sessionIssuedAt int64) (bool, error) {
	client, err := cache.GetRedis()
	if err != nil {
		return false, err
	}

	blockedAt, err := client.Get(ctx, blockedKey(userID)).Int64()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if sessionIssuedAt > 0 && blockedAt > sessionIssuedAt {
		return true, nil
	}
	return false, nil
}

// RefreshOnline 刷新用户在线心跳。
// ttl 传 0 时使用默认 5 分钟。
func RefreshOnline(ctx context.Context, userID uint64, ttl time.Duration) error {
	client, err := cache.GetRedis()
	if err != nil {
		return err
	}
	if ttl <= 0 {
		ttl = defaultOnlineTTL
	}
	return client.Set(ctx, onlineKey(userID), "1", ttl).Err()
}

// IsOnline 检查用户是否在线。
func IsOnline(ctx context.Context, userID uint64) (bool, error) {
	client, err := cache.GetRedis()
	if err != nil {
		return false, err
	}
	n, err := client.Exists(ctx, onlineKey(userID)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetOnlineUsers 返回所有在线用户 ID 列表。
func GetOnlineUsers(ctx context.Context) ([]uint64, error) {
	client, err := cache.GetRedis()
	if err != nil {
		return nil, err
	}

	iter := client.Scan(ctx, 0, onlinePrefix+"*", 1000).Iterator()
	var ids []uint64
	for iter.Next(ctx) {
		key := iter.Val()
		var id uint64
		if _, err := fmt.Sscanf(key, onlinePrefix+"%d", &id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, iter.Err()
}
