// Package auth JWT 认证 + Redis 用户会话管理。
//
// JWT 只管认证（验签 + 过期），无状态。
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

// RevokeUserSession 撤销当前用户已签发的 token，并清理会话与在线状态。
func RevokeUserSession(ctx context.Context, userID uint64) error {
	expireHours := GetExpireTime()
	if expireHours <= 0 {
		expireHours = defaultJWTExpireTime
	}
	if err := BlockUser(ctx, userID, time.Now().Add(time.Duration(expireHours)*time.Hour)); err != nil {
		return err
	}
	return DeleteUserSession(ctx, userID)
}

// BlockUser 封禁用户，token 在此时间之前签发的都会被拒绝。
func BlockUser(ctx context.Context, userID uint64, blockedUntil time.Time) error {
	client, err := cache.GetRedis()
	if err != nil {
		return err
	}
	return client.Set(ctx, blockedKey(userID), blockedUntil.Unix(), time.Until(blockedUntil)).Err()
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
// tokenIat 为 token 签发时间戳，0 表示不检查。
func IsBlocked(ctx context.Context, userID uint64, tokenIat int64) (bool, error) {
	client, err := cache.GetRedis()
	if err != nil {
		return false, err
	}

	blockedAt, err := client.Get(ctx, blockedKey(userID)).Int64()
	if err != nil {
		return false, nil
	}

	if tokenIat > 0 && blockedAt > tokenIat {
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
