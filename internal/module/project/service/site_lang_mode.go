package projectservice

// site_lang_mode.go — 站点语言 URL 方案的**工程级**读取（多语言开关，docs/06-D §5）。
//
// 这个值只读、不写：写入路径是设置页的整页保存（SaveSiteSettings → saveLangURLMode
// 落进 projects.settings.langURLMode）。此处刻意不做「写入后热更新某个进程级变量」——
// 那正是本批消灭的缺陷（见契约 ProjectService.SiteLangURLMode 的注释）。

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	projectdto "go_wp/internal/module/project/dto"
)

// SiteLangURLMode 返回该工程配置的站点语言 URL 方案原文（未配置返回空串）。
//
// 工程不存在按「未配置」处理（返回空串、不报错）：调用方据此回退全局默认方案，
// 与「工程刚被删、构建还在跑」这种时序一致 —— 报错会让一次构建因为工程表的一行
// 消失而失败，而那份构建本来也产不出有用的东西。
func (s *Service) SiteLangURLMode(ctx context.Context, projectID string) (raw string, err error) {
	id := strings.TrimSpace(projectID)
	if id == "" {
		return "", nil
	}
	e, err := s.model.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	if e == nil {
		return "", nil
	}
	return strings.TrimSpace(projectdto.ParseSiteSettings(e.Settings).LangURLMode), nil
}
