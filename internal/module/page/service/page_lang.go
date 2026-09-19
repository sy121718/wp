package pageservice

// page_lang.go — 站点产物语言装配（多语言 P2，docs/06-D）。
//
// 语言是构建环境维度：构建、预览、发布都必须显式携带目标语言，访问路径
// 单点经 pipeline.LangURLRule.Path 计算（禁止各处手拼 "/" + code + path）。
//
// 方案（docs/06-D §5 方案 A'，配置 i18n.site_lang_url_mode）：
//   - default_plain（默认）：默认语言无前缀 /about、/index；非默认语言短码 /en/about；
//   - all_prefix：全语言带短码前缀 /zh/about、/en/about；
//   - off：全语言共用逻辑路径（单语言兼容）。
// 内部逻辑（BuildContext.lang / 数据库 lang 列）始终是完整语言码，
// 只有 URL 段用短码（pipeline.LangURLRule.URLCode）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go_wp/internal/builder"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

// buildLang 解析本次构建语言：请求显式指定优先，否则站点默认语言（i18n.default_lang）。
func buildLang(requested string) string {
	lang := strings.TrimSpace(requested)
	if lang == "" {
		return i18n.GetDefaultLang()
	}
	return lang
}

// langURLRuleOf 构造站点语言 URL 规则（唯一映射点的规则载体）。
//
// 方案取自配置（i18n.site_lang_url_mode / 兼容键 site_lang_prefix），
// 默认语言取自站点清单（project_locales.is_default，缺失回退 i18n.default_lang）——
// 「默认语言无前缀」必须按站点判定，不能只看全局 i18n.default_lang。
func (s *Service) langURLRuleOf(ctx context.Context, projectID string) pipeline.LangURLRule {
	return pipeline.LangURLRuleForProject(ctx, s.project, projectID)
}

// sitePath 逻辑访问路径 → 实际访问路径（本模块唯一入口；实现在 pipeline.LangURLRule.Path）。
// off：规范化逻辑路径；default_plain：默认语言无前缀、其余 /{短码}/path；
// all_prefix：全语言 /{短码}/path（语言根与首页映射为 /index）。
func sitePath(rule pipeline.LangURLRule, lang, logical string) (string, error) {
	return rule.Path(lang, logical)
}

// siteRoutePaths 建页/改草稿阶段的路径占用路径：按站点启用语言（project_locales）
// 各一行，默认语言在前（多语言 P3，docs/06-D §14 D10）。
//
// 关闭语言前缀时多语言映射到同一逻辑路径，这里按路径去重（page_routes 主键是
// (project_id, path)，重复插入必然撞唯一键）；语言清单不可读时回退默认语言一种，
// 与 P3 之前的单语言行为完全一致。
func (s *Service) siteRoutePaths(ctx context.Context, projectID, logical string) ([]string, error) {
	entries, err := s.siteRouteEntries(ctx, projectID, logical)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Path)
	}
	return out, nil
}

// siteRouteEntry 语言 → 该语言下的站点访问路径。
type siteRouteEntry struct {
	Lang string
	Path string
}

// siteRouteEntries 按启用语言（project_locales）计算逻辑路径的各语言站点路径，
// 默认语言在前；关闭前缀时多语言映射到同一路径，按路径去重。
func (s *Service) siteRouteEntries(ctx context.Context, projectID, logical string) ([]siteRouteEntry, error) {
	entries, err := pipeline.SiteRouteEntries(ctx, s.project, projectID, logical)
	if err != nil {
		return nil, err
	}
	out := make([]siteRouteEntry, len(entries))
	for i, e := range entries {
		out[i] = siteRouteEntry{Lang: e.Lang, Path: e.Path}
	}
	return out, nil
}

