// Package analyticsservice analytics 模块业务实现（BIZ-8 访问计数）。
//
// 前提事实（决定了整个模块的形状）：访问面是**静态产物由 http.Dir 直出**，
// Go 不在访客请求路径上 —— 服务端数不出任何一次访问。所以计数只能由客户端打点，
// 本模块负责的是打点之后的收口：形状归一化、匿名化、落库、聚合查询。
package analyticsservice

import (
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsenums "go_wp/internal/module/analytics/enums"
	analyticsmodel "go_wp/internal/module/analytics/model"
	projectcontract "go_wp/internal/module/project/contract"
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
	// pepper 匿名 hash 的盐（装配期经 ResolveAnonSalt 解析后注入）。
	//
	// 没有它，匿名标识的 sha256 可以被枚举反查 —— IPv4 只有 2^32 种取值，
	// 裸哈希等于把 IP 明文换个写法存下来。
	//
	// 它**不再直接取会话密钥**（SEC-013）：盐与会话签名是两种用途，
	// 共用一个值的后果见 ResolveAnonSalt 的说明。
	pepper string
	// now 取当前时间（测试注入固定时钟；生产为 UTC 当下）。
	now func() time.Time
	// projects 工程契约（装配期注入）。
	//
	// 用途只有一个：拿「有哪些工程」与「各工程的访问明细保留几天」。这两件事的真源都在
	// project 模块（`projects` 表是它的），本模块的 model 只碰 page_views / 汇总表 ——
	// 越过模块边界直查 projects 的后果是同一份「列出全部工程」的 SQL 在多个模块各存一份，
	// 各自演化出不同的排序与过滤。
	//
	// 未注入时逐工程扇出与保留期清理都会失败，而不是静默退化成「没有工程」。
	projects projectcontract.ProjectService
	// retention 工程保留策略读取（装配期注入，与 projects 同源）。
	//
	// 单独一个字段而不是复用 projects：契约里这是两个接口（`ProjectService` 太宽，
	// 往它加方法会波及二十多个测试 fake），装配层注入的同一个实例同时满足两者。
	retention projectcontract.RetentionPolicyReader
}

// SetProjects 注入工程契约（装配期调用）。
//
// 同一个实例同时满足两个接口（`ProjectService` 与 `RetentionPolicyReader`），所以一次注入
// 就把两者都接上。第二次断言失败只可能是「注入的是一份只实现 ProjectService 的替身」
// （测试装配常见）—— 那时留空，保留期清理会以「契约缺失」显式失败，而不是静默跳过。
func (s *Service) SetProjects(projects projectcontract.ProjectService) {
	if s == nil || projects == nil {
		return
	}
	s.projects = projects
	s.retention, _ = projects.(projectcontract.RetentionPolicyReader)
}

// projectIDs 全部站点工程 id（逐工程扇出 / 保留期清理的清单来源）。
//
// 取不到工程就显式失败：静默返回空清单会把「装配漏接」伪装成「没有工程需要汇总 /
// 没有工程需要清理」，那正是最难发现的一类失效（定时任务照常跑、日志一行异常都没有）。
func (s *Service) projectIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.projects == nil {
		return nil, errors.New(analyticsenums.ErrInvalidParam)
	}
	list, err := s.projects.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for i := range list {
		if id := strings.TrimSpace(list[i].ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New(analyticsenums.ErrInvalidParam)
	}
	return ids, nil
}

// NewService 创建统计服务。
//
// pepper 应当是 ResolveAnonSalt 的结果：**不要直接把会话密钥传进来** ——
// 那等于让打点哈希与会话签名、CSRF token、购物车签名共用一把密钥（SEC-013）。
// 保留「按传入值哈希」的形状，是为了让装配层自行决定盐的来源。
// pepper 为空时仍按空盐哈希（功能可用，匿名性下降），装配层负责保证它非空。
func NewService(m *analyticsmodel.Model, pepper string) *Service {
	return &Service{m: m, pepper: pepper, now: func() time.Time { return time.Now().UTC() }}
}

