package navigationservice

// Package navigationservice 实现 navigation 模块业务用例（0-C）。
// navigation 表示公开站点导航，与后台权限菜单 menu 严格隔离。
//
// 本文件只放 Service 结构体、构造函数与全局白名单常量；各能力域用例按文件拆开：
// 以及既有的 navigation_scope.go（逐工程定位）与 navigation_stale.go（失效派发）。

// navigations 在迁移 215 里带 FORCE 策略：不带 app.project_id 的读写在非超级角色下
// **静默落空**（读到 0 行、改 0 行、删 0 行且不报错），而这三个入口的请求里只有 id ——
// 后台导航管理页的「更新 / 详情 / 删除」都属此类（Update / Get / Delete）。
//
// 做法与 page / order 侧同形：**先逐工程独立作用域探测出归属**（id 是主键，跨工程不会
// 重复命中），拿到实体自带的 project_id 之后再进事务 —— 事务内的作用域用它，
// 绝不退回「不限工程」。方向也是安全的：探测本身受策略约束，拿不到别的工程的行。
//
// 为什么探测放在事务**外**：rls.InProjectScope 会新开事务、另取连接，放进已开的事务里
// 会让外层未提交的数据不可见、同表写入还可能自锁（见 pkg/rls.ScopeTx 的说明）。
// 工程归属是稳定属性（导航项不会换工程），所以事务外取到的作用域在事务内依然成立。
//
// 工程清单为空或读不到时**显式失败**：静默返回空清单会把「读不到工程表」伪装成
// 「导航项不存在」—— 那正是本批要消灭的 fail-silent。

// 解析调用方回带的 update_time、必要时取库内当前标题做定位，产出可展示的冲突提示。

// 与 inbound/http 同源：同一份白名单、同一个「key：定位」拆法，差别只在取词入口
//（这里走 pkg/i18n.Translate，拿不到 *gin.Context）。

// 解析失败保留记录自身 title/path：构建期不因单个来源实体缺失而整页失败。

// 背景：pipeline.DepKindMenu 常量一直存在，但全仓没有任何发射点、构建期也没登记这条
// 依赖，于是「改公开站点导航（navigations 表）」之后，凡是把该菜单位置烘进产物的页面
// （page）与自动发布实例（presentation）都会永远停在旧字节 —— 导航在页眉/页脚，
// 全站可见，且全程没有任何报错。严重性高于块/内容那一类：它们的失效面是局部，
// 导航的失效面是整站。
//
// 两半配套（缺一不可）：
//   登记：构建期 core.nav 消费菜单位置时记入产物依赖（pipeline.CompileUsage.UseMenu
//         → page / presentation 的依赖提供者写 page_dependencies / presentation_dependencies）；
//   派发：本文件在导航写操作成功后把「哪个工程哪个位置变了」交给 pipeline.Fanout，
//         由它按依赖表反查受影响的来源并标记 stale（+ 自动重建）。

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/module/navigation/contract"
	"go_wp/internal/module/navigation/dto"
	"go_wp/internal/module/navigation/enums"
	"go_wp/internal/module/navigation/model"
	"go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

// 导航类型白名单（与迁移 285 的 CHECK 约束对齐）。
//
// 桌面与移动端是**两个位置、两份数据**（WP 式：两个位置各绑一条菜单）：
// 两端要的菜单项、层级与交互本就不同，合并成"一套数据两种呈现"解决不了。
const (
	kindHeader       = "header"
	kindHeaderMobile = "header_mobile"
	kindFooter       = "footer"
	kindFooterMobile = "footer_mobile"
)

// 菜单项来源白名单（与迁移 054 的 CHECK 约束对齐）。
const (
	sourceCustom   = "custom"
	sourcePage     = "page"
	sourceArticle  = "article"
	sourceProduct  = "product"
	sourceCategory = "category"
	sourceBlock    = "block"
)

// 打开方式白名单（与迁移 054 的 CHECK 约束对齐）。
const (
	targetSelf  = "self"
	targetBlank = "blank"
)

// Service navigation 模块业务实现。
type Service struct {
	m *navigationmodel.Model
	// projects 站点工程契约（装配层注入）：只带 id 的入口要逐工程探测工程归属（DB-009）。
	// 注入的是契约而不是别的模块的 model：本模块只借「列出工程 id」这一个只读能力。
	projects projectcontract.ProjectService
	// sources 来源实体解析器（装配层注入；未注入时来源项退化为记录自身 title/path）。
	sources navigationcontract.SourceResolver
	// staleMenu 导航变更后的依赖失效派发端口（装配层注入；见 navigation_stale.go）。
	// navigation 不 import page / presentation / pipeline，只把「哪个工程哪个位置变了」
	// 交给注入方；未注入时写操作照常成功但不会让任何产物失效（必需端口，装配期自检拦）。
	staleMenu navigationcontract.MenuStaleDispatcher
}

