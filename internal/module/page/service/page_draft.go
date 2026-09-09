package pageservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go_wp/internal/builder"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	pubcontract "go_wp/internal/module/publication/contract"
	pubenums "go_wp/internal/module/publication/enums"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Create 创建 Page、初始 Draft 与 Revision，并原子保留路径。
// nil 请求属于请求不合法（ErrInvalidParam）；文档非法仍返回 ErrInvalidDocument。
func (s *Service) Create(ctx context.Context, req *pagedto.CreateReq) (res *pagedto.PageResp, err error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	if err = s.requireProject(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	if err = validateKind(req.Kind, req.ContentTargetType, req.ContentTargetID); err != nil {
		return nil, err
	}
	path, doc, err := validateDraft(req.DraftPath, req.DraftDocument)
	if err != nil {
		return nil, err
	}
	// 站点主题合入页面文档（保存时快照，改主题时批量刷新）。
	if doc, err = s.mergeActiveTheme(ctx, req.ProjectID, doc); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	page := &pagemodel.PageEntity{
		ID: uuid.NewString(), ProjectID: req.ProjectID, Kind: req.Kind,
		ContentTargetType: req.ContentTargetType, ContentTargetID: req.ContentTargetID,
		DraftPath: path, DraftDocument: doc, DraftVersion: 1, Stale: true,
		CreatedAt: now, UpdatedAt: now,
	}
	// 新页面自动挂到工程当前激活主题（无主题时保持空，由主题创建后回填）。
	if themeID := s.ActiveThemeID(ctx, req.ProjectID); themeID != "" {
		page.ThemeID = &themeID
	}
	revision := &pagemodel.RevisionEntity{
		ID: uuid.NewString(), PageID: page.ID, Version: page.DraftVersion,
		DraftPath: path, DraftDocument: doc, SourceHash: hash(doc), CreatedAt: now,
	}
	// 先经 publication contract 预留草稿路径（冲突返回 ErrPathOccupied），
	// 成功后再原子创建 page + revision；建页失败释放预留（可恢复，无永久分裂）。
	if err = s.reservePath(ctx, page.ProjectID, path, page.ID); err != nil {
		return nil, err
	}
	if err = s.model.CreateWithRevision(ctx, page, revision); err != nil {
		// 建页失败：释放已预留的路径，避免「路径占用残留但页面不存在」。
		if s.routes != nil {
			if derr := s.routes.DeleteRoutesByPage(ctx, &pubcontract.DeleteRoutesReq{ProjectID: page.ProjectID, PageID: page.ID}); derr != nil {
				logger.Scene("page").With("pageId", page.ID).Error(derr, "建页失败后释放预留路径失败")
			}
		}
		return nil, mapPersistenceError(err)
	}
	return pageResp(page), nil
}

// reservePath 经 publication contract 预留草稿路径（页面创建前置）。
// 占用冲突归一为 page 的 ErrPathOccupied；系统错误原样返回。
// routes 为 nil（降级/测试）时跳过预留。
// path 为逻辑草稿路径，此处按「实际访问路径」登记（多语言开启前缀时带前缀，
// 语言取站点默认语言——建页时语言未知，见 siteRoutePath）。
func (s *Service) reservePath(ctx context.Context, projectID, path, pageID string) error {
	if s.routes == nil {
		return nil
	}
	routePath, err := siteRoutePath(path)
	if err != nil {
		return ErrInvalidPath
	}
	err = s.routes.ReservePath(ctx, &pubcontract.ReserveReq{ProjectID: projectID, Path: routePath, PageID: pageID})
	if err == nil {
		return nil
	}
	// 占用冲突归一：唯一约束冲突，或 publication 的 ErrRouteOccupied（资源 key）。
	// 注意不能按中文文案匹配——enums 值已 key 化，文案随语言变化。
	if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "23505") ||
		strings.Contains(err.Error(), pubenums.ErrRouteOccupied) {
		return ErrPathOccupied
	}
	return err
}

