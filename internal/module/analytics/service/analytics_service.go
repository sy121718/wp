// Package analyticsservice analytics 模块业务实现（BIZ-8 访问计数）。
//
// 前提事实（决定了整个模块的形状）：访问面是**静态产物由 http.Dir 直出**，
// Go 不在访客请求路径上 —— 服务端数不出任何一次访问。所以计数只能由客户端打点，
// 本模块负责的是打点之后的收口：形状归一化、匿名化、落库、聚合查询。
package analyticsservice

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsenums "go_wp/internal/module/analytics/enums"
	analyticsmodel "go_wp/internal/module/analytics/model"
)

var _ analyticscontract.AnalyticsService = (*Service)(nil)

// 业务错误哨兵（文案统一取 analyticsenums）。
var (
	ErrInvalidParam = errors.New(analyticsenums.ErrInvalidParam)
	ErrInvalidRange = errors.New(analyticsenums.ErrInvalidRange)
)

// Service 访问统计服务。
type Service struct {
	m *analyticsmodel.Model
	// pepper 匿名 hash 的盐（装配期注入会话密钥）。
	//
	// 没有它，匿名标识的 sha256 可以被枚举反查 —— IPv4 只有 2^32 种取值，
	// 裸哈希等于把 IP 明文换个写法存下来。
	pepper string
	// now 取当前时间（测试注入固定时钟；生产为 UTC 当下）。
	now func() time.Time
}

// NewService 创建统计服务；pepper 为空时仍按空盐哈希（功能可用，安全性下降，装配层负责注入）。
func NewService(m *analyticsmodel.Model, pepper string) *Service {
	return &Service{m: m, pepper: pepper, now: func() time.Time { return time.Now().UTC() }}
}

// hashAnon 对匿名标识做带盐哈希（只留 16 字节，十六进制 32 位）。
//
// 用分隔符把盐与值隔开：不加分隔时 ("ab", "c") 与 ("a", "bc") 会撞成同一个哈希，
// 攻击者可以靠这种边界碰撞构造出与自己相同的匿名指纹（伪造他人计数）。
func (s *Service) hashAnon(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s.pepper + "\x00" + raw))
	return hex.EncodeToString(sum[:16])
}
