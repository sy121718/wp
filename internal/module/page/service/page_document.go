package pageservice

// 写侧为什么需要它：page_draft.go 的保存/创建路径把 builder 的校验错误拼进
//
//	fmt.Errorf("%w: %v", ErrInvalidDocument, builderErr)
//
// 而 ErrInvalidDocument 在 page_handle.go 的 pageErrorStatus 里落 **400**（不是 500），
// 于是两个出口都会把它给用户看：JSON 出口的 pageErrorMessage、页面出口的 pageFacingText。
// 改造前那半句是 builder 的硬编码中文（「顶级节点 0: 组件树深度 11 超过上限 10…」），
// 英文界面上必然中英混排。
//
// 判据为什么是**类型**而不是错误文本：文本嗅探（strings.Contains(err, "深度")）改一个字
// 就静默失效，失效方向还是「明细消失」；builder 侧已把三类致命校验错误做成类型
//（internal/builder/page_validate_err.go），这里用 errors.As 取参数即可，错误文本一字不改。
//
// 兜底：识别不出类别时给一条概括性明细（DetailDocStructureInvalid），而不是空串 ——
// 空串会让「页面文档不合法」变成一句没头没尾的话，用户不知道该改哪里。

// 这一层只做工程校验与委托：路径归一化（去重、剔空）与 SQL 谓词都在 model 里，
// 这里再写一遍就会出现两份「什么算同一路径」的判定。

// 装配编译（方案 C，021_blocks.sql）：内核 CompileFn 注入。
// 页面文档 settings.structure 快照了主题的页眉/页脚块绑定，
// 构建时在此拉取块文档分别编译，HTML/CSS 拼接进页面产物——
// 访问面保持纯静态（无运行时拼接），块内容变更通过 stale 传播触发重建。
// 页面文档内的 core.globalref 节点经 BlockResolver 同样内联展开。

// workbench 工作台预览（Preview/PreviewDraft/BlockPreview）复用本方法，
// 与正式构建共用 compileDocument 装配管线（docs/03-A §4.2 隔离预览）。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/internal/module/block/contract"
	"go_wp/internal/module/media/contract"
	"go_wp/internal/module/page/contract"
	"go_wp/internal/module/page/dto"
	"go_wp/internal/module/page/enums"
	"go_wp/internal/module/page/model"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/module/publication/contract"
	"go_wp/internal/module/publication/enums"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
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

// List 列出页面摘要（不含草稿文档；DraftDocument 为空）。
// 必须带 projectID；themeID 为空时列该工程全部，非空时只列挂在该主题下的页面。
func (s *Service) List(ctx context.Context, req *pagedto.ListReq) (res []pagedto.PageResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, ErrInvalidParam
	}
	if err = s.requireProject(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	entities, err := s.model.ListAll(ctx, req.ProjectID, req.ThemeID)
	if err != nil {
		return nil, err
	}
	res = make([]pagedto.PageResp, 0, len(entities))
	for i := range entities {
		res = append(res, *pageResp(&entities[i]))
	}
	return res, nil
}

// ListPageTitles 列出工程内页面的标题投影（id / 草稿路径 / 激活路径 / SEO 标题）。
//
// 给「要页面标题、但不解析页面文档」的消费方用（导航来源候选）：List 走 model.ListAll，
// 而 ListAll 刻意 Omit("draft_document")，调用方拿到的是空文档、读不出标题 ——
// 于是「页面标题」这一支只能回退成路径（2026-09 修复的用户可见缺陷）。
// 本方法不改动 ListAll 的性能设计，改用 model.ListPageTitles 在 SQL 侧取标题：
// 代价是一行短文本，而不是把整份 JSONB 拉到 Go 侧再解析。
//
// SEOTitle 为空表示作者没在文档 SEO 段填标题，调用方自行回退（读侧不替它编名字）。
func (s *Service) ListPageTitles(ctx context.Context, projectID string) (res []pagedto.PageTitleResp, err error) {
	projectID = strings.TrimSpace(projectID)
	if err = s.requireProject(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := s.model.ListPageTitles(ctx, projectID)
	if err != nil {
		return nil, err
	}
	res = make([]pagedto.PageTitleResp, 0, len(rows))
	for i := range rows {
		res = append(res, pagedto.PageTitleResp{
			ID:         rows[i].ID,
			Kind:       rows[i].Kind,
			DraftPath:  rows[i].DraftPath,
			ActivePath: rows[i].ActivePath,
			SEOTitle:   rows[i].SEOTitle,
		})
	}
	return res, nil
}

// ListDrafts 列出全部未删除页面的草稿文档（多语言 P5c 翻译工作台的全站扫描）。
//
// 只读投影：供工作台按构建期同一套白名单（builder.CollectContentCandidates）
// 统计「同一译文还用在哪些页面」与「全站翻译完成度」。调用方负责缓存（一次扫描
// 读全站草稿 JSONB，代价见 docs/06-D §7.8 与 §15.12）。
//
// 逐工程扇出（DB-009 第三批）：pages 带 FORCE 策略，「全站」由各工程各自一次作用域
// 的查询拼出来。漏作用域时它在换非超级角色后静默返回空集 —— 工作台会显示
// 「全站 0 条草稿 / 翻译完成度 100%」，而不报任何错。
func (s *Service) ListDrafts(ctx context.Context) (res []pagedto.PageDraftResp, err error) {
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	var entities []pagemodel.PageEntity
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		part, lerr := s.model.ListDraftDocuments(ctx, projectID)
		if lerr != nil {
			return nil, lerr
		}
		entities = append(entities, part...)
	}
	res = make([]pagedto.PageDraftResp, 0, len(entities))
	for i := range entities {
		res = append(res, pagedto.PageDraftResp{
			ID: entities[i].ID, ProjectID: entities[i].ProjectID,
			DraftPath: entities[i].DraftPath, DraftDocument: entities[i].DraftDocument,
			UpdatedAt: utils.NewJSONTime(entities[i].UpdatedAt),
		})
	}
	return res, nil
}