// NewService 构造（model 与工程契约注入，不持有 *gorm.DB）。
//
// projects 必填：漏接装配时逐工程定位直接失败（不再回退到直读 projects 表）
// 的只读清单（见 navigation_scope.go 的 projectIDs），并记 warning。
func NewService(m *navigationmodel.Model, projects projectcontract.ProjectService) *Service {
	return &Service{m: m, projects: projects}
}

// 编译期契约断言。
var _ navigationcontract.NavigationService = (*Service)(nil)

// projectIDs 定位用的工程清单。
//
// 数量级很小（站点工程），逐个设一次作用域比在数据层引入 BYPASSRLS 连接便宜得多。
func (s *Service) projectIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.m == nil {
		return nil, errors.New(navigationenums.ErrProjectRequired)
	}
	// 契约未注入就是装配漏接，直接失败。
	//
	// 这里原来回退到 `m.ListAllProjectIDs`（直接读 projects 表）。它能工作，但
	// **工程清单的所有权在 project 模块**，navigation 的 model 层只该碰本模块的表；
	// 回退还会让漏接表现为「一切正常」，于是同一份「列出全部工程」的 SQL 在
	// navigation / block / page / order 里各存一份，四份将来会各自漂移。
	if s.projects == nil {
		return nil, errors.New(navigationenums.ErrProjectRequired)
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
		// 一个工程都没有：不是「导航项不存在」，而是没有可作用域的工程。
		return nil, errors.New(navigationenums.ErrProjectRequired)
	}
	return ids, nil
}

// locateNavigation 按导航项 id 定位实体：逐工程独立作用域按 id 取，命中即返回。
//
// id 是主键（跨工程不会重复），所以逐工程探测的结果是确定的；反过来，
// 「不设作用域按 id 直查」在换非超级角色后是静默的 ErrRecordNotFound ——
// 那种形态会让「导航项明明在，却报不存在」。全部未命中返回 gorm.ErrRecordNotFound，
// 由调用方映射成模块自己的 ErrNotFound。
func (s *Service) locateNavigation(ctx context.Context, id string) (*navigationmodel.NavigationEntity, error) {
	ids, err := s.projectIDs(ctx)
	if err != nil {
		return nil, err
	}
	for _, projectID := range ids {
		if ctx.Err() != nil {
			break
		}
		e, gerr := s.m.Get(ctx, projectID, id)
		if gerr == nil {
			return e, nil
		}
		if !errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, gerr
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// —— 乐观锁辅助（本批新增）——

// parseExpectedUpdatedAt 解析调用方回带的 update_time（乐观锁）。
//
// 主形态是 RFC3339Nano（Go 的 time.Time 默认格式，微秒精度）：管理页表单原样回带它，
// 时区偏移一并带着，解析回来是同一时刻（库列是 timestamptz，比较的是时刻不是字符串）。
// 另接受空格分隔的秒级格式（utils.LayoutSecond）：外部脚本按项目既有口径传值时不必转换
// —— 秒级串只在 update_time 恰好落在整秒时命中，属于调用方的责任。
// 空 / nil = 不做校验（既有调用方与内部路径的兼容形态）；给了却解析不了 = 参数错误
// （不能静默忽略：那样并发保护会被一个坏参数悄悄绕过）。
func parseExpectedUpdatedAt(raw *string) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}
	s := strings.TrimSpace(*raw)
	if s == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339Nano, utils.LayoutSecond} {
		if t, perr := time.ParseInLocation(layout, s, time.Local); perr == nil {
			return &t, nil
		}
	}
	return nil, errors.New(navigationenums.ErrInvalidParam)
}

// staleVersionMessage 乐观锁冲突的对外文案：白名单 key + 「：<定位>」。
//
// 用「：」而不是带参的 key|param 形态：定位信息是菜单项标题（任意文本）。
// 页面出口（localizeFacing）按 enums.SplitFacingDetail 的读法拆出 key 与定位、
// 只翻 key —— 结论现在走提示页响应体（shell.RenderJump），不再经 ?err= 回带，
// 但「key：定位」这条拆分协议两侧仍同源。
func staleVersionMessage(title string) string {
	return navigationenums.ErrStaleVersion + "：" + compactLocation(title)
}

