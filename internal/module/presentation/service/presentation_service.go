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

	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"
	projectcontract "go_wp/internal/module/project/contract"
	pubcontract "go_wp/internal/module/publication/contract"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/internal/pipeline"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/pkg/logger"

	productcontract "go_wp/internal/module/product/contract"
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
	m         *presentationmodel.Model
	templates contenttemplatecontract.ContentTemplateService
	registry  core.EntitySourceRegistry
	project   projectcontract.ProjectService
	// blocks 全局块契约：内容模板内部可用 core.globalref 引用页眉/页脚等区块，
	// 构建期由这里内联展开（与手工 Page 路径同一机制，docs/02-D §1.2）。
	blocks blockcontract.BlockService
	// collection 集合源解析器（装配期注入，可空）：模板内的集合类组件
	// （core.cardstack 绑定 content:product 等）在构建期展开为静态列表数据。
	// 未注入时集合绑定节点构建期显式报错（不静默产出空列表）。
	collection core.CollectionResolver
	// productDS 商品构建期数据源（issue #35）：模板里的商品组件直连受限接口。
	productDS   productcontract.ProductDataSource
	store       *pipeline.LocalStore
	publication *pipeline.LocalPublicationStore
	// routes URL 占用登记契约（publication 模块）。
	//
	// 详情页实例的路径此前只切换访问面符号链接、从不登记 page_routes：页面侧
	// 占用预检看不见详情页，详情页也看不见页面，两边可以先后激活同一路径，
	// 后者直接覆盖前者的线上内容且全程无报错。本次补上登记，改 URL 也才有
	// 「占用预检 + 旧路径 301」的落点。可空（单元测试 / 降级装配）：为 nil 时
	// 只做实例表内的占用预检。
	routes pubcontract.PublicationService
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

// NewService 构造（依赖 contenttemplate 契约 + 实体类型注册表 + project 契约
// + block 契约 + pipeline 内核）。blocks 为 nil 时引用区块的模板构建期显式报错，
// 不静默产出占位结构（构建期必须失败优于线上出现空块）。
func NewService(m *presentationmodel.Model,
	templates contenttemplatecontract.ContentTemplateService,
	registry core.EntitySourceRegistry,
	project projectcontract.ProjectService,
	blocks blockcontract.BlockService,
	routes pubcontract.PublicationService) *Service {
	return &Service{
		m:           m,
		templates:   templates,
		registry:    registry,
		project:     project,
		blocks:      blocks,
		store:       &pipeline.LocalStore{Root: pipeline.DefaultArtifactRoot()},
		publication: &pipeline.LocalPublicationStore{ActiveRoot: pipeline.ActiveRoot()},
		routes:      routes,
	}
}

