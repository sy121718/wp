// Package presentationservice presentation 模块业务实现（0-A2）。
//
// 自动发布：内容实体 + ContentTemplate → 派生快照 → 同一 Publish Compiler
// → ArtifactStore → PublicationStore（与手工 Page 共享管线，docs/02 §3）。
//
// DDL 对齐（本轮修复）：实例/快照/产物/依赖四张表按生产 DDL 读写
// （presentation_instances 用 stale + staged/active_artifact_id 指针，
// 不再有 status / artifact_hash 列）；project_id / template_id 为 NOT NULL
// 外键，创建时经 project 契约解析工程、经 ResolveTemplate 取模板 ID。
//
// MVP 取舍（开发阶段）：DocumentSnapshot 保存模板 AST（含 binding 节点），
// 编译时经 ContentResolver 解析为字面量——而非领域模型 §3.3 的「快照已
// 解析为字面量」。收益：复用 builder.WithContentResolver 注入，避免遍历
// AST 预解析的组件耦合；实体更新后 Rebuild 重编译即得新数据。
package presentationservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	contentcontract "go_wp/internal/module/content/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"
	projectcontract "go_wp/internal/module/project/contract"

	"go_wp/internal/builder"
	"go_wp/internal/pipeline"
	"go_wp/internal/templates"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/pkg/logger"
)

// systemCreator 产物行 created_by 的占位（NOT NULL uuid 列不接受空串）。
const systemCreator = "00000000-0000-0000-0000-000000000000"

// maxAutoRebuildInstances 单次依赖失效触发的自动重建上限（与 page 侧同口径）。
const maxAutoRebuildInstances = 20

// instanceLockStripes 实例级并发锁的分片数。
// 用固定分片而非「按 ID 建锁」：既保证同实例互斥，又不随实例数累积锁对象。
const instanceLockStripes = 64

// Service presentation 模块业务实现。
type Service struct {
	m           *presentationmodel.Model
	templates   contenttemplatecontract.ContentTemplateService
	content     contentcontract.ContentService
	project     projectcontract.ProjectService
	store       *pipeline.LocalStore
	publication *pipeline.LocalPublicationStore
	// instanceLocks 实例分片互斥锁：并发构建同一实例时串行化
	// 「构建 → 落库 → 激活」整段序列，避免产物版本号 MAX+1 竞态与
	// active_artifact_id 指针交错覆盖（线上内容与 DB 指针分裂）。
	instanceLocks [instanceLockStripes]sync.Mutex
}

// lockInstance 取某实体的实例级锁（按 entity 维度：创建与重建互斥同一把）。
func (s *Service) lockInstance(entityType, entityID string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(entityType + ":" + entityID))
	return &s.instanceLocks[h.Sum32()%instanceLockStripes]
}

// NewService 构造（依赖 contenttemplate/content/project 契约 + pipeline 内核）。
func NewService(m *presentationmodel.Model,
	templates contenttemplatecontract.ContentTemplateService,
	content contentcontract.ContentService,
	project projectcontract.ProjectService) *Service {
	return &Service{
		m:           m,
		templates:   templates,
		content:     content,
		project:     project,
		store:       &pipeline.LocalStore{Root: pipeline.DefaultArtifactRoot()},
		publication: &pipeline.LocalPublicationStore{ActiveRoot: pipeline.ActiveRoot()},
	}
}

// 编译期契约断言。
var _ presentationcontract.PresentationService = (*Service)(nil)

// 依赖扇出契约断言（pipeline.Fanout 的失效目标 + 自动重建实现）。
var _ pipeline.DependencyTarget = (*Service)(nil)
var _ pipeline.StaleRebuilder = (*Service)(nil)

// SourceType 实现 pipeline.DependencyTarget：本服务是自动发布实例来源。
func (s *Service) SourceType() string { return pipeline.SourceTypePresentation }

// MarkStaleByDependency 实现 pipeline.DependencyTarget：按依赖源精确标记。
func (s *Service) MarkStaleByDependency(ctx context.Context, kind, key string) ([]string, error) {
	return s.m.MarkStaleByDependency(ctx, kind, key, time.Now().UTC())
}

