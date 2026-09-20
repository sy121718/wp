package presentationservice

// presentation_stale.go — 失效与预览（依赖变更标记、批量重建、只读预览）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"

	"go_wp/internal/builder"
	"go_wp/internal/pipeline"

	"gorm.io/gorm"

	"go_wp/pkg/logger"
)

// MarkStaleByDependency 实现 pipeline.DependencyTarget：按依赖源精确标记。
//
// 为什么逐个工程遍历（DB-009 第二批）：扇出触发的调用方（pipeline.Fanout）只带
// (kind,key)，不带工程；而 presentation_instances 带 FORCE 策略，不设 app.project_id
// 的 UPDATE 会静默匹配 0 行 —— 表现是「内容改了但详情页不再自动重建」，日志里没有异常。
// 工程数量级很小（站点工程），逐个设作用域比在 data 层引入 BYPASSRLS 连接便宜得多。
func (s *Service) MarkStaleByDependency(ctx context.Context, kind, key string) (ids []string, err error) {
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(key) == "" {
		return nil, nil
	}
	if s.project == nil {
		return nil, errors.New(presentationenums.ErrProjectRequired)
	}
	projects, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	at := time.Now().UTC()
	// 模板换代的分流：document 模式只脱离绑定的正文模板，
	// 页眉/页脚结构模板仍参与构建，不能一并豁免。其余依赖源（导航 / 全局块 / 译文）对两种模式都有效，
	// 绝不能分流：漏掉 document 模式会表现为「改了导航但商品页不更新」且无任何报错。
	templateKind := kind == pipeline.DepKindContentTemplate
	seen := make(map[string]bool)
	for _, p := range projects {
		if ctx.Err() != nil {
			break
		}
		var hit []string
		var herr error
		if templateKind {
			hit, herr = s.m.MarkStaleTemplateModeByDependency(ctx, p.ID, kind, key, at)
		} else {
			hit, herr = s.m.MarkStaleByDependency(ctx, p.ID, kind, key, at)
		}
		if herr != nil {
			return nil, herr
		}
		for _, id := range hit {
			if seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// MarkStaleByRegistryVersion 把「**当前产物**由旧组件产出」的自动发布实例标记为待重建。
//
// 这是手工 Page 侧 MarkStaleByRegistryVersion 的对等入口（报告 ARCH-03）：实例的产物行
// 同样保存 registry_version（presentation_persist.go 写的是 builder.RegistryVersion()），
// 但此前没有任何启动期收敛 —— 组件升级后详情页一直是旧组件渲染的字节，
// 后台看不到 stale、日志里也没有任何提示（因为保存版本号本身不会触发任何比对）。
//
// 判据与 page 侧逐字一致：先由本模块从**语言账本**（presentation_publications 里
// active 指向的行，见 model.ListCurrentArtifactIDs）选出各语言当前产物，再按版本比对；
// 历史产物行不参与判定。
//
// 只标记、不重建（与 page 侧同一取舍）：启动时全量重建会拖住启动链。重建由运维经
// RebuildStale 触发，或由后续的 Rebuild/依赖失效自然覆盖 —— 那两条路径都按 stale 收敛。
//
// current 为空（二进制无 VCS 信息等）时不做任何标记；没有任何账本行时同样直接返回。
// 逐工程扇出（DB-009 第二批）：presentation_instances 带 FORCE 策略，无作用域的 UPDATE
// 在换非超级角色后静默 0 行 —— 「标了」与「没标」在日志上会一模一样。
func (s *Service) MarkStaleByRegistryVersion(ctx context.Context, current string) (ids []string, err error) {
	if strings.TrimSpace(current) == "" {
		return nil, nil
	}
	if s.project == nil {
		return nil, errors.New(presentationenums.ErrProjectRequired)
	}
	currentArtifactIDs, err := s.m.ListCurrentArtifactIDs(ctx)
	if err != nil {
		return nil, err
	}
	stale, err := s.m.ListStaleInstanceIDsByRegistryVersion(ctx, current, currentArtifactIDs)
	if err != nil {
		return nil, err
	}
	if len(stale) == 0 {
		return nil, nil
	}
	projects, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	at := time.Now().UTC()
	seen := make(map[string]bool, len(stale))
	for _, p := range projects {
		if ctx.Err() != nil {
			break
		}
		// 每个工程只提交本工程的 id：实例 id 全局唯一，但本工程之外的 id 会被
		// project_id 条件与策略双重拦下（与 page 侧同一扇出手法）。
		hit, merr := s.m.MarkStaleByIDs(ctx, p.ID, stale, at)
		if merr != nil {
			return nil, merr
		}
		for _, id := range hit {
			if seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	logger.Scene("presentation").With("count", len(ids)).With("registryVersion", current).
		With("sample", sampleInstanceIDs(ids)).
		Info("组件注册表版本变化：相关自动发布实例已标记待重建")
	return ids, nil
}

// registryImpactSampleLimit 影响面日志里最多列出的实例 id 数。
//
// 与 page 侧影响面样本同一取舍：组件一换往往是一批实例一起过期，只报条数时运维无法
// 判断「是不是我关心的那个商品详情页」，全列出来又会把日志撑爆 —— 取前 K 个可定位的 id。
const registryImpactSampleLimit = 10

// sampleInstanceIDs 取影响面样本（不超过 registryImpactSampleLimit 个）。
func sampleInstanceIDs(ids []string) []string {
	if len(ids) <= registryImpactSampleLimit {
		return ids
	}
	return ids[:registryImpactSampleLimit]
}

// SetBuildQueue 注入自动重建入队端口（装配期调用；PERF-020）。
//
// 注入后 RebuildStale 改为全量入队：重建由构建队列的消费 worker 执行
// （FOR UPDATE SKIP LOCKED claim 保证多实例部署下同一实例不会被两个 worker 同时重建），
// 触发进程不再在请求路径上持实例锁串行重建。未注入时回退原同步重建行为
// （单实例部署 / 既有测试不受影响）。
func (s *Service) SetBuildQueue(q presentationcontract.BuildQueueEnqueuer) {
	if s == nil {
		return
	}
	s.buildQueue = q
}

// RebuildStale 实现 pipeline.StaleRebuilder：重建受影响实例并重新发布。
//
// 策略（§8.3）：presentation 实例的语义就是「内容驱动的自动发布页面」，
// 创建即上线，因此重建成功后直接回写线上（与 page 侧「仅已发布语言自动回写」
// 的口径在结果上一致——presentation 不存在「从未发布」的实例）。
//
// 队列已接入时（PERF-020）：全部实例入队后立即返回，重建由队列消费侧执行；
// 单条入队失败只记日志（该实例保持 stale，由下次触发兜底）。
// 未接入时回退同步重建：单个实例失败不阻断其余（记日志后继续），返回 nil 由 stale 标记兜底。
func (s *Service) RebuildStale(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if s.buildQueue != nil {
		return s.enqueueStaleRebuilds(ctx, ids)
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
		if err := s.RebuildInstance(ctx, id); err != nil {
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

// enqueueStaleRebuilds 把受影响实例全部交给构建队列（PERF-020）。
//
// 入队是幂等的：队列侧部分唯一索引保证同一实例同时只有一条待办，扇出反复标记
// 同一实例不会堆出多份任务。整批入队是轻量操作，因此不再有单次上限截断——
// 截断过的实例若「下次触发」不来就永远停在 stale。
func (s *Service) enqueueStaleRebuilds(ctx context.Context, ids []string) error {
	queued := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		if err := s.buildQueue.EnqueuePresentationBuild(ctx, id, s.projectOfInstance(ctx, id)); err != nil {
			logger.Scene("dependency").With("presentation_id", id).
				Error(err, "自动重建任务入队失败（实例保持 stale）")
			continue
		}
		queued++
	}
	logger.Scene("dependency").With("queued", queued).With("affected", len(ids)).
		Info("依赖失效的自动重建已交给构建队列")
	return nil
}

// RebuildInstance 按实例 id 重建（构建队列 executor 的执行体；PERF-020）。
//
// 用实例**绑定**的模板重建（issue #14）：依赖失效是内容变更触发的自动重建，
// 不应改变「这个商品用哪套详情模板」——按类型重解析会把切换过的模板悄悄换回去。
func (s *Service) RebuildInstance(ctx context.Context, instanceID string) error {
	inst, err := s.findOneInstanceAnyProject(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("%s: %w", presentationenums.ErrNotFound, err)
	}
	tpl, err := s.resolveBoundTemplate(ctx, inst, "")
	if err != nil {
		return err
	}
	// 依赖失效是内容变更触发的自动重建：实例带覆盖文档时沿用（docs/04-C），
	// 否则一次实体数据更新就会把可视化自定义静默冲回模板文档。
	tpl = instanceDocumentFor(inst, tpl)
	_, err = s.rebuildInstance(ctx, inst, tpl)
	return err
}

// projectOfInstance 解析实例所属工程，供自动重建入队时填显式的工程作用域（审计 DB-01）。
//
// 为什么要在这里多查一次：工程 id 只存在于来源模块，队列不反查来源表（跨模块表访问），
// 而 RebuildStale 的入参是跨工程去重后的 id 列表，调用点没有工程上下文。
// 复用消费侧同一个「逐工程定位」实现 —— 非超级连接角色下，未设 app.project_id 的按 id 直查
// 会被 RLS 静默挡成 0 行，那是本模块唯一可靠的读法。
// 解析不到（实例已删除 / 工程契约未注入）不阻断入队：工程列可空，执行侧仍会自己定位。
func (s *Service) projectOfInstance(ctx context.Context, instanceID string) string {
	inst, err := s.findOneInstanceAnyProject(ctx, instanceID)
	if err != nil {
		return ""
	}
	return inst.ProjectID
}

// findOneInstanceAnyProject 在各工程作用域内逐个按实例 id 定位（DB-009 第二批）。
//
// 为什么需要它：构建队列的消费侧只拿得到实例 id（契约是 EnqueuePresentationBuild
// 一个 id），而 presentation_instances 带 FORCE 策略 —— 没有 app.project_id 的
// 「按 id 直查」在非超级角色下会 0 行，自动重建再也跑不起来。
// 工程数量级很小（站点工程），逐个设作用域查询比给队列协议加工程字段便宜，
// 也比在数据层专门养一条 BYPASSRLS 连接安全（后者等于把隔离关掉）。
func (s *Service) findOneInstanceAnyProject(ctx context.Context, instanceID string) (*presentationmodel.InstanceEntity, error) {
	if ids := strings.TrimSpace(instanceID); ids == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	if s.project == nil {
		return nil, errors.New(presentationenums.ErrProjectRequired)
	}
	projects, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		inst, gerr := s.m.GetInstance(ctx, p.ID, instanceID)
		if gerr == nil {
			return inst, nil
		}
		if !errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, gerr
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (s *Service) PreviewInstance(ctx context.Context, req *presentationdto.PreviewInstanceReq) (res *presentationdto.PreviewInstanceResp, err error) {
	if req == nil || req.EntityType == "" || req.EntityID == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	tpl, err := s.resolveTemplate(ctx, projectID, req.EntityType, req.TemplateID)
	if err != nil {
		return nil, err
	}
	if len(req.DraftDocument) > 0 {
		if !json.Valid(req.DraftDocument) {
			return nil, errors.New(presentationenums.ErrInvalidParam)
		}
		override := *tpl
		override.Document = req.DraftDocument
		tpl = &override
	}
	// urlPath 传空：预览不激活 URL，canonical 由模板 settings.seo 决定（通常为空）。
	// 这是预览与发布在字节上的唯一有意差异（见 presentation_seo.go 取舍 2）。
	// targetLangs 传 nil：预览没有批次概念，语言切换器按线上访问面现状输出；而且
	// urlPath 为空时 logicalPath 也是空，alternates 分支本来就不会走（SEO-026）。
	// usage 传 nil：预览不落依赖表，收集编译期消费线索没有写入点。
	// 模式为预览（审计 ARCH-05）：绑定的结构模板 / 块拿不到时**不失败**（编辑期配置
	// 不完整是常态），降级为带归因的占位；发布路径传 CompileModePublish，那里是硬失败。
	// diags 传 nil 同上：预览不产出 Manifest，归因在占位上。
	html, err := s.renderHTML(ctx, req.EntityType, req.EntityID, "", projectID, "", nil, tpl, nil,
		builder.CompileModePreview, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	return &presentationdto.PreviewInstanceResp{
		HTML: string(html), EntityType: req.EntityType, EntityID: req.EntityID,
		TemplateID: tpl.TemplateID, TemplateName: tpl.TemplateName,
		TemplateVersionID: tpl.VersionID, TemplateVersion: tpl.Version,
	}, nil
}