// renameReservedAllLangs 按启用语言逐语言迁移路径占用（建页/改草稿/改 URL）。
//
// targetLang 非空（改某语言 URL）时：该语言允许迁移本页 active 行（发布时
// reserved 被原地升级为 active，改 URL 的 DB 同步依赖这一迁移），其他语言
// **只迁移 reserved 行**——否则会把别的语言的激活行改到新路径，线上路由丢失。
// targetLang 为空（改草稿路径）时所有语言同等对待。
//
// 任一语言失败即把已迁移的迁回（尽力而为）并返回该错误：路由表与草稿路径必须同源。
func (s *Service) renameReservedAllLangs(ctx context.Context, projectID, pageID, oldLogical, newLogical, targetLang string) error {
	if s.routes == nil {
		return nil
	}
	oldEntries, err := s.siteRouteEntries(ctx, projectID, oldLogical)
	if err != nil {
		return ErrInvalidPath
	}
	newEntries, err := s.siteRouteEntries(ctx, projectID, newLogical)
	if err != nil {
		return ErrInvalidPath
	}
	if len(oldEntries) != len(newEntries) {
		// 语言清单在迁移中途变化（极罕见）：不做半途改名，交由调用方重试。
		return ErrInvalidPath
	}
	done := 0
	for i := range oldEntries {
		oldPath, newPath := oldEntries[i].Path, newEntries[i].Path
		if oldPath == newPath {
			done = i + 1
			continue
		}
		onlyReserved := targetLang != "" && oldEntries[i].Lang != targetLang
		if rerr := s.routes.RenameReserved(ctx, &pubcontract.RenameReservedReq{
			ProjectID: projectID, PageID: pageID, OldPath: oldPath, NewPath: newPath,
			OnlyReserved: onlyReserved,
		}); rerr != nil {
			for j := 0; j < done; j++ {
				if oldEntries[j].Path == newEntries[j].Path {
					continue
				}
				if rberr := s.routes.RenameReserved(ctx, &pubcontract.RenameReservedReq{
					ProjectID: projectID, PageID: pageID,
					OldPath: newEntries[j].Path, NewPath: oldEntries[j].Path,
					OnlyReserved: targetLang != "" && oldEntries[j].Lang != targetLang,
				}); rberr != nil {
					logger.Scene("page").With("pageId", pageID).Error(rberr, "保留路由回迁失败")
				}
			}
			return rerr
		}
		done = i + 1
	}
	return nil
}

// renameReservedAllLangsTx 在**调用方的事务**内按启用语言逐语言迁移路径占用。
//
// 与 renameReservedAllLangs 的差别只有事务边界：这里不做「失败逐个迁回」的补偿 ——
// 外层事务回滚会把已迁移的行一并撤销，补偿反而会在回滚后写出撤销不掉的残留
// （且补偿本身失败时只能记日志，留下一半旧路径一半新路径的路由表）。
// 判据与语义（targetLang 的作用、OnlyReserved 的取舍）与非 Tx 版本逐字一致。
func (s *Service) renameReservedAllLangsTx(ctx context.Context, tx *gorm.DB, projectID, pageID, oldLogical, newLogical, targetLang string) error {
	if s.routes == nil {
		return nil
	}
	oldEntries, err := s.siteRouteEntries(ctx, projectID, oldLogical)
	if err != nil {
		return ErrInvalidPath
	}
	newEntries, err := s.siteRouteEntries(ctx, projectID, newLogical)
	if err != nil {
		return ErrInvalidPath
	}
	if len(oldEntries) != len(newEntries) {
		// 语言清单在迁移中途变化（极罕见）：不做半途改名，交由调用方重试。
		return ErrInvalidPath
	}
	for i := range oldEntries {
		oldPath, newPath := oldEntries[i].Path, newEntries[i].Path
		if oldPath == newPath {
			continue
		}
		onlyReserved := targetLang != "" && oldEntries[i].Lang != targetLang
		if rerr := s.routes.RenameReservedTx(ctx, tx, &pubcontract.RenameReservedReq{
			ProjectID: projectID, PageID: pageID, OldPath: oldPath, NewPath: newPath,
			OnlyReserved: onlyReserved,
		}); rerr != nil {
			return rerr
		}
	}
	return nil
}

// enabledLangsOf 站点启用语言（默认语言在前；清单不可读时回退默认语言一种）。
func (s *Service) enabledLangsOf(ctx context.Context, projectID string) []string {
	return pipeline.EnabledLangs(ctx, s.project, projectID)
}

// defaultLocaleOf 站点默认语言（清单 is_default，缺失回退 i18n.default_lang）。
func (s *Service) defaultLocaleOf(ctx context.Context, projectID string) string {
	return pipeline.DefaultLocale(ctx, s.project, projectID)
}

