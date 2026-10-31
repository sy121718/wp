package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go_wp/pkg/cache"
	"go_wp/pkg/logger"

	"github.com/redis/go-redis/v9"
)

const (
	ContextSessionRevokedKey = "auth_session_revoked"

	userSessionPrefix = "user:session:"
	userBlockedPrefix = "user:blocked:"
	onlinePrefix      = "online:"

	sessionIndexPrefix = "session:sid:"

	defaultSessionTTL = 24 * time.Hour

	defaultRememberMeTTL = 7 * 24 * time.Hour
	defaultOnlineTTL     = 5 * time.Minute

	onlineRefreshMaxTrack = 10000

	blockedTTL = 7 * 24 * time.Hour
)

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

type SessionIndex struct {
	UserID   uint64 `json:"user_id"`
	IssuedAt int64  `json:"issued_at"`
}

func sessionIndexKey(sessionID string) string {
	return sessionIndexPrefix + sessionID
}

func SaveUserSession(ctx context.Context, session *UserSession, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	if err := cache.SetJSON(ctx, sessionKey(session.ID), session, ttl); err != nil {
		return err
	}
	if strings.TrimSpace(session.SessionID) == "" {

		logger.Scene("auth").With("user_id", session.ID).Warn("保存用户会话时缺少 SessionID：句柄索引未写入，该会话无法通过 cookie 认证")
		return nil
	}
	return cache.SetJSON(ctx, sessionIndexKey(session.SessionID), &SessionIndex{
		UserID:   session.ID,
		IssuedAt: time.Now().Unix(),
	}, ttl)
}

func GetSessionIndex(ctx context.Context, sessionID string) (*SessionIndex, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}
	idx, err := cache.GetJSON[SessionIndex](ctx, sessionIndexKey(sessionID))
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &idx, nil
}

func DeleteSessionIndex(ctx context.Context, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	client, err := cache.GetRedis()
	if err != nil {
		return err
	}
	return client.Del(ctx, sessionIndexKey(sessionID)).Err()
}

func RefreshUserSession(ctx context.Context, session *UserSession) error {
	ttl, err := cache.TTL(ctx, sessionKey(session.ID))
	if err != nil || ttl <= 0 {
		ttl = defaultSessionTTL
	}
	return cache.SetJSON(ctx, sessionKey(session.ID), session, ttl)
}

func SessionTTLFor(rememberMe bool) time.Duration {
	if rememberMe {
		return defaultRememberMeTTL
	}
	return defaultSessionTTL
}

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

func DeleteUserSession(ctx context.Context, userID uint64) error {
	client, err := cache.GetRedis()
	if err != nil {
		return err
	}
	return client.Del(ctx, sessionKey(userID), onlineKey(userID)).Err()
}

func RevokeUserSession(ctx context.Context, userID uint64) error {
	if err := BlockUser(ctx, userID, time.Now()); err != nil {
		return err
	}
	return DeleteUserSession(ctx, userID)
}

func BlockUser(ctx context.Context, userID uint64, blockedUntil time.Time) error {
	client, err := cache.GetRedis()
	if err != nil {
		return err
	}

	return client.Set(ctx, blockedKey(userID), blockedUntil.Unix(), blockedTTL).Err()
}

func UnblockUser(ctx context.Context, userID uint64) error {
	client, err := cache.GetRedis()
	if err != nil {
		return err
	}
	return client.Del(ctx, blockedKey(userID)).Err()
}

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

var (
	onlineRefreshMu   sync.Mutex
	onlineLastRefresh = make(map[uint64]time.Time, 256)
)

func RefreshOnline(ctx context.Context, userID uint64, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = defaultOnlineTTL
	}
	minInterval := ttl / 3
	now := time.Now()
	onlineRefreshMu.Lock()
	if last, ok := onlineLastRefresh[userID]; ok && now.Sub(last) < minInterval {
		onlineRefreshMu.Unlock()
		return nil
	}
	if len(onlineLastRefresh) >= onlineRefreshMaxTrack {
		for id, t := range onlineLastRefresh {
			if now.Sub(t) >= ttl {
				delete(onlineLastRefresh, id)
			}
		}
	}
	if len(onlineLastRefresh) < onlineRefreshMaxTrack {
		onlineLastRefresh[userID] = now
	}
	onlineRefreshMu.Unlock()

	client, err := cache.GetRedis()
	if err != nil {
		return err
	}
	return client.Set(ctx, onlineKey(userID), "1", ttl).Err()
}

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
