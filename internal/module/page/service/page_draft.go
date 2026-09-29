package pageservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"go_wp/internal/builder"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pagemodel "go_wp/internal/module/page/model"
	pubcontract "go_wp/internal/module/publication/contract"
	pubenums "go_wp/internal/module/publication/enums"
	"go_wp/internal/pipeline"
	"go_wp/pkg/utils"
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
	// 初始文档：给了蓝图就以蓝图为准（审计 VIS-010）。蓝图是「用完即弃」的初始化输入 ——
	// InitPageDocument 复制完整 AST 并递归生成新节点 ID，之后页面与蓝图再无关系。
	initial := req.DraftDocument
	if blueprintID := strings.TrimSpace(req.BlueprintID); blueprintID != "" {
		built, berr := s.initFromBlueprint(ctx, blueprintID)
		if berr != nil {
			return nil, berr
		}
		initial = built
	}
	if len(initial) == 0 {
		// 既没有文档也没有蓝图：明确拒绝。静默建空页最难被发现 ——
		// 后台显示新建成功，编辑者打开画布才发现是白的。
		return nil, ErrInvalidParam
	}
	path, doc, err := validateDraft(req.DraftPath, initial)
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
	// 预留路由（N 条，按站点启用语言）与建页必须落在**同一个事务**里：两处都是库内的
	// 写入（page_routes 归 publication、pages/page_revisions 归本模块），旧实现靠
	// 「建页失败再删预留」补偿，补偿本身失败时只剩一行日志，留下「预留了但页面不存在」
	// 的孤儿占用（路径永久被占、且没有任何入口能查到它）。
	// 事务回滚把两侧一起撤掉，补偿路径不存在，也就不存在补偿失败。
	if err = s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
		if rerr := s.reservePathTx(ctx, tx, page.ProjectID, path, page.ID); rerr != nil {
			return rerr
		}
		return s.model.CreateWithRevisionTx(ctx, tx, page, revision)
	}); err != nil {
		return nil, mapPersistenceError(err)
	}
	return pageResp(page), nil
}

// initFromBlueprint 从蓝图初始化页面文档（审计 VIS-010）。
//
// 蓝图未注入时报 ErrBlueprintUnavailable 而不是降级建空页：装配缺失是配置错误，
// 应该在创建那一刻就暴露，而不是让编辑者对着空白画布猜。
func (s *Service) initFromBlueprint(ctx context.Context, blueprintID string) (json.RawMessage, error) {
	if s.blueprints == nil {
		return nil, errors.New(pageenums.ErrBlueprintUnavailable)
	}
	doc, err := s.blueprints.InitPageDocument(ctx, blueprintID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", pageenums.ErrBlueprintInvalid, err)
	}
	if len(doc) == 0 {
		return nil, errors.New(pageenums.ErrBlueprintInvalid)
	}
	return doc, nil
}

// reservePathTx 在**调用方的事务**内经 publication contract 预留草稿路径（页面创建前置）。
// 占用冲突归一为 page 的 ErrPathOccupied；系统错误原样返回。
// routes 为 nil（降级/测试）时跳过预留。
//
// 多语言 P3：按站点启用语言（project_locales）逐语言登记占用行，一行冲突即整体失败
// —— 失败由外层事务回滚，不留「半套路由」与已建页面（旧实现是删掉本次已预留的行，
// 删除失败只能记日志）。
func (s *Service) reservePathTx(ctx context.Context, tx *gorm.DB, projectID, path, pageID string) error {
	if s.routes == nil {
		return nil
	}
	routePaths, err := s.siteRoutePaths(ctx, projectID, path)
	if err != nil {
		return ErrInvalidPath
	}
	for _, routePath := range routePaths {
		rerr := s.routes.ReservePathTx(ctx, tx, &pubcontract.ReserveReq{ProjectID: projectID, Path: routePath, PageID: pageID})
		if rerr == nil {
			continue
		}
		// 占用冲突归一：唯一约束冲突，或 publication 的 ErrRouteOccupied（资源 key）。
		// 注意不能按中文文案匹配——enums 值已 key 化，文案随语言变化。
		if errors.Is(rerr, gorm.ErrDuplicatedKey) || strings.Contains(rerr.Error(), "23505") ||
			strings.Contains(rerr.Error(), pubenums.ErrRouteOccupied) {
			return ErrPathOccupied
		}
		return rerr
	}
	return nil
}

