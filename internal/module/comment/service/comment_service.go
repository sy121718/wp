// Package commentservice — comment 模块的服务层（状态机 / 校验 / 限流 / 归口文案）。
//
// 分层与边界：
//
//	· model  —— 表访问（读写 comments，全部在 rls 工程作用域内）；
//	· service —— 业务规则：谁能发（登录）、发什么算合法（长度 / 形状 / 控制字符）、
//	             发得多快要挡（限流）、落什么状态（先审后发）、谁能审（后台控制面）；
//	· inbound —— 协议：HTTP 参数绑定、CSRF、响应格式（页面 / JSON）。
//
// service **不持有 *gorm.DB**（事务边界经 model.TransactionScoped 表达），
// 也不 import 别的模块的 service / model —— 需要实体侧的差异化规则时只问
// contract.EntityPolicy（收窄端口，见该接口的注释）。
package commentservice

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	commentcontract "go_wp/internal/module/comment/contract"
	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	commentmodel "go_wp/internal/module/comment/model"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// errScene 结构化日志场景名（三件套的第三件：原文只进日志）。
const errScene = "comment"

// Service comment 模块的服务实现。
type Service struct {
	model *commentmodel.Model

	// entityTypes 已注册的可评论实体类型（**由拥有该实体的模块声明**，装配期传入）。
	//
	// 保存顺序与 map 两份：顺序服务「后台筛选下拉」（要确定性），map 服务「白名单判定」。
	entityTypes []commentcontract.EntityType
	byType      map[string]commentcontract.EntityType

	// policy 差异化规则端口（可缺）。未注入 = 放行（判断与理由见 contract.EntityPolicy）。
	policy commentcontract.EntityPolicy

	// 提交限流（进程内令牌桶，形态与 internal/middleware/builtin 的 IP 限流同源）。
	identityLimiter *submitLimiter
	sourceLimiter   *submitLimiter

	// now 时间源（测试可替换；默认 time.Now）。
	now func() time.Time
}

// 编译期断言：本实现满足对外契约与片段端口。
var (
	_ commentcontract.CommentService = (*Service)(nil)
	_ commentcontract.FragmentPort   = (*Service)(nil)
)

// NewService 构造（参数直传，不用 Deps 结构体；端口经 setter 注入）。
//
// entityTypes 为**空的**时候本模块整体不可用（任何提交与列表都会被拒），
// 这是刻意的 fail-closed：没人注册实体类型说明装配漏了，而「静默接受任意类型」
// 会让白名单形同虚设（同 AGENTS.md 的「没有 tag 的字段不在白名单里」）。
func NewService(db *commentmodel.Model, entityTypes []commentcontract.EntityType) *Service {
	s := &Service{
		model:           db,
		entityTypes:     append([]commentcontract.EntityType(nil), entityTypes...),
		byType:          make(map[string]commentcontract.EntityType, len(entityTypes)),
		identityLimiter: newLimiter(submitPerIdentity, submitWindow),
		sourceLimiter:   newLimiter(submitPerSource, submitWindow),
		now:             time.Now,
	}
	for _, et := range s.entityTypes {
		s.byType[et.Type] = et
	}
	return s
}

// SetEntityPolicy 注入差异化规则端口（装配期调用；可缺，未注入即放行）。
//
// 形态与 cart.SetMembershipReader / runtimefragment.Deps.SitePageResolver 一致：
// setter + 调用方判空。装配层负责在未注入时留一条 Warn（见 wiring.go 的清单条目）。
func (s *Service) SetEntityPolicy(p commentcontract.EntityPolicy) { s.policy = p }

