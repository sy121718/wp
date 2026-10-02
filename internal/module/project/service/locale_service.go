package projectservice

// locale_service.go — 站点语言清单用例（多语言 P3，docs/06-D §14 D10）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"

	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// errLocaleInvalid 语言清单非法（空清单、重复语言、默认语言未启用、语言码非法）。
var errLocaleInvalid = errors.New("语言清单不合法")

// errLocaleRetireNeeded 禁用语言需要显式确认（审计 I18N-017）：该语言还有已激活路径，
// 直接保存会在访问面留下无人认领的 /en/… 路由。
var errLocaleRetireNeeded = errors.New("禁用语言需要确认")

// ErrLocaleNoTranslations 该语言在 sys_i18n 里没有任何启用中的词条（站点级准入门槛，U1）。
//
// 导出（与其它 project 业务哨兵同形）：读侧（inbound/http 的页面错误归口）要按
// errors.Is 分类成「可展示的业务错误」，而不是落 500 + 归口文案。
var ErrLocaleNoTranslations = errors.New("该语言没有任何界面词条")

// ListLocales 列出站点语言清单（默认语言在前；无记录时返回空切片）。
func (s *Service) ListLocales(ctx context.Context, projectID string) (res []projectdto.LocaleResp, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, errLocaleInvalid
	}
	rows, err := s.model.ListLocales(ctx, projectID)
	if err != nil {
		return nil, err
	}
	res = make([]projectdto.LocaleResp, 0, len(rows))
	for _, r := range rows {
		res = append(res, projectdto.LocaleResp{
			Lang: r.Lang, SortOrder: r.SortOrder, IsDefault: r.IsDefault, Enabled: r.Enabled,
		})
	}
	return res, nil
}

// EnabledLangs 返回站点启用语言（默认语言在前，其余按清单顺序）。
//
// 无清单记录（工程刚创建、测试 schema 无本表）时回退「站点默认语言」一种，
// 保证单语言行为与 P3 之前完全一致。
func (s *Service) EnabledLangs(ctx context.Context, projectID string) (langs []string, err error) {
	rows, err := s.model.ListLocales(ctx, projectID)
	if err != nil {
		// 表缺失/查询失败：回退单语言（绝不因语言清单不可读而阻断构建/发布主链）。
		return []string{i18n.GetDefaultLang()}, nil
	}
	for _, r := range rows {
		if r.Enabled {
			langs = append(langs, r.Lang)
		}
	}
	if len(langs) == 0 {
		return []string{i18n.GetDefaultLang()}, nil
	}
	return langs, nil
}

// DefaultLocale 返回站点默认语言：清单里 is_default 那一行，缺失时回退全局默认语言（sys_config 的 i18n 组 default_lang）。
func (s *Service) DefaultLocale(ctx context.Context, projectID string) (lang string, err error) {
	rows, err := s.model.ListLocales(ctx, projectID)
	if err != nil {
		return i18n.GetDefaultLang(), nil
	}
	for _, r := range rows {
		if r.IsDefault {
			return r.Lang, nil
		}
	}
	return i18n.GetDefaultLang(), nil
}

