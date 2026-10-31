package masterdataservice

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/utils"
)

const (
	defaultPageSize = 20
	maxPageSize     = 200

	maxFieldValueLen = 4000

	truncatedSuffix = "…（已截断）"
)

type Service struct {
	m       *masterdatamodel.Model
	project projectcontract.ProjectService
}

func NewService(m *masterdatamodel.Model, project projectcontract.ProjectService) *Service {
	return &Service{m: m, project: project}
}

var _ masterdatacontract.MasterDataService = (*Service)(nil)

type queryArgs struct {
	filter masterdatamodel.ChangeFilter
	page   int
	size   int
}

func (s *Service) resolveQuery(ctx context.Context, projectID, entityType, entityID, field, action,
	keyword, operatorID, since, until string, page, size int) (args queryArgs, err error) {
	entityType = strings.TrimSpace(entityType)
	entityID = strings.TrimSpace(entityID)
	action = strings.TrimSpace(action)
	if entityType != "" && !masterdataenums.IsValidEntityType(entityType) {
		return args, masterdataenums.Err(masterdataenums.ErrEntityTypeInvalid)
	}
	if action != "" && !masterdataenums.IsValidAction(action) {
		return args, masterdataenums.Err(masterdataenums.ErrActionInvalid)
	}
	if entityID != "" {
		if _, perr := uuid.Parse(entityID); perr != nil {
			return args, masterdataenums.Err(masterdataenums.ErrInvalidParam)
		}
	}
	sinceTime, terr := parseSince(since)
	if terr != nil {
		return args, terr
	}
	untilTime, terr := parseUntil(until)
	if terr != nil {
		return args, terr
	}
	if sinceTime != nil && untilTime != nil && !sinceTime.Before(*untilTime) {
		return args, masterdataenums.Err(masterdataenums.ErrTimeRangeInvalid)
	}
	resolvedProject, err := s.resolveProjectID(ctx, projectID)
	if err != nil {
		return args, err
	}
	args = queryArgs{
		filter: masterdatamodel.ChangeFilter{
			ProjectID: resolvedProject, EntityType: entityType, EntityID: entityID,
			Field: strings.TrimSpace(field), Action: action,
			Keyword: strings.TrimSpace(keyword), OperatorID: strings.TrimSpace(operatorID),
			Since: sinceTime, Until: untilTime,
		},
		page: normalizePage(page),
		size: normalizeSize(size),
	}
	return args, nil
}

func (s *Service) resolveProjectID(ctx context.Context, projectID string) (id string, err error) {
	if strings.TrimSpace(projectID) != "" {
		return strings.TrimSpace(projectID), nil
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return "", err
	}
	if len(list) != 1 {
		return "", masterdataenums.Err(masterdataenums.ErrProjectRequired)
	}
	return list[0].ID, nil
}

func normalizePage(page int) int {
	return utils.NormalizePaging(page, defaultPageSize, defaultPageSize, maxPageSize).Page
}

func normalizeSize(size int) int {
	return utils.NormalizePaging(1, size, defaultPageSize, maxPageSize).Size
}

func parseTimeValue(raw string) (t *time.Time, dateOnly bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false, nil
	}
	if parsed, perr := time.Parse(time.RFC3339, raw); perr == nil {
		utc := parsed.UTC()
		return &utc, false, nil
	}
	if parsed, perr := time.ParseInLocation("2006-01-02 15:04:05", raw, time.UTC); perr == nil {
		utc := parsed.UTC()
		return &utc, false, nil
	}
	if parsed, perr := time.ParseInLocation("2006-01-02", raw, time.UTC); perr == nil {
		utc := parsed.UTC()
		return &utc, true, nil
	}
	return nil, false, masterdataenums.Err(masterdataenums.ErrInvalidParam)
}

func parseSince(raw string) (t *time.Time, err error) {
	t, _, err = parseTimeValue(raw)
	return t, err
}

func parseUntil(raw string) (t *time.Time, err error) {
	parsed, dateOnly, perr := parseTimeValue(raw)
	if perr != nil || parsed == nil {
		return parsed, perr
	}
	if dateOnly {
		next := parsed.AddDate(0, 0, 1)
		return &next, nil
	}
	return parsed, nil
}