// localizeMenuURL 导航项 URL 本地化：站内绝对路径（以 / 开头）按本语言方案映射，
// 外链（http/https///mailto/tel/#）与相对路径原样保留。
//
// 必要性：导航 URL 来自 navigation 表；多语言下产物里的菜单链接必须指向
// 「本语言的访问路径」（默认语言无前缀、非默认语言短码前缀），否则访客点菜单 404。
//
// 幂等：导航来源可能存的是某语言的访问路径（如页面 active_path 已带 /en 前缀），
// 先用同一规则反查为逻辑路径再加本语言前缀，避免 /en/en/about。
func (s *Service) localizeMenuURL(ctx context.Context, projectID, lang, raw string) string {
	return pipeline.LocalizeMenuURL(ctx, s.project, projectID, lang, raw)
}

// MarkStaleByRegistryVersion 把「产物由旧组件产出」的页面标记为待重建。
//
// 触发时机：服务启动时。组件是编译进二进制的（Go 实现 + embed 模板），部署新组件后
// 没有任何运行时事件能通知内核「已有产物过期」—— 只能靠产物元数据里的
// registry_version 指纹（builder.RegistryVersion）与本进程当前值比对。
//
// 只标记、不重建：重建交给运维经 RebuildStale 触发，或由后续的编辑/发布自然覆盖。
// 启动时全量构建会拖住启动链，且对「只想先看一眼」的部署是意外副作用。
//
// current 为空（二进制无 VCS 信息等）时不做任何标记 —— 宁可不标记也不全站误标。
//
// 标记粒度是**页面级**而非语言级，这是有意的保守选择：只要该页任一语言的产物由旧组件
// 产出，整页标记。重建走 RebuildStale，它按站点启用语言逐个构建 —— 内容没变的语言会因
// 产物内容寻址（同 hash 的 PutArtifact 是 no-op）而零写入，代价只是重复编译的 CPU 时间。
// 反过来做语言级精确标记需要新增「待重建语言」状态存储（现有 stale 是 pages 表的布尔列，
// 没有语言维度），且一旦漏标就是「线上继续跑旧组件产物且无人察觉」—— 风险收益不成正比。
func (s *Service) MarkStaleByRegistryVersion(ctx context.Context, current string) (ids []string, err error) {
	if strings.TrimSpace(current) == "" || s.artifacts == nil {
		return nil, nil
	}
	ids, err = s.artifacts.ListPageIDsByOtherRegistryVersion(ctx, current)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	// 逐工程扇出（DB-009 第三批）：待标记的页面 id 来自产物元数据，可能横跨多个工程，
	// 而 pages 带 FORCE 策略。每个工程各自一次独立作用域的事务，本工程之外的行由
	// project_id 条件与策略双重拦下 —— 不把多工程的 id 并进同一次作用域查询。
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	at := time.Now().UTC()
	marked := &staleIDCollector{}
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		hit, merr := s.model.MarkStaleByIDs(ctx, projectID, ids, at)
		if merr != nil {
			return nil, merr
		}
		marked.add(hit)
	}
	out := marked.list()
	logger.Scene("page").With("count", len(out)).With("registryVersion", current).
		Info("组件注册表版本变化：相关页面已标记待重建")
	// 影响面回执：组件一变往往是一批页面一起过期，只有这里同时握着「哪个组件版本」
	// 与「哪几页」两件事 —— 过了这里两者就再也对不上。
	s.logStaleImpact(ctx, "registry:"+current, out)
	return out, nil
}

// I18nStalePeer 文案词条 / 内容译文变更时的**其它发布来源**失效端口（消费者侧最窄接口）。
//
// 由 page 的 MarkStaleForI18n 统一扇出：i18n 的保存路径（商品翻译 / 页面翻译 /
// 站点设置 / 导航翻译）历来只调 page.MarkStaleForI18n，新增发布来源时若要求每处
// 都记得补一次调用，漏接面就是 N 处；集中到 1 处后漏接只可能是「装配没接」。
// 未注入 = 只标页面（既有行为不变）。
type I18nStalePeer interface {
	// MarkStaleForI18n 是该来源的自足入口（自己枚举工程、各自提交）。
	//
	// 保留在这里与 …Tx 配对，和 publication 契约「非 Tx + Tx 同列」的形态一致：
	// 没有外层事务的调用方（运维脚本之类）用它即可。
	MarkStaleForI18n(ctx context.Context) error
	// MarkStaleForI18nTx 在**调用方的事务**内标记该工程的其它发布来源。
	//
	// 本模块的扇出只走这一条：pages 的标记与 peer 的标记是同一批失效判定
	//（同一批词条 / 译文变更的两个后果），各自提交必然留下「页面已标、实例未标」的
	// 半截状态 —— 而实例那一侧**没有任何自动补的入口**（要等下一次词条保存才会收敛），
	// 现象是「改了译文，商品详情页仍是旧字节」且日志里什么都没有。Tx 变体让两侧落进
	// 同一个事务：要么都标上，要么一起回滚、由调用方重试（标记幂等，重跑无副作用）。
	// 工程由调用方给定（它自己逐工程扇出），作用域由实现方在传入的 tx 上设置。
	MarkStaleForI18nTx(ctx context.Context, tx *gorm.DB, projectID string) error
}