// Delete 软删页面：deleted_at 置时间（审计留痕，行保留），并释放该页面
// 全部路径占用（reserved/active/redirect）。释放后同路径可被新页面重新创建，
// 解决「软删后路由残留、路径永久占用」的能力缺口。
// nil/空 ID 属于请求不合法（ErrInvalidParam）；页面不存在或已软删统一返回 ErrPageNotFound。
//
// 顺序：解除访问面激活（删 active 符号链接）→ 媒体引用清理 → **同一事务**里
// 清理路由占用 + 软删页面。
//
// 为什么必须先解激活：/site 直接服务 active 目录的文件系统状态（不查 DB），
// 只清路由行不会让内容下线；而清完路由就查不到「该删哪个链接」了，
// 残留符号链接会变成不可恢复的幽灵页面。
//
// 可重入：前三步都是幂等的（未激活时 Deactivate 返回 nil；DeleteRoutesByPage 无行
// 也不报错；媒体引用同步按 refKind+refID 全量替换）。任一步失败后重发同一次 Delete
// 即可继续 —— 页面记录仍在（软删只在最后一步），不会出现「删了一半再也删不掉」。
//
// DB 两步同事务：只清路由不软删会留下「页面还在但路径全释放」的窗口（新页面可以抢占
// 同一路径，而旧页面仍可被保存/发布）；只软删不清路由则留下永久占用。合并后两者
// 要么都生效、要么都不生效，残留只可能停在「访问面已下线、DB 尚未落定」，
// 而那是重发一次 Delete 就能收敛的形态。
func (s *Service) Delete(ctx context.Context, req *pagedto.DeleteReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return ErrInvalidParam
	}
	// 先定位页面拿 projectID（路由清理需要 project 维度）。逐工程探测（DB-009 第四批）：
	// 删除请求只带 pageId，而 pages 带 FORCE 策略 —— 不带作用域的直查在换非超级角色后
	// 会一律报「页面不存在」，删除功能整体失效。
	page, err := s.locatePageInProjects(ctx, req.ID)
	if err != nil {
		return mapPersistenceError(err)
	}
	if s.routes != nil {
		// 先按已激活路径把页面从访问面下线，再清 DB 路由占用。
		paths, lerr := s.routes.ListActivePaths(ctx, &pubcontract.ListActivePathsReq{
			ProjectID: page.ProjectID, PageID: req.ID,
		})
		if lerr != nil {
			return lerr
		}
		if err = s.deactivatePaths(paths); err != nil {
			return err
		}
	}
	// 先清媒体引用再软删：引用缓存失败则整单删除中断，避免留下「页面已删、引用仍在」的残留。
	// 媒体引用表归 media 模块（无 …Tx 变体），因此它在事务之外，且必须在软删之前 ——
	// 软删之后媒体模块就读不到这个 refID 对应的引用了。
	if s.media != nil {
		if _, rerr := s.media.SyncReferences(ctx, &mediacontract.SyncRefsInput{
			RefKind:  "page",
			RefID:    req.ID,
			RefTitle: page.DraftPath,
		}); rerr != nil {
			return rerr
		}
	}
	// DB 两步同事务：路由占用清理（publication 的 …Tx）+ 软删（含 page_publications /
	// page_stagings 清理）。软删带工程作用域（DB-009 第二批）：pages 带 FORCE 策略，
	// 越界写会被 WITH CHECK 直接拒绝而不是静默改到别的工程。
	if err = s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
		if s.routes != nil {
			if rerr := s.routes.DeleteRoutesByPageTx(ctx, tx, &pubcontract.DeleteRoutesReq{
				ProjectID: page.ProjectID, PageID: req.ID,
			}); rerr != nil {
				return rerr
			}
		}
		return s.model.SoftDeleteTx(ctx, tx, page.ProjectID, req.ID, time.Now().UTC())
	}); err != nil {
		return mapPersistenceError(err)
	}
	return nil
}

