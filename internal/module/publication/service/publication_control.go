package pubservice

import (
	"context"
	"encoding/json"
	"errors"
	"go_wp/internal/seo"
	"strings"
	"time"

	"go_wp/pkg/i18n"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RenameReserved 修改页面的草稿路径占用；仅允许 reserved 状态改名。
func (s *Service) RenameReserved(ctx context.Context, req *pubdto.RenameReservedReq) (err error) {
	if req == nil {
		return errors.New(pubenums.ErrInvalidParam)
	}
	oldPath, err := normalizePath(req.OldPath)
	if err != nil {
		return err
	}
	newPath, err := normalizePath(req.NewPath)
	if err != nil {
		return err
	}
	if oldPath == newPath {
		return nil
	}
	now := time.Now().UTC()
	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		// 区分「旧路径无任何占用」与「旧路径不是 reserved」：
		// 前者视为幂等成功（草稿路由可能尚未建立）；后者需按归属判断——
		// 本页 active/redirect 行是页面改 URL 流程（UpdateURL）的 DB 同步步骤
		// （发布时 reserved 被原地升级为 active，无独立 reserved 行），必须允许迁移；
		// 他人 active/redirect 行禁止直接改名（不应触碰他人线上路径）。
		var existing pubmodel.RouteEntity
		switch ferr := tx.Where("project_id = ? AND path = ?", req.ProjectID, oldPath).
			First(&existing).Error; {
		case errors.Is(ferr, gorm.ErrRecordNotFound):
			return nil
		case ferr != nil:
			return ferr
		}
		if existing.RouteKind != pubmodel.RouteReserved {
			// OnlyReserved：调用方（多语言下改其他语言 URL）只希望迁移草稿占用，
			// 明确要求不动本页 active/redirect 行。
			if req.OnlyReserved {
				return nil
			}
			if existing.PageID == nil || *existing.PageID != req.PageID {
				return errors.New(pubenums.ErrRouteActiveRename)
			}
		}
		// 先清理新路径上本页 active 残留（避免迁移行与 (project_id, path)
		// 唯一约束冲突），再迁移旧路径行（reserved 或本页 active）。
		if err := tx.Where("project_id = ? AND path = ? AND page_id = ? AND route_kind = ?",
			req.ProjectID, newPath, req.PageID, pubmodel.RouteActive).
			Delete(&pubmodel.RouteEntity{}).Error; err != nil {
			return err
		}
		result := tx.Model(&pubmodel.RouteEntity{}).
			Where("project_id = ? AND path = ? AND page_id = ?", req.ProjectID, oldPath, req.PageID).
			Updates(map[string]any{"path": newPath, "updated_at": now})
		if result.Error != nil {
			// 新路径被他人占用时 UPDATE 撞 (project_id, path) 唯一约束——
			// 归一为 ErrRouteOccupied（语义：改名目标路径已被其他页面占用）。
			if errors.Is(result.Error, gorm.ErrDuplicatedKey) || strings.Contains(result.Error.Error(), "23505") {
				return errRouteOccupied
			}
			return result.Error
		}
		if result.RowsAffected != 1 {
			// 旧路径被其他页面 reserved 占用（page_id 不匹配）：保持幂等成功，
			// 不触碰他人草稿占用。
			return nil
		}
		return nil
	})
	return err
}

