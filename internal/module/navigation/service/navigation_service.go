package navigationservice

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

const (
	kindHeader       = "header"
	kindHeaderMobile = "header_mobile"
	kindFooter       = "footer"
	kindFooterMobile = "footer_mobile"
)

const (
	sourceCustom   = "custom"
	sourcePage     = "page"
	sourceArticle  = "article"
	sourceProduct  = "product"
	sourceCategory = "category"
	sourceBlock    = "block"
)

const (
	targetSelf  = "self"
	targetBlank = "blank"
)

type Service struct {
	m *navigationmodel.Model

	projects projectcontract.ProjectService

	sources navigationcontract.SourceResolver

	sourceRowsPort sourceRows

	rowsTx sourceRowsTxPort

	writeTx TxRunner

	menuPages navigationcontract.PageStalePort

	menuInstances navigationcontract.PresentationStalePort
	menuItems     navigationcontract.MenuItemStalePort

	menuItemInstances navigationcontract.MenuItemPresentationStalePort
	menuQueue         navigationcontract.MenuStaleEnqueuePort

	staleMenu navigationcontract.MenuStaleDispatcher
}

func NewService(m *navigationmodel.Model, projects projectcontract.ProjectService) *Service {
	s := &Service{m: m, projects: projects}
	if m != nil {
		s.rowsTx = navigationRowsModelTx{m: m}

		s.writeTx = m
	}
	return s
}

type NavigationWriteDeps struct {
	Pages navigationcontract.PageStalePort

	Instances navigationcontract.PresentationStalePort

	Items navigationcontract.MenuItemStalePort

	ItemInstances navigationcontract.MenuItemPresentationStalePort

	Queue navigationcontract.MenuStaleEnqueuePort
}

func (s *Service) SetWritePorts(d NavigationWriteDeps) {
	if s == nil {
		return
	}
	s.menuPages, s.menuInstances = d.Pages, d.Instances
	s.menuItems, s.menuItemInstances = d.Items, d.ItemInstances
	s.menuQueue = d.Queue
}

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

var _ navigationcontract.NavigationService = (*Service)(nil)

func (s *Service) projectIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.m == nil {
		return nil, navigationenums.Err(navigationenums.ErrProjectRequired)
	}

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

		return nil, navigationenums.Err(navigationenums.ErrProjectRequired)
	}
	return ids, nil
}

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

		return navigationenums.Err(navigationenums.ErrInvalidParent)
	}
	if selfID == "" {
		return nil
	}

	for _, id := range navDescendantIDs(rows, selfID) {
		if id == *parentID {
			return navigationenums.Err(navigationenums.ErrInvalidParent)
		}
	}
	return nil
}

func staleVersionMessage(title string) error {
	return navigationenums.ErrDetail(navigationenums.ErrStaleVersion, compactLocation(title))
}

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

func (s *Service) staleTitle(ctx context.Context, e *navigationmodel.NavigationEntity) string {
	if cur, err := s.m.Get(ctx, e.ProjectID, e.ID); err == nil && cur != nil && strings.TrimSpace(cur.Title) != "" {
		return cur.Title
	}
	return e.Title
}

func facingDetailSepForLang(lang string) string {
	if strings.HasPrefix(strings.TrimSpace(lang), "zh") {
		return "："
	}
	return ": "
}

func (s *Service) SetSourceResolver(r navigationcontract.SourceResolver) { s.sources = r }

func (s *Service) SourceGroups(ctx context.Context, projectID string) (groups []navigationcontract.SourceGroup, err error) {
	if s.sources == nil {
		return nil, nil
	}
	return s.sources.Candidates(ctx, projectID)
}

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

func (s *Service) SetMenuStaleDispatcher(d navigationcontract.MenuStaleDispatcher) {
	if s == nil {
		return
	}
	s.staleMenu = d
}

func (s *Service) invalidateMenu(ctx context.Context, projectID, kind string) {
	projectID = strings.TrimSpace(projectID)
	kind = strings.TrimSpace(kind)
	if projectID == "" || kind == "" {

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

func (s *Service) markMenuStaleInTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) error {

	if _, perr := uuid.Parse(strings.TrimSpace(projectID)); perr != nil {
		return navigationenums.Err(navigationenums.ErrInvalidParam)
	}
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

func (s *Service) markNavigationStaleInTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string) error {
	navigationID = strings.TrimSpace(navigationID)
	if navigationID == "" {
		return nil
	}

	if _, err := s.menuItems.MarkNavigationStaleTx(ctx, tx, projectID, navigationID); err != nil {
		return err
	}

	if _, err := s.menuItemInstances.MarkInstanceNavigationStaleTx(ctx, tx, projectID, navigationID, time.Now().UTC()); err != nil {
		return err
	}
	return nil
}

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

func (s *Service) InvalidateMenuLabels(ctx context.Context, projectID string, kinds []string) {
	for _, kind := range kinds {
		if ctx.Err() != nil {
			return
		}
		s.invalidateMenu(ctx, projectID, kind)
	}
}