// RebuildStale 实现 pipeline.StaleRebuilder：重建受影响实例并重新发布。
//
// 策略（§8.3）：presentation 实例的语义就是「内容驱动的自动发布页面」，
// 创建即上线，因此重建成功后直接回写线上（与 page 侧「仅已发布语言自动回写」
// 的口径在结果上一致——presentation 不存在「从未发布」的实例）。
// 单个实例失败不阻断其余（记日志后继续），返回 nil 由 stale 标记兜底。
func (s *Service) RebuildStale(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > maxAutoRebuildInstances {
		logger.Scene("dependency").With("affected", len(ids)).With("limit", maxAutoRebuildInstances).
			Warn("自动重建超出单次上限，剩余实例保持 stale 等待后续触发")
		ids = ids[:maxAutoRebuildInstances]
	}
	rebuilt := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			return nil
		}
		inst, err := s.m.GetInstance(ctx, id)
		if err != nil {
			logger.Scene("dependency").With("presentation_id", id).Warn("自动重建跳过：实例不存在或已删除")
			continue
		}
		tpl, err := s.templates.ResolveTemplate(ctx, inst.EntityType)
		if err != nil {
			logger.Scene("dependency").With("presentation_id", id).
				Error(err, "自动重建跳过：无可用内容模板")
			continue
		}
		if _, err = s.rebuildInstance(ctx, inst, tpl); err != nil {
			logger.Scene("dependency").With("presentation_id", id).
				Error(err, "依赖失效后的自动重建失败（实例保持 stale）")
			continue
		}
		rebuilt++
	}
	if rebuilt > 0 {
		logger.Scene("dependency").With("rebuilt", rebuilt).Info("依赖失效后的自动重建完成")
	}
	return nil
}

