package buildservice

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	builddto "go_wp/internal/module/build/dto"
	buildenums "go_wp/internal/module/build/enums"
	buildmodel "go_wp/internal/module/build/model"
)

var menuKinds = map[string]struct{}{
	"header":        {},
	"header_mobile": {},
	"footer":        {},
	"footer_mobile": {},
}

func (s *Service) EnqueueMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string) (inputVersion int64, err error) {
	if s == nil || s.m == nil {
		return 0, buildenums.Err(buildenums.ErrInvalidParam)
	}
	kind = strings.TrimSpace(kind)
	if _, ok := menuKinds[kind]; !ok {
		return 0, buildenums.Err(buildenums.ErrInvalidMenuKind)
	}
	sourceID, ok := builddto.MenuStaleSourceID(projectID, kind)
	if !ok {
		return 0, buildenums.Err(buildenums.ErrInvalidParam)
	}

	canonicalProject, cerr := canonicalProjectID(sourceID)
	if cerr != nil {
		return 0, cerr
	}

	version, err := s.m.NextMenuStaleVersionTx(tx, canonicalProject, kind)
	if err != nil {
		return 0, err
	}

	e := &buildmodel.Entity{
		SourceType: buildmodel.SourceTypeMenuStale,
		SourceID:   sourceID,
		ProjectID:  &canonicalProject,

		Lang:           "",
		Intent:         buildmodel.IntentDependency,
		DraftVersion:   0,
		BuildInputHash: menuStaleInputHash(canonicalProject, kind, version),
		InputVersion:   &version,

		ContextKey: &sourceID,
		Status:     buildmodel.StatusPending,
		CreateTime: s.now(),
	}
	if cerr := s.m.CreateTx(tx, e); cerr != nil {
		return 0, cerr
	}
	return version, nil
}

func canonicalProjectID(sourceID string) (string, error) {
	projectID, _, ok := builddto.SplitMenuStaleSourceID(sourceID)
	if !ok {
		return "", buildenums.Err(buildenums.ErrInvalidParam)
	}
	if _, err := uuid.Parse(projectID); err != nil {
		return "", buildenums.Err(buildenums.ErrInvalidParam)
	}
	return projectID, nil
}

func menuStaleInputHash(projectID, kind string, version int64) string {
	return fmt.Sprintf("menu_stale:%s:%s#%d", projectID, kind, version)
}
