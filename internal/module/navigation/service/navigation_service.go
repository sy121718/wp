package navigationservice

// Package navigationservice 实现 navigation 模块业务用例（0-C）。
// navigation 表示公开站点导航，与后台权限菜单 menu 严格隔离。
//
// 本文件只放 Service 结构体、构造函数与全局白名单常量；各能力域用例按文件拆开：
// 以及既有的 navigation_service.go（逐工程定位）与 navigation_stale.go（失效派发）。

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

	"github.com/google/uuid"
	"gorm.io/gorm"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/rls"
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
	// sourceRowsPort 共享来源索引的行端口注入点（仅测试设置；nil = 走 model 直传）。
	//
	// 见 navigation_translation_source.go 的 OpenSourceIndex / sourceProvider：
	// 它存在只是为了让「某个菜单位置读失败」可被单测注入，装配与线上行为不受影响。
	sourceRowsPort sourceRows
	// rowsTx 事务内行读取端口（译文写路径的来源索引用；见 navigation_translation_source_tx.go）。
	//
	// 为什么单独一个字段而不是复用 sourceRowsPort：那个端口的读法是「自开作用域」的
	// （rls.InProjectScope 会新开事务、另取连接），写路径要求读在**同一事务**上 ——
	// 两者语义不同（快照不同），共用一个字段等于让调用方猜「这次拿到的是哪一种」。
	// nil 时写路径的 OpenSourceIndexTx 显式失败（不返回空索引）。
	rowsTx sourceRowsTxPort
	// writeTx 菜单 CRUD 的事务边界（写路径专用；nil = 未装配，写入口 fail closed）。
	//
	// 为什么 CRUD 也要事务：译文写路径已把「译文 + 页 stale + 实例 stale + 可靠入队」
	// 收进同一事务并持有工程级内容写锁（i18n.LockProjectContentWrite）。菜单 CRUD 若仍
	// 各写各的自动提交，锁就只串行化了一半的写者 —— 译文事务在锁内读到的来源是「提交前
	// 快照」，并发新增到同 hash 的另一个位置不会被派发 stale，而那一侧没有任何补偿入口。
	// 所以 CRUD 必须进同一把锁、同一个事务，并在事务内完成失效标记。
	writeTx TxRunner
	// menuPages / menuItems / menuQueue 是 CRUD 同事务失效的三个端口（与译文写路径同源：
	// 消费者侧最小形状，由装配层注入）。缺任一端口即装配缺陷，写入口 fail closed ——
	// 只写数据不派发失效正是本批要消灭的「保存成功、线上不变」。
	menuPages navigationcontract.PageStalePort
	// menuInstances 自动发布实例的按位置失效（presentation 模块）。
	//
	// 与译文写路径的 w.instances 是**同一个端口**：两条写链对「哪个位置要让实例重编」
	// 的口径必须一致，否则同一次变更在页面上生效、在实例上停在旧 HTML。
	menuInstances navigationcontract.PresentationStalePort
	menuItems     navigationcontract.MenuItemStalePort
	// menuItemInstances 自动发布实例的按**菜单项**失效（presentation 模块）。
	//
	// 与 menuItems（手工页面按项）成对：core 的 UseNavigation 只记 navigation:{itemID}，
	// 只登记了这一支的实例产物既不被位置键命中，也不会被页面的项键标记代替。
	menuItemInstances navigationcontract.MenuItemPresentationStalePort
	menuQueue         navigationcontract.MenuStaleEnqueuePort
	// staleMenu 导航变更后的依赖失效派发端口（装配层注入；见 navigation_stale.go）。
	// navigation 不 import page / presentation / pipeline，只把「哪个工程哪个位置变了」
	// 交给注入方；未注入时写操作照常成功但不会让任何产物失效（必需端口，装配期自检拦）。
	staleMenu navigationcontract.MenuStaleDispatcher
}