// deactivatePaths 解除一组路径的访问面激活（删除 active 符号链接，幂等）。
// publication 为 nil（降级装配 / 单元测试）时跳过。
func (s *Service) deactivatePaths(paths []string) error {
	if s.publication == nil {
		return nil
	}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if derr := s.publication.Deactivate(p); derr != nil {
			return fmt.Errorf("解除访问面激活失败 %s: %w", p, derr)
		}
	}
	return nil
}

// pageDocumentDetail 把 builder 的文档校验错误映射成一条可翻译的补充说明编码串。
//
// 返回空串只在 i18n.ErrorDetail 判定「参数值含协议分隔符」时发生（本函数的参数都是
// strconv 出来的数字，实际不会）；这时调用点得到的是「key：」形态的业务错误，
// 读侧按协议丢弃空明细 —— 不显示半截句子。
func pageDocumentDetail(err error) string {
	if err == nil {
		return i18n.ErrorDetail(pageenums.DetailDocStructureInvalid)
	}
	var depthErr *builder.NodeDepthError
	if errors.As(err, &depthErr) {
		return i18n.ErrorDetail(pageenums.DetailDocNodeDepthExceed,
			"index", strconv.Itoa(depthErr.Index),
			"depth", strconv.Itoa(depthErr.Depth),
			"max", strconv.Itoa(depthErr.Max))
	}
	var nodeErr *builder.NodeInvalidError
	if errors.As(err, &nodeErr) {
		return i18n.ErrorDetail(pageenums.DetailDocNodeInvalid, "index", strconv.Itoa(nodeErr.Index))
	}
	var setErr *builder.PageSettingsError
	if errors.As(err, &setErr) {
		return i18n.ErrorDetail(pageenums.DetailDocSettingsInvalid)
	}
	if errors.Is(err, builder.ErrPageDocumentEmpty) {
		return i18n.ErrorDetail(pageenums.DetailDocEmpty)
	}
	return i18n.ErrorDetail(pageenums.DetailDocStructureInvalid)
}

// KindsOfPaths 按已发布的访问路径批量反查页面类型（path → kind）。
//
// 返回的 map 只含**本工程内、未删除、且已发布**的路径；查不到的路径不出现在结果里
// （调用方据此把它们排除，而不是猜一个默认类型）。
func (s *Service) KindsOfPaths(ctx context.Context, projectID string, paths []string) (kinds map[string]string, err error) {
	projectID = strings.TrimSpace(projectID)
	if err = s.requireProject(ctx, projectID); err != nil {
		return nil, err
	}
	return s.model.KindsOfPaths(ctx, projectID, paths)
}

// errCompileFailed 标记编译阶段失败。compileDocument 以 %w 包裹，
// 调用方经 errors.Is 区分「编译失败」与「组件模板加载/文档渲染失败」——
// 预览需要据此分类 422（编译失败）与 500（其余内部错误），构建路径仅关心 err != nil。
var errCompileFailed = errors.New("页面编译失败")

// previewValidationProblem 复算容错校验，判断这次编译失败是不是「作者可操作的组件配置问题」。
//
// 为什么在失败之后复算，而不是让 builder.Compile 直接返回带标记的错误：builder 是共享构建
// 内核，它的错误形状被构建期与其它模块一起依赖，改它要动别人的调用面；这里只需要在**失败
// 路径**上补一个类型标记，代价是失败时多一次纯内存校验（ValidatePageTolerant 不查库、不渲染）。
//
// 判据为什么可信：Compile 的第一件事就是同一个 ValidatePageTolerant（builder.go 的
// Compile 开头），校验不过它会**原样返回**该错误 —— 所以「复算报错」与「编译因此失败」
// 是同一件事，且两边文本逐字相同（不会出现「标记的原因不是真正的原因」）。
//
// 副作用：文档里存在「配置不完整」节点时，ValidatePageTolerant 会再记一条 Warn
// （Compile 里那次已经记过）。它只在编译已经失败时发生，可接受。
func previewValidationProblem(page *builder.Page) error {
	if page == nil {
		return nil
	}
	if _, err := builder.ValidatePageTolerant(page); err != nil {
		return err
	}
	return nil
}

