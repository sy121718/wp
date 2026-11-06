package navigationservice

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/facing"
)

type sourceCandidate struct {
	Title string
	Label string
	URL   string
}

type sourceTarget struct {
	sourceID string
	title    string
	path     string
}

// sourceIDEntry 是调用方提交的来源标识：id 保留原始语义（去空白），canonical 用于按
// 数据库 uuid 身份去重与写库；两者不同只在大小写/非标准表示这类别名上。
type sourceIDEntry struct {
	id        string
	canonical string
}

func canonicalUUIDString(raw string) (string, bool) {
	parsed, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	return parsed.String(), true
}

func candidateKey(raw string) string {
	if canonical, ok := canonicalUUIDString(raw); ok {
		return canonical
	}
	return strings.TrimSpace(raw)
}

// normalizeSourceIDs 先拒绝空标识，再按 uuid 身份去重并检查批量上限；
// 非法 uuid 仍作为整批拒绝原因是参数错误，而不是等数据库报 22P02 变成 500。
// requested 是 uuid 身份去重后（含非法项）的真实请求项数，供单条/多项主消息判定。
func normalizeSourceIDs(raw []string) (entries []sourceIDEntry, requested int, blocked []facing.BlockedItem, tooMany error) {
	seen := make(map[string]struct{}, len(raw))
	pending := make([]struct {
		id        string
		canonical string
		valid     bool
	}, 0, len(raw))
	for _, value := range raw {
		id := strings.TrimSpace(value)
		canonical, valid := canonicalUUIDString(id)
		key := id
		if valid {
			key = canonical
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		requested++
		if id == "" {
			blocked = append(blocked, facing.BlockedItem{ID: value, ReasonKey: navigationenums.ErrInvalidParam})
			continue
		}
		pending = append(pending, struct {
			id        string
			canonical string
			valid     bool
		}{id: id, canonical: canonical, valid: valid})
	}
	if len(blocked) > 0 {
		return nil, requested, blocked, nil
	}
	if len(pending) == 0 {
		return nil, 0, nil, nil
	}
	if len(pending) > bulkids.MaxIDs {
		return nil, requested, nil, &bulkids.IDsError{Count: len(pending), Max: bulkids.MaxIDs}
	}
	for _, item := range pending {
		if !item.valid {
			blocked = append(blocked, facing.BlockedItem{ID: item.id, ReasonKey: navigationenums.ErrInvalidParam})
			continue
		}
		entries = append(entries, sourceIDEntry{id: item.id, canonical: item.canonical})
	}
	return entries, requested, blocked, nil
}

// AddSources 把来源候选同一事务写入菜单：工程内容写锁、候选读取、完整预校验、
// 路径查重、排序与写入都在服务层；任一候选缺失、无公开路径、非法或与已有路径冲突
// 都整批拒绝，不逐条继续提交。
//
// 剩余边界：来源候选端口读取的是其它模块的实时视图，不共用本事务连接，因此本批
// 与来源模块之间不是强一致快照；本事务保证的是导航表自身的路径唯一与整批原子性。
func (s *Service) AddSources(ctx context.Context, req *navigationdto.AddSourceReq) (added int, err error) {
	if req == nil {
		return 0, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	projectID := strings.TrimSpace(req.ProjectID)
	kind := strings.TrimSpace(req.Kind)
	sourceType := normalizeSourceType(strings.TrimSpace(req.SourceType))
	if projectID == "" || strings.TrimSpace(req.SourceType) == "" {
		return 0, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	if !isValidKind(kind) {
		return 0, navigationenums.Err(navigationenums.ErrInvalidKind)
	}
	if sourceType == sourceCustom || !isValidSourceType(sourceType) {
		return 0, navigationenums.Err(navigationenums.ErrInvalidSource)
	}

	entries, requested, blocked, tooMany := normalizeSourceIDs(req.IDs)
	if tooMany != nil {
		return 0, tooMany
	}
	if len(blocked) > 0 {
		return 0, RejectBatch(requested, blocked)
	}
	if len(entries) == 0 {
		return 0, navigationenums.Err(navigationenums.ErrInvalidParam)
	}

	at := time.Now().UTC()
	targets := make([]sourceTarget, 0, len(entries))
	if werr := s.withMenuWriteTx(ctx, projectID, func(tx *gorm.DB) error {
		groups, cerr := s.SourceGroups(ctx, projectID)
		if cerr != nil {
			return cerr
		}
		candidates := make(map[string]sourceCandidate, len(entries))
		for _, group := range groups {
			if group.Type != sourceType {
				continue
			}
			for _, item := range group.Items {
				candidates[candidateKey(item.ID)] = sourceCandidate{
					Title: item.Title, Label: item.Label, URL: item.URL,
				}
			}
		}

		rows, lerr := s.m.ListTx(ctx, tx, projectID, kind)
		if lerr != nil {
			return lerr
		}
		existingPaths := make(map[string]struct{}, len(rows))
		maxSortOrder := 0
		for _, row := range rows {
			existingPaths[row.Path] = struct{}{}
			if (row.ParentID == nil || strings.TrimSpace(*row.ParentID) == "") && row.SortOrder > maxSortOrder {
				maxSortOrder = row.SortOrder
			}
		}

		plannedPaths := make(map[string]struct{}, len(entries))
		blocked := make([]facing.BlockedItem, 0)
		for _, entry := range entries {
			candidate, found := candidates[entry.canonical]
			if !found {
				blocked = append(blocked, facing.BlockedItem{ID: entry.id, ReasonKey: navigationenums.ErrNotFound})
				continue
			}
			path := strings.TrimSpace(candidate.URL)
			title := strings.TrimSpace(candidate.Title)
			if title == "" {
				title = strings.TrimSpace(candidate.Label)
			}
			if path == "" {
				blocked = append(blocked, facing.BlockedItem{
					ID: entry.id, Name: title, ReasonKey: navigationenums.NoPublicPath,
				})
				continue
			}
			if verr := validateField(title, path, kind); verr != nil {
				blocked = append(blocked, facing.BlockedItem{ID: entry.id, Name: title, ReasonKey: navigationenums.ErrInvalidParam})
				continue
			}
			st, sid, _, serr := normalizeSource(sourceType, &entry.canonical, targetSelf)
			if serr != nil || st == sourceCustom || sid == nil {
				blocked = append(blocked, facing.BlockedItem{ID: entry.id, Name: title, ReasonKey: navigationenums.ErrInvalidSource})
				continue
			}
			if _, exists := existingPaths[path]; exists {
				blocked = append(blocked, facing.BlockedItem{ID: entry.id, Name: title, ReasonKey: navigationenums.ErrPathTaken})
				continue
			}
			if _, duplicate := plannedPaths[path]; duplicate {
				blocked = append(blocked, facing.BlockedItem{ID: entry.id, Name: title, ReasonKey: navigationenums.ErrPathTaken})
				continue
			}
			plannedPaths[path] = struct{}{}
			targets = append(targets, sourceTarget{sourceID: *sid, title: title, path: path})
		}
		if len(blocked) > 0 {
			return RejectBatch(requested, blocked)
		}

		sortOrder := maxSortOrder
		for _, target := range targets {
			sortOrder++
			sourceID := target.sourceID
			entity := &navigationmodel.NavigationEntity{
				ID: uuid.NewString(), ProjectID: projectID, Title: target.title, Path: target.path,
				Kind: kind, SortOrder: sortOrder,
				SourceType: sourceType, SourceID: &sourceID, Target: targetSelf,
				CreatedAt: at, UpdatedAt: at,
			}
			if cerr := s.m.CreateTx(ctx, tx, entity); cerr != nil {
				return cerr
			}
			if nerr := s.markNavigationStaleInTx(ctx, tx, projectID, entity.ID); nerr != nil {
				return nerr
			}
		}
		return s.markMenuStaleInTx(ctx, tx, projectID, kind, at)
	}); werr != nil {
		return 0, werr
	}
	return len(targets), nil
}