// compactLocation 定位信息（菜单项标题）的收敛：去控制字符、限长。
//
// 标题来自用户输入：收敛控制字符并限长（40 字），避免过长的标题把提示页正文
// 撑成一大段 —— 结论现在走响应体，不再受查询参数长度上限约束，但正文仍要可读。
func compactLocation(title string) string {
	title = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, title))
	if title == "" {
		return "-"
	}
	const maxRunes = 40
	if rs := []rune(title); len(rs) > maxRunes {
		return string(rs[:maxRunes]) + "…"
	}
	return title
}

// staleTitle 冲突提示里的定位信息：优先取库内**当前**标题（冲突之后它才是真相），
// 读不到（已被删除 / 作用域异常）时回退调用方手上的旧标题。
func (s *Service) staleTitle(ctx context.Context, e *navigationmodel.NavigationEntity) string {
	if cur, err := s.m.Get(ctx, e.ProjectID, e.ID); err == nil && cur != nil && strings.TrimSpace(cur.Title) != "" {
		return cur.Title
	}
	return e.Title
}

// 编译期断言：业务错误文案出口（工作台检查器这类跨模块消费者用）。
var _ navigationcontract.FacingTexter = (*Service)(nil)

// FacingText 把本模块业务错误转成指定语言下可直接展示的一句话（契约 FacingTexter）。
//
// 与 inbound/http 的两个出口同源：同一份白名单、同一个「key：定位」拆法（判据都在 enums），
// 差别只在取词入口 —— 这里走 pkg/i18n.Translate（消费者拿不到 *gin.Context），
// 那里走 pkg/response 的取词。两处说法因此不会漂。
func (s *Service) FacingText(lang string, err error) string {
	tr := func(key, fallback string) string { return i18n.Translate(key, fallback, lang) }
	if err != nil {
		if msg, ok := navigationenums.HitFacingMessage(err.Error()); ok {
			if key, detail, hasDetail := navigationenums.SplitFacingDetail(msg); hasDetail {
				return tr(key, key) + facingDetailSepForLang(lang) + detail
			}
			if key, param, hasParam := strings.Cut(msg, "|"); hasParam {
				// 带参形态：%s 由这里填（pkg/i18n 只做取词，不做占位符替换）。
				return strings.Replace(tr(key, key), "%s", param, 1)
			}
			return tr(msg, msg)
		}
	}
	return tr(navigationenums.ErrInternal, "操作失败，请稍后重试（细节只进日志）")
}

// facingDetailSepForLang 定位信息的分隔符按语言取（中文全角，其余「: 」）。
// 与 inbound/http 的 facingDetailSep 同一取值口径：提示条最终是给人看的一句话。
func facingDetailSepForLang(lang string) string {
	if strings.HasPrefix(strings.TrimSpace(lang), "zh") {
		return navigationenums.FacingDetailSep
	}
	return ": "
}

// SetSourceResolver 注入来源实体解析器（启动期装配调用一次，之后只读）。
func (s *Service) SetSourceResolver(r navigationcontract.SourceResolver) { s.sources = r }

// SourceGroups 返回该工程可加入菜单的来源候选（未注入解析器时为空）。
func (s *Service) SourceGroups(ctx context.Context, projectID string) (groups []navigationcontract.SourceGroup, err error) {
	if s.sources == nil {
		return nil, nil
	}
	return s.sources.Candidates(ctx, projectID)
}

// resolveSourceTitles 递归把来源实体的标题/URL 写回菜单树。
// 解析失败保留记录自身值（构建期不因单个来源实体缺失而整页失败）。
func (s *Service) resolveSourceTitles(ctx context.Context, projectID string, nodes []*navigationdto.NavigationNode) {
	if s.sources == nil {
		return
	}
	for _, n := range nodes {
		if n.SourceType != sourceCustom && n.SourceID != nil && *n.SourceID != "" {
			title, url, err := s.sources.ResolveSource(ctx, projectID, n.SourceType, *n.SourceID)
			if err != nil {
				logger.Scene("navigation").
					With("sourceType", n.SourceType).With("sourceId", *n.SourceID).
					Warn("导航来源实体解析失败，回退记录自身标题/链接")
			} else {
				if title != "" {
					n.Title = title
				}
				if url != "" {
					n.Path = url
				}
			}
		}
		s.resolveSourceTitles(ctx, projectID, n.Children)
	}
}

// SetMenuStaleDispatcher 注入导航失效派发端口（装配期调用一次，之后只读）。
//
// 未注入时不 panic、只在本服务的写路径上记 error 日志：导航 CRUD 是最常用的后台操作，
// 让它整体不可用换取一条装配期错误不划算；装配缺陷由 routes 的 wiring 自检在启动时
// 直接拦下（见 internal/routers/wiring.go 的 navigation.SetMenuStaleDispatcher）。
func (s *Service) SetMenuStaleDispatcher(d navigationcontract.MenuStaleDispatcher) {
	if s == nil {
		return
	}
	s.staleMenu = d
}

