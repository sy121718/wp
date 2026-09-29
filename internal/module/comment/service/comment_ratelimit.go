package commentservice

// comment_ratelimit.go — 提交端点的业务级限流（防刷）。
//
// 与 internal/middleware/builtin 的 IP 限流的分工：
//
//   · 中间件那层是**通用**保护（/api/* 与页面的每分钟 N 次），它按 IP 计数、
//     不知道「评论」这件事，也无法按**身份**计数；
//   · 这一层是**能力级**保护：同一账号每分钟最多几条、同一来源每分钟最多几条。
//
// 为什么必须按身份而不是只按 IP：一个脚本用一百个账号刷，按 IP 计数会把它当成
// 「一个正常用户」（每个账号都远未触顶），而按账号计数能在第一条之后就压住它。
// 反过来，只按账号也不行：注册是免费的，换账号的成本是零 —— 两条一起才算防刷。
//
// 为什么是**进程内**令牌桶（而不是 Redis 计数）：与既有 IP 限流同一条口径
// （rate_limit.go 已写明「限流状态为进程内令牌桶，重启即重置；多实例部署需外置存储」）。
// 这里刻意与它保持一致，而不是给评论单独发明一套跨实例计数 —— 两套不同口径的限流
// 出现在同一个请求链上，运维很难解释「为什么这里限得住、那里限不住」。
//
// 口径与代价：**漏过**的窗口是「多实例部署下每个实例各算一份额度」，
// 方向是宽松（不是误伤正常用户）；而它与审核队列一起构成两道闸（先审后发），
// 所以宽松的偏差可接受。真正需要严格全局配额时，把这里换成 pkg/cache 的
// Redis 计数器即可（service 内部唯一改动点）。

import (
	"strconv"
	"time"

	"github.com/didip/tollbooth/v7"
	"github.com/didip/tollbooth/v7/limiter"

	commentenums "go_wp/internal/module/comment/enums"
)

const (
	// submitPerIdentity 同一账号在 submitWindow 内允许的提交次数。
	//
	// 取值依据：正常访客写一条评论要几十秒（读 + 打字 + 提交），5 条/分钟已经远高于
	// 真实节奏；而对脚本来说，这个上限把「一分钟灌几千条」压到 5 条。
	submitPerIdentity = 5
	// submitPerSource 同一来源（IP 哈希）在 submitWindow 内允许的提交次数。
	//
	// 比账号额度宽 3 倍：同一个出口 IP 后面可能坐着多个人（公司网络、校园网、NAT），
	// 按账号额度卡来源会误伤「同事同时在同一个站评论」；而 15 条/分钟仍然能挡住
	// 「一个脚本换了十几个账号继续刷」。
	submitPerSource = 15
	// submitWindow 限流窗口（两条额度共用同一个窗口长度）。
	submitWindow = time.Minute
	// limiterBucketTTL 不活跃 key 的令牌桶存活时间（与 builtin.rateLimitBucketTTL 同值同理由：
	// 长期不活跃的 key 不该在内存里无限累积）。
	limiterBucketTTL = 10 * time.Minute
)

// submitLimiter 能力级令牌桶（tollbooth 的薄封装）。
//
// 为什么不让 service 直接持有 *limiter.Limiter：LimitReached 的语义（「取一次令牌，
// 返回是否已耗尽」）与「限流判定」这件事之间差一层翻译 —— 封一层之后，测试可以
// 直接构造一个很小的额度来验证第 N+1 次被拒，而不必理解 tollbooth 的 API。
type submitLimiter struct {
	lmt *limiter.Limiter
}

// newLimiter 构造一个按 key 计数的令牌桶。
//
// burst 必须显式设为 limit：tollbooth 默认 burst=0 会让**所有**请求立即被拒
// （rate_limit.go 的注释记过这个坑）。
func newLimiter(limit int, window time.Duration) *submitLimiter {
	if limit <= 0 || window <= 0 {
		// 非法参数：不构造限流器（判定恒放行）。
		return &submitLimiter{}
	}
	lmt := tollbooth.NewLimiter(float64(limit)/window.Seconds(), &limiter.ExpirableOptions{
		DefaultExpirationTTL: limiterBucketTTL,
	})
	lmt.SetBurst(limit)
	return &submitLimiter{lmt: lmt}
}

// allow 取一次令牌；返回 false 表示额度已耗尽（本次请求应被拒绝）。
func (l *submitLimiter) allow(key string) bool {
	if l == nil || l.lmt == nil || key == "" {
		return true
	}
	return !l.lmt.LimitReached(key)
}

// checkSubmitRate 提交前的限流判定（命中任一维度即拒绝）。
//
// 顺序：**身份额度先判、来源额度后判**。两个额度都被消耗时才拒绝，
// 这是刻意的：只判身份会让「多账号刷」完全绕过，只判来源会让 NAT 后面的正常
// 用户互相拖累。
//
// ipHash 为空时跳过来源维度（拿不到 IP 的请求在真实部署里不该存在，
// 但测试与反代误配下会出现 —— 此时按身份判仍然有效，不因为一个取不到的字段全放开）。
func (s *Service) checkSubmitRate(userID uint64, ipHash string) error {
	if !s.identityLimiter.allow("u:" + strconv.FormatUint(userID, 10)) {
		return errParam(commentenums.ErrRateLimited)
	}
	if ipHash != "" && !s.sourceLimiter.allow("ip:"+ipHash) {
		return errParam(commentenums.ErrRateLimited)
	}
	return nil
}