// SetTimeSource 替换时间源（测试用；不传即 time.Now）。
//
// 只影响「新行的 create_time / update_time」这类由 service 决定的值 ——
// 排序与分页仍以数据库里的列为准。
func (s *Service) SetTimeSource(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

// IsRegisteredEntityType 实体类型是否已注册（fail-closed：未注册一律拒）。
func (s *Service) IsRegisteredEntityType(entityType string) bool {
	_, ok := s.byType[strings.TrimSpace(entityType)]
	return ok
}

// EntityTypeLabels 已注册实体类型的展示名（顺序 = 注册顺序）。
//
// tr 由调用点给（后台页用 shell.TranslateFor）：本包不依赖 gin / shell，
// 取词函数一律从 inbound 传进来（同 productcontract.EntityTypeLabel 的约定）。
func (s *Service) EntityTypeLabels(tr func(key, fallback string) string) []commentdto.EntityType {
	out := make([]commentdto.EntityType, 0, len(s.entityTypes))
	for _, et := range s.entityTypes {
		label := et.Label.Fallback
		if tr != nil {
			label = tr(et.Label.Key, et.Label.Fallback)
		}
		out = append(out, commentdto.EntityType{Type: et.Type, Label: label})
	}
	return out
}

// FacingText 契约出口：把业务错误转成一句可展示文案（三件套的读侧）。
//
// 命中白名单 → 按 lang 取词；未命中 → 记结构化日志 + 归口文案（原文只进日志）。
func (s *Service) FacingText(lang string, err error) string {
	if err == nil {
		return ""
	}
	// 消费方的差异化规则拒绝（PolicyDeniedError）：按**请求语言**取它自己的词条。
	//
	// 为什么不能拿去撞本模块白名单：那不是本模块的词条，一定撞不上，
	// 最终会把「买过才能评」这类**可行动**的原因归口成「操作失败」。
	// 为什么必须是「key + 兜底」而不是一个成品字符串：文案是消费方的、语言只有这里知道 ——
	// 只给中文成品的话，英文站点会看到一句中文。
	if denied, ok := asPolicyDenied(err); ok {
		fallback := strings.TrimSpace(denied.Fallback)
		key := strings.TrimSpace(denied.Key)
		if fallback == "" {
			// 消费方没给中文兜底：回落本模块的归口业务文案。
			// 绝不返回空串（页面上会是一块空白），也不用 ErrInternal（吞掉可行动原因）。
			fallback = i18n.Translate(commentenums.ErrNotAllowed, "当前不满足这条内容的评论条件。", lang)
			key = ""
		}
		if key == "" {
			return fallback
		}
		return i18n.Translate(key, fallback, lang)
	}
	msg := err.Error()
	if _, ok := commentenums.HitFacingMessage(msg); ok {
		return i18n.Translate(msg, msg, lang)
	}
	logger.Scene(errScene).Error(err, "comment 契约出口遇到未归类的错误（原文只进日志）")
	return i18n.Translate(commentenums.ErrInternal, "操作失败，请稍后重试（细节只进日志）", lang)
}

// —— 内部：校验 ——

// validateProject 校验工程 id（合法 uuid；缺失 / 形状非法都归到 ErrProjectRequired）。
//
// 单独一个函数的原因：后台审核队列只需要工程（它按状态 / 类型 / 关键词筛，不需要实体 id），
// 而公开列表需要完整三元组 —— 两块共用的那一半应当只有一份实现。
func (s *Service) validateProject(projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || len([]rune(projectID)) > 64 {
		return errParam(commentenums.ErrProjectRequired)
	}
	if _, err := uuid.Parse(projectID); err != nil {
		return errParam(commentenums.ErrProjectRequired)
	}
	return nil
}

// validateTarget 校验 (工程, 实体类型, 实体 id) 三元组。
//
// 与「不信任客户端」相关的三条都在这里：
//   - 工程必须是合法 uuid（策略谓词里有 ::uuid 强转，非法值会让 PG 报
//     "invalid input syntax for type uuid"，错误归属变得像数据库故障）；
//   - 实体类型必须在**注册表**内（白名单，fail-closed）；
//   - 实体 id 必须是受限字符集（长度 + 字符白名单），而不是任意串 ——
//     它会被写进列、将来可能出现在链接里，形状校验是唯一能在入口挡住注入类输入的地方。
func (s *Service) validateTarget(projectID, entityType, entityID string) error {
	if err := s.validateProject(projectID); err != nil {
		return err
	}
	entityType = strings.TrimSpace(entityType)
	if entityType == "" {
		return errParam(commentenums.ErrEntityTypeUnknown)
	}
	if len(entityType) > commentenums.MaxEntityTypeLen || !s.IsRegisteredEntityType(entityType) {
		return errParam(commentenums.ErrEntityTypeUnknown)
	}
	if !isSafeEntityID(entityID) {
		return errParam(commentenums.ErrEntityIDInvalid)
	}
	return nil
}

// isSafeEntityID 实体 id 的形状校验：非空、长度受限、字符集受限。
//
// 允许的字符集是 [A-Za-z0-9_-]：uuid、雪花 id、slug 都在其中；
// 刻意**不允许空格与路径分隔符** —— 这个值将来可能进链接与属性，宽字符集是
// 注入类缺陷的温床（同 internal/module/*/handler 的既有判据）。
func isSafeEntityID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > commentenums.MaxEntityIDLen {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// normalizeBody 清洗并校验正文（客户端传来的任何东西都不信任）。
//
// 清洗只做一件事：去掉控制字符（保留 \n / \r / \t）。评论是纯文本，展示层
// 由 Jet 默认转义 —— 所以这里**不做 HTML 转义**（转义放进库会让「&amp;」这种
// 字面量被存下来，之后无论怎么渲染都是错的）。
func normalizeBody(body string) (string, error) {
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, body)
	cleaned = strings.TrimSpace(cleaned)
	if utf8.RuneCountInString(cleaned) < commentenums.MinBodyLen {
		return "", errParam(commentenums.ErrBodyRequired)
	}
	if utf8.RuneCountInString(cleaned) > commentenums.MaxBodyLen {
		return "", errParam(commentenums.ErrBodyTooLong)
	}
	return cleaned, nil
}

// normalizePaging 归一页码与每页条数（不信任请求方）。
func normalizePaging(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = commentenums.DefaultPageSize
	}
	if pageSize > commentenums.MaxPageSize {
		pageSize = commentenums.MaxPageSize
	}
	return page, pageSize
}

// errParam 构造一条**可展示**的业务错误（值即 enums 的 key）。
//
// 形态约定：白名单命中靠**整串相等**（见 enums.HitFacingMessage），
// 所以业务错误一律用 errors.New(key)，不要 fmt.Errorf 拼上下文 ——
// 拼过的串不再等于 key，会被归口成「系统内部错误」（CQ-010 的教训）。
func errParam(key string) error { return &facingError{key: key} }

// facingError 可对外的业务错误（值是一条命中白名单的 key）。
type facingError struct{ key string }

func (e *facingError) Error() string { return e.key }