// assembleCompile 装配感知编译：页眉块 + 页面主体 + 页脚块。
// 内容引用面只存 URL 快照，构建期零解析（不查媒体库）。
// 无绑定无引用时输出与默认编译字节一致（hash 兼容历史产物）；
// 块文档缺失/非法降级为空片段，不阻塞构建主链。
// 解析失败回退默认编译；解析成功则与预览共用 compileDocument 装配管线。
func (s *Service) assembleCompile(ctx context.Context, in pipeline.BuildInput) ([]byte, error) {
	page, err := builder.ParsePage(in.DocJSON)
	if err != nil {
		logger.Scene("build").With("err", err).Warn("页面文档解析失败，回退默认编译")
		return pipeline.DefaultCompile(ctx, in)
	}
	// 默认语言取自**冻结计划**（审计 I18N-01）：逻辑路径是「剥掉语言前缀」得来的，
	// 而「哪个语言不带前缀」由默认语言决定 —— 现场解析会让改过 is_default 的重建
	// 把 /en/about 原样当成逻辑路径，再加一次前缀变成 /en/en/about。
	projectID, currentPath := s.pageContextOfWithDefault(ctx, in.PageID, in.Lang, planDefaultLang(in.Plan))
	// 语言来自构建输入（内核按 PageRecord.Lang 注入，见 pipeline.BuildInput）；
	// 访问路径仍取页面记录的逻辑路径，前缀在 compileDocument 内单点计算。
	// 依赖线索记录器（审计 VIS-006）：构建路径传入，编译期记录消费过的系统页面槽位。
	// 发布模式（CompileModePublish）：显式绑定但拿不到的结构依赖让本次构建失败。
	// 归因收集器来自内核的 BuildInput（与 Usage 同一条路子），编译期填充、由内核写进 Manifest。
	// 发布计划同样来自构建输入（审计 I18N-01）：它是这次构建的站点环境的一部分，
	// 装配层只原样使用，不再回读语言配置。
	html, err := s.compileDocument(ctx, page, projectID, currentPath, in.Lang, in.Usage, true,
		builder.CompileModePublish, in.Diagnostics, in.Plan)
	if err != nil {
		if errors.Is(err, errCompileFailed) {
			logger.Scene("build").Error(err, "页面编译失败")
		}
		return nil, err
	}
	s.syncMediaRefs(ctx, in.PageID, currentPath, html)
	return html, nil
}

// syncMediaRefs 构建期写入媒体引用缓存（02-B 第 4 能力，docs/02-B §2 引用保护）。
//
// 时机：**构建期**，不是每次编辑——引用关系是产物事实（文档里写的 URL 未必都进产物，
// 条件渲染/块内联/CMS 集合展开后只有编译结果才权威），且构建期天然幂等
// （同一文档重复构建写入同一集合，差集为空零写入）。
//
// 失败一律降级：引用缓存是保护性元数据，不是构建输入，不阻断发布主链。
// 标题参数用页面逻辑路径（pages 表无标题列，标题在文档内），仅用于删除拦截提示。
func (s *Service) syncMediaRefs(ctx context.Context, pageID, pagePath string, html []byte) {
	if s.media == nil || strings.TrimSpace(pageID) == "" || len(html) == 0 {
		return
	}
	if _, err := s.media.SyncReferencesFromHTML(ctx, "page", pageID, pagePath, string(html)); err != nil {
		logger.Scene("build").With("page_id", pageID).Warn("媒体引用缓存同步失败（已降级，不阻断构建）")
	}
}

