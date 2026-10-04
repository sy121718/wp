package projectservice

// theme_list.go — 主题的列举与激活态读取。
//
// 列表类入口的工程作用域各有来源：ListThemes 由调用方给出工程；
// ListThemesByBlockID 只有块 id（块变更扇出），故逐工程查询后合并 ——
// 工程数量级很小，而漏标记的代价是站点一直显示旧内容。

import (
	"context"
	"errors"

	"gorm.io/gorm"

	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
)

// ListThemes 列出工程全部主题。
func (s *Service) ListThemes(ctx context.Context, projectID string) (res []projectdto.ThemeResp, err error) {
	entities, err := s.model.ListThemes(ctx, projectID)
	if err != nil {
		return nil, err
	}
	res = make([]projectdto.ThemeResp, 0, len(entities))
	for i := range entities {
		res = append(res, toThemeResp(&entities[i]))
	}
	return res, nil
}

// ListThemesByBlockID 列出页眉/页脚槽位绑定了指定全局块的全部主题。
func (s *Service) ListThemesByBlockID(ctx context.Context, blockID string) (res []projectdto.ThemeResp, err error) {
	// 块变更的扇出只有块 id，没有工程；themes 带 FORCE 策略，不分工程设作用域
	// 就会静默命中 0 行（表现为「改了页眉块但页面不被标记待重建」）。
	// 逐工程查询后合并：工程数量级很小，而漏标记的代价是站点一直显示旧内容。
	//
	// 工程表为空时不再回退「不限工程」（DB-009 第三批）：那个分支在换非超级角色后
	// 会命中 0 行，把「读不出工程表」伪装成「没有主题受影响」。宁可显式失败。
	projects, err := s.model.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return nil, ErrThemeProjectRequired
	}
	var entities []projectmodel.ThemeEntity
	for _, p := range projects {
		if ctx.Err() != nil {
			break
		}
		hit, herr := s.model.ListThemesByBlockID(ctx, p.ID, blockID)
		if herr != nil {
			return nil, herr
		}
		entities = append(entities, hit...)
	}
	res = make([]projectdto.ThemeResp, 0, len(entities))
	for i := range entities {
		res = append(res, toThemeResp(&entities[i]))
	}
	return res, nil
}

// GetActiveTheme 取工程当前激活主题。
// 工程尚无主题属合法状态：返回 (nil, nil)，调用方以 theme == nil 判断。
func (s *Service) GetActiveTheme(ctx context.Context, projectID string) (res *projectdto.ThemeResp, err error) {
	entity, err := s.model.GetActiveTheme(ctx, projectID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toThemeRespPtr(entity), nil
}