// Activate 把路径占用切换为 active（两段式回执，docs/03-pipeline.md §9）：
//
//	第一段：pending 回执在独立事务中先行持久化并提交——进程在后续任一步
//	崩溃时，恢复流程（RollbackReceipts）有据可查；
//	第二段：路由事务内完成占用归属校验 + 路由切换 + 置 committed，三者原子；
//	路由事务失败时把 pending 回执补偿为 rolled_back（补偿失败则保持
//	pending 供下次恢复处理）。
//
// 占用归属校验：目标路径已被其他页面（或展示实例，page_id 为空）占用时
// 返回 ErrRouteOccupied，绝不覆盖他人占用（H2/H7 的 DB 层兜底）。
func (s *Service) Activate(ctx context.Context, req *pubdto.ActivateReq) (res *pubdto.RouteResp, err error) {
	if req == nil {
		return nil, errors.New(pubenums.ErrInvalidParam)
	}
	path, err := normalizePath(req.Path)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	receiptData, merr := json.Marshal(receiptPayload{To: req.ArtifactID})
	if merr != nil {
		receiptData = json.RawMessage(`{}`)
	}
	receipt := &pubmodel.ReceiptEntity{
		ID: uuid.NewString(), SourceType: "page", SourceID: req.PageID,
		Action: receiptAction(req.Action, "activate"), Path: path,
		ToArtifact: strPtr(req.ArtifactID), ReceiptState: pubmodel.ReceiptPending,
		ReceiptData: receiptData, CreatedAt: now,
	}

	// 第一段：pending 回执独立事务提交（故障恢复依据，H1）。
	if cerr := s.model.Transaction(ctx, func(tx *gorm.DB) error {
		return tx.Create(receipt).Error
	}); cerr != nil {
		logger.Scene("publication").With("url", path).With("kind", "activate").Error(cerr, "pending 回执写入失败")
		return nil, cerr
	}

	// 第二段：路由事务（占用归属校验 + 路由切换 + 置 committed）。
	//
	// 原子抢占：单条 INSERT ... ON CONFLICT DO UPDATE（PG 方言，主库）替代
	// 原「SELECT 无锁检查 → UPDATE → CREATE」三语句。语义：
	//   - 无既有占用            → 插入 active 行（RowsAffected=1）；
	//   - 既有占用且归属者本人    → 原地升级 active（含 reserved 升级，
	//                              RowsAffected=1，幂等重复激活）；
	//   - 既有占用且非归属者      → DO UPDATE WHERE 不匹配，PG 静默 DO NOTHING
	//                              （RowsAffected=0）→ ErrRouteOccupied。
	// 消除原实现 SELECT→CREATE 的 TOCTOU 窗口：并发抢占时败者不再产生失败的
	// CREATE 撞 23505 与补偿回执，唯一约束冲突在语句内被原子消化。
	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		pageIDCopy := req.PageID
		result := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "project_id"}, {Name: "path"}},
			// DO UPDATE 仅当冲突行归属者本人（page_id 相同）；他人页面或
			// 展示实例（page_id 为 NULL）时 WHERE 不成立 → 0 行 → occupied。
			Where: clause.Where{Exprs: []clause.Expression{
				clause.Expr{SQL: "page_routes.page_id = EXCLUDED.page_id"},
			}},
			DoUpdates: clause.AssignmentColumns([]string{"route_kind", "artifact_id", "updated_at"}),
		}).Create(&pubmodel.RouteEntity{
			ProjectID: req.ProjectID, Path: path, PageID: &pageIDCopy,
			RouteKind: pubmodel.RouteActive, ArtifactID: strPtr(req.ArtifactID), UpdatedAt: now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			// 目标路径被其他实体占用：ON CONFLICT 未执行更新。
			return errRouteOccupied
		}
		return markReceipt(tx, receipt.ID, pubmodel.ReceiptCommitted, now)
	})
	if err != nil {
		// 路由事务失败：pending → rolled_back（补偿失败保持 pending 供恢复）。
		if rberr := s.model.Transaction(ctx, func(tx *gorm.DB) error {
			return markReceipt(tx, receipt.ID, pubmodel.ReceiptRolledBack, time.Now().UTC())
		}); rberr != nil {
			logger.Scene("publication").With("url", path).With("receiptId", receipt.ID).
				Error(rberr, "pending 回执补偿失败（保持 pending 供恢复流程处理）")
		}
		if errors.Is(err, errRouteOccupied) {
			return nil, errors.New(pubenums.ErrRouteOccupied)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(pubenums.ErrRouteNotFound)
		}
		logger.Scene("publication").With("url", path).With("kind", "activate").Error(err, "路由激活失败")
		return nil, err
	}
	route, err := s.model.GetRoute(ctx, req.ProjectID, path)
	if err != nil {
		return nil, err
	}
	return routeResp(route), nil
}

// ReservePath 创建草稿路径 reserved 占用（页面创建时预留）。
// 路径已被其他实体占用（含展示实例）时返回 ErrRouteOccupied——
// 替代原 page model 三表事务里直接 INSERT reserved 撞主键的检测方式，
// 使 URL 占用单一归 publication 所有。
func (s *Service) ReservePath(ctx context.Context, req *pubdto.ReserveReq) (err error) {
	if req == nil {
		return errors.New(pubenums.ErrInvalidParam)
	}
	path, err := normalizePath(req.Path)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	pageIDCopy := req.PageID
	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		if cerr := tx.Create(&pubmodel.RouteEntity{
			ProjectID: req.ProjectID, Path: path, PageID: &pageIDCopy,
			RouteKind: pubmodel.RouteReserved, UpdatedAt: now,
		}).Error; cerr != nil {
			if errors.Is(cerr, gorm.ErrDuplicatedKey) || strings.Contains(cerr.Error(), "23505") {
				return errRouteOccupied
			}
			return cerr
		}
		return nil
	})
	if errors.Is(err, errRouteOccupied) {
		return errors.New(pubenums.ErrRouteOccupied)
	}
	return err
}

