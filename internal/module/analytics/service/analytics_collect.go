package analyticsservice

// analytics_collect.go — 公开打点的收口（形状归一化 + 匿名化 + 静默失败）。
//
// 打点请求的每一个字段都来自浏览器、都可被手工构造，所以这里的原则是：
// **不合法就丢弃，绝不因为坏输入给访客任何反馈，也绝不把坏数据写进统计。**
// 端点层只回 204 空响应；写库失败只记日志（计数是增强能力，不是页面前置）。

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	analyticsdto "go_wp/internal/module/analytics/dto"
	analyticsmodel "go_wp/internal/module/analytics/model"
	"go_wp/pkg/logger"

	"github.com/google/uuid"
)

const (
	// maxPathLen 路径长度上限（与迁移里的 CHECK 约束同值：服务端先截断，数据库再兜底）。
	maxPathLen = 512
	// maxReferrerLen 来源域长度上限。
	maxReferrerLen = 255
)

// langPattern 语言码形状：字母、数字与连字符（与项目其它语言码口径一致）。
var langPattern = regexp.MustCompile("^[A-Za-z0-9-]{1,35}$")

// Collect 记录一次页面浏览。
//
// **静默失败**是设计而不是偷懒：访客点开页面时不需要知道统计有没有写进去；
// 把打点失败暴露成错误，等于让统计故障有机会影响访客体验。
func (s *Service) Collect(ctx context.Context, req *analyticsdto.CollectReq) (err error) {
	if req == nil {
		return nil
	}
	e, ok := s.buildEntity(req)
	if !ok {
		// 形状不合法（伪造的工程 id、不存在的路径形态…）直接丢弃。
		// 这里**不记日志**：公开端点会被扫描器反复打，记日志等于给自己造一个放大攻击面。
		return nil
	}
	if ierr := s.m.Insert(ctx, e); ierr != nil {
		logger.Scene("analytics").With("project", e.ProjectID).Error(ierr, "页面浏览写入失败（已静默降级，不影响访客）")
	}
	return nil
}

// buildEntity 把上报请求归一化为一条待落库记录；ok=false 表示该请求应被丢弃。
func (s *Service) buildEntity(req *analyticsdto.CollectReq) (e *analyticsmodel.PageViewEntity, ok bool) {
	projectID := strings.TrimSpace(req.ProjectID)
	if _, perr := uuid.Parse(projectID); perr != nil {
		// 工程 id 必须是 uuid：一是防伪造，二是别让非法 id 打到数据库才报错
		//（PostgreSQL 的 uuid 解析失败是 22P02，会把「脏数据」变成「写入异常」）。
		return nil, false
	}
	path := sanitizeTrackPath(req.Path)
	if path == "" {
		return nil, false
	}
	lang := strings.TrimSpace(req.Lang)
	if !langPattern.MatchString(lang) {
		lang = ""
	}
	return &analyticsmodel.PageViewEntity{
		ProjectID:    projectID,
		Path:         path,
		Lang:         lang,
		SessionID:    s.hashAnon(req.Session),
		VisitorHash:  s.hashAnon(req.Visitor),
		ReferrerHost: normalizeReferrer(req.Referrer),
		UAClass:      uaClass(req.UserAgent),
		IPHash:       s.hashAnon(req.IP),
		ViewedAt:     s.now(),
	}, true
}

// sanitizeTrackPath 清洗访客上报的打点路径：只保留 pathname 部分并截断到上限。
//
// 语义与 pkg/pathkit.NormalizeRoutePath **不同，刻意不合并**（审计 CQ-012）：
// 这里处理的是访客可控的自由文本 —— 服务端宁可丢弃也不能拒绝（拒绝等于把
// 「哪些路径被记录」变成一个可探测的信号），所以它不返回错误，也不做「同一路径
// 只允许一种写法」的归一：协议相对 URL（"//evil.example.com/x"）原样入库，
// 作为脏数据被看见，而不是被悄悄改写成另一条站内路径。
//
// 查询串与锚点一律丢掉：它们会把 /product?id=1 与 /product?id=2 拆成两条统计，
// 也会把访客带进来的任意内容（含潜在的个人信息）写进数据库。
// 非 "/" 开头、空路径返回空串（调用方据此丢弃）。
//
// 名字里的 sanitize 而不是 normalize：路由路径的归一化只有 pkg/pathkit 一处，
// 两者混名会让「为什么这里不拒绝畸形路径」看起来像缺陷。
func sanitizeTrackPath(raw string) string {
	path := strings.TrimSpace(raw)
	if path == "" {
		return ""
	}
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	if !strings.HasPrefix(path, "/") {
		return ""
	}
	if len(path) > maxPathLen {
		path = path[:maxPathLen]
	}
	return path
}

// normalizeReferrer 归一化来源：只留域名，去协议、路径与大小写差异。
func normalizeReferrer(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	host := raw
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		host = u.Host
	}
	// 构造出来的输入可能带路径（track.js 只发域名，但端点不能假设调用方是自家脚本）。
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	host = strings.ToLower(host)
	if len(host) > maxReferrerLen {
		host = host[:maxReferrerLen]
	}
	return host
}

// botMarkers 爬虫 / 监控类 UA 标记（粗粒度即可：统计要的是「人来的还是机器来的」）。
var botMarkers = []string{
	"bot", "spider", "crawler", "crawl", "slurp", "curl/", "wget", "python-requests",
	"go-http-client", "headlesschrome", "phantomjs", "monitor", "uptimerobot", "pingdom",
	"facebookexternalhit", "semrush", "ahrefs", "bytespider", "petalbot",
}

// uaClass 把 UA 归到粗粒度分类：bot / tablet / mobile / desktop；空 UA 归空串。
//
// 分类由**服务端**从请求头判定，而不是采信上报里的字段：客户端可以自称任何设备，
// 而 UA 头是浏览器自己发的（要伪造得用非浏览器客户端，那类流量本来就该归到 bot）。
func uaClass(ua string) string {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return ""
	}
	lower := strings.ToLower(ua)
	for _, marker := range botMarkers {
		if strings.Contains(lower, marker) {
			return "bot"
		}
	}
	if strings.Contains(lower, "ipad") || strings.Contains(lower, "tablet") ||
		strings.Contains(lower, "playbook") || strings.Contains(lower, "silk") {
		return "tablet"
	}
	if strings.Contains(lower, "mobi") || strings.Contains(lower, "android") ||
		strings.Contains(lower, "iphone") || strings.Contains(lower, "ipod") ||
		strings.Contains(lower, "windows phone") {
		return "mobile"
	}
	return "desktop"
}
