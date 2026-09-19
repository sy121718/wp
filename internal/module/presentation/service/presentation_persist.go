package presentationservice

// presentation_persist.go — 落库与产物登记（重建、持久化、依赖写入、产物属主清单）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"

	"go_wp/internal/builder"
	"go_wp/internal/pipeline"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Rebuild 实体数据更新后重建：重解析模板+实体 → 重新编译发布。
func (s *Service) Rebuild(ctx context.Context, req *presentationdto.RebuildReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.EntityID == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	// 工程作用域（DB-009 第二批）：反查实例在本工程内进行。
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	// 反查实例（entity_id 匹配）。
	inst, err := s.findByEntityID(ctx, projectID, req.EntityID)
	if err != nil {
		return nil, errors.New(presentationenums.ErrNotFound)
	}
	// req.TemplateID 非空 = 切换绑定并重新发布（验收 4：切换模板重新发布后产物随之变化）。
	// 换底稿 = 放弃自定义（docs/04-C）：切换路径不应用实例覆盖文档，产物按新模板编译，
	// 覆盖列在 persistBuild 的事务里一并清除；非切换路径则优先用实例自己的文档
	//（binding 照常解析，实体数据更新不丢自定义）。
	switching := strings.TrimSpace(req.TemplateID) != ""
	tpl, err := s.resolveBoundTemplate(ctx, inst, req.TemplateID)
	if err != nil {
		return nil, err
	}
	// 非切换：按渲染模式取底稿（document 模式用该商品文档，模板照常解析数据）。
	if !switching {
		tpl = instanceDocumentFor(inst, tpl)
	}
	return s.rebuildInstance(ctx, inst, tpl)
}

// PreviewInstance 发布前预览（issue #14 验收 3）：按指定（或默认）模板渲染实体。
//
// 全程只读：不写快照/产物行/指针、不落盘产物、不激活 URL —— 预览看到的就是
// 发布时会产出的字节（与 buildArtifact 共用 renderHTML），但线上与库表状态不变。
// ListArtifactHashes 列出本模块认领的全部产物 hash（IDX-015 反向对账的属主清单）。
//
// 只读且不含内容：反查「磁盘上这个目录是不是我们产出的」只需要这一个答案。
// 与 page 模块的同名方法同义 —— 两个模块共用一个 artifacts 根，
// 漏接任何一方都会让对账把对方的产物误报成孤儿。
func (s *Service) ListArtifactHashes(ctx context.Context) (hashes []string, err error) {
	return s.m.ListArtifactHashes(ctx)
}

// rebuildInstance 实例重建主链：编译发布 → 新快照 → 新产物行 → 指针切换 → 依赖落库。
func (s *Service) rebuildInstance(ctx context.Context, inst *presentationmodel.InstanceEntity,
	tpl *contenttemplatecontract.ResolvedTemplate) (res *presentationdto.InstanceResp, err error) {
	// 实例级互斥：并发重建同一实例时串行化，避免版本号竞态与指针交错。
	lock := s.lockInstance(inst.EntityType, inst.EntityID)
	lock.Lock()
	defer lock.Unlock()

	if _, err = s.publishAllLangs(ctx, inst, tpl, inst.URLPath, nil); err != nil {
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
			if uerr := s.m.UpdateInstanceTemplateTx(tx, inst.ProjectID, inst.ID, tpl.TemplateID, now); uerr != nil {
				return uerr
			}
			inst.TemplateID = tpl.TemplateID
			// 换底稿 = 放弃自定义（docs/04-C）：产物已按新模板编译，覆盖列若不清除，
			// 下次重建又会拿旧自定义文档覆盖新模板 —— 与本次「切换」的语义自相矛盾。
			if len(inst.OverrideDocument) > 0 {
				if cerr := s.m.ClearInstanceOverrideTx(tx, inst.ProjectID, inst.ID, "", now); cerr != nil {
					return cerr
				}
				inst.OverrideDocument = nil
			}
		}
		// 改 URL 与快照/产物/指针同事务：产物烘的是新路径的 canonical，实例
		// 必须同步指向新路径，否则下次重建会拿旧路径重编，线上内容与路由脱节。
		if urlPath != "" && inst.URLPath != urlPath {
			if uerr := s.m.UpdateInstanceURLTx(tx, inst.ProjectID, inst.ID, urlPath, now); uerr != nil {
				return uerr
			}
			inst.URLPath = urlPath
		}
		version, verr := s.m.NextArtifactVersionTx(tx, inst.ID)
		if verr != nil {
			return verr
		}
		defaultLang := pipeline.DefaultLocale(ctx, s.project, inst.ProjectID)
		aid, aerr := s.recordArtifactTx(ctx, tx, inst, snapID, built, defaultLang, version, now)
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
		if uerr := s.m.UpdateInstancePointersTx(tx, inst.ProjectID, inst); uerr != nil {
			return uerr
		}
		return s.persistDependenciesTx(tx, inst.ID, aid, built.Manifest.Dependencies, now)
	})
	return artifactID, err
}

// recordArtifactTx 事务内写产物行（同 hash 幂等复用，避免重建时产物行膨胀）。
func (s *Service) recordArtifactTx(ctx context.Context, tx *gorm.DB, inst *presentationmodel.InstanceEntity,
	snapID string, built builtArtifact, lang string, version int64, now time.Time) (artifactID string, err error) {
	if existing, gerr := s.m.GetArtifactByHashTx(tx, inst.ID, built.Hash); gerr == nil {
		return existing.ID, nil
	} else if !errors.Is(gerr, gorm.ErrRecordNotFound) {
		return "", gerr
	}
	manifestJSON, err := json.Marshal(built.Manifest)
	if err != nil {
		return "", err
	}
	e := &presentationmodel.ArtifactEntity{
		ID: uuid.NewString(), PresentationInstanceID: inst.ID, SnapshotID: snapID,
		Version: version, Lang: strings.TrimSpace(lang), SourceHash: built.Manifest.SourceHash,
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
