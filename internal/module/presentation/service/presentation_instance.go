package presentationservice

// 详情页与归档页的区别不在模板语法，而在**页面身份**：详情页讲一个实体，
// 归档页列这个实体下的内容。两者由同一个实体驱动，所以实例表需要角色维度
//（迁移 175），模板侧也需要按角色取（contenttemplate 的 template_role）。
//
// 本文件只负责一件事：给定一个实体，确保它的归档页存在。
// 没有归档模板时**静默跳过** —— 那是正常状态（不是每个站点都要归档页），
// 报错会让「新建一个分类」变成一件会失败的事。

// 双轨语义（docs/04-C-instance-override.md）：
//   template（默认）：文档 = 绑定模板文档；模板更新可全局下发；
//   document：文档 = override_document；模板更新不影响它（UI 上叫「独立文档」）。
//
// 分叉判据是**文档结构真的变了**，不是「保存了商品」：改名称/价格/描述属于实体数据，
// 不改文档 → 不分叉；改了又改回去（归一化后与生效底稿相同）→ 同样不分叉。
// 判定必须由服务端做（前端拿不到"生效底稿"的权威字节），因此未确认时返回
// ErrDetachConfirmRequired，前端据此弹确认并带 confirmDetach 重试。

// 与手工页面 page.Service.UpdateURL 同一语义，动作清单逐条对齐（顺序不可换）：
//  1. 路径归一化 + 同路径短路；
//  2. 占用预检（page_routes 与 presentation_instances 两个真源）；
//  3. 按新路径重新构建产物（canonical / OG / JSON-LD 已烘进 HTML 字节，必须重编）；
//  4. 落库：快照 + 产物行 + 指针 + url_path（同一事务）；
//  5. 激活新路径（访问面符号链接 + page_routes 登记）；
//  6. 旧路径处置：WithRedirect 落盘 301 产物并激活，否则取消旧路径激活。
//
// 与手工页面唯一的差别：presentation 没有草稿路径概念（实例创建即发布），
// 因此没有「纯草稿只迁移路径」那条分支。
//
// 为什么改 URL 必须重建产物：静态站里路径不是元数据而是产物的一部分 ——
// canonical / og:url / JSON-LD 的 url 在编译期就写死在 HTML 字节里，改路径而
// 不重编会留下「页面仍宣称自己住在旧地址」的自相矛盾。动态站（WP）改 slug
// 立刻生效，是因为它的 URL 每次请求现算；静态发布没有这个奢侈。

// 内容模板是「完整文档层」，内部可以引用页眉/页脚/信任徽章等全局区块
// （docs/02-D §1.2：两者是包含关系，不是二选一）；构建期由本适配器把区块
// 文档内联进同一次编译输出，访问面仍是纯静态（无运行时拼接）。
//
// 与手工 Page 路径（page/service/page_document.go）同口径：
//   - 同一次编译内按块 ID 缓存（同一块被多次引用只查一次库）；
//   - 失败结果同样缓存，避免重复查库；
//   - reuse_mode=template 的块是「一次性复制」语义，不允许被引用展开，
//     命中即报错暴露（正常流程下副本已在插入时并入文档）。

// 回滚复用既有内核与既有表（不引入 page_revisions）：
//   · 产物指针回滚：publication.Activate + 实例指针切换（秒级，不重编译）；
//   · 快照级文档回滚：取 document_snapshots 的历史文档重发（重新编译，数据取最新）。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/internal/module/block/contract"
	"go_wp/internal/module/contenttemplate/contract"
	"go_wp/internal/module/presentation/dto"
	"go_wp/internal/module/presentation/enums"
	"go_wp/internal/module/presentation/model"
	"go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
	"go_wp/pkg/rls"
)

// CreateInstance 创建自动发布实例：解析模板 → 编译 → 发布 → 记快照/产物/依赖。
func (s *Service) CreateInstance(ctx context.Context, req *presentationdto.CreateInstanceReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.EntityType == "" || req.EntityID == "" || req.URLPath == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	// 角色（审计 EDT-004）：空 = detail，既有调用方逐字不变。
	role := strings.TrimSpace(req.InstanceRole)
	if role == "" {
		role = presentationmodel.InstanceRoleDetail
	}
	if !presentationmodel.IsValidInstanceRole(role) {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	// 工程作用域先解析（DB-009 第二批）：presentation_instances 带 FORCE 策略，
	// 幂等查询也在工程内进行 —— 跨工程按实体查会把别人的实例当成自己的返回。
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}

	// 实例级互斥只覆盖「幂等检查 → 占用预检 → 建行」这一段（PERF-01）：它保护的是
	//「同一实体不会被并发建出两行、同一条路径不会被并发占两次」。编译与产物落盘
	// 由 publishAllLangs 的分段锁负责，不再占着这把锁（一份语言慢不该拖住创建路径）。
	//
	// 用内联函数而不是 defer：进发布前必须放锁 —— sync.Mutex 不可重入，而
	// publishAllLangs 的冻结段会自己再取同一把锁。
	var (
		inst        *presentationmodel.InstanceEntity
		existing    *presentationmodel.InstanceEntity
		logicalPath string
	)
	lock := s.lockInstance(req.EntityType, req.EntityID)
	if err = func() error {
		lock.Lock()
		defer lock.Unlock()

		// 同实体**同角色**已存在实例 → 视为幂等（返回已有）。
		// 必须带角色：同一个分类既有详情页也可能有归档页，只按实体查会把先建的当成
		// 「已存在」返回 —— 于是「给分类建归档页」静默变成「拿到详情页实例」。
		if row, gerr := s.m.GetInstanceByEntityRole(ctx, projectID, req.EntityType, req.EntityID, role); gerr == nil {
			existing = row
			return nil
		} else if !errors.Is(gerr, gorm.ErrRecordNotFound) {
			return gerr
		}
		// 解析模板版本 + 实体解析器。req.TemplateID 非空 = 发布时显式指定用哪套命名模板
		// （issue #14 验收 2）；为空 = 按实体类型取默认模板（既有行为逐字不变）。
		tpl, rerr := s.resolveTemplate(ctx, projectID, req.EntityType, req.TemplateID)
		if rerr != nil {
			return rerr
		}
		// 路径先归一化：产物 canonical、访问面符号链接与 page_routes 登记必须落在
		// 同一个字符串上（FS 侧本来就归一化），否则 /shop/x/ 与 /shop/x 会被当成
		// 两个路径，路由行指向的位置与实际内容不符。
		path, perr := s.normalizeLogicalPath(ctx, projectID, req.URLPath)
		if perr != nil {
			return fmt.Errorf("%s: %w", presentationenums.ErrInvalidPath, perr)
		}
		logicalPath = path
		// 占用预检：逻辑路径下全部语言访问路径 + 逻辑路径本身。
		if ferr := s.ensureLogicalPathFree(ctx, projectID, logicalPath, ""); ferr != nil {
			return ferr
		}
		// 记实例（url_path 存逻辑路径；各语言访问路径在 publication 表）。
		now := time.Now().UTC()
		inst = &presentationmodel.InstanceEntity{
			ID: uuid.NewString(), ProjectID: projectID, EntityType: req.EntityType,
			EntityID: req.EntityID, InstanceRole: role, URLPath: logicalPath, TemplateID: tpl.TemplateID,
			Stale: true, CreatedAt: now, UpdatedAt: now,
		}
		return s.m.CreateInstance(ctx, inst)
	}(); err != nil {
		return nil, err
	}
	if existing != nil {
		return s.toResp(ctx, existing)
	}

	// mode=nil：创建实例不改渲染模式（默认 template，首次编辑才可能转独立）。
	//
	// 模板按刚写进 template_id 的**绑定**解析（而不是重新按实体类型取默认模板）：
	// 创建与发布之间冒出一个新的类型默认模板时，实例不该被悄悄换到别的模板上。
	if _, err = s.publishAllLangs(ctx, inst, publishIntent{
		resolveDoc: func(ctx context.Context, fresh *presentationmodel.InstanceEntity) (*contenttemplatecontract.ResolvedTemplate, error) {
			return s.resolveBoundTemplate(ctx, fresh, req.TemplateID)
		},
		logicalPath: logicalPath,
	}); err != nil {
		return nil, publishFailedErr(err)
	}
	return s.toResp(ctx, inst)
}

