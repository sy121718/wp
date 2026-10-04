package blockservice

// block_normalize.go — 全局块字段归一化（category / kind / reuse_mode 白名单）。
// 取值口径与迁移里的 CHECK 约束对齐，越界值一律回落到默认值。

import (
	"regexp"
	"strings"

	blockmodel "go_wp/internal/module/block/model"
)

// categoryPattern category 值白名单：小写字母、数字、下划线、连字符，1~50 字符。
// 收紧为分组键友好的稳定格式（防注入，保证管理端/工作台分组与 URL 传参稳定）。
var categoryPattern = regexp.MustCompile(`^[a-z0-9_-]{1,50}$`)

// normalizeCategory 归一化块分类：空默认 general，非空校验白名单。
func normalizeCategory(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return blockmodel.DefaultCategory, nil
	}
	if !categoryPattern.MatchString(s) {
		return "", ErrInvalidCategory
	}
	return s, nil
}

// kindWhitelist 块类型白名单（docs/02-D §4/§5）：站点骨架 + 复用内容段 + 布局骨架 + 片段模板。
var kindWhitelist = map[string]bool{
	blockmodel.KindBlock: true, blockmodel.KindHeader: true, blockmodel.KindFooter: true,
	blockmodel.KindAnnouncement: true, blockmodel.KindSidebar: true, blockmodel.KindBreadcrumb: true,
	blockmodel.KindDrawer: true, blockmodel.KindSearch: true,
	blockmodel.KindCTA: true, blockmodel.KindTrust: true, blockmodel.KindBrands: true,
	blockmodel.KindContact: true, blockmodel.KindAbout: true,
	blockmodel.KindBanner: true, blockmodel.KindGrid: true,
	blockmodel.KindSnippet: true,
}

// normalizeKind 归一化块类型：空默认 block，仅接受白名单内的类型，
// 其余返回 ErrInvalidKind（拒绝而非静默改写为 block）。
func normalizeKind(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return blockmodel.KindBlock, nil
	}
	if !kindWhitelist[s] {
		return "", ErrInvalidKind
	}
	return s, nil
}

// normalizeReuseMode 归一化复用方式（docs/02-D §5）：空默认 global（存量语义），
// 仅接受 global/template，其余返回 ErrInvalidReuseMode。
func normalizeReuseMode(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "":
		return blockmodel.ReuseGlobal, nil
	case blockmodel.ReuseGlobal:
		return blockmodel.ReuseGlobal, nil
	case blockmodel.ReuseTemplate:
		return blockmodel.ReuseTemplate, nil
	default:
		return "", ErrInvalidReuseMode
	}
}