// invalidateMenu 把一次导航写操作的结果派发给依赖扇出。
//
// 时机：**持久化写入之后**。当前三个写入口（Create / Update / Delete）各自只有一次
// 写语句（gorm 自动提交），所以「写成功后调用」就是「提交后调用」。若将来在同一个
// 入口里再加一处持久化写入（按硬规则必须包进同一个事务），这段派发必须留在事务**外面** ——
// 在事务内派发的话，事务一旦回滚，标记已经落库：那是一份没有任何对应内容变更的 stale，
// 表现为受影响页面被反复重建，且查不出是谁改的。
//
// 失败只记日志、不影响导航写入：失效派发是内容写入的后置副作用（与 content 扇出
// 同一口径），让已经成功的导航保存返回失败只会把用户导向「重试」，而重试并不能修好扇出。
func (s *Service) invalidateMenu(ctx context.Context, projectID, kind string) {
	projectID = strings.TrimSpace(projectID)
	kind = strings.TrimSpace(kind)
	if projectID == "" || kind == "" {
		// 键的两个分量缺一就构造不出 menu:{projectID}:{kind}（pipeline.MenuKey），
		// 派发只会打出一批误命中的 key。静默返回：这属于上面的调用方写错了参数。
		return
	}
	if s.staleMenu == nil {
		logger.Scene("navigation").With("project_id", projectID).With("kind", kind).
			Error(errors.New("导航失效派发端口未注入"),
				"导航变更未派发 stale，已发布页面/实例会停在旧导航上；请检查装配（wiring: navigation.SetMenuStaleDispatcher）")
		return
	}
	if err := s.staleMenu.InvalidateMenu(ctx, projectID, kind); err != nil {
		logger.Scene("navigation").With("project_id", projectID).With("kind", kind).
			Error(err, "导航变更后的失效派发失败（产物保持原状，需手动重建）")
	}
}

// invalidateNavigation 把一次「具体菜单项」写操作的结果派发给依赖扇出。
//
// 与 invalidateMenu 并列：按项引用的依赖键是 navigation:{itemID}（pipeline.NavigationKey），
// 只有本方法能命中。失败同样只记日志 —— 与导航位置派发同一口径（内容已写入，
// 标记失败只影响「下次构建会不会主动带上」）。
func (s *Service) invalidateNavigation(ctx context.Context, projectID, navigationID string) {
	projectID = strings.TrimSpace(projectID)
	navigationID = strings.TrimSpace(navigationID)
	if projectID == "" || navigationID == "" {
		return
	}
	if s.staleMenu == nil {
		logger.Scene("navigation").With("project_id", projectID).With("navigation_id", navigationID).
			Error(errors.New("导航失效派发端口未注入"),
				"菜单项变更未派发 stale，按项引用它的页面/实例会停在旧菜单上；请检查装配（wiring: navigation.SetMenuStaleDispatcher）")
		return
	}
	if err := s.staleMenu.InvalidateNavigation(ctx, projectID, navigationID); err != nil {
		logger.Scene("navigation").With("project_id", projectID).With("navigation_id", navigationID).
			Error(err, "菜单项变更后的失效派发失败（产物保持原状，需手动重建）")
	}
}

// InvalidateMenuLabels 按位置逐个派发失效（导航译文工作台写 sys_translation 后用）。
//
// 为什么走这条而不是自己拿实例契约去标：菜单标签的译文与菜单项本身属于**同一个**
// 依赖键（menu:{projectID}:{kind}，构建期 core.nav 消费位置时登记）——
// 译文写入与导航项写入必须打同一个键，否则「改了菜单文字」与「改了菜单项」两条路径
// 会各自维护一套口径，迟早分叉（分叉的表现是页面重建了一次，实例没有）。
// 键的构造与扇出都在发布内核（pipeline.MenuKey / MenuStaleAdapter），这里只转发。
//
// 时机同样是**持久化写入之后**（译文 Upsert 已经落库），失败只记日志 ——
// 与 invalidateMenu 同一口径：译文本身已写好，标记失败只影响「下次构建会不会主动带上」。
func (s *Service) InvalidateMenuLabels(ctx context.Context, projectID string, kinds []string) {
	for _, kind := range kinds {
		if ctx.Err() != nil {
			return
		}
		s.invalidateMenu(ctx, projectID, kind)
	}
}
