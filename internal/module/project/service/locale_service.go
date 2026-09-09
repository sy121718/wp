package projectservice

// locale_service.go — 站点语言清单用例（多语言 P3，docs/06-D §14 D10）。

import (
	"context"
	"errors"
	"strings"
	"time"

	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"

	"go_wp/pkg/i18n"
)

// errLocaleInvalid 语言清单非法（空清单、重复语言、默认语言未启用、语言码非法）。
var errLocaleInvalid = errors.New("语言清单不合法")

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

// DefaultLocale 返回站点默认语言：清单里 is_default 那一行，缺失时回退 i18n.default_lang。
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
	if err = s.model.ReplaceLocales(ctx, req.ProjectID, rows); err != nil {
		return nil, err
	}
	return s.ListLocales(ctx, req.ProjectID)
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