// NewService 构造（model 与工程契约注入，不持有 *gorm.DB）。
//
// projects 必填：漏接装配时逐工程定位直接失败（不再回退到直读 projects 表）
// 的只读清单（见 navigation_service.go 的 projectIDs），并记 warning。
// rowsTx 的生产实现在这里**直接注入**：它只依赖 m，没有任何跨模块能力，构造器一眼
// 看得出它不为空。此前该字段只在单测里被手动填充，生产实例上恒为 nil ——
// OpenSourceIndexTx 因此对每一次真实写译文都返回 ErrProjectRequired，而单测（自己塞桩）
// 全绿：这是「测试桩掩盖了生产断点」的典型形态。
func NewService(m *navigationmodel.Model, projects projectcontract.ProjectService) *Service {
	s := &Service{m: m, projects: projects}
	if m != nil {
		s.rowsTx = navigationRowsModelTx{m: m}
		// 事务边界同样只依赖 m：写入口在端口齐备前会 fail closed（见 writePortsReady）。
		s.writeTx = m
	}
	return s
}

// NavigationWriteDeps 菜单 CRUD 同事务失效需要的跨模块端口（构造期注入一次）。
//
// 三个端口与译文写路径**同一组真实实现**（装配层建一次、两个构造点共用），因此两条写
// 路径对「哪个位置失效、哪些页面要重建」的口径不可能漂移。
type NavigationWriteDeps struct {
	// Pages 手工页面按位置失效（page 模块）。
	Pages navigationcontract.PageStalePort
	// Instances 自动发布实例按位置失效（presentation 模块）。
	//
	// 必需端口，不是可选优化：实例的旧编译若不在同一事务里推进 input_version，
	// 它晚到的产物会盖掉新 HTML 并把 stale 清掉 —— 可靠入队只保证「会重建一次」，
	// 保证不了「旧编译晚到不能覆盖新结果」。版本屏障只能与菜单写同事务给出。
	Instances navigationcontract.PresentationStalePort
	// Items 手工页面按菜单项失效（page 模块）。
	Items navigationcontract.MenuItemStalePort
	// ItemInstances 自动发布实例按菜单项失效（presentation 模块）。
	//
	// 必需端口：按项引用的实例既不在位置键反查里，也没有同事务屏障时，
	// 它的旧编译晚到会通过激活谓词覆盖新 HTML 并清掉 stale。
	ItemInstances navigationcontract.MenuItemPresentationStalePort
	// Queue 自动发布实例的可靠重建入队（build 模块）。
	Queue navigationcontract.MenuStaleEnqueuePort
}

// SetWritePorts 注入菜单 CRUD 的同事务失效端口（装配期调用一次，之后只读）。
func (s *Service) SetWritePorts(d NavigationWriteDeps) {
	if s == nil {
		return
	}
	s.menuPages, s.menuInstances = d.Pages, d.Instances
	s.menuItems, s.menuItemInstances = d.Items, d.ItemInstances
	s.menuQueue = d.Queue
}

// writePortsReady 写入口的装配自检（fail closed，不做「尽力而为」降级）。
//
// 只写数据、不派发失效的菜单 CRUD 正是此前那种「保存成功、线上停在旧菜单」的形态，
// 所以缺端口时整笔拒绝，而不是打完日志照常提交。
func (s *Service) writePortsReady() error {
	if s == nil || s.m == nil || s.writeTx == nil {
		return navigationenums.ErrTranslationWriteUnavailableError()
	}
	if s.menuPages == nil || s.menuInstances == nil ||
		s.menuItems == nil || s.menuItemInstances == nil || s.menuQueue == nil {
		return navigationenums.ErrTranslationWriteUnavailableError()
	}
	return nil
}