// CreateInstance 创建自动发布实例：解析模板 → 编译 → 发布 → 记快照/产物/依赖。
func (s *Service) CreateInstance(ctx context.Context, req *presentationdto.CreateInstanceReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.EntityType == "" || req.EntityID == "" || req.URLPath == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	// 实例级互斥：同一实体的「构建 → 落库 → 激活」整体串行，
	// 并发请求不会各自推进产物版本号，也不会交错覆盖 active 指针。
	lock := s.lockInstance(req.EntityType, req.EntityID)
	lock.Lock()
	defer lock.Unlock()

	// 同实体已存在实例 → 视为幂等（返回已有；无需再解析工程）。
	if existing, gerr := s.m.GetInstanceByEntity(ctx, req.EntityType, req.EntityID); gerr == nil {
		return s.toResp(ctx, existing)
	} else if !errors.Is(gerr, gorm.ErrRecordNotFound) {
		return nil, gerr
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	// 解析模板版本 + 实体解析器。
	tpl, err := s.templates.ResolveTemplate(ctx, req.EntityType)
	if err != nil {
		return nil, errors.New(presentationenums.ErrNoTemplate)
	}
	// 顺序：构建（产物落盘幂等，不触碰线上）→ 实例落库 → 快照/产物行/指针 → 激活。
	// 落库失败时线上保持原样、可直接重试；反之「先激活后落库」会让线上渲染出
	// 错误实体内容且没有任何恢复入口。
	built, err := s.buildArtifact(ctx, req.EntityType, req.EntityID, req.URLPath, tpl)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	// 记实例（project_id / template_id 为 NOT NULL 外键，必须落库）。
	now := time.Now().UTC()
	inst := &presentationmodel.InstanceEntity{
		ID: uuid.NewString(), ProjectID: projectID, EntityType: req.EntityType,
		EntityID: req.EntityID, URLPath: req.URLPath, TemplateID: tpl.TemplateID,
		Stale: true, CreatedAt: now, UpdatedAt: now,
	}
	if err = s.m.CreateInstance(ctx, inst); err != nil {
		return nil, err
	}
	if err = s.persistBuild(ctx, inst, tpl, built, now); err != nil {
		return nil, err
	}
	// 落库全部成功后才上线。激活失败时实例与指针已存在（线上仍是旧内容），
	// 可经 Rebuild 自愈，不产生「线上有内容、DB 无记录」的分裂。
	if err = s.activate(req.URLPath, built); err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	return s.toResp(ctx, inst)
}

// Rebuild 实体数据更新后重建：重解析模板+实体 → 重新编译发布。
func (s *Service) Rebuild(ctx context.Context, req *presentationdto.RebuildReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.EntityID == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	// 反查实例（entity_id 匹配）。
	inst, err := s.findByEntityID(ctx, req.EntityID)
	if err != nil {
		return nil, errors.New(presentationenums.ErrNotFound)
	}
	tpl, err := s.templates.ResolveTemplate(ctx, inst.EntityType)
	if err != nil {
		return nil, errors.New(presentationenums.ErrNoTemplate)
	}
	return s.rebuildInstance(ctx, inst, tpl)
}

// rebuildInstance 实例重建主链：编译发布 → 新快照 → 新产物行 → 指针切换 → 依赖落库。
func (s *Service) rebuildInstance(ctx context.Context, inst *presentationmodel.InstanceEntity,
	tpl *contenttemplatecontract.ResolvedTemplate) (res *presentationdto.InstanceResp, err error) {
	// 实例级互斥：并发重建同一实例时串行化，避免版本号竞态与指针交错。
	lock := s.lockInstance(inst.EntityType, inst.EntityID)
	lock.Lock()
	defer lock.Unlock()

	built, err := s.buildArtifact(ctx, inst.EntityType, inst.EntityID, inst.URLPath, tpl)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	if err = s.persistBuild(ctx, inst, tpl, built, time.Now().UTC()); err != nil {
		return nil, err
	}
	if err = s.activate(inst.URLPath, built); err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	return s.toResp(ctx, inst)
}

// builtArtifact 一次构建的产物信息（编译结果 + 存储定位 + Manifest 依赖）。
type builtArtifact struct {
	Hash     string
	Loc      pipeline.Locator
	Manifest pipeline.Manifest
}

// persistBuild 把一次构建结果落库：快照行 → 产物行 → 实例指针 → 依赖记录。
//
// 顺序不可颠倒：presentation_artifacts 以复合外键引用 (snapshot_id, instance_id)，
// 快照必须先存在；实例的 active/staged 指针又引用产物行。
func (s *Service) persistBuild(ctx context.Context, inst *presentationmodel.InstanceEntity,
	tpl *contenttemplatecontract.ResolvedTemplate, built builtArtifact, now time.Time) error {
	snapID := uuid.NewString()
	snap := &presentationmodel.SnapshotEntity{
		ID: snapID, PresentationInstanceID: inst.ID,
		SourceTemplateVersionID: tpl.VersionID, SourceEntityRevisionID: inst.EntityID,
		Document: tpl.Document, CreatedAt: now,
	}
	// 四步跨四张表，必须同一事务：任一中间失败都会留下自相矛盾的实例状态
	//（产物行已写而 active 指针仍指旧产物、依赖记录指向不存在的产物等）。
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := s.m.CreateSnapshotTx(tx, snap); err != nil {
			return err
		}
		artifactID, err := s.recordArtifactTx(ctx, tx, inst, snapID, built, now)
		if err != nil {
			return err
		}
		inst.CurrentSnapshotID = &snapID
		inst.StagedSnapshotID = &snapID
		inst.StagedArtifactID = &artifactID
		inst.ActiveArtifactID = &artifactID
		inst.Stale = false
		inst.PublishedAt = &now
		inst.UpdatedAt = now
		if err = s.m.UpdateInstancePointersTx(tx, inst); err != nil {
			return err
		}
		return s.persistDependenciesTx(tx, inst.ID, artifactID, built.Manifest.Dependencies, now)
	})
}