// compileDocument 装配编译已解析的页面文档为完整 HTML 字节：
// 组件模板 Set 选择（embed / CompositeSet）、BlockResolver/PluginResolver/
// CollectionResolver/ThemeSettings 注入、Compile、页眉/页脚块内联、RenderDocument。
// 解析由调用方负责（构建路径 ParsePage + 降级；预览路径 json.Unmarshal + 空文档检查）。
// 编译失败以 %w 包裹 errCompileFailed，其余失败原样返回。
// projectID 为本次编译的站点工程 ID（页面文档不携带，由调用方按页面记录注入）；
// 供导航等站点级资源解析使用，为空时绑定菜单位置的导航节点在编译期显式报错。
// currentPath 为页面逻辑访问路径，用于导航「当前项」高亮（空 = 不标记）；
// 多语言开启前缀时，此处统一转换为带前缀路径后再比对（与导航项 URL 同源）。
// lang 为本次构建语言（空 = 站点默认语言）：驱动组件文案取词（构建期冻结快照）
// 与导航项 URL 前缀，是「同一文档每个语言一份独立产物」的语言维度。
// withPublishScope 控制语言切换器是否按「访问面是否已发布」过滤：
// 构建与发布路径传 true（审计 I18N-021：不过滤会把用户送到 404）；
// **预览传 false** —— 预览是编辑期行为，作者在看「这份文档会长什么样」，
// 与「哪些语言已经发布过」无关。按发布面过滤会让刚加的语言在预览里凭空消失，
// 作者只会以为切换器坏了。
// mode 为本次编译的用途（审计 ARCH-05）：发布路径传 CompileModePublish —— 显式绑定但
// 拿不到的结构模板会让这里直接失败；预览路径传 CompileModePreview —— 降级为带归因的占位。
// diags 为降级归因收集器（预览传 nil）：发布路径由内核经 BuildInput 注入，
// 编译期收集的「被容忍的降级」最终写进产物 Manifest。
// plan 为本次构建**已冻结**的发布计划（审计 I18N-01，预览 / 首次构建传 nil）：
// 非空时站点语言表与默认语言都取自它，编译期不再回读 project_locales。
func (s *Service) compileDocument(ctx context.Context, page *builder.Page, projectID, currentPath, lang string, usage core.UsageRecorder, withPublishScope bool, mode builder.CompileMode, diags *builder.DegradeCollector, plan *pipeline.PublicationPlan, extra ...builder.CompileOption) ([]byte, error) {
	// 组件模板 Set + 插件装配（EDT-003 共用 pipeline.ComponentSetWithPlugins）。
	asm := pipeline.LoadPluginAssembly(ctx, s.plugins)
	set, pluginOpts, err := pipeline.ComponentSetWithPlugins(asm)
	if err != nil {
		return nil, err
	}
	resolver := newBlockResolverAdapter(s, ctx, projectID)
	// 构建语言与取词函数：WithLanguage 决定 RenderContext.Lang；
	// WithTranslator 注入「构建开始时刻冻结」的词条快照——构建中途刷新 i18n 缓存
	// 不影响本次产物字节（确定性构建不变量，docs/06-D §2.3/§12）。
	opts := []builder.CompileOption{
		builder.WithContext(ctx), builder.WithBlockResolver(resolver), builder.WithComponentSet(set),
		builder.WithCompileMode(mode),
	}
	if diags != nil {
		opts = append(opts, builder.WithDegradeCollector(diags))
	}
	opts = append(opts, pluginOpts...)
	opts = append(opts, pipeline.LocaleCompileOptions(lang)...)
	opts = append(opts, pipeline.ClientAssetOptions()...)
	if s.content != nil {
		opts = append(opts, builder.WithCollectionResolver(s.content))
	}
	// 商品数据源（issue #35）：商品专用组件直连受限接口取数据。
	if s.productDS != nil {
		opts = append(opts, builder.WithProductDataSource(s.productDS))
	}
	// 结算表单的国家下拉（core.checkoutForm）：站点级静态数据，按**本页语言**取一份。
	//
	// 取到空清单时不注入选项 —— 组件那边会按「未注入」处理：表单里有国家字段时构建失败
	// （真因由装配层的适配器记日志）。这里刻意不做「拿空清单兜底注入」：
	// 那会让失败从构建期滑到运行时（访客面对一个只有默认国家的下拉）。
	if s.checkoutCountries != nil {
		if countries := s.checkoutCountries(ctx, lang); len(countries) > 0 {
			opts = append(opts, builder.WithCheckoutCountries(countries))
		}
	}
	// 站点级装配（EDT-003）：导航 / 槽位 / 高亮 / hreflang / srcset —— 与 presentation 共用 pipeline.SiteCompileOptions。
	var mediaProbe func(context.Context, string) []mediacontract.VariantRef
	if s.media != nil {
		mediaProbe = s.media.ProbeImageVariants
	}
	siteOpts, serr := pipeline.SiteCompileOptions(pipeline.SiteCompilePorts{
		Project: s.project, Navigation: s.navigation, SitePages: s, MediaProbe: mediaProbe,
		// 语言切换器的发布状态查询（审计 I18N-021）：已登记但未发布的语言不进切换器 ——
		// 那不是「暂时没有内容」，而是一个必然 404 的链接。
		//
		// 判定来源是**访问面本身**（active 目录的符号链接），与访客看到的完全一致；
		// 查数据库的路由表只会得出「已登记 = 可见」，那正是这条 finding 的成因。
		// 每页每语言一次 lstat，成本可忽略。
		RoutePublished: publishScope(withPublishScope, func(accessPath string) bool {
			state, ierr := s.publication.Inspect(accessPath)
			return ierr == nil && state != nil && state.Kind != "none"
		}),
	}, pipeline.SiteCompileParams{
		Ctx: ctx, ProjectID: projectID, Lang: lang, LogicalPath: currentPath,
		CurrentPath: pipeline.HighlightPathWithDefault(ctx, s.project, projectID, compileDefaultLang(ctx, s.project, projectID, plan), lang, currentPath),
		Plan:        plan,
	})
	if serr != nil {
		return nil, fmt.Errorf("%w: %v", errCompileFailed, serr)
	}
	opts = append(opts, siteOpts...)
	// 主题快照注入：settings.theme（保存时合入的 ThemeSettings 快照）→ 编译进产物。
	if page.Settings.Theme != nil {
		opts = append(opts, builder.WithThemeSettings(page.Settings.Theme))
	}
	// 结构槽位（审计 VIS-001）：页眉 / 页脚的绑定展开成 root 首尾的槽位节点，
	// 与页面主体走**同一次编译** —— 不再由装配层把块单独编译后拼字符串。
	//
	// 拼字符串的问题不在字节，而在「块不在 AST 里」：翻译候选、失效依赖、
	// workbench 画布、main 地标判定各要一份特判，漏一处就是「页眉改了但页面没重建」。
	// 展开之后，槽位节点的语义与作者手动插入的 core.globalref 完全一致。
	//
	// 结构模板优先、块绑定回退（pipeline.BuildStructureSlots，与自动发布实例路径同一份实现）。
	// **回退只在预览成立**（审计 ARCH-05）：发布路径下，显式绑定了模板却拿不到
	// （不存在 / 跨工程 / 文档非法）直接失败 —— 旧行为会静默回退到块绑定（甚至什么都不渲染），
	// 一次配错的模板绑定就这样被发布成一份缺页眉的页面，而构建接口返回成功。
	// 没绑定（该槽位本来就不产出内容）依旧按显式设计处理。
	//
	// 顺序：必须在取词器构造**之前**算出槽位 —— 结构模板的文档不在块表里，它的可翻译
	// 文本要经叠加了解析器的 slotResolver 才能进候选集合（否则模板里的文案永远不翻译，
	// 且不报任何错）。
	slotRes, slotErr := pipeline.BuildStructureSlots(ctx, pipeline.StructureSlotInput{
		Port: s.structureTemplates, ProjectID: projectID, Structure: page.Settings.Structure,
		Inner: resolver, Mode: mode, Diagnostics: diags,
	})
	if slotErr != nil {
		// 结构绑定拿不到 = 本次产物必然缺一截：发布路径必须整体失败（产物不落行、
		// 暂存指针不推进、线上保持不变），而不是继续编译出一份不完整的页面。
		// 预览路径不会走到这里（预览模式按降级处理），所以这个错误只可能是发布失败。
		return nil, fmt.Errorf("%w: %v", errCompileFailed, slotErr)
	}
	slotList, slotResolver := slotRes.Slots, slotRes.Resolver
	// 内容翻译（多语言 P5b，docs/06-D §7.7）：作者在编辑器里填写的文本（按钮文字/
	// 标题/alt/图注/富文本）按组件 Translatable 白名单替换。每页每语言**构造一次**
	// 取词器——先收集候选（本页 AST + 页眉/页脚块 + core.globalref 内联块，见
	// collectContentCandidates）→ ShouldTranslateContent 过滤 → **一次**批量 SQL 取回
	// 译文，组件渲染期零查库（§7.7「零查库」）。默认语言与单语言站点跳过（产物即原文）。
	var contentTranslator *i18n.ContentTranslator
	var contentCandidates int
	opts, contentTranslator, contentCandidates = pipeline.AppendContentTranslationFor(
		opts, ctx, projectID, compileDefaultLang(ctx, s.project, projectID, plan), lang,
		page, slotResolver.ResolveBlockRoot, s.newContentTranslator)
	opts = append(opts, pipeline.AnalyticsCompileOptions(ctx, s.project, projectID)...)
	if len(slotList) > 0 {
		// 出现模板槽位时必须换成叠加了虚拟引用的解析器：模板文档不在块表里，
		// 原解析器按引用 ID 去查库会直接报「块不存在」。
		opts = append(opts, builder.WithBlockResolver(slotResolver))
		opts = append(opts, builder.WithStructureSlots(slotList...))
	}
	// 依赖线索：只记录**真实消费**的槽位（预览路径传 nil，不记录）。
	if usage != nil {
		opts = append(opts, builder.WithUsageRecorder(usage))
	}
	// 调用方追加的编译选项：**最后追加**，语义上只允许「加东西」（如编辑器画布的
	// 槽位标记层），不允许覆盖上面按装配顺序定好的选项（发布模式、降级收集器…）。
	opts = append(opts, extra...)
	compiled, err := builder.Compile(page, opts...)
	if err != nil {
		// 组件校验问题（配置不完整 / 非法，如「手风琴至少需要一个折叠项」）是**作者可操作**
		// 的提示：带上 PreviewProblem 标记交给消费侧（工作台画布）判别并透出，
		// 否则作者只能看到一句泛化的「预览编译失败」。
		//
		// 包装方式保留「页面编译失败: 」前缀（%w 的文本与原来的 %w: %v 逐字相同），
		// 构建日志不漂移；errCompileFailed 在链上出现两次（外层 + PreviewProblem.cause），
		// 是为了让 errors.Is(err, errCompileFailed) 与 errors.Is(err, problem.cause) 同时成立。
		//
		// 其余错误（装配缺失 / 渲染期失败）保持原样、**不带**标记：原文只进日志。
		if verr := previewValidationProblem(page); verr != nil {
			return nil, fmt.Errorf("%w: %w", errCompileFailed,
				pagecontract.NewPreviewProblem(verr.Error(), errCompileFailed))
		}
		return nil, fmt.Errorf("%w: %v", errCompileFailed, err)
	}
	// L3 构建期缺失告警（决策 F14 第三层）：统计本页未命中译文数并记日志，
	// **不阻断构建**（缺译文已在取词器内回退原文，产物照常产出）。
	// 位置在块内联之后：页眉/页脚与 globalref 内联块的缺失同样计入（取词器为同一实例）。
	if contentTranslator != nil {
		pipeline.LogContentTranslationMisses(lang, contentCandidates, contentTranslator.Misses())
	}
	doc, err := builder.RenderDocument(compiled)
	if err != nil {
		return nil, err
	}
	return []byte(doc), nil
}