// SaveLocales 全量保存站点语言清单（幂等；同工程全量替换）。
//
// 校验：至少一种语言、语言码白名单（字母/数字/连字符）、语言不重复、
// 至多一个默认语言（未标注时取第一个），默认语言必须启用。
func (s *Service) SaveLocales(ctx context.Context, req *projectdto.LocalesSaveReq) (res []projectdto.LocaleResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" || len(req.Locales) == 0 {
		return nil, errLocaleInvalid
	}
	now := time.Now().UTC()
	seen := map[string]bool{}
	rows := make([]projectmodel.LocaleEntity, 0, len(req.Locales))
	defaultIdx := -1
	for idx, item := range req.Locales {
		lang, lerr := normalizeLangCode(item.Lang)
		if lerr != nil {
			return nil, lerr
		}
		if seen[lang] {
			return nil, errLocaleInvalid
		}
		seen[lang] = true
		enabled := true
		if item.Enabled != nil {
			enabled = *item.Enabled
		}
		if item.IsDefault {
			if defaultIdx >= 0 {
				return nil, errLocaleInvalid
			}
			if !enabled {
				return nil, errLocaleInvalid
			}
			defaultIdx = idx
		}
		rows = append(rows, projectmodel.LocaleEntity{
			ProjectID: req.ProjectID, Lang: lang, SortOrder: idx, IsDefault: false,
			Enabled: enabled, CreatedAt: now, UpdatedAt: now,
		})
	}
	// 未显式标注默认语言时取首个启用语言（避免出现「无默认语言」的清单）。
	if defaultIdx < 0 {
		for i := range rows {
			if rows[i].Enabled {
				defaultIdx = i
				break
			}
		}
		if defaultIdx < 0 {
			return nil, errLocaleInvalid
		}
	}
	rows[defaultIdx].IsDefault = true

	// 站点级准入（U1）：**新增 / 新启用**的语言必须有界面词条，否则拒绝保存。
	//
	// 语义分工（与 U2 的「审核报告 + 一键取消」成对）：
	//   · 这里拦的是「这个语言整体还没准备好」—— 一个词条都没有的语言上线，站点上所有
	//     固定文案都会逐字段回退原文，运营看到的是「语言已启用」，访客看到的是原始语言；
	//   · U2 处理的是「语言准备好了（有词条），但某些页面的内容译文没填」—— 那是内容
	//     层面的缺失，按页面 × 语言逐条呈现并允许把某个页面从该语言撤下来。
	//
	// **只校新增 / 新启用**（第二次改动）：已存在的语言一律放行。否则判据将来一调整
	// （阈值、口径、甚至 sys_i18n 一次误清理），老站点会因为存量数据被**整体拒绝保存**——
	// 用户改不了任何东西，而原因与他正在做的操作无关。判据只作用于「这次新加进来的」。
	//
	// 读不到词条数（库故障）时同样拒绝：放行的后果是静默启用一个空语言，
	// 而拒绝的后果只是「稍后重试」—— 方向取 fail closed。
	beforeEnabled := s.enabledLangsOf(ctx, req.ProjectID)
	wasEnabled := make(map[string]bool, len(beforeEnabled))
	for _, l := range beforeEnabled {
		wasEnabled[l] = true
	}
	for i := range rows {
		if !rows[i].Enabled || wasEnabled[rows[i].Lang] {
			continue
		}
		n, cerr := i18n.CountByLang(ctx, rows[i].Lang)
		switch {
		case errors.Is(cerr, i18n.ErrI18nUnavailable):
			// 「没有接入词条存储」不等于「这个语言没有词条」——这是两种不同的事实：
			//   · 词条存储未接（pkg/i18n 的全局库句柄为空）：只出现在测试进程与精简进程里，
			//     生产装配的 database 组件恒先于本模块就绪；此时放行并留一条日志。
			//     若在这里拒绝，任何没有装配文案存储的调用方都保存不了语言清单
			//     （实测：四个 page feature 用例就是被这条卡住的）。
			//   · 查询本身失败（库在、SQL/连接出错）：下面那支一律拒绝 —— 放行的后果是
			//     静默启用一个空语言，而拒绝的后果只是「稍后重试」。
			logger.Scene("project").With("project", req.ProjectID).With("lang", rows[i].Lang).
				Warn("词条存储未接入，跳过语言准入校验（生产装配下不会出现）")
			continue
		case cerr != nil:
			logger.Scene("project").With("project", req.ProjectID).With("lang", rows[i].Lang).
				Error(cerr, "校验语言词条数失败，本次保存被拒绝")
			return nil, cerr
		}
		if n == 0 {
			logger.Scene("project").With("project", req.ProjectID).With("lang", rows[i].Lang).
				Warn("新增/新启用的语言没有任何启用中的词条，拒绝保存")
			return nil, fmt.Errorf("%w: %s", ErrLocaleNoTranslations, rows[i].Lang)
		}
	}

	// 禁用语言会留下一批**失去归属**的已激活路由（审计 I18N-017）：
	// 访问面还服务着 /en/…，而语言清单里已经没有 en —— 后台再没有任何入口能改它或
	// 下掉它，只能人工登机器删符号链接。所以这是一次需要显式确认的不可逆变更。
	before := s.enabledLangsOf(ctx, req.ProjectID)
	after := map[string]bool{}
	for i := range rows {
		if rows[i].Enabled {
			after[rows[i].Lang] = true
		}
	}
	removed := make([]string, 0, len(before))
	for _, lang := range before {
		if !after[lang] {
			removed = append(removed, lang)
		}
	}
	if len(removed) > 0 && s.retire != nil {
		total, impacted := s.localeRetireImpact(ctx, req.ProjectID, removed)
		if total > 0 && !req.ConfirmRetire {
			return nil, fmt.Errorf("%w：禁用 %s 会让 %d 条已激活路径失去归属，确认后重试",
				errLocaleRetireNeeded, strings.Join(impacted, "、"), total)
		}
	}
	if err = s.model.ReplaceLocales(ctx, req.ProjectID, rows); err != nil {
		return nil, err
	}
	// 清单先落库再下线路由：倒过来会出现「路由已下掉但语言仍在清单里」的中间态，
	// 而那种状态在后台看起来一切正常，只有访问面是坏的。
	if len(removed) > 0 {
		if s.retire == nil {
			logger.Scene("project").With("project", req.ProjectID).
				Warn("语言下线端口未接入，被禁用语言的路由不会被清理")
		} else {
			for _, lang := range removed {
				if _, rerr := s.retire.RetireLocale(ctx, req.ProjectID, lang); rerr != nil {
					return nil, fmt.Errorf("语言 %s 的路由下线失败: %w", lang, rerr)
				}
			}
		}
	}
	return s.ListLocales(ctx, req.ProjectID)
}

// enabledLangsOf 当前启用语言（读取失败返回 nil：调用方按「无变化」处理，不阻断保存）。
func (s *Service) enabledLangsOf(ctx context.Context, projectID string) []string {
	list, err := s.ListLocales(ctx, projectID)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if item.Enabled {
			out = append(out, item.Lang)
		}
	}
	return out
}

// localeRetireImpact 统计这批语言各有几条已激活路径（总数为 0 时不必打扰运营）。
func (s *Service) localeRetireImpact(ctx context.Context, projectID string, langs []string) (total int, impacted []string) {
	for _, lang := range langs {
		n, err := s.retire.LocaleRetireImpact(ctx, projectID, lang)
		if err != nil {
			continue
		}
		if n > 0 {
			total += n
			impacted = append(impacted, lang)
		}
	}
	return total, impacted
}

// normalizeLangCode 语言码白名单校验（与 pipeline.NormalizeLang 同规则：
// 字母/数字/连字符，长度 ≤ 35；project 模块不依赖构建内核，故本地实现）。
func normalizeLangCode(raw string) (string, error) {
	lang := strings.TrimSpace(raw)
	if lang == "" || len(lang) > 35 {
		return "", errLocaleInvalid
	}
	for _, r := range lang {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return "", errLocaleInvalid
		}
	}
	return lang, nil
}
