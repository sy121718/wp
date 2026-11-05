package pipeline

import (
	"go_wp/pkg/publish"
	"context"
	"errors"

	navigationcontract "go_wp/internal/module/navigation/contract"
)

type MenuStaleAdapter struct {
	Fanout *publish.Fanout
}

func NewMenuStaleAdapter(f *publish.Fanout) *MenuStaleAdapter {
	return &MenuStaleAdapter{Fanout: f}
}

func (a *MenuStaleAdapter) InvalidateMenu(ctx context.Context, projectID, kind string) error {
	if a == nil || a.Fanout == nil {
		return errors.New("导航失效适配器未接入依赖扇出（装配缺陷）")
	}
	a.Fanout.InvalidateKeys(ctx, publish.MenuKey(projectID, kind))
	return nil
}

func (a *MenuStaleAdapter) InvalidateNavigation(ctx context.Context, projectID, navigationID string) error {
	if a == nil || a.Fanout == nil {
		return errors.New("导航失效适配器未接入依赖扇出（装配缺陷）")
	}
	_ = projectID
	a.Fanout.InvalidateKeys(ctx, publish.NavigationKey(navigationID))
	return nil
}

var _ navigationcontract.MenuStaleDispatcher = (*MenuStaleAdapter)(nil)
