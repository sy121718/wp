package userservice

// user_session_store.go — 访客会话的 Redis 台账（设备视图的唯一真源）。
//
// 会话状态**不入库**：Redis 既是「这个请求算不算已登录」的判据，也是设备列表的数据源。
// 原先落库的 user_sessions 表已删除 —— 它只是一份台账，过期即无读路径，却要额外维护
// 行级清理任务（保留期声明 + 每日调度 + 批量删除 SQL），而设备视图本来就允许「过期即消失」。
//
// 两个 key：
//   - gwp:userauth:sess:<sha256(令牌)>  → 会话 JSON（TTL = 会话有效期）
//   - gwp:userauth:user:<userID>:sess   → ZSET，member = 令牌哈希，score = 签发时间（秒）
//
// 一致性口径（这里没有事务，只能靠顺序与自愈）：
//   - 写：先写索引、再写会话（先有归属，后有待读对象）；会话写失败就把索引成员摘掉。
//   - 删：会话本体与索引成员都要删，任一步失败都算失败 —— 「踢不掉」比「索引多一条」严重。
//   - 读：列表时遇到「索引里有、会话已过期」的成员就地 ZREM。TTL 只挂在会话 key 上，
//     索引不会自己变干净，不自愈的话设备列表会一直挂着早就失效的设备。

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"go_wp/pkg/cache"
)

// userSessionIndexPrefix 用户设备索引的 Redis 前缀（与会话前缀同族，便于运维巡查）。
const userSessionIndexPrefix = "gwp:userauth:user:"

// userSessionIndexKey 用户在 Redis 里的设备索引 key。
func userSessionIndexKey(userID uint64) string {
	return userSessionIndexPrefix + strconv.FormatUint(userID, 10) + ":sess"
}

// indexSession 把一个会话哈希写进用户设备索引，并把索引 TTL 续到上限。
//
// TTL 取「记住我」的最大有效期而不是本次会话的 TTL：索引必须在**最长**的那条会话
// 之后才过期，否则短会话先走、索引先消失，长会话还在却从设备列表里掉了。
func indexSession(ctx context.Context, userID uint64, hash string, issuedAt time.Time) error {
	client, err := cache.GetRedis()
	if err != nil {
		return err
	}
	key := userSessionIndexKey(userID)
	if err = client.ZAdd(ctx, key, redis.Z{
		Score:  float64(issuedAt.Unix()),
		Member: hash,
	}).Err(); err != nil {
		return err
	}
	return client.Expire(ctx, key, userSessionRememberTTL).Err()
}

// listSessionHashes 列用户的设备哈希，按签发时间倒序（最近登录的排在前面）。
func listSessionHashes(ctx context.Context, userID uint64) (hashes []string, err error) {
	client, err := cache.GetRedis()
	if err != nil {
		return nil, err
	}
	return client.ZRevRange(ctx, userSessionIndexKey(userID), 0, -1).Result()
}

// unindexSession 从索引摘掉一个哈希（撤销与自愈共用）。
func unindexSession(ctx context.Context, userID uint64, hash string) error {
	client, err := cache.GetRedis()
	if err != nil {
		return err
	}
	return client.ZRem(ctx, userSessionIndexKey(userID), hash).Err()
}

// dropSession 删除一条设备会话：会话本体 + 索引成员。
func dropSession(ctx context.Context, userID uint64, hash string) error {
	if strings.TrimSpace(hash) == "" {
		return nil
	}
	if err := cache.Delete(ctx, sessionKeyByHash(hash)); err != nil {
		return err
	}
	return unindexSession(ctx, userID, hash)
}