// DeleteRoutesByPage 清理页面全部路径占用（reserved/active/redirect 任一 kind），
// 页面删除时释放路径。幂等（无占用时 RowsAffected=0 不报错）。
func (s *Service) DeleteRoutesByPage(ctx context.Context, req *pubdto.DeleteRoutesReq) (err error) {
	if req == nil {
		return errors.New(pubenums.ErrInvalidParam)
	}
	result := s.model.RouteDB(ctx).
		Where("project_id = ? AND page_id = ?", req.ProjectID, req.PageID).
		Delete(&pubmodel.RouteEntity{})
	return result.Error
}

// IsPathOccupied 查询路径是否被其他实体占用（page_id 为空即展示实例占用，
// page_id 非 excludePageID 即他人页面占用），供页面创建/发布前预检。
func (s *Service) IsPathOccupied(ctx context.Context, req *pubdto.IsOccupiedReq) (occupied bool, err error) {
	if req == nil {
		return false, errors.New(pubenums.ErrInvalidParam)
	}
	path, err := normalizePath(req.Path)
	if err != nil {
		return false, err
	}
	var foreign int64
	if err = s.model.RouteDB(ctx).
		Where("project_id = ? AND path = ? AND (page_id IS NULL OR page_id <> ?)", req.ProjectID, path, req.ExcludePageID).
		Count(&foreign).Error; err != nil {
		return false, err
	}
	return foreign > 0, nil
}

// Deactivate 取消路径占用；路由不存在时幂等返回。
func (s *Service) Deactivate(ctx context.Context, req *pubdto.DeactivateReq) (err error) {
	if req == nil {
		return errors.New(pubenums.ErrInvalidParam)
	}
	path, err := normalizePath(req.Path)
	if err != nil {
		return err
	}
	result := s.model.RouteDB(ctx).
		Where("project_id = ? AND path = ? AND page_id IS NOT NULL AND route_kind = ?",
			req.ProjectID, path, pubmodel.RouteActive).
		Delete(&pubmodel.RouteEntity{})
	if result.Error != nil {
		logger.Scene("publication").With("url", path).With("kind", "deactivate").Error(result.Error, "路由取消失败")
		return result.Error
	}
	return nil
}