// SetI18nStalePeer 注入其它发布来源的译文失效端口（装配期调用；可空 = 只标页面）。
func (s *Service) SetI18nStalePeer(peer I18nStalePeer) {
	s.i18nPeer = peer
}

// MarkStaleForI18n 把全部页面与**其它发布来源**标记为待重建（文案词条变更后调用）。
//
// 与 Manifest 的 i18n 依赖条目（DependencyKind=i18n）配套：
// 依赖条目负责「产物字节与词条 revision 的对应关系」，本方法负责「变更后重新排队」。
// 调用方：后台 i18n CRUD（docs/06-D §14 D7）或运维脚本 —— 它们都没有工程上下文。
//
// 逐工程扇出（DB-009 第三批）：这是本模块最典型的「跨工程扇出」入口。pages 带 FORCE
// 策略时，「整站标记」只能由每个工程各自一次作用域内的 UPDATE 拼出来；不设作用域的
// 全表 UPDATE 在换非超级角色后匹配 0 行且不报错 —— 文案改了，站点却一直是旧的。
//
// 其余发布来源（自动发布实例等）集中在**这一个入口**扇出：它们同样在构建期取词注入字节，
// 词条 / 译文一变同样过期。为什么不让四条保存路径各自记得调一次（商品翻译 / 页面翻译 /
// 站点设置 / 导航翻译）—— 漏一处的表现是「改了译文，那一类页面仍是旧字节」且日志里
// 什么都没有（本项目反复吃过的静默失效）。
//
// **每个工程一个事务，页面侧与 peer 同进同出**（2026-09 收口）：此前 peer 的标记排在
// 页面侧扇出之后各自提交，pages 已标、peer 未标时本方法直接返回错误 —— 而 peer 那一侧
// 没有任何自动补的入口，自动发布实例会一直渲染旧字节，直到下一次词条保存才偶然收敛。
// 同库跨模块的写按 AGENTS.md 用 tx 透传（peer 的 …Tx 变体，接口见 I18nStalePeer，
// 装配接线见 routers/assembly_publish.go）：任一步失败整个工程一起回滚 —— 要么两边都标上，
// 要么两边都没标，由调用方重试（幂等）。这也是为什么日志只在事务提交后记：回滚的工程
// 什么都没发生，不该出现在「本次影响面」里。
//
// 单个工程失败不中断其余工程：多工程之间本来就是各自独立的事务（RLS 作用域是单值
// 会话变量，不能合并成一次查询），一个工程的数据库错误不该让别的工程停在旧字节。
// 全部工程处理完后用 errors.Join 汇总返回 —— 不吞错，也不谎报成功。
func (s *Service) MarkStaleForI18n(ctx context.Context) error {
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return err
	}
	hit := &staleIDCollector{}
	at := time.Now().UTC()
	var failed []error
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		var ids []string
		terr := s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
			got, merr := s.model.MarkStaleForI18nTx(ctx, tx, projectID, at)
			if merr != nil {
				return merr
			}
			ids = got
			if s.i18nPeer != nil {
				return s.i18nPeer.MarkStaleForI18nTx(ctx, tx, projectID)
			}
			return nil
		})
		if terr != nil {
			// 该工程两侧都已随事务回滚（什么都没标），记下是哪个工程并继续处理其余工程。
			failed = append(failed, fmt.Errorf("工程 %s 的译文失效标记失败: %w", projectID, terr))
			continue
		}
		hit.add(ids)
	}
	// 影响面回执：只记**已提交**的命中集合 —— 回滚的工程不在其中，两份记录不会互相撒谎。
	s.logStaleImpact(ctx, "i18n", hit.list())
	if len(failed) > 0 {
		return errors.Join(failed...)
	}
	return nil
}