// recordArtifactTx 事务内写产物行（同 hash 幂等复用，避免重建时产物行膨胀）。
func (s *Service) recordArtifactTx(ctx context.Context, tx *gorm.DB, inst *presentationmodel.InstanceEntity,
	snapID string, built builtArtifact, now time.Time) (artifactID string, err error) {
	if existing, gerr := s.m.GetArtifactByHashTx(tx, inst.ID, built.Hash); gerr == nil {
		return existing.ID, nil
	} else if !errors.Is(gerr, gorm.ErrRecordNotFound) {
		return "", gerr
	}
	manifestJSON, err := json.Marshal(built.Manifest)
	if err != nil {
		return "", err
	}
	version, err := s.m.NextArtifactVersionTx(tx, inst.ID)
	if err != nil {
		return "", err
	}
	e := &presentationmodel.ArtifactEntity{
		ID: uuid.NewString(), PresentationInstanceID: inst.ID, SnapshotID: snapID,
		Version: version, SourceHash: built.Manifest.SourceHash,
		BuildInputManifest: manifestJSON, BuildInputHash: built.Manifest.BuildInputHash,
		ArtifactProvider: "local", ArtifactKey: built.Loc.Key, ArtifactHash: built.Hash,
		CompilerVersion: built.Manifest.CompilerVersion,
		// 真实注册表版本（组件模板 + Props 结构 + 二进制 revision 的指纹）。
		// 原来与 CompilerVersion 写同一个常量 "internal-builder"，等于空转：
		// 自动发布实例的产物也就无法参与「组件更新后识别待重建页面」的比对，
		// 与手工 Page 路径行为不一致（见 builder.RegistryVersion）。
		RegistryVersion: builder.RegistryVersion(),
		Manifest:        manifestJSON, PayloadState: "available", Note: "",
		CreatedBy: systemCreator, CreatedAt: now,
	}
	if err = s.m.CreateArtifactTx(tx, e); err != nil {
		return "", err
	}
	return e.ID, nil
}

// persistDependenciesTx 事务内把本次产物的依赖集合写入 presentation_dependencies。
//
// 依赖记录是失效追踪的投影而非构建输入；但它必须与快照/产物行/指针同生共死，
// 否则会出现「指针已切到新产物、依赖记录仍是旧集合」的漂移，导致后续
// fan-out 按错误的依赖反查（漏标 stale 或误标）。
func (s *Service) persistDependenciesTx(tx *gorm.DB, instanceID, artifactID string, deps []pipeline.Dependency, now time.Time) error {
	if strings.TrimSpace(instanceID) == "" || strings.TrimSpace(artifactID) == "" {
		return nil
	}
	rows := make([]presentationmodel.DependencyEntity, 0, len(deps))
	seen := map[[2]string]bool{}
	for _, d := range deps {
		kind, key := strings.TrimSpace(d.Kind), strings.TrimSpace(d.Key)
		if kind == "" || key == "" {
			continue
		}
		if seen[[2]string{kind, key}] {
			continue
		}
		seen[[2]string{kind, key}] = true
		row := presentationmodel.DependencyEntity{
			PresentationID: instanceID, ArtifactID: artifactID,
			DependencyKind: kind, DependencyKey: key, LastChecked: now,
		}
		if rev := strings.TrimSpace(d.Revision); rev != "" {
			r := rev
			row.Revision = &r
		}
		rows = append(rows, row)
	}
	return s.m.ReplaceDependenciesTx(tx, artifactID, rows)
}

// Get 按 ID 查询。
func (s *Service) Get(ctx context.Context, req *presentationdto.GetReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	inst, err := s.m.GetInstance(ctx, req.ID)
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
	rows, err := s.m.ListInstances(ctx, req.EntityType)
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
	inst, err := s.m.GetInstance(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(presentationenums.ErrNotFound)
		}
		return err
	}
	// 反激活失败必须中止删除：否则实例行已删、URL 占用残留，后续同路径
	// 发布/激活会被「已占用」拒绝且无实例可查（状态分裂）。
	if err = s.publication.Deactivate(inst.URLPath); err != nil {
		return fmt.Errorf("删除实例前反激活 URL 失败: %w", err)
	}
	return s.m.DeleteInstance(ctx, req.ID)
}