// Detail 查询当前 Page Draft。
// nil/空 ID 属于请求不合法（ErrInvalidParam）；合法 ID 无页面才返回 ErrPageNotFound。
func (s *Service) Detail(ctx context.Context, req *pagedto.DetailReq) (res *pagedto.PageResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrInvalidParam
	}
	page, err := s.model.GetByID(ctx, req.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPageNotFound
	}
	if err != nil {
		return nil, err
	}
	return pageResp(page), nil
}

// SaveDraft 使用 draftVersion 乐观锁保存完整 Draft AST，并追加不可变 Revision。
// 本方法不会修改 active/staged Artifact；任何 URL 新旧占用变化与 Revision 在同一事务提交。
// nil/空 ID 属于请求不合法（ErrInvalidParam）；合法 ID 无页面才返回 ErrPageNotFound。
func (s *Service) SaveDraft(ctx context.Context, req *pagedto.SaveDraftReq) (res *pagedto.PageResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrInvalidParam
	}
	path, doc, err := validateDraft(req.DraftPath, req.DraftDocument)
	if err != nil {
		return nil, err
	}
	page, err := s.model.GetByID(ctx, req.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPageNotFound
	}
	if err != nil {
		return nil, err
	}
	if req.ExpectedVersion != page.DraftVersion {
		return nil, ErrDraftVersionConflict
	}
	// 站点主题合入（保存时快照）。
	if doc, err = s.mergeActiveTheme(ctx, page.ProjectID, doc); err != nil {
		return nil, err
	}

	nextVersion := page.DraftVersion + 1
	now := time.Now().UTC()
	revision := &pagemodel.RevisionEntity{
		ID: uuid.NewString(), PageID: page.ID, Version: nextVersion,
		DraftPath: path, DraftDocument: doc, SourceHash: hash(doc), CreatedAt: now,
	}
	changedPath := page.DraftPath != path
	// 改路径时先经 publication contract 迁移 reserved 占用：改到他人占用路径
	// 在此失败（RenameReserved 撞 newPath 唯一约束），草稿尚未提交，保持原路径
	// 与版本不变（保留原三表事务的「路径冲突整体回滚」语义）。
	if changedPath && s.routes != nil {
		oldRoutePath, oerr := siteRoutePath(page.DraftPath)
		newRoutePath, nerr := siteRoutePath(path)
		if oerr != nil || nerr != nil {
			return nil, ErrInvalidPath
		}
		if rerr := s.routes.RenameReserved(ctx, &pubcontract.RenameReservedReq{
			ProjectID: page.ProjectID, PageID: page.ID,
			OldPath: oldRoutePath, NewPath: newRoutePath,
		}); rerr != nil {
			return nil, mapPersistenceError(rerr)
		}
	}
	if err = s.model.SaveDraftWithRevision(ctx, page.ID, page.DraftVersion,
		path, doc, nextVersion, now, revision); err != nil {
		// 草稿提交失败（版本冲突）：已迁移的 reserved 需回迁，保持路径占用与草稿一致。
		if changedPath && s.routes != nil {
			oldRoutePath, oerr := siteRoutePath(path)
			newRoutePath, nerr := siteRoutePath(page.DraftPath)
			if oerr == nil && nerr == nil {
				if rerr := s.routes.RenameReserved(ctx, &pubcontract.RenameReservedReq{
					ProjectID: page.ProjectID, PageID: page.ID,
					OldPath: oldRoutePath, NewPath: newRoutePath,
				}); rerr != nil {
					logger.Scene("page").With("pageId", page.ID).Error(rerr, "草稿提交失败后回迁保留路由失败")
				}
			}
		}
		return nil, mapPersistenceError(err)
	}
	page.DraftPath = path
	page.DraftDocument = doc
	page.DraftVersion = nextVersion
	page.Stale = true
	page.UpdatedAt = now
	return pageResp(page), nil
}