// Detail 查询当前 Page Draft。
// nil/空 ID 或空 projectID 属于请求不合法（ErrInvalidParam）；合法 ID 无页面才返回 ErrPageNotFound。
func (s *Service) Detail(ctx context.Context, req *pagedto.DetailReq) (res *pagedto.PageResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.ProjectID) == "" {
		return nil, ErrInvalidParam
	}
	if err = s.requireProject(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	page, err := s.model.GetByID(ctx, req.ID, req.ProjectID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPageNotFound
	}
	if err != nil {
		return nil, err
	}
	res = pageResp(page)
	// 每语言激活状态（多语言 P3）：列表/详情投影里 ActivePath 只是单值镜像，
	// 多语言站点需要看到「哪些语言在线、各自路径与产物」。
	if res.Publications, err = s.publicationsOf(ctx, page.ID); err != nil {
		return nil, err
	}
	return res, nil
}

// ProjectOfPage 按页面 id 返回所属工程 id。
//
// 后台入口（画布预览 / 历史恢复 / 译文保存）手上只有 pageId，而 Detail 把 projectID
// 当作必填的越权防护 scope —— 少它只会得到 ErrInvalidParam，在页面上表现为 404
// 「页面不存在」，很难联想到是「少传了一个 scope 参数」。让调用方先问一次「这页属于
// 谁」再带 scope 去查，比给 Detail 开一个「不带工程过滤」的后门更安全：
// 越权防护的判据仍然只有一处（model.GetByID 的 projectID 参数）。
func (s *Service) ProjectOfPage(ctx context.Context, pageID string) (projectID string, err error) {
	id := strings.TrimSpace(pageID)
	if id == "" {
		return "", ErrInvalidParam
	}
	// 逐工程定位（DB-009 第四批）：本方法存在的意义就是「在没有工程上下文时问出归属」，
	// 不带作用域的直查在换非超级角色后会一律报「页面不存在」—— 那正好废掉它。
	page, err := s.locatePageInProjects(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrPageNotFound
	}
	if err != nil {
		return "", err
	}
	return page.ProjectID, nil
}

// publicationsOf 读取页面每语言激活状态投影（无记录时返回 nil）。
func (s *Service) publicationsOf(ctx context.Context, pageID string) (out []pagedto.PagePublicationResp, err error) {
	rows, err := s.model.ListPublications(ctx, pageID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out = make([]pagedto.PagePublicationResp, 0, len(rows))
	for _, r := range rows {
		at := r.PublishedAt
		out = append(out, pagedto.PagePublicationResp{
			Lang: r.Lang, ActivePath: r.ActivePath, ArtifactID: r.ArtifactID,
			ArtifactHash: r.ArtifactHash, PublishedAt: utils.NewJSONTimePtr(&at),
		})
	}
	return out, nil
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
	// 逐工程定位（DB-009 第四批）：page.ProjectID 是后续 SaveDraftWithRevision、
	// 路径迁移与修订收敛的作用域来源。
	page, err := s.locatePageInProjects(ctx, req.ID)
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
	// 保留路由迁移要按站点启用语言逐语言做，而语言集合**在事务之外**解析（审计 I18N-02 收尾）：
	// 事务内再读一次语言表，读失败就只会迁移默认语言的保留路由，其余语言的 reserved 行
	// 停在旧路径而草稿照常提交。这里是作者可操作的写入口，口径沿用软回退（enabledLangsOf，
	// 理由见 page_lang.go）：一次读库抖动不该让作者存不了草稿，降级后果可由下一次保存 /
	// 发布按完整清单补齐；改 URL / 发布那些「会写发布事实」的路径才用发布硬口径。
	routeLangs := s.enabledLangsOf(ctx, page.ProjectID)
	// 路径占用迁移与草稿提交落在**同一个事务**里：改到他人占用路径在此失败
	//（RenameReserved 撞 newPath 唯一约束），草稿保持原路径与版本不变；版本冲突
	//（乐观锁）时路径迁移随事务一并撤销。旧实现是「先迁路由、失败再回迁」，
	// 回迁失败只有一行日志 —— 路由表会停在「路径已改名、草稿没提交」的错位状态。
	if err = s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
		if changedPath {
			if rerr := s.renameReservedAllLangsTx(ctx, tx, renameReservedInput{
				ProjectID: page.ProjectID, PageID: page.ID,
				OldLogical: page.DraftPath, NewLogical: path, Langs: routeLangs,
			}); rerr != nil {
				return rerr
			}
		}
		return s.model.SaveDraftWithRevisionTx(ctx, tx, page.ProjectID, page.ID, page.DraftVersion,
			path, doc, nextVersion, now, revision)
	}); err != nil {
		return nil, mapPersistenceError(err)
	}
	page.DraftPath = path
	page.DraftDocument = doc
	page.DraftVersion = nextVersion
	page.Stale = true
	page.UpdatedAt = now
	// 保存后顺手收敛该页历史快照（IDX-005）：一次保存就是一份完整文档快照，
	// 等每日任务来清会让高频编辑的页面在一天内堆出大量副本。失败不影响保存结果。
	s.pruneRevisions(ctx, page.ProjectID, page.ID)
	return pageResp(page), nil
}

