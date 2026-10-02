package pageservice

// page_langs.go — 页面级语言排除（迁移 491：pages.excluded_langs）。
//
// 语义：被排除的语言**本页不产出**，也不进语言切换器 / hreflang / sitemap；空 = 全部站点语言都产出。
// 同一工程里「法务页只做中文、首页做全语言」是**单页**的产出范围，不是站点语言清单的子集 ——
// 所以它是页面级列，而不是又一份工程级语言清单（那会与 project_locales 形成两个真源）。
//
// 与 language 退役（page_locale_retire.go 的 RetireLocale，按 project + lang 整站退役）的分工：
// 那个是「站点不再有这种语言」，本文件是「站点有、这一页不产出」。两者都**必须真的下线产物** ——
// 只写列不让访问面改变，等于后台说「已排除」而线上还在服务那一份字节。

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pagemodel "go_wp/internal/module/page/model"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
)

// 页面级语言排除的业务错误（后台按 enums 白名单透出文案）。
var (
	// ErrPageLangExcluded 该语言已被本页排除：发布 / 构建入口据此跳过。
	//
	// 这是**正常的业务状态**（作者主动排除），不是故障：批量发布把它记成 skipped，
	// 单语言入口把它报成可读错误，都不当成失败。
	ErrPageLangExcluded = errors.New(pageenums.ErrPageLangExcluded)
	// ErrCannotExcludeDefaultLang 不允许排除默认语言。
	//
	// 默认语言是站点的基准：它的产物承载 x-default，且 default_plain 方案下「默认语言无前缀」
	// 是路径映射的锚点。排除它会让所有互指指向一条不存在的路径 —— 拒绝比事后解释便宜。
	ErrCannotExcludeDefaultLang = errors.New(pageenums.ErrCannotExcludeDefaultLang)
	// ErrLangAlreadyExcluded 该语言此前已被排除（幂等入口不重复下线）。
	ErrLangAlreadyExcluded = errors.New(pageenums.ErrLangAlreadyExcluded)
	// ErrLangNotExcluded 该语言不在本页的排除集合里（无从恢复）。
	ErrLangNotExcluded = errors.New(pageenums.ErrLangNotExcluded)
)

// pageExcludesLang 本页是否排除了该语言（大小写敏感：语言码是存储值，不是展示文案）。
func pageExcludesLang(page *pagemodel.PageEntity, lang string) bool {
	if page == nil {
		return false
	}
	l := strings.TrimSpace(lang)
	if l == "" {
		return false
	}
	for _, raw := range page.ExcludedLangs {
		if strings.TrimSpace(raw) == l {
			return true
		}
	}
	return false
}

// PageLangStates 该页各**启用**语言的排除与发布状态（默认语言在前，供后台面板渲染）。
//
// 只列站点启用语言：排除一个不在清单里的语言没有意义（它本来就不产出），
// 而展示它会让面板看起来像「还差一件事没做」。
func (s *Service) PageLangStates(ctx context.Context, pageID string) (rows []pagedto.PageLangState, err error) {
	page, err := s.getExistingPage(ctx, pageID)
	if err != nil {
		return nil, err
	}
	langs := s.enabledLangsOf(ctx, page.ProjectID)
	def := s.defaultLocaleOf(ctx, page.ProjectID)
	pubs, err := s.model.ListPublications(ctx, page.ID)
	if err != nil {
		return nil, err
	}
	published := make(map[string]bool, len(pubs))
	for i := range pubs {
		published[strings.TrimSpace(pubs[i].Lang)] = strings.TrimSpace(pubs[i].ActivePath) != ""
	}
	rows = make([]pagedto.PageLangState, 0, len(langs))
	for _, lang := range langs {
		rows = append(rows, pagedto.PageLangState{
			Lang:      lang,
			IsDefault: lang == def,
			Excluded:  pageExcludesLang(page, lang),
			Published: published[lang],
		})
	}
	return rows, nil
}

