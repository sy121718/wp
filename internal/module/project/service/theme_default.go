package projectservice

// theme_default.go — 默认主题的幂等补齐（不影响用户已有的选择，只补空）。
//
// 起点不存在时页面的继承链（主题 → 页面 → 组件）拿不到任何令牌，故建站即需一套主题；
// 补齐动作逐工程走 CreateTheme 的既有逻辑，单个工程失败只记日志、不阻断其余。

import (
	"context"
	"encoding/json"

	"go_wp/internal/builder"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/logger"
)

// ensureDefaultTheme 工程一套主题都没有时，建一套默认主题（后台设计语言色值）并激活。
//
// 为什么建站就要有主题：继承链是「主题 → 页面 → 组件」，起点不存在时页面拿不到任何令牌、
// 组件只能落到各自的内置 fallback —— 用户还没开始配，前台就已经是一套不属于自己的配色。
// 已有主题（哪怕没激活）时不动：用户自己的选择优先，本函数只补空。
func (s *Service) ensureDefaultTheme(ctx context.Context, projectID string) (created bool, err error) {
	count, err := s.model.CountThemes(ctx, projectID)
	if err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	raw, err := json.Marshal(builder.DefaultThemeSettings())
	if err != nil {
		return false, err
	}
	if _, err = s.CreateTheme(ctx, &projectdto.ThemeCreateReq{
		ProjectID: projectID,
		Name:      builder.DefaultThemeName,
		Settings:  raw,
	}); err != nil {
		return false, err
	}
	return true, nil
}

// EnsureDefaultThemes 给尚无任何主题的工程补上默认主题（幂等，启动时调用）。
//
// 覆盖存量：本能力上线前建的工程没有主题，页面也就一直没有主题可继承。
// 逐工程补齐而不是批量 SQL —— 主题创建要走名称查重与首建激活的既有逻辑。
func (s *Service) EnsureDefaultThemes(ctx context.Context) (fixed int, err error) {
	projects, err := s.model.ListAll(ctx)
	if err != nil {
		return 0, err
	}
	for _, p := range projects {
		id := p.ID
		created, cerr := s.ensureDefaultTheme(ctx, id)
		if cerr != nil {
			// 单个工程失败不阻断其余：补齐是兜底动作，不是建站主链路。
			logger.Scene("project").With("project", id).Error(cerr, "默认主题补齐失败")
			continue
		}
		if created {
			fixed++
		}
	}
	return fixed, nil
}