// Redirect 把旧路径占用改为 redirect 并指向重定向产物（两段式回执，与 Activate 对齐）。
//
//	第一段：pending 回执在独立事务中先行持久化——进程在后续任一步崩溃时
//	恢复流程（RollbackReceipts）有据可查；
//	第二段：路由事务内完成占用切换 + 置 committed，二者原子；失败时把
//	pending 回执补偿为 rolled_back（补偿失败保持 pending 供恢复）。
func (s *Service) Redirect(ctx context.Context, req *pubdto.RedirectReq) (res *pubdto.RouteResp, err error) {
	if req == nil {
		return nil, errors.New(pubenums.ErrInvalidParam)
	}
	oldPath, err := normalizePath(req.OldPath)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()

	// ArtifactID 允许为空（重定向产物不入库，DTO 契约）：回执不得写入空串 uuid。
	var toArtifact *string
	if strings.TrimSpace(req.ArtifactID) != "" {
		toArtifact = strPtr(req.ArtifactID)
	}
	receiptData, merr := json.Marshal(map[string]string{"redirect": req.ArtifactID})
	if merr != nil {
		receiptData = json.RawMessage(`{}`)
	}
	receipt := &pubmodel.ReceiptEntity{
		ID: uuid.NewString(), SourceType: "page", SourceID: req.PageID,
		Action: "redirect", Path: oldPath,
		ToArtifact: toArtifact, ReceiptState: pubmodel.ReceiptPending,
		ReceiptData: receiptData, CreatedAt: now,
	}

	// 第一段：pending 回执独立事务提交（故障恢复依据，对齐 Activate）。
	if cerr := s.model.Transaction(ctx, func(tx *gorm.DB) error {
		return tx.Create(receipt).Error
	}); cerr != nil {
		logger.Scene("publication").With("url", oldPath).With("kind", "redirect").Error(cerr, "pending 回执写入失败")
		return nil, cerr
	}

	// 第二段：路由事务（占用切换 + 置 committed）。
	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		result := tx.Model(&pubmodel.RouteEntity{}).
			Where("project_id = ? AND path = ? AND page_id = ?", req.ProjectID, oldPath, req.PageID).
			Updates(map[string]any{
				"route_kind": pubmodel.RouteRedirect,
				"updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			// 无既有占用时直接建立 redirect 行（幂等）。
			pageIDCopy := req.PageID
			if err := tx.Create(&pubmodel.RouteEntity{
				ProjectID: req.ProjectID, Path: oldPath, PageID: &pageIDCopy,
				RouteKind: pubmodel.RouteRedirect, UpdatedAt: now,
			}).Error; err != nil {
				// 对他人占用路径建 redirect 行撞 (project_id, path) 唯一约束——
				// 归一为 ErrRouteOccupied（语义：重定向目标路径已被其他页面占用）。
				if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "23505") {
					return errRouteOccupied
				}
				return err
			}
		}
		if toArtifact != nil {
			if err := tx.Model(&pubmodel.RouteEntity{}).
				Where("project_id = ? AND path = ?", req.ProjectID, oldPath).
				Update("artifact_id", *toArtifact).Error; err != nil {
				return err
			}
		}
		return markReceipt(tx, receipt.ID, pubmodel.ReceiptCommitted, now)
	})
	if err != nil {
		// 路由事务失败：pending → rolled_back（补偿失败保持 pending 供恢复）。
		if rberr := s.model.Transaction(ctx, func(tx *gorm.DB) error {
			return markReceipt(tx, receipt.ID, pubmodel.ReceiptRolledBack, time.Now().UTC())
		}); rberr != nil {
			logger.Scene("publication").With("url", oldPath).With("receiptId", receipt.ID).
				Error(rberr, "pending 回执补偿失败（保持 pending 供恢复流程处理）")
		}
		if errors.Is(err, errRouteOccupied) {
			return nil, errors.New(pubenums.ErrRouteOccupied)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(pubenums.ErrRouteNotFound)
		}
		logger.Scene("publication").With("url", oldPath).With("kind", "redirect").Error(err, "路由重定向失败")
		return nil, err
	}
	route, err := s.model.GetRoute(ctx, req.ProjectID, oldPath)
	if err != nil {
		return nil, err
	}
	return routeResp(route), nil
}

// RollbackReceipts 启动恢复：全部 pending 回执标记 rolled_back，返回处理数量。
func (s *Service) RollbackReceipts(ctx context.Context) (count int64, err error) {
	now := time.Now().UTC()
	result := s.model.ReceiptDB(ctx).
		Where("receipt_state = ?", pubmodel.ReceiptPending).
		Updates(map[string]any{"receipt_state": pubmodel.ReceiptRolledBack, "completed_at": now})
	return result.RowsAffected, result.Error
}

func receiptAction(action, fallback string) string {
	if strings.TrimSpace(action) == "" {
		return fallback
	}
	return action
}

// errRouteOccupied 事务内占位冲突哨兵，外层映射为 pubenums.ErrRouteOccupied。
var errRouteOccupied = errors.New(pubenums.ErrRouteOccupied)

// receiptPayload 回执数据结构化序列化（替代手工拼接 JSON，避免特殊字符生成非法 jsonb）。
type receiptPayload struct {
	To string `json:"to,omitempty"`
}

func markReceipt(tx *gorm.DB, id, state string, now time.Time) error {
	return tx.Model(&pubmodel.ReceiptEntity{}).
		Where("id = ?", id).
		Updates(map[string]any{"receipt_state": state, "completed_at": now}).Error
}

// maxRoutePathLen 路由路径长度上限（超长路径会导致 FS 激活 ENAMETOOLONG 与 DB 行膨胀）。
const maxRoutePathLen = 500