// ExcludePageLang 排除某语言：**下线它的产物**并清掉该语言的全部发布态，再写排除列。
//
// 三段顺序与 RetireLocale 逐字同源（理由一样，见那里的论证）：
//
//  1. 校验放在最前（页面存在、语言启用、不是默认语言、此前未排除）—— 校验失败时
//     一点都不该动，包括访问面；
//  2. **先清访问面符号链接**（跨系统动作，不能进数据库事务：事务回滚撤不掉已删的链接；
//     反过来「链接已删、事务失败」是可重跑收敛的，因为指针还在、下次仍能算出该删哪些）；
//  3. **再在一个事务里做全部数据库写入**：排除列 + 该语言的发布/暂存指针 + 该语言的
//     page_routes 占用 + 该语言的发布计划行。此前这几处各自提交会留下
//     「列改了但产物还在线上」或「产物删了但列没改」的半截状态 —— 前者是前台 404 与
//     后台显示不一致，后者是「排除没生效但页面说排除了」。
//
// 返回下线掉的路径数（0 表示该语言本来就没发布过，仍是成功的排除）。
func (s *Service) ExcludePageLang(ctx context.Context, pageID, lang string) (retired int, err error) {
	page, err := s.getExistingPage(ctx, pageID)
	if err != nil {
		return 0, err
	}
	l := strings.TrimSpace(lang)
	if l == "" {
		return 0, ErrInvalidParam
	}
	if pageExcludesLang(page, l) {
		return 0, ErrLangAlreadyExcluded
	}
	if l == strings.TrimSpace(s.defaultLocaleOf(ctx, page.ProjectID)) {
		return 0, ErrCannotExcludeDefaultLang
	}
	if !langEnabled(ctx, s, page.ProjectID, l) {
		return 0, ErrInvalidParam
	}

	// 该语言当前的激活路径（可能为空：排除一个还没发布过的语言是正常操作）。
	pub, perr := s.model.GetPublication(ctx, page.ID, l)
	if perr != nil && !errors.Is(perr, gorm.ErrRecordNotFound) {
		return 0, perr
	}
	activePath := ""
	if pub != nil {
		activePath = strings.TrimSpace(pub.ActivePath)
	}
	if activePath != "" {
		if derr := s.deactivatePaths([]string{activePath}); derr != nil {
			return 0, derr
		}
		retired = 1
	}

	excluded := appendExcludedLang(page.ExcludedLangs, l)
	if terr := s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
		if uerr := s.model.UpdateExcludedLangsTx(ctx, tx, page.ProjectID, page.ID, excluded); uerr != nil {
			return uerr
		}
		if pub != nil {
			if derr := s.model.DeletePublicationsByLangTx(ctx, tx, page.ProjectID, page.ID, l); derr != nil {
				return derr
			}
		}
		if serr := s.model.DeleteStagingsByLangTx(ctx, tx, page.ProjectID, page.ID, l); serr != nil {
			return serr
		}
		// 发布计划行同删：它冻结的是「这次发布依据哪几种语言」，被排除语言的计划没有任何
		// 重建入口会再读到；留着只会在解除排除后被误当成「仍然有效的冻结输入」复用。
		if plerr := s.model.DeletePublicationPlansByLangTx(ctx, tx, page.ProjectID, page.ID, l); plerr != nil {
			return plerr
		}
		if activePath != "" && s.routes != nil {
			if rerr := s.routes.DeactivateTx(ctx, tx, &pubcontract.DeactivateReq{
				ProjectID: page.ProjectID, Path: activePath,
			}); rerr != nil {
				return rerr
			}
		}
		return nil
	}); terr != nil {
		return 0, terr
	}

	logger.Scene("publication").With("pageId", page.ID).With("lang", l).With("retired", retired).
		Info("已排除本页的该语言并下线其产物")
	return retired, nil
}

// RestorePageLang 解除排除（只解除，**不自动重新发布**）。
//
// 为什么不做自动重发：重新发布是一次产出上线动作（会改访问面、写激活路径、触发站点文件
// 与互指刷新），不该由「后台勾选框」隐式触发 —— 作者解除排除后按常规发布入口发布即可，
// 那一步有完整的回执、依赖失效与回滚语义。这里只把产出范围恢复成「该语言也产出」。
//
// 单处写入（一列），因此不开事务（AGENTS 的事务判据以「两处及以上持久化写入」为准）。
func (s *Service) RestorePageLang(ctx context.Context, pageID, lang string) (err error) {
	page, err := s.getExistingPage(ctx, pageID)
	if err != nil {
		return err
	}
	l := strings.TrimSpace(lang)
	if l == "" {
		return ErrInvalidParam
	}
	if !pageExcludesLang(page, l) {
		return ErrLangNotExcluded
	}
	remaining := make([]string, 0, len(page.ExcludedLangs))
	for _, raw := range page.ExcludedLangs {
		if strings.TrimSpace(raw) == l {
			continue
		}
		remaining = append(remaining, strings.TrimSpace(raw))
	}
	if err = s.model.UpdateExcludedLangs(ctx, page.ProjectID, page.ID, remaining); err != nil {
		return err
	}
	logger.Scene("publication").With("pageId", page.ID).With("lang", l).
		Info("已解除本页的语言排除（不自动重新发布，重新发布走常规发布入口）")
	return nil
}