// ResolveAnonSalt 解析打点匿名哈希的盐，并返回该盐是否独立于会话密钥（SEC-013）。
//
// 问题的准确表述：带盐哈希本身没问题，问题在**密钥复用**。会话密钥同时用于签名
// 会话 cookie、签发 CSRF token、给购物车 cookie 签名。把同一个值再拿来当打点盐，
// 会同时锁死两件事：① 任何一处泄露都波及全部用途；② 为了不让历史 IP 哈希断档，
// 运维不敢轮换会话密钥。解耦之后这两件事各自独立。
//
// 盐从哪来（两级）：
//
//  1. 配置项 analytics.pepper（推荐）：显式配置的独立盐，与会话密钥完全无关 ——
//     轮换会话密钥不再影响 IP 哈希。生成方式：openssl rand -hex 32。
//  2. 未配置时由会话密钥经 HKDF-SHA256 派生：单向派生意味着盐泄露推不出会话密钥
//     （密钥复用已消除），也**没有写死常量盐**（那等于把密钥复用换成密钥公开）。
//     但盐的取值仍随会话密钥变化：轮换会话密钥会让新旧 IP 哈希不可比 ——
//     这是「明确接受」的降级形态，装配层会记一条告警，生产应配置 analytics.pepper。
//
// 轮换与历史哈希：盐在进程启动时解析一次，轮换 = 改配置 + 重启，不影响已落库的行。
// 轮换后历史行的 ip_hash 由旧盐产生，与新行不可互相比对（无法反向重算 ——
// 哈希的单向性正是它的用途）。当前 ip_hash 没有读取方：只落库、随保留期清理
// （见 analytics_retention.go），所以轮换的实际代价为零。将来若引入「按 ip_hash 去重」
// 的统计口径，必须同时支持多代盐的候选匹配（那时才需要 analytics.pepper_previous
// 一类的旧盐列表），否则跨轮换的同一个人会被算成两个人。
func ResolveAnonSalt(independent, sessionSecret string) (salt string, independentSalt bool) {
	if v := strings.TrimSpace(independent); v != "" {
		return v, true
	}
	return deriveAnonSalt(sessionSecret), false
}

// anonSaltInfo HKDF 的 info 标签：把派生结果绑定到「打点匿名哈希」这一个用途。
//
// 会话 / CSRF / 购物车将来若也要派生，必须各用各的标签；共用标签就是把
// 密钥复用换个写法再犯一次。
const anonSaltInfo = "go_wp/analytics/anon-hash/v1"

// anonSaltLen 派生盐的字节长度（32 字节 → 十六进制 64 位）。
const anonSaltLen = 32

// anonSaltMinLen 显式配置的独立盐的最短长度（与会话密钥同一门槛：32 字符）。
//
// 短盐是可枚举的：IPv4 只有 2^32 种取值，盐一弱，带盐哈希就退回裸哈希。
// 这里只告警不拒绝启动 —— 拒绝会把「盐配短了」升级成「站点起不来」，代价不对等；
// 弱盐的实际后果是可被反查，而装配期日志足以让人发现。
const anonSaltMinLen = 32

// WeakAnonSalt 判断显式配置的独立盐是否偏弱（过短）。
//
// 只对独立盐有意义：派生盐恒为 64 位十六进制，不会弱。
func WeakAnonSalt(salt string) bool {
	return len(strings.TrimSpace(salt)) < anonSaltMinLen
}

// deriveAnonSalt 由会话密钥派生打点盐（HKDF-SHA256）。
//
// 返回十六进制串而不是原始字节：盐最终以字符串拼进 sha256 的输入，
// 统一成 hex 让「配置的独立盐」与「派生的盐」在调用方看来是同一种东西。
// 空会话密钥返回空串（保持旧行为：空盐下功能可用，匿名性下降）。
func deriveAnonSalt(sessionSecret string) string {
	if sessionSecret == "" {
		return ""
	}
	key, err := hkdf.Key(sha256.New, []byte(sessionSecret), nil, anonSaltInfo, anonSaltLen)
	if err != nil {
		// 参数都是常量，HKDF 只在密钥长度非法时报错，这里不可达。
		return ""
	}
	return hex.EncodeToString(key)
}

// hashAnon 对匿名标识做带盐哈希（只留 16 字节，十六进制 32 位）。
//
// 用分隔符把盐与值隔开：不加分隔时 ("ab", "c") 与 ("a", "bc") 会撞成同一个哈希，
// 攻击者可以靠这种边界碰撞构造出与自己相同的匿名指纹（伪造他人计数）。
//
// 盐由装配层经 ResolveAnonSalt 解析（SEC-013）：独立盐配置下轮换会话密钥
// 不改变本函数的输出，历史 IP 哈希因此不受会话密钥轮换影响。
func (s *Service) hashAnon(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s.pepper + "\x00" + raw))
	return hex.EncodeToString(sum[:16])
}
