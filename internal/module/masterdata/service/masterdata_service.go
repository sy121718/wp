// Package masterdataservice 主数据变更记录业务实现（issue #19）。
//
// 职责边界（与库存流水严格分离）：
//
//   - 本模块记的是**结构化字段的配置变更**：谁、什么时候、把哪条主数据的哪个字段
//     从什么改成了什么（商品 / 变体 / 货源）；
//   - 库存流水（inventory_stock_movements）记的是**数量变动**：入库 / 出库 / 盘点
//     让可用量怎么变。两者各记一处、互不替代：一次采购收货既会在库存流水留一条「+10」，
//     又可能因成本价回写在本模块留一条「成本价 12.00 → 13.50」，这是两件事。
//
// 本模块不认识商品表与库存表：调用方把「改前 / 改后」的字段快照递进来，
// 本模块做 diff、落库、查询。因此本模块不持有 *gorm.DB，只持有自己的 model。
package masterdataservice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	"go_wp/pkg/utils"
	projectcontract "go_wp/internal/module/project/contract"
)

const (
	// defaultPageSize / maxPageSize 变更记录与实体清单的分页。
	defaultPageSize = 20
	maxPageSize     = 200
	// maxFieldValueLen 单个字段值的存储上限（对接配置之类的自由形状字段可能很大）：
	// 超过即截断并留标记 —— 审计要留下「这里确实变过」，但不做数据仓库。
	maxFieldValueLen = 4000
	// truncatedSuffix 截断标记（让人一眼看出值不完整，而不是以为原值就这么长）。
	truncatedSuffix = "…（已截断）"
)

// Service 主数据变更记录业务实现。
//
// 只持有本模块 model 与 project 契约；不持有 *gorm.DB。
type Service struct {
	m       *masterdatamodel.Model
	project projectcontract.ProjectService
}

// NewService 构造。
func NewService(m *masterdatamodel.Model, project projectcontract.ProjectService) *Service {
	return &Service{m: m, project: project}
}

// 编译期契约断言。
var _ masterdatacontract.MasterDataService = (*Service)(nil)

// queryArgs 归一化后的查询参数（filter 与分页）。
type queryArgs struct {
	filter masterdatamodel.ChangeFilter
	page   int
	size   int
}

// resolveQuery 归一化查询参数：校验实体类型 / 动作 / 实体 id / 时间区间，解析工程与分页。
//
// 校验一律前置：让「拼错的筛选条件」得到明确报错，而不是静默返回全量或空集
// （把 uuid 写错当成「没有记录」，会让人误以为审计数据丢了）。
func (s *Service) resolveQuery(ctx context.Context, projectID, entityType, entityID, field, action,
	keyword, operatorID, since, until string, page, size int) (args queryArgs, err error) {
	entityType = strings.TrimSpace(entityType)
	entityID = strings.TrimSpace(entityID)
	action = strings.TrimSpace(action)
	if entityType != "" && !masterdataenums.IsValidEntityType(entityType) {
		return args, errors.New(masterdataenums.ErrEntityTypeInvalid)
	}
	if action != "" && !masterdataenums.IsValidAction(action) {
		return args, errors.New(masterdataenums.ErrActionInvalid)
	}
	if entityID != "" {
		if _, perr := uuid.Parse(entityID); perr != nil {
			return args, errors.New(masterdataenums.ErrInvalidParam)
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
		return args, errors.New(masterdataenums.ErrTimeRangeInvalid)
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

// resolveProjectID 解析工程：显式指定优先，否则取唯一工程。
func (s *Service) resolveProjectID(ctx context.Context, projectID string) (id string, err error) {
	if strings.TrimSpace(projectID) != "" {
		return strings.TrimSpace(projectID), nil
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return "", err
	}
	if len(list) != 1 {
		return "", errors.New(masterdataenums.ErrProjectRequired)
	}
	return list[0].ID, nil
}

// normalizePage / normalizeSize 分页归一化（越界一律收口，不报错）。
func normalizePage(page int) int {
	return utils.NormalizePaging(page, defaultPageSize, defaultPageSize, maxPageSize).Page
}

func normalizeSize(size int) int {
	return utils.NormalizePaging(1, size, defaultPageSize, maxPageSize).Size
}

// parseTimeValue 解析时间入参：支持 RFC3339、'2006-01-02 15:04:05' 与纯日期 '2006-01-02'。
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
	return nil, false, errors.New(masterdataenums.ErrInvalidParam)
}

// parseSince 解析区间起点（含端点）：只填日期时取当日 00:00（含当天）。
func parseSince(raw string) (t *time.Time, err error) {
	t, _, err = parseTimeValue(raw)
	return t, err
}

// parseUntil 解析区间终点（**不含**端点）：只填日期时取次日 00:00 ——
// 「查到 2026-09-11 为止」在直觉上包含 9 月 11 日整天，写成非半开区间
// 才会出现「填了今天却查不到今天的记录」这种反直觉结果。
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