// buildArtifact 编译模板 AST（经实体 resolver）→ 产物落盘（**不激活**）。
//
// 激活由调用方在实例落库成功后单独执行（见 activate）：先激活后落库时，
// 一旦落库失败，线上已渲染出新实体内容却没有任何恢复入口。
func (s *Service) buildArtifact(ctx context.Context, entityType, entityID, urlPath string,
	tpl *contenttemplatecontract.ResolvedTemplate) (built builtArtifact, err error) {
	page, err := builder.ParsePage(tpl.Document)
	if err != nil {
		return built, err
	}
	resolver, err := s.content.ResolverFor(ctx, entityType, entityID)
	if err != nil {
		return built, err
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		return built, err
	}
	compiled, err := builder.Compile(page,
		builder.WithContext(ctx),
		builder.WithComponentSet(set),
		builder.WithContentResolver(resolver))
	if err != nil {
		return built, err
	}
	doc, err := builder.RenderDocument(compiled)
	if err != nil {
		return built, err
	}
	html := []byte(doc)
	sourceHash := pipeline.SHA256(tpl.Document)
	artifact, err := pipeline.NewArtifact(html, &pipeline.Manifest{
		ManifestSchemaVersion:     1,
		PageDocumentSchemaVersion: 1,
		SourceID:                  entityID,
		SourceType:                pipeline.SourceTypePresentation,
		CanonicalPath:             urlPath,
		SourceHash:                sourceHash,
		BuildInputHash:            sourceHash,
		Dependencies:              presentationDependencies(entityType, entityID, tpl.TemplateID),
	})
	if err != nil {
		return built, err
	}
	loc, err := s.store.PutArtifact(artifact)
	if err != nil {
		return built, err
	}
	return builtArtifact{Hash: artifact.Hash, Loc: loc, Manifest: artifact.Manifest}, nil
}

// activate 把本次产物激活到线上 URL（覆盖 active 符号链接）。
// 调用时机：实例 / 快照 / 产物行 / 指针全部落库成功之后。
func (s *Service) activate(urlPath string, built builtArtifact) error {
	if err := s.publication.Activate(urlPath, built.Loc); err != nil {
		return fmt.Errorf("激活 %s 失败: %w", urlPath, err)
	}
	return nil
}

// presentationDependencies 本次产物的依赖源集合。
//
//   - direct_content:{type}:{id}    —— 内容实体字段变化（PIPE-3 自动重建的触发键，
//     与 content 模块 notifyContentChanged 声明的键逐字一致）；
//   - content_template:{templateID} —— 模板版本变化（kind 见 pipeline.DepKindContentTemplate；
//     当前无来源模块触发该键，登记用于审计与后续接入）。
func presentationDependencies(entityType, entityID, templateID string) []pipeline.Dependency {
	dep := pipeline.DirectContentKey(entityType, entityID)
	out := []pipeline.Dependency{{Kind: dep.Kind, Key: dep.Key}}
	if strings.TrimSpace(templateID) != "" {
		out = append(out, pipeline.Dependency{
			Kind: pipeline.DepKindContentTemplate,
			Key:  "content_template:" + templateID,
		})
	}
	return out
}

// resolveProjectID 解析实例所属工程：显式传入优先（校验存在），
// 否则经 project 契约取唯一工程；无工程或多工程时要求显式指定。
func (s *Service) resolveProjectID(ctx context.Context, explicit string) (string, error) {
	if id := strings.TrimSpace(explicit); id != "" {
		if s.project != nil {
			ok, err := s.project.Exists(ctx, id)
			if err != nil {
				return "", err
			}
			if !ok {
				return "", errors.New(presentationenums.ErrProjectNotFound)
			}
		}
		return id, nil
	}
	if s.project == nil {
		return "", errors.New(presentationenums.ErrProjectRequired)
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return "", err
	}
	if len(list) != 1 {
		return "", errors.New(presentationenums.ErrProjectRequired)
	}
	return list[0].ID, nil
}

// findByEntityID 按内容实体 ID 反查实例。
func (s *Service) findByEntityID(ctx context.Context, entityID string) (*presentationmodel.InstanceEntity, error) {
	rows, err := s.m.ListInstances(ctx, "")
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
		ID: e.ID, ProjectID: e.ProjectID, EntityType: e.EntityType, EntityID: e.EntityID,
		URLPath: e.URLPath, TemplateID: e.TemplateID, Stale: e.Stale,
		UpdatedAt: e.UpdatedAt.Format("2006-01-02 15:04"),
	}
	if e.ActiveArtifactID != nil {
		resp.Status = presentationenums.StatusActive
		resp.ArtifactID = *e.ActiveArtifactID
		if art, aerr := s.m.GetArtifact(ctx, *e.ActiveArtifactID); aerr == nil {
			resp.ArtifactHash = art.ArtifactHash
		}
	} else {
		resp.Status = presentationenums.StatusDraft
	}
	if e.CurrentSnapshotID != nil {
		resp.SnapshotID = *e.CurrentSnapshotID
	}
	return resp, nil
}
