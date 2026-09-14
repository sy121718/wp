package runtimefragment

// fragment_cache.go — 只读 GET 片段 HTML 短 TTL 缓存（PERF-002）。
//
// 缓存键 = 能力类型 + 工程版本号 + 语言 + 语义参数哈希；版本号在商品写操作后递增，
// 旧键自然 miss。Redis 不可用或未初始化时静默降级为实时渲染。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"go_wp/pkg/cache"
)

const (
	fragmentCacheKeyPrefix = "gwp:frag:html:"
	fragmentVerKeyPrefix   = "gwp:frag:ver:"
	defaultFragmentTTL     = 60 * time.Second
	fragmentTTLJitter      = 15 * time.Second
)

// cacheableGETFragments 允许缓存的 GET 能力（不含库存/价格/购物车/账号等实时数据）。
var cacheableGETFragments = map[string]bool{
	"productList":   true,
	"searchResults": true,
}

// BumpFragmentCacheVersion 使某工程下片段 HTML 缓存失效（商品写操作后调用）。
func BumpFragmentCacheVersion(ctx context.Context, projectID string) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || !cache.IsInited() {
		return
	}
	client, err := cache.GetRedis()
	if err != nil {
		return
	}
	_ = client.Incr(ctx, fragmentVerKeyPrefix+projectID).Err()
}

func fragmentCacheVersion(ctx context.Context, projectID string) string {
	if projectID == "" || !cache.IsInited() {
		return "0"
	}
	client, err := cache.GetRedis()
	if err != nil {
		return "0"
	}
	v, err := client.Get(ctx, fragmentVerKeyPrefix+projectID).Result()
	if err != nil {
		return "0"
	}
	return v
}

func fragmentCacheKey(ctx context.Context, specType string, r *Request) (key string, ok bool) {
	if r == nil || !cacheableGETFragments[specType] {
		return "", false
	}
	projectID := strings.TrimSpace(r.Params["projectId"])
	if projectID == "" {
		return "", false
	}
	keys := make([]string, 0, len(r.Params))
	for k := range r.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(specType)
	b.WriteByte('|')
	b.WriteString(projectID)
	b.WriteByte('|')
	b.WriteString(fragmentCacheVersion(ctx, projectID))
	b.WriteByte('|')
	b.WriteString(strings.TrimSpace(r.Lang))
	for _, k := range keys {
		b.WriteByte('|')
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(r.Params[k])
	}
	sum := sha256.Sum256([]byte(b.String()))
	return fragmentCacheKeyPrefix + hex.EncodeToString(sum[:]), true
}

func getCachedFragmentHTML(ctx context.Context, key string) (html string, hit bool) {
	if key == "" || !cache.IsInited() {
		return "", false
	}
	client, err := cache.GetRedis()
	if err != nil {
		return "", false
	}
	payload, err := client.Get(ctx, key).Bytes()
	if err != nil {
		return "", false
	}
	return string(payload), true
}

func setCachedFragmentHTML(ctx context.Context, key, html string) {
	if key == "" || html == "" || !cache.IsInited() {
		return
	}
	client, err := cache.GetRedis()
	if err != nil {
		return
	}
	ttl := defaultFragmentTTL
	if fragmentTTLJitter > 0 {
		ttl = cacheTTLWithJitter(defaultFragmentTTL, fragmentTTLJitter)
	}
	_ = client.Set(ctx, key, html, ttl).Err()
}

// cacheTTLWithJitter 复制 pkg/cache 的抖动逻辑（避免片段包反向依赖未导出 helper）。
func cacheTTLWithJitter(base, jitter time.Duration) time.Duration {
	if base <= 0 || jitter <= 0 {
		return base
	}
	n := time.Now().UnixNano() % int64(jitter)
	return base + time.Duration(n)
}

// renderWithOptionalCache GET 片段在可缓存能力上尝试读/写 Redis。
func renderWithOptionalCache(ctx context.Context, specType string, spec Spec, r *Request) (html string, cacheHit bool, err error) {
	cacheKey, cacheable := fragmentCacheKey(ctx, specType, r)
	if cacheable {
		if cached, hit := getCachedFragmentHTML(ctx, cacheKey); hit {
			return cached, true, nil
		}
	}
	html, err = spec.Render(ctx, r)
	if err != nil || !cacheable {
		return html, false, err
	}
	setCachedFragmentHTML(ctx, cacheKey, html)
	return html, false, nil
}