// blockResolverAdapter 适配 block 契约为 builder 的 BlockResolver
// （core.globalref 构建期展开引用块内容）。
// 缓存为单次编译内块解析缓存：同一块被引用多次时只查一次库，且**候选收集与
// 渲染展开共用同一份缓存**（内容翻译的块内候选不会额外产生一次块查询）。
type blockResolverAdapter struct {
	s *Service
	// projectID 与 ctx 一起构成块查询的 scope：block.Detail 把工程归属当作必填，
	// 缺它只会拿到「参数缺失」，而这一层是降级不报错的（构建继续、产物少一截）。
	projectID string
	ctx       context.Context
	cache     map[string]*builder.Page
	errs      map[string]error
}

// newBlockResolverAdapter 构造单次编译的块解析适配器（缓存随编译实例存活）。
func newBlockResolverAdapter(s *Service, ctx context.Context, projectID string) *blockResolverAdapter {
	return &blockResolverAdapter{
		s: s, ctx: ctx, projectID: projectID,
		cache: map[string]*builder.Page{}, errs: map[string]error{},
	}
}

// ResolveBlockRoot 按块 ID 返回块文档 root 节点。
// 防御（docs/02-D §5/§9）：reuse_mode=template 的块是「一次性复制」语义，
// 不允许经 core.globalref 引用展开——正常流程下副本已在插入时并入页面文档，
// 此处命中说明引用被绕过编辑器写入，构建期即报错暴露而非静默按引用渲染。
func (a *blockResolverAdapter) ResolveBlockRoot(blockID string) ([]*core.Node, error) {
	page, err := a.blockPage(blockID)
	if err != nil {
		return nil, err
	}
	return page.Root, nil
}