// GetByEntity 按内容实体查询实例（后台「详情页模板」页读当前绑定与发布状态）。
func (s *Service) GetByEntity(ctx context.Context, req *presentationdto.GetByEntityReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.EntityType == "" || req.EntityID == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	// 工程作用域（DB-009 第二批）：实例表的读必须带 app.project_id，
	// 否则换非超级角色后静默 0 行 —— 表现为「详情页模板显示未绑定」。
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.m.GetInstanceByEntity(ctx, projectID, req.EntityType, req.EntityID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(presentationenums.ErrNotFound)
		}
		return nil, err
	}
	return s.toResp(ctx, inst)
}

// Get 按 ID 查询。
func (s *Service) Get(ctx context.Context, req *presentationdto.GetReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.m.GetInstance(ctx, projectID, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(presentationenums.ErrNotFound)
		}
		return nil, err
	}
	return s.toResp(ctx, inst)
}

// List 按类型列表。
func (s *Service) List(ctx context.Context, req *presentationdto.ListReq) (list []*presentationdto.InstanceResp, err error) {
	if req == nil {
		req = &presentationdto.ListReq{}
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListInstances(ctx, projectID, req.EntityType)
	if err != nil {
		return nil, err
	}
	out := make([]*presentationdto.InstanceResp, 0, len(rows))
	for _, r := range rows {
		resp, rerr := s.toResp(ctx, r)
		if rerr != nil {
			return nil, rerr
		}
		out = append(out, resp)
	}
	return out, nil
}

// Delete 删除实例（级联删本模块从属行 + 反激活 URL）。
func (s *Service) Delete(ctx context.Context, req *presentationdto.DeleteReq) (err error) {
	if req == nil || req.ID == "" {
		return errors.New(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return err
	}
	inst, err := s.m.GetInstance(ctx, projectID, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(presentationenums.ErrNotFound)
		}
		return err
	}
	// 反激活失败必须中止删除：否则实例行已删、URL 占用残留，后续同路径
	// 发布/激活会被「已占用」拒绝且无实例可查（状态分裂）。
	//
	// 要覆盖**全部**已激活路径而不只是当前 url_path：改过 URL 的实例还有一条
	// 旧路径的 301 链接，漏掉它 = 实例删了、线上旧路径仍 301 到一个死页面。
	for _, p := range s.instanceActivePaths(ctx, inst) {
		if derr := s.publication.Deactivate(p); derr != nil {
			return fmt.Errorf("删除实例前反激活 URL 失败 %s: %w", p, derr)
		}
	}
	// 路由占用同步释放 + 实例行删除：两处都是持久化写入，且分属两个模块
	// （page_routes 在 publication、实例行在 presentation），按 AGENTS.md
	// 「写操作的事务与回滚」必须落进**同一个事务**（tx 透传，不做补偿）。
	//
	// 只删实例行会把 page_routes 里本实例的 active/redirect 行留成悬空引用
	// （外键指向已删除的实例），同路径再发布永远被拒；反过来只删路由行则留下
	// 「实例还在、路径已释放」的分裂状态。两种半截状态都由这个事务消除。
	//
	// 反激活（上面的循环）**刻意留在事务外**：它删的是访问面目录里的符号链接 ——
	// 文件系统不在数据库事务边界内，属 AGENTS.md 允许的「补偿只用于跨库/外部系统」
	// 那一类，且 Deactivate 幂等、可重跑。顺序也是刻意的：先反激活再开事务，
	// 事务回滚时链接已下线（线上 404，而不是把已删内容继续挂在旧路径上），
	// 重跑一次 Delete 会再走一遍幂等反激活 + 事务即可收敛；把反激活放到提交之后，
	// 失败就留下「实例没了、链接还在」的死路径，且再没有实例行可以据以定位它们。
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		// 工程作用域由**调用方**设进这个事务：publication 的 …Tx 变体的契约就是
		// 「调用方负责开启事务 + 设置作用域」（见 publication_route_tx.go 文件头），
		// 而 page_routes 带 FORCE 策略（迁移 215）—— 不设作用域时那条删行在非超级
		// 角色下匹配 0 行且不报错（fail closed），随后删实例行会被外键拒绝。
		if serr := rls.ScopeTx(tx, inst.ProjectID); serr != nil {
			return serr
		}
		if s.routes != nil {
			if rerr := s.routes.DeleteRoutesByPresentationTx(ctx, tx, &pubcontract.DeleteRoutesByPresentationReq{
				ProjectID: inst.ProjectID, PresentationID: inst.ID,
			}); rerr != nil {
				return fmt.Errorf("删除实例前释放路由占用失败: %w", rerr)
			}
		}
		return s.m.DeleteInstanceTx(tx, projectID, req.ID)
	}); err != nil {
		return err
	}
	return nil
}

