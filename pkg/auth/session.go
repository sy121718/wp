// Package auth Session + Cookie 认证 + Redis 用户会话管理。
//
// Cookie 会话（cookie_session.go）只管认证（识别当前用户），无服务端状态。
// Redis 管理用户信息、封禁标记、在线心跳。
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
	// ContextSessionRevokedKey 标记当前请求已注销，禁止中间件续签和刷新在线状态。
	ContextSessionRevokedKey = "auth_session_revoked"

	userSessionPrefix = "user:session:"
	userBlockedPrefix = "user:blocked:"
	onlinePrefix      = "online:"

	// sessionIndexPrefix 会话句柄索引的前缀（P1 句柄化）：
	// session:sid:{sessionID} → SessionIndex{user_id, issued_at}。
	// cookie 里只有句柄，身份必须能从句柄反查回来，这就是那张映射表。
	sessionIndexPrefix = "session:sid:"

	defaultSessionTTL = 24 * time.Hour
	// defaultRememberMeTTL 勾选「记住我」时 Redis 会话的存活时长。
	// 必须与 cookie 的有效期一致（cookie_session.go rememberMeSessionMaxAge = 7d），
	// 否则会出现「cookie 还在、Redis 会话已过期」的静默掉线：用户在第 25 小时
	// 之后回到后台，浏览器仍带着登录 cookie，却被判定未登录并跳回登录页。
	defaultRememberMeTTL = 7 * 24 * time.Hour
	defaultOnlineTTL     = 5 * time.Minute
	// onlineRefreshMaxTrack 进程内心跳节流表容量上限（PERF-005）。
	onlineRefreshMaxTrack = 10000

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

// SessionIndex 会话句柄 → 身份的映射（P1 句柄化）。
//
// IssuedAt 也放在这里（而不是 cookie 里）有两个理由：它是封禁判断的基准
// （IsBlocked 的 blockedAt > sessionIssuedAt），放服务端才可信；
// 而且 cookie 因此可以只留一个句柄，不含任何语义字段。
type SessionIndex struct {
	UserID   uint64 `json:"user_id"`
	IssuedAt int64  `json:"issued_at"`
}

func sessionIndexKey(sessionID string) string {
	return sessionIndexPrefix + sessionID
}

// SaveUserSession 将用户会话信息写入 Redis，**同时写入句柄索引**。
// ttl 传 0 时使用默认 24h。
//
// 明细与索引必须同批写：cookie 里只有句柄，缺了索引的会话就是一个解析不出来的句柄 ——
// 表现为「刚登录就未登录」，而且不报错。放在同一个函数里正是为了让每个登录调用点
// 不必各写一遍（漏掉一处的表现就是那条登录路径静默失效）。
func SaveUserSession(ctx context.Context, session *UserSession, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	if err := cache.SetJSON(ctx, sessionKey(session.ID), session, ttl); err != nil {
		return err
	}
	if strings.TrimSpace(session.SessionID) == "" {
		// 没有句柄就没有索引可写，cookie 侧也无法认证 —— 这是调用方的缺陷，
		// 留痕而不是静默（静默的表现是「登录成功但立刻失效」，极难定位）。
		logger.Scene("auth").With("user_id", session.ID).Warn("保存用户会话时缺少 SessionID：句柄索引未写入，该会话无法通过 cookie 认证")
		return nil
	}
	return cache.SetJSON(ctx, sessionIndexKey(session.SessionID), &SessionIndex{
		UserID:   session.ID,
		IssuedAt: time.Now().Unix(),
	}, ttl)
}

// GetSessionIndex 按会话句柄取身份索引。不存在时返回 (nil, nil)。
//
// fail-safe：句柄无效 / 已过期 / 已登出都返回 nil，由调用方按未登录处理。
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

// DeleteSessionIndex 删除会话句柄索引（登出路径）。
//
// 强制下线（RevokeUserSession）只拿到 userID、拿不到句柄，因此它会留下一个**残留索引**：
// 那条索引仍指向该 userID，但对应的 user:session:{userID} 已被删除，所以校验时
// GetUserSession 返回 nil → 依旧被拒（fail-safe）。残留只是脏数据，随 TTL 自然消失；
// 而同账号重新登录会覆盖 user:session:{userID}，旧句柄因 SessionID 不匹配继续被拒。
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

// RefreshUserSession 覆盖会话内容但**保持剩余 TTL 不变**。
//
// 刻意**不写会话索引**：索引里的 issued_at 是封禁判断的基准
// （IsBlocked 的 blockedAt > sessionIssuedAt），回填资料时把它刷成当前时间会让
// 已生效的封禁失效 —— 表现为「强制下线之后点一下个人资料又回来了」。
//
// 登录之外的路径（读取个人信息时顺带回填 Redis）不能用 SaveUserSession(…, 0)：
// 那会把「记住我」的 7 天有效期悄悄缩回默认 24h，用户在第 25 小时被踢回登录页。
// key 已不存在（会话已过期）时按默认 24h 重建，与登录语义一致。
func RefreshUserSession(ctx context.Context, session *UserSession) error {
	ttl, err := cache.TTL(ctx, sessionKey(session.ID))
	if err != nil || ttl <= 0 {
		ttl = defaultSessionTTL
	}
	return cache.SetJSON(ctx, sessionKey(session.ID), session, ttl)
}

// SessionTTLFor 返回会话在 Redis 中的存活时长（与登录 cookie 的有效期一一对应）。
//
// 登录侧必须用它而不是传 0：传 0 会落到 24h 默认值，而勾选「记住我」时
// cookie 写的是 7 天，两者不一致 → 第 25 小时起用户带着有效 cookie 被判未登录。
func SessionTTLFor(rememberMe bool) time.Duration {
	if rememberMe {
		return defaultRememberMeTTL
	}
	return defaultSessionTTL
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

// onlineLastRefresh 进程内记录上次写 Redis 心跳的时间，用于节流（PERF-005）。
var (
	onlineRefreshMu   sync.Mutex
	onlineLastRefresh = make(map[uint64]time.Time, 256)
)

// RefreshOnline 刷新用户在线心跳。
// ttl 传 0 时使用默认 5 分钟。
//
// 后台 HTMX 局部刷新会让每个已认证请求都触发一次写；只在距上次刷新超过 ttl/3
// 时才写 Redis，在线判定误差不超过该间隔。
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