// SetCollectionResolver 注入集合源解析器（装配期调用，与其它可选依赖同模式：
// 不进构造参数）。传入 nil 表示模板不支持集合绑定。
func (s *Service) SetCollectionResolver(r core.CollectionResolver) { s.collection = r }

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
		// 用实例**绑定**的模板重建（issue #14）：依赖失效是内容变更触发的自动重建，
		// 不应改变「这个商品用哪套详情模板」——按类型重解析会把切换过的模板悄悄换回去。
		tpl, err := s.resolveBoundTemplate(ctx, inst, "")
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
	// 解析模板版本 + 实体解析器。req.TemplateID 非空 = 发布时显式指定用哪套命名模板
	// （issue #14 验收 2）；为空 = 按实体类型取默认模板（既有行为逐字不变）。
	tpl, err := s.resolveTemplate(ctx, req.EntityType, req.TemplateID)
	if err != nil {
		return nil, err
	}
	// 路径先归一化：产物 canonical、访问面符号链接与 page_routes 登记必须落在
	// 同一个字符串上（FS 侧本来就归一化），否则 /shop/x/ 与 /shop/x 会被当成
	// 两个路径，路由行指向的位置与实际内容不符。
	urlPath, err := pipeline.NormalizeURL(strings.TrimSpace(req.URLPath))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrInvalidPath, err)
	}
	// 占用预检必须在任何写操作之前：FS 激活一旦先跑并覆盖了别人的线上产物，
	// 就没有回滚入口（产物内容寻址，旧链接指向的是别人的产物）。
	if err = s.ensurePathFree(ctx, projectID, urlPath, ""); err != nil {
		return nil, err
	}
	// 顺序：构建（产物落盘幂等，不触碰线上）→ 实例落库 → 快照/产物行/指针 → 激活。
	// 落库失败时线上保持原样、可直接重试；反之「先激活后落库」会让线上渲染出
	// 错误实体内容且没有任何恢复入口。
	built, err := s.buildArtifact(ctx, req.EntityType, req.EntityID, urlPath, projectID, tpl)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	// 记实例（project_id / template_id 为 NOT NULL 外键，必须落库）。
	now := time.Now().UTC()
	inst := &presentationmodel.InstanceEntity{
		ID: uuid.NewString(), ProjectID: projectID, EntityType: req.EntityType,
		EntityID: req.EntityID, URLPath: urlPath, TemplateID: tpl.TemplateID,
		Stale: true, CreatedAt: now, UpdatedAt: now,
	}
	if err = s.m.CreateInstance(ctx, inst); err != nil {
		return nil, err
	}
	artifactID, err := s.persistBuild(ctx, inst, tpl, built, now, "")
	if err != nil {
		return nil, err
	}
	// 落库全部成功后才上线。激活失败时实例与指针已存在（线上仍是旧内容），
	// 可经 Rebuild 自愈，不产生「线上有内容、DB 无记录」的分裂。
	if err = s.activate(urlPath, built); err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	// 路由登记放在激活之后（占用已预检，同归属者重复登记幂等）：登记失败只记
	// 日志 —— 线上已激活是不可逆事实，把失败抛给调用方只会让它重发同一内容；
	// 缺的登记由下一次重建补上（Rebuild 也会登记）。
	if rerr := s.registerRoute(ctx, inst, urlPath, artifactID); rerr != nil {
		logger.Scene("build").With("instanceId", inst.ID).With("url", urlPath).
			Warn("详情页路由登记失败（线上已激活，下次重建自动补登）: " + rerr.Error())
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
	// req.TemplateID 非空 = 切换绑定并重新发布（验收 4：切换模板重新发布后产物随之变化）。
	tpl, err := s.resolveBoundTemplate(ctx, inst, req.TemplateID)
	if err != nil {
		return nil, err
	}
	return s.rebuildInstance(ctx, inst, tpl)
}

// GetByEntity 按内容实体查询实例（后台「详情页模板」页读当前绑定与发布状态）。
func (s *Service) GetByEntity(ctx context.Context, req *presentationdto.GetByEntityReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.EntityType == "" || req.EntityID == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	inst, err := s.m.GetInstanceByEntity(ctx, req.EntityType, req.EntityID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(presentationenums.ErrNotFound)
		}
		return nil, err
	}
	return s.toResp(ctx, inst)
}

// PreviewInstance 发布前预览（issue #14 验收 3）：按指定（或默认）模板渲染实体。
//
// 全程只读：不写快照/产物行/指针、不落盘产物、不激活 URL —— 预览看到的就是
// 发布时会产出的字节（与 buildArtifact 共用 renderHTML），但线上与库表状态不变。
func (s *Service) PreviewInstance(ctx context.Context, req *presentationdto.PreviewInstanceReq) (res *presentationdto.PreviewInstanceResp, err error) {
	if req == nil || req.EntityType == "" || req.EntityID == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	tpl, err := s.resolveTemplate(ctx, req.EntityType, req.TemplateID)
	if err != nil {
		return nil, err
	}
	// urlPath 传空：预览不激活 URL，canonical 由模板 settings.seo 决定（通常为空）。
	// 这是预览与发布在字节上的唯一有意差异（见 presentation_seo.go 取舍 2）。
	html, err := s.renderHTML(ctx, req.EntityType, req.EntityID, "", projectID, tpl)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	return &presentationdto.PreviewInstanceResp{
		HTML: string(html), EntityType: req.EntityType, EntityID: req.EntityID,
		TemplateID: tpl.TemplateID, TemplateName: tpl.TemplateName,
		TemplateVersionID: tpl.VersionID, TemplateVersion: tpl.Version,
	}, nil
}

// resolveTemplate 解析本次构建使用的模板版本（issue #14）。
//
// templateID 非空 = 显式指定某套命名模板，并校验实体类型一致（不允许拿商品模板
// 去渲染文章）；为空 = 按实体类型取默认模板（既有行为）。
func (s *Service) resolveTemplate(ctx context.Context, entityType, templateID string) (tpl *contenttemplatecontract.ResolvedTemplate, err error) {
	if id := strings.TrimSpace(templateID); id != "" {
		tpl, err = s.templates.ResolveTemplateByID(ctx, id)
		if err != nil {
			return nil, errors.New(presentationenums.ErrNoTemplate)
		}
		if tpl.EntityType != entityType {
			return nil, errors.New(presentationenums.ErrTemplateTypeMismatch)
		}
		return tpl, nil
	}
	tpl, err = s.templates.ResolveTemplate(ctx, entityType)
	if err != nil {
		return nil, errors.New(presentationenums.ErrNoTemplate)
	}
	return tpl, nil
}