// instanceActivePaths 实例在访问面上已激活的全部路径（当前路径 + 历史 301 路径）。
//
// 查询失败时退化为当前 url_path：清理不完整优于因查询失败而删不掉实例 ——
// 前者是可发现、可重试的残留，后者是卡死的资源。
func (s *Service) instanceActivePaths(ctx context.Context, inst *presentationmodel.InstanceEntity) []string {
	var paths []string
	if pubs, perr := s.m.ListActivePathsForInstance(ctx, inst.ID); perr == nil && len(pubs) > 0 {
		paths = append(paths, pubs...)
	} else if inst.URLPath != "" {
		paths = append(paths, inst.URLPath)
	}
	if s.routes == nil {
		return dedupePaths(paths)
	}
	extra, err := s.routes.ListActivePathsByPresentation(ctx, &pubcontract.ListActivePathsByPresentationReq{
		ProjectID: inst.ProjectID, PresentationID: inst.ID,
	})
	if err != nil {
		logger.Scene("build").With("instanceId", inst.ID).
			Warn("读取实例已激活路径失败，仅清理已登记路径: " + err.Error())
		return dedupePaths(paths)
	}
	return dedupePaths(append(paths, extra...))
}

func dedupePaths(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, p := range in {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// findByEntityID 在**本工程内**按内容实体 ID 反查实例。
func (s *Service) findByEntityID(ctx context.Context, projectID, entityID string) (*presentationmodel.InstanceEntity, error) {
	rows, err := s.m.ListInstances(ctx, projectID, "")
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.EntityID == entityID {
			return r, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// toResp 实体 → 响应。
//
// Status 不再是表列：由 active_artifact_id 指针推导（active / draft），
// ArtifactHash 取活跃产物行的哈希。
func (s *Service) toResp(ctx context.Context, e *presentationmodel.InstanceEntity) (resp *presentationdto.InstanceResp, err error) {
	resp = &presentationdto.InstanceResp{
		InstanceRole: e.InstanceRole,
		ID:           e.ID, ProjectID: e.ProjectID, EntityType: e.EntityType, EntityID: e.EntityID,
		URLPath: e.URLPath, TemplateID: e.TemplateID, Stale: e.Stale,
		UpdatedAt: e.UpdatedAt.Format("2006-01-02 15:04"),
	}
	if e.ActiveArtifactID != nil {
		resp.Status = presentationenums.StatusActive
		resp.ArtifactID = *e.ActiveArtifactID
		// 只要一个哈希：列表路径按实例行数放大，绝不能为它把产物整行（含 manifest
		// 这个 JSONB）拽出来 —— 见 model.GetArtifactHash 的列白名单。
		if h, herr := s.m.GetArtifactHash(ctx, *e.ActiveArtifactID); herr == nil {
			resp.ArtifactHash = h
		}
	} else {
		resp.Status = presentationenums.StatusDraft
	}
	if e.CurrentSnapshotID != nil {
		resp.SnapshotID = *e.CurrentSnapshotID
	}
	// 渲染模式（迁移 282）与可编辑底稿（docs/04-C-instance-override.md）：
	//   document → 覆盖文档即该商品当前文档；
	//   template → 取当前快照文档，供 workbench 以「当前生效布局」为起点编辑
	//             （首次编辑即播种，避免空白画布），并用它判定「文档结构是否真的变了」。
	resp.RenderMode = presentationmodel.NormalizeRenderMode(e.RenderMode)
	if presentationmodel.IsDocumentMode(e.RenderMode) && len(e.OverrideDocument) > 0 {
		resp.Document = e.OverrideDocument
	} else if e.CurrentSnapshotID != nil {
		if snap, serr := s.m.GetSnapshot(ctx, *e.CurrentSnapshotID); serr == nil {
			resp.Document = snap.Document
			// 「预设有新版本」的判定依据：document 模式不会自动跟随模板，
			// 只能靠快照记录的模板版本与模板最新版比对来提示用户。
			resp.SourceTemplateVersionID = snap.SourceTemplateVersionID
		}
	}
	return resp, nil
}

// ArchivePathFor 归档页的访问路径。
//
// 规则：/{entityType}/{slug}，如 /product_category/electronics（实体类型用注册表口径）。
// 用实体类型做前缀而不是可配置前缀：两个类型配成同一个前缀时，它们在访问面上
// 无法区分，而冲突要到实际请求 404 或串页才会被发现。
//
// 为什么商品分类的归档前缀是 /product_category/ 而不是更顺眼的 /category/：
// 后者是 siteurl.DefaultPatterns[product_category] 里**分类详情页**的路径模式 ——
// 归档页若共用同一前缀，同一个分类的详情实例与归档实例会争抢同一个 URL，
// 先建的那个占用、后建的被 ensureLogicalPathFree 拒绝（"归档页建不起来"再次出现，
// 而且这次是路径冲突，不是类型错误）。开发库实测 presentation_instances 为空、
// page_routes 无 /category/ 行，没有存量归档 URL 需要迁移，故直接采用注册表口径。
func ArchivePathFor(entityType, slug string) string {
	return "/" + strings.Trim(strings.TrimSpace(entityType), "/") + "/" +
		strings.Trim(strings.TrimSpace(slug), "/")
}

// EnsureArchiveInstance 确保某实体的归档页存在（审计 EDT-004）。
//
// 调用方是实体侧（分类 / 标签 / 品牌的增删改）。幂等：已有归档实例时直接返回它。
func (s *Service) EnsureArchiveInstance(ctx context.Context, req *presentationdto.EnsureArchiveReq) (res *presentationdto.EnsureArchiveResp, err error) {
	if req == nil || strings.TrimSpace(req.EntityType) == "" || strings.TrimSpace(req.EntityID) == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		// 没有 slug 就没有稳定的访问路径。跳过而不是编一个路径：
		// 编出来的路径会在实体补上 slug 后变成一条永远 404 的死链。
		return &presentationdto.EnsureArchiveResp{Skipped: "实体没有 slug，无法生成归档路径"}, nil
	}
	if s.templates == nil {
		return &presentationdto.EnsureArchiveResp{Skipped: "模板契约未装配"}, nil
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	tpl, terr := s.templates.ResolveTemplateByRoleScoped(ctx, projectID, req.EntityType, contenttemplatecontract.TemplateRoleArchive)
	if terr != nil {
		// 没有归档模板是最常见的情况，按「跳过」处理；其它错误照常上报。
		if strings.Contains(terr.Error(), "not found") || strings.Contains(terr.Error(), "ErrNotFound") {
			return &presentationdto.EnsureArchiveResp{Skipped: "该实体类型未配置归档模板"}, nil
		}
		return nil, terr
	}

	wantPath := ArchivePathFor(req.EntityType, slug)
	inst, cerr := s.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: projectID, EntityType: req.EntityType, EntityID: req.EntityID,
		URLPath: wantPath, TemplateID: tpl.TemplateID,
		InstanceRole: "archive",
	})
	if cerr != nil {
		return nil, cerr
	}
	// 路径自适应：实体改名（slug 变化）后归档页要跟着走，旧路径留 301。
	// 不更新的话归档页会一直挂在旧 slug 上，而站点里没有任何链接指向它 ——
	// 从后台看「归档页正常发布着」，从访问面看它已经是个孤儿。
	if inst.URLPath != wantPath {
		updated, uerr := s.UpdateURL(ctx, &presentationdto.UpdateURLReq{
			ID: inst.ID, ProjectID: inst.ProjectID, NewPath: wantPath, WithRedirect: true,
		})
		if uerr != nil {
			return nil, uerr
		}
		inst = updated
	}
	return &presentationdto.EnsureArchiveResp{InstanceID: inst.ID, Created: true}, nil
}

// SaveOverrideDocument 保存商品独立文档并重建发布。
//
// 模式与文档的落库发生在 publishAllLangs → persistMultiLangArtifacts 的同一个事务里
// （与快照/产物/指针一体），不是先写一列再发布 —— 理由见 instanceModePending 注释。
func (s *Service) SaveOverrideDocument(ctx context.Context, req *presentationdto.SaveOverrideReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || strings.TrimSpace(req.InstanceID) == "" || len(req.Document) == 0 {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	if !json.Valid(req.Document) {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.m.GetInstance(ctx, projectID, req.InstanceID)
	if err != nil {
		return nil, errors.New(presentationenums.ErrNotFound)
	}
	// 两个前置判定（幂等保存 / 转入独立的确认）是**只读**的，不再占实例锁（PERF-01）：
	// 真正需要互斥的是随后的发布（它按自己的分段锁做版本分配与指针推进）。
	// 判定用的是调用方这一次读到的实例行；并发期间模式被别的写路径改掉时，
	// 以本次请求的显式意图（写入这份独立文档）为准。
	tpl, err := s.resolveBoundTemplate(ctx, inst, "")
	if err != nil {
		return nil, err
	}
	// 生效底稿：document 模式 = 该商品文档；template 模式 = 模板文档。
	effective := instanceDocumentFor(inst, tpl)

	// 结构未变 = 不分叉、不重建（幂等保存：反复保存同一份不改动也算成功）。
	if documentsEqual(json.RawMessage(req.Document), effective.Document) {
		return s.toResp(ctx, inst)
	}

	// 从「跟随模板」转入「独立文档」必须显式确认：这是用户唯一会真的丢掉
	// 「模板全局同步」能力的时刻，判据只有服务端算得准（见文件头注释）。
	if !presentationmodel.IsDocumentMode(inst.RenderMode) && !req.ConfirmDetach {
		return nil, errors.New(presentationenums.ErrDetachConfirmRequired)
	}

	mode := &instanceModePending{
		renderMode: presentationmodel.RenderModeDocument,
		document:   json.RawMessage(req.Document),
	}
	// 编译底稿 = 本次提交的文档；binding 照常解析，实体数据取最新（数据不丢）。
	// 按**锁内重读**的实例行重新套用：冲突重试时它可能已经被别的批次改过。
	if _, err = s.publishAllLangs(ctx, inst, publishIntent{
		resolveDoc: func(ctx context.Context, fresh *presentationmodel.InstanceEntity) (*contenttemplatecontract.ResolvedTemplate, error) {
			bound, rerr := s.resolveBoundTemplate(ctx, fresh, "")
			if rerr != nil {
				return nil, rerr
			}
			pending := *fresh
			pending.OverrideDocument = json.RawMessage(req.Document)
			pending.RenderMode = presentationmodel.RenderModeDocument
			return instanceDocumentFor(&pending, bound), nil
		},
		mode: mode,
	}); err != nil {
		return nil, publishFailedErr(err)
	}
	return s.toResp(ctx, inst)
}

// ReapplyPreset 重新套用预设：放弃该商品独立文档，回到跟随模板（可反悔的另一半）。
//
// 与「转入独立」对称：模式与文档的清除同样落在发布事务里，避免
// 「模式已回 template、文档列还留着旧自定义」的矛盾行。
func (s *Service) ReapplyPreset(ctx context.Context, req *presentationdto.ReapplyPresetReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || strings.TrimSpace(req.InstanceID) == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.m.GetInstance(ctx, projectID, req.InstanceID)
	if err != nil {
		return nil, errors.New(presentationenums.ErrNotFound)
	}
	// switchTo 非空 = 同时换一套模板（换底稿）；模板身份切换由发布事务内的
	// UpdateInstanceTemplateTx 落库，这里只负责给出意图。
	//
	// 实例锁由发布会话分段持有（PERF-01）：解析绑定与编译在锁外，版本分配与指针推进在锁内。
	switchTo := strings.TrimSpace(req.TemplateID)
	mode := &instanceModePending{renderMode: presentationmodel.RenderModeTemplate}
	if _, err = s.publishAllLangs(ctx, inst, publishIntent{
		resolveDoc: func(ctx context.Context, fresh *presentationmodel.InstanceEntity) (*contenttemplatecontract.ResolvedTemplate, error) {
			return s.resolveBoundTemplate(ctx, fresh, switchTo)
		},
		mode: mode,
	}); err != nil {
		return nil, publishFailedErr(err)
	}
	return s.toResp(ctx, inst)
}

// ClearOverride 兼容旧契约：语义等同「重新套用预设」（放弃独立文档、回到跟随模板）。
//
// 保留它是因为商品详情页/模板面板既有调用点都用它；新代码请直接用 ReapplyPreset
// （名字与双轨语义一致，避免「清覆盖」被读成「清空内容」）。
func (s *Service) ClearOverride(ctx context.Context, req *presentationdto.ClearOverrideReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	return s.ReapplyPreset(ctx, &presentationdto.ReapplyPresetReq{
		InstanceID: req.InstanceID, ProjectID: req.ProjectID, TemplateID: req.TemplateID,
	})
}

// UpdateURL 修改已发布实例的线上路径（改 URL）。
func (s *Service) UpdateURL(ctx context.Context, req *presentationdto.UpdateURLReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	// 工程作用域（DB-009 第二批）：实例表的定位、占用预检、锁内重读全部在本工程内进行。
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.locateInstance(ctx, projectID, req)
	if err != nil {
		return nil, err
	}
	newLogical, err := s.normalizeLogicalPath(ctx, inst.ProjectID, req.NewPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrInvalidPath, err)
	}
	// 实例级互斥只覆盖「锁内重读 → 同路径判定 → 占用预检」这一段（PERF-01）：
	// 它保护的是「同一条新路径不会被两个并发改 URL 同时抢到」。编译与产物落盘交给
	// publishAllLangs 的分段锁 —— 内联函数一返回就放锁，否则会和它自取的同一把锁自锁。
	var (
		oldPubs    []presentationmodel.PublicationEntity
		oldLogical string
	)
	lock := s.lockInstance(inst.EntityType, inst.EntityID)
	if err = func() error {
		lock.Lock()
		defer lock.Unlock()

		// 锁内重读：等锁期间实例可能已被重建、改过 URL 或删除。
		fresh, gerr := s.m.GetInstance(ctx, inst.ProjectID, inst.ID)
		if gerr != nil {
			return errors.New(presentationenums.ErrNotFound)
		}
		inst = fresh
		oldLogical = s.instanceLogicalPath(ctx, inst)
		if newLogical == oldLogical {
			return errors.New(presentationenums.ErrSamePath)
		}
		oldPubs, _ = s.m.ListPublications(ctx, inst.ID)
		// 预检：新逻辑路径下全部语言访问路径均空闲。
		return s.ensureLogicalPathFree(ctx, inst.ProjectID, newLogical, inst.ID)
	}(); err != nil {
		return nil, err
	}

	// mode=nil：改 URL 不是改渲染模式。
	primaryArtifactID, err := s.publishAllLangs(ctx, inst, publishIntent{
		// 模板沿用实例当前绑定（改 URL 不是换模板）：空显式 id = 绑定优先且不回落
		//「同类型最新」，由发布会话按锁内重读的行解析。
		resolveDoc: func(ctx context.Context, fresh *presentationmodel.InstanceEntity) (*contenttemplatecontract.ResolvedTemplate, error) {
			return s.resolveBoundTemplate(ctx, fresh, "")
		},
		logicalPath: newLogical,
	})
	if err != nil {
		return nil, publishFailedErr(err)
	}
	// 旧路径处置：逐语言取消激活或 301。
	for _, pub := range oldPubs {
		if pub.ActivePath == "" {
			continue
		}
		if derr := s.disposeOldPath(ctx, inst, pub.ActivePath, newLogical, primaryArtifactID, req.WithRedirect); derr != nil {
			logger.Scene("build").With("instanceId", inst.ID).With("oldPath", pub.ActivePath).
				Warn("改 URL 后旧路径处置失败（新路径已生效）: " + derr.Error())
		}
	}
	logger.Scene("build").With("instanceId", inst.ID).With("oldPath", oldLogical).With("newPath", newLogical).
		Info("详情页 URL 修改完成")
	return s.toResp(ctx, inst)
}

// locateInstance 按 ID 或 (实体类型, 实体 id) 定位实例。
//
// 两种入口都保留：后台「详情页模板」页只持有实体 id（列表行给的是商品/文章），
// 而 API 调用方通常持有实例 id。
func (s *Service) locateInstance(ctx context.Context, projectID string, req *presentationdto.UpdateURLReq) (*presentationmodel.InstanceEntity, error) {
	if id := strings.TrimSpace(req.ID); id != "" {
		inst, err := s.m.GetInstance(ctx, projectID, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, errors.New(presentationenums.ErrNotFound)
			}
			return nil, err
		}
		return inst, nil
	}
	entityType, entityID := strings.TrimSpace(req.EntityType), strings.TrimSpace(req.EntityID)
	if entityType == "" || entityID == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	inst, err := s.m.GetInstanceByEntity(ctx, projectID, entityType, entityID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(presentationenums.ErrNotFound)
		}
		return nil, err
	}
	return inst, nil
}

// ensurePathFree 占用预检：目标路径不得被其他页面或展示实例占用。
//
// 两个真源都要查，缺一不可：
//   - page_routes：手工页面与（本次起）已登记的展示实例的占用；
//   - presentation_instances.url_path：本次之前创建的历史实例 —— 它们的占用
//     从未进过路由表，只查路由表会放行「抢一个历史详情页的路径」，随后 FS
//     激活直接覆盖对方线上内容。
//
// excludeInstanceID 为空的场景是创建实例（自己还不存在，无需排除）。
func (s *Service) ensurePathFree(ctx context.Context, projectID, path, excludeInstanceID string) error {
	if s.routes != nil {
		occupied, err := s.routes.IsPathOccupied(ctx, &pubcontract.IsOccupiedReq{
			ProjectID: projectID, Path: path, ExcludePresentationID: excludeInstanceID,
		})
		if err != nil {
			return err
		}
		if occupied {
			logger.Scene("build").With("url", path).Warn("详情页路径预检被拒绝：已被其他实体占用")
			return errors.New(presentationenums.ErrPathOccupied)
		}
	}
	if _, err := s.m.FindInstanceByPath(ctx, projectID, path, excludeInstanceID); err == nil {
		logger.Scene("build").With("url", path).Warn("详情页路径预检被拒绝：已被其他展示实例占用")
		return errors.New(presentationenums.ErrPathOccupied)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if _, err := s.m.FindInstanceByActivePath(ctx, projectID, path, excludeInstanceID); err == nil {
		logger.Scene("build").With("url", path).Warn("详情页路径预检被拒绝：已被其他展示实例的多语言路径占用")
		return errors.New(presentationenums.ErrPathOccupied)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return nil
}

// registerRoute 把实例的线上路径登记进 page_routes（active，同归属者幂等）。
//
// 登记的目的是让「谁占着这个路径」成为可查事实：不登记时页面侧发布/改 URL 的
// 占用预检（IsPathOccupied 查 page_routes）看不见详情页，两边可以先后激活同一
// 路径，后者覆盖前者的线上内容且全程无报错。routes 为 nil（降级装配）时跳过。
func (s *Service) registerRoute(ctx context.Context, inst *presentationmodel.InstanceEntity, urlPath, artifactID string) error {
	if s.routes == nil {
		return nil
	}
	if _, err := s.routes.Activate(ctx, &pubcontract.ActivateReq{
		ProjectID:      inst.ProjectID,
		Path:           urlPath,
		PresentationID: inst.ID,
		ArtifactID:     artifactID,
	}); err != nil {
		return err
	}
	return nil
}

// disposeOldPath 处置改 URL 后遗留的旧路径。
//
//   - withRedirect：落盘 301 产物（不经过 Compiler，只有 redirect.json）并激活
//     到旧路径，同时把路由行标记为 redirect —— 旧链接继续可用，SEO 权重转移；
//   - 否则：取消旧路径的访问面激活并释放路由占用 —— 旧链接 404。
//
// 调用时机固定在「新路径已激活」之后，失败由调用方记日志而不回滚新路径。
func (s *Service) disposeOldPath(ctx context.Context, inst *presentationmodel.InstanceEntity,
	oldPath, newPath, artifactID string, withRedirect bool) error {
	if withRedirect {
		ra, err := pipeline.NewRedirectArtifact(newPath, 301)
		if err != nil {
			return err
		}
		loc, err := s.store.PutRedirect(ra)
		if err != nil {
			return err
		}
		if err = s.publication.Activate(oldPath, loc); err != nil {
			return err
		}
		if s.routes != nil {
			if _, err = s.routes.Redirect(ctx, &pubcontract.RedirectReq{
				ProjectID:      inst.ProjectID,
				OldPath:        oldPath,
				PresentationID: inst.ID,
				ArtifactID:     artifactID,
			}); err != nil {
				return err
			}
		}
		return nil
	}
	if err := s.publication.Deactivate(oldPath); err != nil {
		return err
	}
	if s.routes != nil {
		if err := s.routes.Deactivate(ctx, &pubcontract.DeactivateReq{
			ProjectID:      inst.ProjectID,
			Path:           oldPath,
			PresentationID: inst.ID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// blockResolverAdapter 把 block 契约适配为 builder 的 core.BlockResolver。
type blockResolverAdapter struct {
	blocks blockcontract.BlockService
	ctx    context.Context
	// projectID 是块查询的必填 scope（block.Detail 用它做跨工程越权防护）：
	// 漏传只会得到「参数缺失」，而这里是降级路径 —— 构建照常完成、产物少一截。
	projectID string
	cache     map[string]*builder.Page
	errs      map[string]error
}

// newBlockResolverAdapter 构造单次编译的块解析适配器（缓存随编译实例存活）。
func newBlockResolverAdapter(blocks blockcontract.BlockService, ctx context.Context, projectID string) *blockResolverAdapter {
	return &blockResolverAdapter{
		blocks: blocks, ctx: ctx, projectID: projectID,
		cache: map[string]*builder.Page{}, errs: map[string]error{},
	}
}

// ResolveBlockRoot 实现 core.BlockResolver：按块 ID 返回块文档 root 节点。
func (a *blockResolverAdapter) ResolveBlockRoot(blockID string) ([]*core.Node, error) {
	page, err := a.blockPage(blockID)
	if err != nil {
		return nil, err
	}
	return page.Root, nil
}

// blockPage 解析块文档为 builder.Page（带缓存）。
func (a *blockResolverAdapter) blockPage(blockID string) (*builder.Page, error) {
	if page, ok := a.cache[blockID]; ok {
		return page, nil
	}
	if err, ok := a.errs[blockID]; ok {
		return nil, err
	}
	fail := func(err error) (*builder.Page, error) {
		a.errs[blockID] = err
		return nil, err
	}
	if a.blocks == nil {
		return fail(fmt.Errorf("全局块 %s 不可用（block 契约未装配）", blockID))
	}
	block, err := a.blocks.Detail(a.ctx, &blockcontract.DetailReq{ProjectID: a.projectID, ID: blockID})
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
	a.cache[blockID] = page
	return page, nil
}

// 编译期断言：适配器实现 core.BlockResolver。
var _ core.BlockResolver = (*blockResolverAdapter)(nil)

// ResolveStructureDocument 实现 pipeline.StructureTemplatePort：结构槽位（页眉 / 页脚）
// 绑定的结构模板文档来源。
//
// 薄适配：版本解析与文档严格校验都在 contenttemplate 契约里
// （ResolveTemplateByIDScoped 按模板类型校验，并拒绝结构模板里的字段绑定），
// 这里只把「契约未装配 / 模板不存在 / 文档为空」统一成错误 —— 三者对构建期的含义
// 是同一个：这套模板不可用，回退到该槽位的块绑定（见 pipeline.BuildStructureSlots）。
func (s *Service) ResolveStructureDocument(ctx context.Context, projectID, templateID string) ([]byte, error) {
	if s == nil || s.templates == nil {
		return nil, fmt.Errorf("结构模板 %s 不可用（contenttemplate 契约未装配）", templateID)
	}
	tpl, err := s.templates.ResolveTemplateByIDScoped(ctx, projectID, templateID)
	if err != nil {
		return nil, err
	}
	if tpl == nil || len(tpl.Document) == 0 {
		return nil, fmt.Errorf("结构模板 %s 无可用文档", templateID)
	}
	return tpl.Document, nil
}

// 编译期断言：本服务是结构模板解析端口（结构槽位的模板来源）。
var _ pipeline.StructureTemplatePort = (*Service)(nil)

// ListBlockSourceRefs 列出文档树引用了该块的自动发布实例（审计 ARCH-02）。
//
// 逐工程扇出（DB-009 第二批）：块 id 说不出工程，而 presentation_instances 带 FORCE 策略 ——
// 漏作用域时这条查询静默返回空，删除保护会据此**放行**（正是本 finding 要拦住的形态）。
//
// Detail 区分 override_document 与 snapshot：两者的解除路径不同（改实例文档 vs
// 改模板后重建），合并成一条会让操作者以为改完一处就够。
func (s *Service) ListBlockSourceRefs(ctx context.Context, blockID string) (out []blockcontract.BlockUsage, err error) {
	if s == nil || s.project == nil {
		// 没有工程契约就枚举不出工程，而 instances 带 FORCE 策略：宁可显式失败，
		// 也不返回空集合 —— 空集合在删除保护里等于「没有引用」。
		return nil, errors.New("project 契约未装配，无法逐工程反查自动发布实例的块引用")
	}
	projects, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range projects {
		projectID := strings.TrimSpace(projects[i].ID)
		if projectID == "" {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		rows, rerr := s.m.ListBlockDocumentRefs(ctx, projectID, blockID)
		if rerr != nil {
			return nil, rerr
		}
		for j := range rows {
			label := strings.TrimSpace(rows[j].URLPath)
			if label == "" {
				label = strings.TrimSpace(rows[j].EntityType) + ":" + strings.TrimSpace(rows[j].EntityID)
			}
			detail := "snapshot"
			if !rows[j].FromSnapshot {
				detail = "override_document"
			}
			out = append(out, blockcontract.BlockUsage{
				Kind: blockcontract.UsageKindPresentationInstance, ProjectID: projectID,
				EntityID: rows[j].InstanceID, Label: label, Detail: detail,
			})
		}
	}
	return out, nil
}

// instanceModePending 一次发布要落的模式变更（nil = 本次不改模式）。
//
// 为什么走参数而不是先单独写一列：模式与文档必须和快照/产物/指针同一事务
// （persistMultiLangArtifacts 的那一个 tx），否则中间态「文档已换、模式没换」
// 会被下一次模板更新按 template 模式重建回模板文档 —— 自定义凭空消失。
type instanceModePending struct {
	renderMode string
	document   json.RawMessage
}

// instanceDocumentFor 按渲染模式取编译底稿。
//
// template：原样返回模板（每次构建参与，模板更新可全局下发）；
// document：以覆盖文档为底稿（binding 照常解析 → 实体数据仍取最新）。
// 这里复用 render.go 的 withInstanceDocument（只换文档、保留模板身份字段），
// 不重复实现覆盖应用逻辑。
func instanceDocumentFor(inst *presentationmodel.InstanceEntity,
	tpl *contenttemplatecontract.ResolvedTemplate) *contenttemplatecontract.ResolvedTemplate {
	if inst != nil && presentationmodel.IsDocumentMode(inst.RenderMode) {
		return withInstanceDocument(inst, tpl)
	}
	return tpl
}

// documentsEqual 文档归一化后比较：判定「结构是否真的变了」。
//
// encoding/json 序列化 map 时按键排序，因此同一结构的不同书写顺序结论一致；
// 解析失败按「不同」处理 —— 让后续编译给出明确的文档错误，而不是在这里静默放过
// （静默放过会表现为「保存后什么都没发生」，是本项目最难查的一类问题）。
func documentsEqual(a, b json.RawMessage) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	an, aerr := json.Marshal(av)
	bn, berr := json.Marshal(bv)
	if aerr != nil || berr != nil {
		return false
	}
	return bytes.Equal(an, bn)
}

// RollbackArtifact 产物指针回滚：把实例的线上指针切回历史产物（秒级，不重新编译）。
//
// 与 page 侧 Rollback 同一语义（只切指针、不动暂存/文档指针）：presentation 的产物行
// 在自家表里（presentation_artifacts），所以按 hash 找行 + 校验文件在位 + 重新激活 URL，
// 最后在**一个事务**里落指针。
func (s *Service) RollbackArtifact(ctx context.Context, req *presentationdto.RollbackArtifactReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || strings.TrimSpace(req.InstanceID) == "" || strings.TrimSpace(req.TargetHash) == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.m.GetInstance(ctx, projectID, req.InstanceID)
	if err != nil {
		return nil, errors.New(presentationenums.ErrNotFound)
	}
	// 产物指针回滚不重新编译，整段（查产物 → 校验文件在位 → 激活 → 切指针）都留在
	// 实例锁内：它是纯粹的指针切换，与发布会话的两个锁内段共用同一把锁即可（PERF-01）。
	lock := s.lockInstance(inst.EntityType, inst.EntityID)
	lock.Lock()
	defer lock.Unlock()

	art, aerr := s.m.GetArtifactByHash(ctx, inst.ID, req.TargetHash)
	if aerr != nil {
		return nil, errors.New(presentationenums.ErrRollbackTargetMiss)
	}
	// 文件不在位（误删 / 磁盘损坏）不能切指针：切了就是「路由指向不存在的文件」，
	// 比拒绝回滚严重得多（线上直接 404，且审计里看不出原因）。
	loc := pipeline.ArtifactLocator(art.ArtifactHash)
	if verr := s.store.VerifyArtifact(loc, art.ArtifactHash); verr != nil {
		logger.Scene("build").With("presentation_id", inst.ID).With("hash", art.ArtifactHash).
			Warn("产物回滚目标文件不在位，拒绝回滚")
		return nil, errors.New(presentationenums.ErrRollbackTargetMiss)
	}
	if aerr = s.publication.Activate(inst.URLPath, loc); aerr != nil {
		logger.Scene("build").With("presentation_id", inst.ID).With("path", inst.URLPath).
			Error(aerr, "产物回滚激活失败")
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrRollbackFailed, aerr)
	}
	now := time.Now().UTC()
	artID := art.ID
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		inst.ActiveArtifactID = &artID
		inst.StagedArtifactID = &artID
		inst.Stale = false
		inst.PublishedAt = &now
		inst.UpdatedAt = now
		return s.m.UpdateInstancePointersTx(tx, inst.ProjectID, inst)
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrRollbackFailed, err)
	}
	return s.toResp(ctx, inst)
}

// RollbackDocument 快照级文档回滚：取历史快照的文档重发（重新编译，实体数据取最新）。
//
// 语义上等于「把这份历史文档作为该商品当前文档」→ 实例进入 document 模式
// （模板更新不再覆盖它），这与「内容回到那一版」的直觉一致；想回到跟随模板走
// ReapplyPreset（可反悔）。
func (s *Service) RollbackDocument(ctx context.Context, req *presentationdto.RollbackDocumentReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || strings.TrimSpace(req.InstanceID) == "" || strings.TrimSpace(req.SnapshotID) == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.m.GetInstance(ctx, projectID, req.InstanceID)
	if err != nil {
		return nil, errors.New(presentationenums.ErrNotFound)
	}
	// 快照读取与归属校验是只读的，不需要实例锁（PERF-01）：需要互斥的是随后的发布，
	// 它按自己的分段锁完成版本分配、落库与指针推进。
	snap, serr := s.m.GetSnapshot(ctx, req.SnapshotID)
	if serr != nil {
		return nil, errors.New(presentationenums.ErrRollbackTargetMiss)
	}
	// 归属校验：快照表没有 project_id，拿错实例的快照必须显式拒绝
	//（否则会把别的商品的内容发布到这个商品的 URL 上）。
	if snap.PresentationInstanceID != inst.ID {
		return nil, errors.New(presentationenums.ErrSnapshotMismatch)
	}
	mode := &instanceModePending{
		renderMode: presentationmodel.RenderModeDocument,
		document:   snap.Document,
	}
	if _, err = s.publishAllLangs(ctx, inst, publishIntent{
		resolveDoc: func(ctx context.Context, fresh *presentationmodel.InstanceEntity) (*contenttemplatecontract.ResolvedTemplate, error) {
			bound, rerr := s.resolveBoundTemplate(ctx, fresh, "")
			if rerr != nil {
				return nil, rerr
			}
			pending := *fresh
			pending.OverrideDocument = snap.Document
			pending.RenderMode = presentationmodel.RenderModeDocument
			return instanceDocumentFor(&pending, bound), nil
		},
		mode: mode,
	}); err != nil {
		return nil, publishFailedErr(err)
	}
	return s.toResp(ctx, inst)
}

// ListSnapshots 实例的历史快照清单（新→旧），供后台选择文档回滚目标。
//
// 先定位实例再查快照：快照表没有工程列，用实例的工程作用域兜住越权读。
// CountByTemplate 按绑定模板统计实例数（template 模式 / document 模式各多少）。
//
// 给后台的「编辑模板（影响 N 个商品）」提供影响面数字：模板编辑是全局动作，
// 按钮上不写清影响范围，用户只能凭猜 —— 猜错的代价是整站商品页一起变样。
// 只数未删除的实例，且按模式分开：转独立后的商品**不**受模板更新影响，
// 把它们的数量算进「影响面」会吓退用户（数字必须是真的）。
func (s *Service) CountByTemplate(ctx context.Context, req *presentationdto.CountByTemplateReq) (
	res *presentationdto.CountByTemplateResp, err error) {
	if req == nil || strings.TrimSpace(req.TemplateID) == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	tplMode, docMode, err := s.m.CountInstancesByTemplate(ctx, projectID, req.TemplateID)
	if err != nil {
		return nil, err
	}
	return &presentationdto.CountByTemplateResp{
		TemplateMode: tplMode, DocumentMode: docMode, Total: tplMode + docMode,
	}, nil
}

func (s *Service) ListSnapshots(ctx context.Context, req *presentationdto.ListSnapshotsReq) (
	list []*presentationdto.SnapshotSummary, err error) {
	if req == nil || strings.TrimSpace(req.InstanceID) == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.m.GetInstance(ctx, projectID, req.InstanceID)
	if err != nil {
		return nil, errors.New(presentationenums.ErrNotFound)
	}
	rows, err := s.m.ListSnapshots(ctx, inst.ID, req.Limit)
	if err != nil {
		return nil, err
	}
	list = make([]*presentationdto.SnapshotSummary, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		list = append(list, &presentationdto.SnapshotSummary{
			ID:                      r.ID,
			SourceTemplateVersionID: r.SourceTemplateVersionID,
			CreatedAt:               r.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	return list, nil
}