// appendExcludedLang 追加一个语言（保序去重；语言码是存储值，比较前只 trim 不折叠大小写）。
func appendExcludedLang(existing []string, lang string) []string {
	out := make([]string, 0, len(existing)+1)
	for _, raw := range existing {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	out = append(out, lang)
	return out
}

// langEnabled 该语言是否在站点启用清单里。
//
// 走**发布口径**（publishLangsOf，清单读不到即报错 → 这里按「不在清单里」处理并拒绝写）：
// 排除动作会真的下线产物，判断依据不能是「降级成默认语言一种」的那份清单 ——
// 那会让一次读库抖动把合法语言判成「不在清单里」而拒绝，或更糟地放行不该放行的语言。
func langEnabled(ctx context.Context, s *Service, projectID, lang string) bool {
	langs, err := s.publishLangsOf(ctx, projectID)
	if err != nil {
		return false
	}
	for _, l := range langs {
		if strings.TrimSpace(l) == lang {
			return true
		}
	}
	return false
}

// dropExcludedLangs 从站点语言输入里扣掉本页排除的语言。
//
// 调用点只有一处（发布计划冻结），但它是「排除在**冻结时**生效」这条取舍的落点：
// 被排除语言不是本次发布的输入（不产出产物），留在计划里会让 Manifest.SiteLangs 与
// 计划本身声称「发布了该语言」—— 而产物根本不存在，属冻结事实说谎。
//
// 默认语言被排除是**不该发生**的（ExcludePageLang 拒绝）：存量数据里若真有，
// 这里按「默认语言在前」的既定顺序取剩余集合的首项，绝不产出空默认语言
// （空默认语言会让所有互指都不是 x-default，见 SiteLangInputsOfPlan 的同款处理）。
func dropExcludedLangs(inputs pipeline.SiteLangInputs, excluded []string) pipeline.SiteLangInputs {
	if len(excluded) == 0 || len(inputs.SiteLangs) == 0 {
		return inputs
	}
	skip := make(map[string]bool, len(excluded))
	for _, raw := range excluded {
		if v := strings.TrimSpace(raw); v != "" {
			skip[v] = true
		}
	}
	out := make([]string, 0, len(inputs.SiteLangs))
	for _, lang := range inputs.SiteLangs {
		if skip[strings.TrimSpace(lang)] {
			continue
		}
		out = append(out, lang)
	}
	if len(out) == 0 {
		// 全被排除（含默认语言）—— 保留原集合，让上游的「空语言表」判据去处理，
		// 而不是在这里造一份空输入把失败点推远。
		return inputs
	}
	inputs.SiteLangs = out
	if skip[inputs.DefaultLang] {
		inputs.DefaultLang = out[0]
	}
	return inputs
}

// planHasExcludedLang 计划里是否含有本页当前排除的语言（有则整份计划失效重冻）。
func planHasExcludedLang(plan pipeline.PublicationPlan, excluded []string) bool {
	if len(excluded) == 0 {
		return false
	}
	skip := make(map[string]bool, len(excluded))
	for _, raw := range excluded {
		if v := strings.TrimSpace(raw); v != "" {
			skip[v] = true
		}
	}
	for _, lang := range plan.SiteLangs {
		if skip[strings.TrimSpace(lang)] {
			return true
		}
	}
	return false
}

// langPublishSkipped 批量发布结果里「被排除而跳过」的状态值。
//
// 与 "failed" 分开是刻意的：跳过的成因是作者主动排除（业务决策），失败是系统没做到。
// 合成一个值会让回执看起来「这次发布出错了」，而运维的第一反应是重试。
const langPublishSkipped = "skipped"