// ListRevisions 查询 Page 历史草稿快照（最新在前）。
// nil/空 PageID 属于请求不合法（ErrInvalidParam）；合法 ID 无页面才返回 ErrPageNotFound。
func (s *Service) ListRevisions(ctx context.Context, req *pagedto.RevisionReq) (res []pagedto.RevisionResp, err error) {
	if req == nil || strings.TrimSpace(req.PageID) == "" {
		return nil, ErrInvalidParam
	}
	if _, err = s.model.GetByID(ctx, req.PageID); errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPageNotFound
	} else if err != nil {
		return nil, err
	}
	list, err := s.model.ListRevisions(ctx, req.PageID)
	if err != nil {
		return nil, err
	}
	res = make([]pagedto.RevisionResp, 0, len(list))
	for _, r := range list {
		res = append(res, pagedto.RevisionResp{
			ID: r.ID, PageID: r.PageID, Version: r.Version, DraftPath: r.DraftPath,
			DraftDocument: r.DraftDocument, SourceHash: r.SourceHash, CreatedAt: r.CreatedAt,
		})
	}
	return res, nil
}

func (s *Service) requireProject(ctx context.Context, projectID string) error {
	// 空/空白工程 ID 属于参数错误（ErrInvalidParam）；合法 ID 无工程才返回 ErrProjectNotFound。
	if strings.TrimSpace(projectID) == "" {
		return ErrInvalidParam
	}
	exists, err := s.project.Exists(ctx, projectID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrProjectNotFound
	}
	return nil
}

// validateKind 执行 pages 表同等领域约束，防止无效 Kind/ContentTarget 入库。
func validateKind(kind, targetType string, targetID *string) error {
	if targetID != nil && strings.TrimSpace(*targetID) == "" {
		return ErrInvalidKind
	}
	noTarget := func() bool { return targetType == "none" && targetID == nil }
	target := func(expected string) bool { return targetType == expected && targetID != nil }
	switch kind {
	case "home", "archive", "search", "notFound":
		if noTarget() {
			return nil
		}
	case "page", "article", "product", "category", "tag":
		if target(kind) {
			return nil
		}
	}
	return ErrInvalidKind
}

// validateDraft 解析并验证 Page Document，再规范化 URL。
func validateDraft(rawPath string, rawDoc json.RawMessage) (path string, doc json.RawMessage, err error) {
	path, err = normalizePagePath(rawPath)
	if err != nil {
		return "", nil, err
	}
	page, err := builder.ParsePage(rawDoc)
	if err != nil {
		return "", nil, ErrInvalidDocument
	}
	// 容错校验：编辑中间态允许「某个组件还没配好」（编译时跳过该节点），
	// 只拦截致命问题（设置非法 / 深度超限）。
	if _, err = builder.ValidatePageTolerant(page); err != nil {
		return "", nil, fmt.Errorf("%w: %v", ErrInvalidDocument, err)
	}
	// 重新编码保证存储 JSON 的规范格式；Document 不接受任意散乱字节。
	doc, err = json.Marshal(page)
	if err != nil {
		return "", nil, ErrInvalidDocument
	}
	return path, doc, nil
}

func normalizePagePath(raw string) (string, error) {
	path, err := pipeline.NormalizeURL(raw)
	if err != nil {
		return "", ErrInvalidPath
	}
	return path, nil
}

func mapPersistenceError(err error) error {
	if errors.Is(err, pagemodel.ErrDraftVersionConflict) {
		return ErrDraftVersionConflict
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "duplicate key") || strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
		return ErrPathOccupied
	}
	// publication contract 的占用错误（ErrRouteOccupied key）归一为 page 的 ErrPathOccupied。
	if strings.Contains(err.Error(), pubenums.ErrRouteOccupied) {
		return ErrPathOccupied
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrPageNotFound
	}
	return err
}

func pageResp(page *pagemodel.PageEntity) *pagedto.PageResp {
	themeID := ""
	if page.ThemeID != nil {
		themeID = *page.ThemeID
	}
	return &pagedto.PageResp{
		ID: page.ID, ProjectID: page.ProjectID, ThemeID: themeID, Kind: page.Kind, ContentTargetType: page.ContentTargetType,
		ContentTargetID: page.ContentTargetID, DraftPath: page.DraftPath, ActivePath: page.ActivePath,
		StagedArtifactID: page.StagedArtifactID, ActiveArtifactID: page.ActiveArtifactID,
		DraftDocument: page.DraftDocument, DraftVersion: page.DraftVersion, Stale: page.Stale,
		CreatedAt: page.CreatedAt, UpdatedAt: page.UpdatedAt,
	}
}

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