// withMenuWriteTx 在调用方事务里执行一次菜单写：设作用域 → 取工程级内容写锁 → 业务写 + 失效。
//
// 顺序不可交换：锁必须在读来源/读路径唯一性之前取（否则读到的是锁外快照），
// 而作用域必须先于锁（scope 是 set_config，锁键按显式 projectID 派生，两者互不依赖，
// 但同事务内的语句必须都在正确的作用域下执行）。
//
// 锁与译文写路径是**同一把**（i18n.LockProjectContentWrite，键 = 规范 uuid + "i18n.content:"）：
// 这就是「菜单 CRUD 与译文写互斥」的落点 —— 同 hash 新增位置时，译文事务不会在
// 「已读来源、未提交」的窗口里漏掉新位置；反过来译文正在提交时，CRUD 会等它结束。
func (s *Service) withMenuWriteTx(ctx context.Context, projectID string, fn func(tx *gorm.DB) error) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	if err := s.writePortsReady(); err != nil {
		return err
	}
	canonical, perr := uuid.Parse(projectID)
	if perr != nil {
		return navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	projectID = canonical.String()
	return s.writeTx.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		if lerr := i18n.LockProjectContentWrite(ctx, tx, projectID); lerr != nil {
			return lerr
		}
		return fn(tx)
	})
}

// 编译期契约断言。
var _ navigationcontract.NavigationService = (*Service)(nil)

// projectIDs 定位用的工程清单。
//
// 数量级很小（站点工程），逐个设一次作用域比在数据层引入 BYPASSRLS 连接便宜得多。
func (s *Service) projectIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.m == nil {
		return nil, navigationenums.Err(navigationenums.ErrProjectRequired)
	}
	// 契约未注入就是装配漏接，直接失败。
	//
	// 这里原来回退到 `m.ListAllProjectIDs`（直接读 projects 表）。它能工作，但
	// **工程清单的所有权在 project 模块**，navigation 的 model 层只该碰本模块的表；
	// 回退还会让漏接表现为「一切正常」，于是同一份「列出全部工程」的 SQL 在
	// navigation / block / page / order 里各存一份，四份将来会各自漂移。
	if s.projects == nil {
		return nil, navigationenums.Err(navigationenums.ErrProjectRequired)
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
		return nil, navigationenums.Err(navigationenums.ErrProjectRequired)
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
	return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
}

// validateParentTx 事务内版的父引用校验（同一份规则，只换连接）。
//
// 环检测走同一棵树：用 tx 列本工程本位置的全部行拼子树 —— 复用自动提交版的
// navDescendantIDs 规则，不另写一份「怎么算后代」。
func (s *Service) validateParentTx(ctx context.Context, tx *gorm.DB, projectID, kind, selfID string, parentID *string) error {
	if parentID == nil {
		return nil
	}
	rows, err := s.m.ListTx(ctx, tx, projectID, kind)
	if err != nil {
		return err
	}
	found := false
	for _, r := range rows {
		if r.ID == *parentID {
			found = true
			break
		}
	}
	if !found {
		// 不存在 / 跨工程 / 跨类型（kind 过滤已把后两者排除在结果之外）。
		return navigationenums.Err(navigationenums.ErrInvalidParent)
	}
	if selfID == "" {
		return nil
	}
	// 成环：自己或自己的后代不能当父级。
	for _, id := range navDescendantIDs(rows, selfID) {
		if id == *parentID {
			return navigationenums.Err(navigationenums.ErrInvalidParent)
		}
	}
	return nil
}