// buildDependencies 构建期依赖（pipeline.DependencyProvider 实现）。
//
// 两条依赖（Kind 同为 i18n，Key 区分资源）：
//   - i18n:site —— 组件固定文案（sys_i18n 开发者词条，P4）：改文案 → revision 变化
//     → 依赖比对不等 → 触发重建（docs/06-D §10.4）；
//   - i18n:content —— 内容译文（sys_translation，P5b）：**只在本次构建确实走内容翻译
//     时登记**。这是 §9 的关键约束：缺译文时构建期回退原文，若依赖里没有这条记录，
//     补齐译文后 revision 未变 → 不触发重建 → 站点长期停留在回退内容。
//
// 为什么「有候选即登记」而非「有缺失才登记」：命中译文的字段同样依赖 sys_translation，
// 改译文/补齐译文都会改变产物字节，两者都必须触发重建。
//
// PIPE-3 起追加「可精确表达」的依赖源（pageDependencyKeys）：块引用、页面绑定的
// 内容实体、文档内集合源。它们写入 Manifest 后由构建路径落库（page_dependencies），
// 依赖源变更时即可按 (kind,key) 反查受影响页面——不再退化为全站标记。
// 页面查询失败只降级为「不登记这些依赖」，不阻断构建（i18n 两条仍登记）。
func (s *Service) buildDependencies(ctx context.Context, in pipeline.BuildInput) []pipeline.Dependency {
	// 逐工程定位一次（DB-009 第四批/第五批）：构建输入不带工程，而 pages 带 FORCE 策略。
	// 定位结果同时供「内容译文 revision 的工程作用域」与「页面依赖源登记」使用 ——
	// 两次都依赖它，且第二次原本就在做同一件事。
	page, pageErr := s.locatePageInProjects(ctx, in.PageID)
	projectID := ""
	if page != nil {
		projectID = page.ProjectID
	}
	deps := []pipeline.Dependency{pipeline.I18NDependency(i18n.Revision())}
	if s.pageUsesContentTranslation(ctx, in) {
		// 内容译文的 revision 必须**带工程作用域**（DB-009 第五批）：sys_translation 的策略
		// 放行「本工程行 + 全局行」，不带作用域时本工程译文的写入不会推进 revision，
		// 于是补齐译文后依赖比对仍相等 → 不触发重建 → 站点长期停留在回退原文。
		// 这条正是上面注释点名要防的失效，只是触发者是 RLS 而不是代码笔误。
		deps = append(deps, pipeline.I18NContentDependency(i18n.ContentRevisionForProject(ctx, projectID)))
	}
	// 系统页面槽位（审计 VIS-006）：只登记**本页真实消费过**的槽位，来源是编译期记录。
	//
	// 与下面从文档静态推导的依赖不同：文档里没有「我用了购物车槽位」这种声明，
	// 那是组件渲染时才取的。静态扫节点类型要维护一张「组件 → 槽位」映射表，
	// 而那张表与渲染代码迟早漂移 —— 漂移的表现是槽位换绑后该页不重建，
	// 站点上旧链接继续生效且无人报错。
	for _, slot := range in.Usage.SiteSlotList() {
		deps = append(deps, pipeline.Dependency{Kind: pipeline.DepKindSiteSlot, Key: slot})
	}
	// 公开站点导航（审计遗留缺口）：判据与槽位逐字一致 —— 只登记**本次编译真实消费过**
	// 的菜单位置（core.nav 绑定 menu=header/footer，渲染期经 RenderContext.UseMenu 记录）。
	//
	// 为什么用编译期记录而不是静态扫文档：绑定可能藏在页眉/页脚块里（块文档不参与
	// 本页文档的静态扫描），也可能来自 presentation 的模板文档 —— 记录下来的才是
	// 「产物字节里确实烘了这份菜单」的事实。
	//
	// 键带工程 ID（pipeline.MenuKey）：导航是工程级资源，位置名只有 header/footer，
	// 不带工程 ID 时反查会跨工程误标。projectID 读不到（页面定位失败）时不登记，
	// 与上面 pageDependencyKeys 的降级口径一致。
	if pid := strings.TrimSpace(projectID); pid != "" {
		for _, kind := range in.Usage.MenuList() {
			k := pipeline.MenuKey(pid, kind)
			deps = append(deps, pipeline.Dependency{Kind: k.Kind, Key: k.Key})
		}
	}
	// 页面依赖源（PIPE-3）：用开头那次定位的结果；读不到页面时降级为不登记这些依赖
	// （依赖失效时该页不再自动重建），但不阻断构建。
	if pageErr == nil {
		deps = append(deps, s.pageDependencyKeys(ctx, page)...)
	} else {
		logger.Scene("dependency").With("page_id", in.PageID).
			Warn("页面依赖源登记跳过：页面记录读取失败（已降级，不阻断构建）")
	}
	return deps
}