// normalizePath 规范化路径：连续去除结尾斜杠（根路径除外），
// 并拒绝长度超限、含空格/URL 分隔符/引号/控制字符、路径穿越的输入。
// 非法输入属于参数格式错误，统一返回 ErrInvalidParam（与资源占用语义区分）。
func normalizePath(raw string) (string, error) {
	if raw == "" || !strings.HasPrefix(raw, "/") {
		return "", errors.New(pubenums.ErrInvalidParam)
	}
	for len(raw) > 1 && strings.HasSuffix(raw, "/") {
		raw = strings.TrimSuffix(raw, "/")
	}
	if len(raw) > maxRoutePathLen {
		return "", errors.New(pubenums.ErrInvalidParam)
	}
	for _, r := range raw {
		if r == ' ' || r == '?' || r == '#' || r == '"' || r == '\'' || r == '\\' || r < 0x20 || r == 0x7f {
			return "", errors.New(pubenums.ErrInvalidParam)
		}
	}
	if strings.Contains(raw, "/../") || strings.Contains(raw, "/./") ||
		strings.HasSuffix(raw, "/..") || strings.HasSuffix(raw, "/.") {
		return "", errors.New(pubenums.ErrInvalidParam)
	}
	return raw, nil
}

func strPtr(s string) *string { return &s }

func routeResp(e *pubmodel.RouteEntity) *pubdto.RouteResp {
	return &pubdto.RouteResp{
		ProjectID: e.ProjectID, Path: e.Path, PageID: e.PageID,
		RouteKind: e.RouteKind, ArtifactID: e.ArtifactID, UpdatedAt: e.UpdatedAt,
	}
}

// RefreshSiteFiles 生成/刷新站点级 SEO 产物（sitemap.xml + robots.txt）。
//
// langs 为站点启用语言（默认语言在前），defaultLang 用于 x-default（多语言 P3）。
// 语言清单由调用方（page 装配层，持有 project 契约）传入——publication 不跨模块查语言。
func (s *Service) RefreshSiteFiles(ctx context.Context, projectID, baseURL, dir string, langs []string, defaultLang string) (err error) {
	if projectID == "" || dir == "" {
		return nil
	}
	paths, err := s.model.ListActivePaths(ctx, projectID)
	if err != nil {
		return err
	}
	return seo.WriteSiteFiles(dir, baseURL, sitemapEntries(baseURL, paths, langs, defaultLang))
}

// sitemapEntries 已激活路径 → sitemap 条目。
//
// 多语言（开启前缀且 ≥2 语言）时按「逻辑路径」分组：同一逻辑路径的各语言版本
// 互相输出 xhtml:link 互指（含 x-default）。单语言或未开启前缀时输出与 P3 之前一致。
func sitemapEntries(baseURL string, paths, langs []string, defaultLang string) []seo.SitemapEntry {
	if !i18n.SiteLangURLsSeparated() || len(langs) < 2 {
		return seo.EntriesFromPaths(baseURL, paths)
	}
	// 语言归属用与构建期完全相同的规则（唯一映射点 pipeline.LangURLRule）：
	// default_plain 下 /about 归属默认语言、/en/about 归属 en-US，两者互为一组。
	rule := pipeline.NewLangURLRule(true, i18n.SiteLangURLPrefixDefault(), defaultLang, i18n.URLCodeOverrides())
	byLogical := map[string]map[string]string{}
	logicalOf := map[string]string{}
	for _, p := range paths {
		lang, logical, ok := rule.Locate(p, langs)
		if !ok {
			continue
		}
		if byLogical[logical] == nil {
			byLogical[logical] = map[string]string{}
		}
		byLogical[logical][lang] = p
		logicalOf[p] = logical
	}
	out := make([]seo.SitemapEntry, 0, len(paths))
	for _, p := range paths {
		entry := seo.EntryForPath(baseURL, p)
		logical, ok := logicalOf[p]
		if !ok {
			out = append(out, entry)
			continue
		}
		group := byLogical[logical]
		if len(group) < 2 {
			out = append(out, entry)
			continue
		}
		for _, l := range langs {
			alt, exists := group[l]
			if !exists {
				continue
			}
			entry.Alternates = append(entry.Alternates, seo.SitemapAlternate{
				Lang: l, Href: seo.JoinURL(baseURL, alt),
			})
		}
		if alt, exists := group[defaultLang]; exists {
			entry.Alternates = append(entry.Alternates, seo.SitemapAlternate{
				Lang: "x-default", Href: seo.JoinURL(baseURL, alt),
			})
		}
		out = append(out, entry)
	}
	return out
}

// 语言归属判定已下沉到 pipeline.LangURLRule.Locate（构建期与 sitemap 同一份规则），
// 见 sitemapEntries：默认语言无前缀方案下，未带任何已知短码前缀的路径归属默认语言。