// ListRevisions 查询 Page 历史草稿快照（最新在前）。
// nil/空 PageID 属于请求不合法（ErrInvalidParam）；合法 ID 无页面才返回 ErrPageNotFound。
func (s *Service) ListRevisions(ctx context.Context, req *pagedto.RevisionReq) (res []pagedto.RevisionResp, err error) {
	if req == nil || strings.TrimSpace(req.PageID) == "" {
		return nil, ErrInvalidParam
	}
	// 先逐工程定位页面：page_revisions 没有 project_id 列、不受策略约束，
	// 「这页属于哪个工程」只能由父实体给出（DB-009 第四批）。
	page, err := s.locatePageInProjects(ctx, req.PageID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPageNotFound
	} else if err != nil {
		return nil, err
	}
	list, err := s.model.ListRevisions(ctx, page.ProjectID, req.PageID)
	if err != nil {
		return nil, err
	}
	res = make([]pagedto.RevisionResp, 0, len(list))
	for _, r := range list {
		res = append(res, pagedto.RevisionResp{
			ID: r.ID, PageID: r.PageID, Version: r.Version, DraftPath: r.DraftPath,
			DraftDocument: r.DraftDocument, SourceHash: r.SourceHash, CreatedAt: utils.NewJSONTime(r.CreatedAt),
		})
	}
	return res, nil
}

func (s *Service) requireProject(ctx context.Context, projectID string) error {
	// 空/空白工程 ID 属于参数错误（ErrInvalidParam）；合法 ID 无工程才返回 ErrProjectNotFound。
	if strings.TrimSpace(projectID) == "" {
		return ErrInvalidParam
	}
	// 装配缺失时给明确错误而不是 nil 解引用 panic：panic 会把「工程服务没接上」
	// 伪装成一次崩溃，而真正的信息（哪个方法调用、缺哪个依赖）反而丢了。
	if s.project == nil {
		return ErrProjectNotFound
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
	if pageenums.ValidatePageContentContract(kind, targetType, targetID) {
		return nil
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
		// 明细走 ErrorDetail 协议（可翻译），不是 builder 的中文原文 ——
		// ErrInvalidDocument 落 400，两个出口都会把它展示给用户（见 page_document_detail.go）。
		return "", nil, fmt.Errorf("%w: %s", ErrInvalidDocument, pageDocumentDetail(err))
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
		CreatedAt: utils.NewJSONTime(page.CreatedAt), UpdatedAt: utils.NewJSONTime(page.UpdatedAt),
	}
}

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