// resolveBoundTemplate 解析实例重建要用的模板：实例绑定的模板是权威。
//
// 未显式指定时**不**回落「同类型最新模板」——否则切换模板后一次内容更新
// 就会把产物换回别的模板（验收 4 的反面）。绑定模板已被删除或类型不符时
// 回落类型默认模板并记日志：重建优先于报错，产物仍能自愈。
func (s *Service) resolveBoundTemplate(ctx context.Context, inst *presentationmodel.InstanceEntity,
	explicitID string) (*contenttemplatecontract.ResolvedTemplate, error) {
	if strings.TrimSpace(explicitID) != "" {
		return s.resolveTemplate(ctx, inst.EntityType, explicitID)
	}
	if id := strings.TrimSpace(inst.TemplateID); id != "" {
		tpl, err := s.templates.ResolveTemplateByID(ctx, id)
		if err == nil && tpl.EntityType == inst.EntityType {
			return tpl, nil
		}
		logger.Scene("build").With("presentation_id", inst.ID).With("template_id", id).
			Warn("实例绑定的模板不可用，本次重建回落到该类型的默认模板")
	}
	return s.resolveTemplate(ctx, inst.EntityType, "")
}

// rebuildInstance 实例重建主链：编译发布 → 新快照 → 新产物行 → 指针切换 → 依赖落库。
func (s *Service) rebuildInstance(ctx context.Context, inst *presentationmodel.InstanceEntity,
	tpl *contenttemplatecontract.ResolvedTemplate) (res *presentationdto.InstanceResp, err error) {
	// 实例级互斥：并发重建同一实例时串行化，避免版本号竞态与指针交错。
	lock := s.lockInstance(inst.EntityType, inst.EntityID)
	lock.Lock()
	defer lock.Unlock()

	built, err := s.buildArtifact(ctx, inst.EntityType, inst.EntityID, inst.URLPath, inst.ProjectID, tpl)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	artifactID, err := s.persistBuild(ctx, inst, tpl, built, time.Now().UTC(), "")
	if err != nil {
		return nil, err
	}
	if err = s.activate(inst.URLPath, built); err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	// 重建时重新登记路由：本次之前创建的实例从未登记过，重建是它们自愈的
	// 唯一时机（幂等，已登记的同归属者行原地升级不会报冲突）。
	if rerr := s.registerRoute(ctx, inst, inst.URLPath, artifactID); rerr != nil {
		logger.Scene("build").With("instanceId", inst.ID).With("url", inst.URLPath).
			Warn("详情页路由登记失败（线上已激活）: " + rerr.Error())
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
//
// urlPath 非空且与实例当前路径不同 = 本次是改 URL：url_path 与产物行/指针
// 同事务改写（理由见 model.UpdateInstanceURLTx）。返回值是本次生效的产物行
// ID，调用方用它登记 page_routes（路由的 artifact_id 是 uuid 列，必须写产物
// 行主键而非内容 hash）。
func (s *Service) persistBuild(ctx context.Context, inst *presentationmodel.InstanceEntity,
	tpl *contenttemplatecontract.ResolvedTemplate, built builtArtifact, now time.Time,
	urlPath string) (artifactID string, err error) {
	snapID := uuid.NewString()
	snap := &presentationmodel.SnapshotEntity{
		ID: snapID, PresentationInstanceID: inst.ID,
		SourceTemplateVersionID: tpl.VersionID, SourceEntityRevisionID: inst.EntityID,
		Document: tpl.Document, CreatedAt: now,
	}
	// 四步跨四张表，必须同一事务：任一中间失败都会留下自相矛盾的实例状态
	//（产物行已写而 active 指针仍指旧产物、依赖记录指向不存在的产物等）。
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if cerr := s.m.CreateSnapshotTx(tx, snap); cerr != nil {
			return cerr
		}
		// 模板切换（issue #14）与快照/产物/指针同事务：产物来自哪套模板，
		// 实例就必须记着哪套，否则下次重建会退回旧模板。
		if inst.TemplateID != tpl.TemplateID {
			if uerr := s.m.UpdateInstanceTemplateTx(tx, inst.ID, tpl.TemplateID, now); uerr != nil {
				return uerr
			}
			inst.TemplateID = tpl.TemplateID
		}
		// 改 URL 与快照/产物/指针同事务：产物烘的是新路径的 canonical，实例
		// 必须同步指向新路径，否则下次重建会拿旧路径重编，线上内容与路由脱节。
		if urlPath != "" && inst.URLPath != urlPath {
			if uerr := s.m.UpdateInstanceURLTx(tx, inst.ID, urlPath, now); uerr != nil {
				return uerr
			}
			inst.URLPath = urlPath
		}
		aid, aerr := s.recordArtifactTx(ctx, tx, inst, snapID, built, now)
		if aerr != nil {
			return aerr
		}
		artifactID = aid
		inst.CurrentSnapshotID = &snapID
		inst.StagedSnapshotID = &snapID
		inst.StagedArtifactID = &aid
		inst.ActiveArtifactID = &aid
		inst.Stale = false
		inst.PublishedAt = &now
		inst.UpdatedAt = now
		if uerr := s.m.UpdateInstancePointersTx(tx, inst); uerr != nil {
			return uerr
		}
		return s.persistDependenciesTx(tx, inst.ID, aid, built.Manifest.Dependencies, now)
	})
	return artifactID, err
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
	//
	// 要覆盖**全部**已激活路径而不只是当前 url_path：改过 URL 的实例还有一条
	// 旧路径的 301 链接，漏掉它 = 实例删了、线上旧路径仍 301 到一个死页面。
	for _, p := range s.instanceActivePaths(ctx, inst) {
		if derr := s.publication.Deactivate(p); derr != nil {
			return fmt.Errorf("删除实例前反激活 URL 失败 %s: %w", p, derr)
		}
	}
	// 路由占用同步释放：只删实例行会把 page_routes 里本实例的 active/redirect
	// 行留成悬空引用（外键指向已删除的实例），同路径再发布永远被拒。
	if s.routes != nil {
		if rerr := s.routes.DeleteRoutesByPresentation(ctx, &pubcontract.DeleteRoutesByPresentationReq{
			ProjectID: inst.ProjectID, PresentationID: inst.ID,
		}); rerr != nil {
			return fmt.Errorf("删除实例前释放路由占用失败: %w", rerr)
		}
	}
	return s.m.DeleteInstance(ctx, req.ID)
}