// blockPage 解析块文档为 builder.Page（带缓存；失败结果同样缓存，避免重复查库）。
func (a *blockResolverAdapter) blockPage(blockID string) (*builder.Page, error) {
	if a.cache != nil {
		if page, ok := a.cache[blockID]; ok {
			return page, nil
		}
	}
	if a.errs != nil {
		if err, ok := a.errs[blockID]; ok {
			return nil, err
		}
	}
	fail := func(err error) (*builder.Page, error) {
		if a.errs != nil {
			a.errs[blockID] = err
		}
		return nil, err
	}
	if a.s == nil || a.s.blocks == nil {
		return fail(fmt.Errorf("全局块 %s 不可用", blockID))
	}
	block, err := a.s.blocks.Detail(a.ctx, &blockcontract.DetailReq{ProjectID: a.projectID, ID: blockID})
	if err != nil || block == nil {
		return fail(fmt.Errorf("全局块 %s 不可用", blockID))
	}
	if block.ReuseMode == "template" {
		return fail(fmt.Errorf("全局块 %s 为一次性复制片段，不能被引用展开", blockID))
	}
	page, err := builder.ParsePage(block.Document)
	if err != nil {
		return fail(err)
	}
	if a.cache != nil {
		a.cache[blockID] = page
	}
	return page, nil
}

// planDefaultLang 从冻结计划取默认语言（空 = 未冻结，调用方回退现场解析）。
func planDefaultLang(plan *pipeline.PublicationPlan) string {
	if plan == nil {
		return ""
	}
	return strings.TrimSpace(plan.DefaultLang)
}