// staleVersionMessage 乐观锁冲突的业务错误：ErrStaleVersion + 定位信息（撞了的菜单项标题）。
//
// 用 ErrDetail 而不是带参的 key|param 形态：定位信息是菜单项标题（任意文本，可能是任意长度）。
// 明细按 ASCII「: 」拼 —— pkg/facing 的复合形态判据只认这一种；出口拿到 Fact.Tail 后
// 按语言决定渲染用的分隔符（中文全角、其余半角）。
func staleVersionMessage(title string) error {
	return navigationenums.ErrDetail(navigationenums.ErrStaleVersion, compactLocation(title))
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

// facingDetailSepForLang 定位信息的分隔符按语言取（中文全角，其余「: 」）。
// 与 inbound/http 的 facingDetailSep 同一取值口径：提示条最终是给人看的一句话。
func facingDetailSepForLang(lang string) string {
	if strings.HasPrefix(strings.TrimSpace(lang), "zh") {
		return "："
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

// markMenuStaleInTx 菜单写事务内的按位置失效：页标记 → 实例标记 → 可靠入队。
//
// 三件事都必须在调用方事务内，且与被改动的菜单行一起提交：
//   - 页面的 stale 标记（同一 tx，标 stale 与 input_version + 1 一条语句完成）；
//   - 自动发布实例的同形标记（同一 tx，同样是「标 stale + 推进 input_version」）；
//   - menu_stale 任务的可靠入队（同一 tx，worker 提交后才可能领到）。
//
// 为什么不是 invalidateMenu：那条走 pipeline.Fanout，它**自开事务**（rls.InProjectScope）
// 且异步 go runRebuild —— 在调用方事务里调用它既不在同一事务、失败也无法回滚，等于
// 把已经写好的菜单回滚掉却留下标记。
//
// 为什么实例标记**不能**由入队代替（这里此前写反过，主审已退回）：
// 「新增位置零命中」只是对**新位置**成立（实例的旧产物里确实还没有这个依赖）；而对
// **已有位置/已有实例**，改一次菜单项就会让它们的旧编译过期。若此时不推进实例的
// input_version，那个旧编译晚到时会通过激活谓词（代次没变）、写进新 HTML 并清掉
// stale —— 可靠入队只保证「会重建一次」，保证不了「旧编译晚到不能覆盖」。版本屏障
// 只有与菜单写同事务给出才有意义。零命中允许，不调用不允许。
func (s *Service) markMenuStaleInTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) error {
	if _, err := s.menuPages.MarkMenuStaleTx(ctx, tx, projectID, kind, at); err != nil {
		return err
	}
	if _, err := s.menuInstances.MarkInstanceMenuStaleTx(ctx, tx, projectID, kind, at); err != nil {
		return err
	}
	if _, err := s.menuQueue.EnqueueMenuStaleTx(ctx, tx, projectID, kind); err != nil {
		return err
	}
	return nil
}

// markNavigationStaleInTx 菜单写事务内的**按菜单项**失效：手工页面 + 自动发布实例。
//
// 只登记了 navigation:{itemID} 的产物不会被按位置键命中（页眉只放某一支），所以按项
// 写操作两条键都要打：位置键覆盖「整个位置的产物」，项键覆盖「只引用这一支的页面与实例」。
//
// 实例侧必须同事务：这不是「可靠重建」，而是**版本屏障** —— 推进实例 input_version 后，
// 已在路上的旧编译即使晚到也会被激活谓词挡掉。可靠重建由按 kind 的 menu_stale 任务负责
// （CRUD 已入队），两者不是一件事，不能互相替代。
func (s *Service) markNavigationStaleInTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string) error {
	navigationID = strings.TrimSpace(navigationID)
	if navigationID == "" {
		return nil
	}
	// 页：手工页面标 stale（同 tx，标 stale 与代次 +1 一条语句）。
	if _, err := s.menuItems.MarkNavigationStaleTx(ctx, tx, projectID, navigationID); err != nil {
		return err
	}
	// 实例：同形屏障，按项键 navigation:{itemID}。零命中是合法结果（没有实例只引用这一支），
	// 但**必须调用** —— 有实例时它推进 input_version，旧编译晚到会被激活谓词挡掉。
	// 位置键（markMenuStaleInTx）命中不到「只引用这一支」的实例，两者不能互相替代。
	if _, err := s.menuItemInstances.MarkInstanceNavigationStaleTx(ctx, tx, projectID, navigationID, time.Now().UTC()); err != nil {
		return err
	}
	return nil
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