// pageUsesContentTranslation 判定本次构建是否用到内容翻译（依赖登记的判据）。
//
// 判据与 compileDocument 的接入条件**同源**（语言维度 + 可翻译输入），保证
// 「登记了依赖」与「产物确实可能随译文变化」一致：
//   - 语言为空（单语言站点）/ 等于站点默认语言 → 否（产物即原文，决策 F1）；
//   - 文档解析失败 → 否（构建主链会自行报错，依赖登记不额外阻断）。
//
// 块内文本补齐后，判据必须同时覆盖「本页引用了块」（页眉/页脚绑定或 core.globalref）：
// 这类页面的本页 AST 可能一个候选都没有，但块内文本会进产物——漏登记会让补齐译文后
// 不触发重建（§9 关键约束）。此处按**保守超集**判定：只要引用了块就登记，不为此额外
// 解析块文档（精确集合由 compileDocument 在编译期算出；多登记一条依赖只在 sys_translation
// revision 变化时才会失效，而该 revision 是全局 max(update_time)，故不增加重建噪音）。
func (s *Service) pageUsesContentTranslation(ctx context.Context, in pipeline.BuildInput) bool {
	lang := strings.TrimSpace(in.Lang)
	if lang == "" {
		return false
	}
	projectID, _ := s.pageContextOf(ctx, in.PageID, lang)
	if lang == s.defaultLocaleOf(ctx, projectID) {
		return false
	}
	page, err := builder.ParsePage(in.DocJSON)
	if err != nil {
		return false
	}
	return pageMayUseContentTranslation(page)
}

// pageMayUseContentTranslation 页面「可能」用到内容翻译：本页有候选，或引用了块。
//
// 与 compileDocument 的接入条件同源（后者进一步解析块文档得到精确候选集合）：
// 引用块（settings.structure 页眉/页脚绑定 / core.globalref 节点）意味着产物里
// 可能含块内文本，必须登记 i18n:content 依赖。
func pageMayUseContentTranslation(page *builder.Page) bool {
	if page == nil {
		return false
	}
	if len(builder.CollectContentCandidates(page)) > 0 {
		return true
	}
	// 任何槽位绑定都可能带可翻译文本（页眉 / 页脚 / 公告条…），不逐字段判。
	if !page.Settings.Structure.IsEmpty() {
		return true
	}
	return len(builder.ReferencedBlockIDs(page.Root)) > 0
}

// contentTranslationEnabled 判定本次编译是否接入内容翻译（与 pageUsesContentTranslation
// 的语言维度同源；文档已在装配层解析，故此处只看语言）。
func (s *Service) contentTranslationEnabled(ctx context.Context, projectID, lang string) bool {
	return pipeline.ContentTranslationEnabled(ctx, s.project, projectID, lang)
}

// reportContentMisses 记录构建期内容译文缺失（L3 埋点，决策 F14 第三层）。
//
// 只记日志、不阻断构建：缺译文已在取词器内回退原文（§7.7），告警用于提醒
// 「上线前有字段仍是原文」；补齐译文后依赖条目（i18n:content）会触发重建。
func reportContentMisses(lang string, candidates int, misses int64) {
	if misses <= 0 {
		return
	}
	logger.Scene("build").
		With("lang", lang).
		With("candidates", candidates).
		With("misses", misses).
		Warn("构建期内容译文缺失，已回退原文（补齐译文后需重建，docs/06-D §9）")
}