// compileDefaultLang 本次编译的默认语言：冻结计划优先，否则现场解析（审计 I18N-01）。
//
// 单独抽一层是为了让「冻结的默认语言」只有一个入口。它同时是四件事的判据：
// 导航当前项高亮路径、hreflang 的 x-default、default_plain 方案下哪个语言不带前缀、
// 以及哪种语言需要走内容翻译。四处各读一次现场配置，就会出现
// 「x-default 指向 /about，而导航当前项高亮挂在 /en/about」这类自相矛盾的产物。
func compileDefaultLang(ctx context.Context, project projectcontract.ProjectService, projectID string, plan *pipeline.PublicationPlan) string {
	if plan != nil && strings.TrimSpace(plan.DefaultLang) != "" {
		return strings.TrimSpace(plan.DefaultLang)
	}
	return pipeline.DefaultLocale(ctx, project, projectID)
}

// publishScope 按开关返回发布状态查询：false 时返回 nil，
// 语言切换器就退化成「全部语言都列出」（I18N-021 接入前的行为），这正是预览要的。
func publishScope(enabled bool, fn func(string) bool) func(string) bool {
	if !enabled {
		return nil
	}
	return fn
}

// CompilePreview 基于未落盘文档 JSON 编译完整 HTML（预览专用：不落盘、不影响产物）。
// 复用与正式构建同源的编译管线（compileDocument），仅错误语义按预览需求分类：
//   - 文档解析失败（JSON 非法或空文档）→ ErrPreviewInvalidDocument；
//   - 编译失败 → ErrPreviewCompileFailed；
//   - 组件模板加载/文档渲染失败 → 原样透传（调用方映射为内部错误）。
//
// projectID 为页面所属站点工程（调用方从页面记录取；块预览传块所属工程），
// currentPath 为页面逻辑访问路径（导航当前项高亮；块预览传空），
// lang 为预览目标语言（空 = 站点默认语言；工作台按 ?lang= 切换预览语言），
// 与正式构建一致地驱动导航等站点级资源解析——画布所见即产物。
//
// canvasFrames 打开结构槽位的「画布标记层」（core.RenderContext.CanvasSlotFrames）：
// 只有工作台编辑器画布（?editor=1）才该打开。它是给编辑器看的元信息，不是页面内容 ——
// 关掉时产物字节与「把块内容直接写在页面里」逐字节一致（VIS-001 的不变量）。
func (s *Service) CompilePreview(ctx context.Context, docJSON []byte, projectID, currentPath, lang string, canvasFrames bool) (html []byte, err error) {
	var page *builder.Page
	if err = json.Unmarshal(docJSON, &page); err != nil || page == nil {
		if err == nil {
			err = errors.New("草稿文档为空")
		}
		logger.Scene("build").With("err", err).Warn("预览文档解析失败")
		return nil, fmt.Errorf("%w: %v", pagecontract.ErrPreviewInvalidDocument, err)
	}
	// 预览不产出 Manifest，因此不记录依赖线索（usage 传 nil）。
	// 也不注入归因收集器：预览的归因体现在**占位上**（哪个槽位 / 哪个节点 / 哪份来源 /
	// 为什么没展开，见 data-sky-* 属性），没有 Manifest 可写。
	// 模式为预览：显式绑定但拿不到的结构依赖在这里不失败（编辑期配置不完整是常态），
	// 与发布路径共用同一份装配管线，这就是两者的唯一差异。
	// plan 传 nil：预览不读发布计划 —— 作者看的是「这份草稿用当前配置能长什么样」，
	// 而冻结计划描述的是**已发布产物**依据的输入（审计 I18N-01）。
	// 编辑器画布的槽位标记层：按调用方开关追加（见方法注释）。
	var extra []builder.CompileOption
	if canvasFrames {
		extra = append(extra, builder.WithCanvasSlotFrames())
	}
	html, err = s.compileDocument(ctx, page, projectID, currentPath, buildLang(lang), nil, false,
		builder.CompileModePreview, nil, nil, extra...)
	if err != nil {
		if errors.Is(err, errCompileFailed) {
			logger.Scene("build").Error(err, "预览编译失败")
			// 用 %w 而不是 %v 保留错误链：把内层类型转成字符串会丢掉
			// pagecontract.PreviewProblem（作者可操作的组件校验提示），
			// 消费侧（workbench）就只能靠嗅探文本或压成一句泛化文案。
			// 文本形态与原来的 "%w: %v" 逐字相同（两个 %w 也按同样格式拼接）。
			return nil, fmt.Errorf("%w: %w", pagecontract.ErrPreviewCompileFailed, err)
		}
		logger.Scene("build").Error(err, "预览文档渲染失败")
		return nil, err
	}
	return html, nil
}