// instanceActivePaths 实例在访问面上已激活的全部路径（当前路径 + 历史 301 路径）。
//
// 查询失败时退化为当前 url_path：清理不完整优于因查询失败而删不掉实例 ——
// 前者是可发现、可重试的残留，后者是卡死的资源。
func (s *Service) instanceActivePaths(ctx context.Context, inst *presentationmodel.InstanceEntity) []string {
	paths := []string{inst.URLPath}
	if s.routes == nil {
		return paths
	}
	extra, err := s.routes.ListActivePathsByPresentation(ctx, &pubcontract.ListActivePathsByPresentationReq{
		ProjectID: inst.ProjectID, PresentationID: inst.ID,
	})
	if err != nil {
		logger.Scene("build").With("instanceId", inst.ID).
			Warn("读取实例已激活路径失败，仅清理当前路径: " + err.Error())
		return paths
	}
	return append(paths, extra...)
}

// buildArtifact 编译模板 AST（经 entity resolver）→ 产物落盘（**不激活**）。
//
// 激活由调用方在实例落库成功后单独执行（见 activate）：先激活后落库时，
// 一旦落库失败，线上已渲染出新实体内容却没有任何恢复入口。
func (s *Service) buildArtifact(ctx context.Context, entityType, entityID, urlPath, projectID string,
	tpl *contenttemplatecontract.ResolvedTemplate) (built builtArtifact, err error) {
	html, err := s.renderHTML(ctx, entityType, entityID, urlPath, projectID, tpl)
	if err != nil {
		return built, err
	}
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

// renderHTML 把模板 AST 经实体 resolver 编译为最终 HTML 字节（**不落盘、不落库、不激活**）。
//
// 发布（buildArtifact）与预览（PreviewInstance）共用这一份渲染，保证「预览看到的就是
// 发布出来的」；区别只在于发布还要把字节包成 Artifact 落盘并推进指针。
//
// urlPath 是该实例的线上路径（预览传空）：它是 SEO 头 canonical 的来源，
// 见 presentation_seo.go —— 唯一注入点的第二半（渲染函数本身不认识 SEO）。
func (s *Service) renderHTML(ctx context.Context, entityType, entityID, urlPath, projectID string,
	tpl *contenttemplatecontract.ResolvedTemplate) (html []byte, err error) {
	page, err := builder.ParsePage(tpl.Document)
	if err != nil {
		return nil, err
	}
	if s.registry == nil {
		return nil, errors.New(presentationenums.ErrRegistryMissing)
	}
	// 字段绑定的数据源白名单校验（不变量 4）：模板保存时已校验一次，这里再校一次
	// 是为了兜住「模板保存后才新增/改名数据源」与直连 service 的调用路径。
	if err = builder.ValidateFieldRefs(page, entityType, s.registry); err != nil {
		return nil, err
	}
	// 构建语言（工程默认语言）：实体字段的可翻译文本按它取译文（语境 实体.字段名）。
	// 语言进构建上下文，解析器据此取译文——语言不进 AST，产物仍由「模板 + 数据」唯一确定。
	lang := s.resolveLang(ctx, projectID)
	buildCtx := core.WithBuildLang(ctx, lang)
	resolver, err := s.registry.ResolverFor(buildCtx, entityType, entityID)
	if err != nil {
		return nil, err
	}
	// 实体字段驱动的 SEO 头（见 presentation_seo.go）：把 seoTitle / seoDescription 等
	// 喂进 page.Settings.SEO，随后的 builder.Compile 由既有的 BuildSEOHead 统一产出
	// canonical / OG / Twitter / JSON-LD。放在 Compile 之前是唯一有效的时机 ——
	// SEO 头在 compile 内一次性生成，之后没有回填入口。
	if err = applyEntitySEO(page, entityType, urlPath, s.registry.FieldWhitelist(entityType), resolver); err != nil {
		return nil, err
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		return nil, err
	}
	compileOpts := []builder.CompileOption{
		builder.WithContext(buildCtx),
		builder.WithComponentSet(set),
		builder.WithContentResolver(resolver),
		// 工程上下文：集合源（商品等分工程的数据）按它取数，不跨站点串数据。
		builder.WithProjectID(projectID),
		// 全局块内联展开（core.globalref）：内容模板可引用页眉/页脚/信任徽章等区块，
		// 与手工 Page 路径同一机制；未注入 block 契约时引用即报错（不静默出占位）。
		builder.WithBlockResolver(newBlockResolverAdapter(s.blocks, buildCtx)),
		// 组件固定文案取词：构建开始时刻的词条快照（确定性构建不变量）。
		builder.WithLanguage(lang), builder.WithTranslator(i18n.Snapshot(lang)),
	}
	compileOpts = append(compileOpts, pipeline.ClientAssetOptions()...)
	// 集合源注入（issue #9）：模板里的集合类组件按白名单展开商品等集合数据。
	if s.collection != nil {
		compileOpts = append(compileOpts, builder.WithCollectionResolver(s.collection))
	}
	// 商品数据源（issue #35）：与 page 路径同一注入方式。
	if s.productDS != nil {
		compileOpts = append(compileOpts, builder.WithProductDataSource(s.productDS))
	}
	// 站点统计代码（BIZ-8）：GA4 测量 ID 来自 SiteSettings 快照（空值 = 零字节注入）。
	// 与手工 Page 路径同一来源、同一判据（形状校验在 builder 侧单点）。
	if ga4 := s.siteGA4MeasurementID(ctx, projectID); ga4 != "" {
		compileOpts = append(compileOpts, builder.WithGA4MeasurementID(ga4))
	}
	compiled, err := builder.Compile(page, compileOpts...)
	if err != nil {
		return nil, err
	}
	doc, err := builder.RenderDocument(compiled)
	if err != nil {
		return nil, err
	}
	return []byte(doc), nil
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

// resolveLang 本次构建的目标语言 = 工程默认语言（语言清单 is_default，
// 缺失回退 i18n.default_lang）。
//
// 取不到时返回空串（不翻译，产物即原文）。多语言「每语言一份产物」的实例维度
// 属商品多语言票（issue #12）范围：那时只需在此处改为按实例语言解析。
func (s *Service) resolveLang(ctx context.Context, projectID string) string {
	if s.project == nil || strings.TrimSpace(projectID) == "" {
		return ""
	}
	lang, err := s.project.DefaultLocale(ctx, projectID)
	if err != nil {
		logger.Scene("build").With("project_id", projectID).
			Warn("构建语言解析失败，本次构建按原文输出: " + err.Error())
		return ""
	}
	return strings.TrimSpace(lang)
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

// SetProductDataSource 注入商品构建期数据源（issue #35，装配期调用）。
func (s *Service) SetProductDataSource(ds productcontract.ProductDataSource) { s.productDS = ds }
