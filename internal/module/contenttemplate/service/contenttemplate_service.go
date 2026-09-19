// Package contenttemplateservice contenttemplate 模块业务实现（0-A2）。
package contenttemplateservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
)

// systemCreator 版本行 created_by 的占位（NOT NULL uuid 列）。
//
// 与 artifact 模块 defaultCreator 同一口径：uuid 列不接受空串，
// 无登录上下文的自动写入统一落零值 UUID（表示「系统写入」）。
const systemCreator = "00000000-0000-0000-0000-000000000000"

// Service contenttemplate 模块业务实现。
type Service struct {
	m        *contenttemplatemodel.Model
	project  projectcontract.ProjectService
	registry core.EntitySourceRegistry
	// invalidator 依赖失效扇出端口（编排层注入，可空）。
	//
	// 沿用 content 模块的同道范式：失效是写入的**后置副作用**，失败只记日志，
	// 不能反向让已经成功的模板保存报错。
	invalidator DependencyInvalidator
	// impact 引用反查端口（装配层注入，见 contenttemplate_impact.go；可空）。
	//
	// 影响面提示与删除保护共用它 —— 页面文档的 settings.structure 绑定写在 JSONB 里，
	// 本模块的表看不见，靠它把「谁在引用」拿回来。
	impact contenttemplatecontract.TemplateImpactPort
}

// NewService 构造（model + project 契约 + 实体类型注册表注入，不持有 *gorm.DB）。
// project 用于解析模板所属工程（content_templates.project_id 为 NOT NULL 外键）；
// registry 提供「实体类型是否合法」的判据（取代对内容模块的直接依赖）。
func NewService(m *contenttemplatemodel.Model, project projectcontract.ProjectService,
	registry core.EntitySourceRegistry) *Service {
	return &Service{m: m, project: project, registry: registry}
}

// DependencyInvalidator 依赖失效端口（消费者侧最窄接口，与 content 模块同一范式）。
type DependencyInvalidator interface {
	Invalidate(ctx context.Context, kind, key string)
}

// SetInvalidator 注入依赖失效端口（装配期调用；未注入时模板改动只落库、不触发重建）。
func (s *Service) SetInvalidator(inv DependencyInvalidator) { s.invalidator = inv }

// notifyTemplateChanged 模板产生新版本 / 切换生效后的失效扇出。
//
// 缺这条的现象：改了模板（页眉 / 详情结构），引用它的页面与实例**永远停在旧字节**，
// 日志里什么都没有 —— 这正是本批要修的那类静默失效。
func (s *Service) notifyTemplateChanged(ctx context.Context, templateID string) {
	if s == nil || s.invalidator == nil || strings.TrimSpace(templateID) == "" {
		return
	}
	k := pipeline.ContentTemplateKey(templateID)
	s.invalidator.Invalidate(ctx, k.Kind, k.Key)
}

// validEntityType 实体类型是否合法（注册表为 nil 时视为不合法，避免静默放行）。
//
// 结构模板类型（页眉 / 页脚）走独立白名单：它们不是内容实体、没有字段来源，
// 往注册表里塞假来源会给出"可以配字段绑定"的假许可。
func (s *Service) validEntityType(entityType string) bool {
	if contenttemplatemodel.IsStructureTemplateType(entityType) {
		return true
	}
	return s.registry != nil && s.registry.IsValidType(entityType)
}

// 编译期契约断言。
var _ contenttemplatecontract.ContentTemplateService = (*Service)(nil)

// ListBlockSourceRefs 列出文档树引用了该块的内容模板（审计 ARCH-02）。
//
// 逐工程扇出（DB-009 第三批）：块 id 说不出工程，而 content_templates 带 FORCE 策略 ——
// 漏作用域时这条查询静默返回空，删除保护会据此放行。
//
// 历史版本按 (模板, 版本) 各记一条：Detail 写出版本号，提示里才说得清「是哪一版还在引用」。
func (s *Service) ListBlockSourceRefs(ctx context.Context, blockID string) (out []blockcontract.BlockUsage, err error) {
	ids, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	for _, projectID := range ids {
		if ctx.Err() != nil {
			break
		}
		rows, rerr := s.m.ListBlockDocumentRefs(ctx, projectID, blockID)
		if rerr != nil {
			return nil, rerr
		}
		for i := range rows {
			detail := ""
			if rows[i].FromVersion {
				detail = fmt.Sprintf("version %d", rows[i].Version)
			}
			out = append(out, blockcontract.BlockUsage{
				Kind: blockcontract.UsageKindContentTemplate, ProjectID: projectID,
				EntityID: rows[i].ID, Label: rows[i].Name, Detail: detail,
			})
		}
	}
	return out, nil
}

// fanoutProjectIDs 逐工程扇出用的工程清单（DB-009 第三批）。
//
// content_templates 带 FORCE 策略，作用域必须是一个具体 uuid；而一批契约入口的签名里
// 没有工程参数（dashboard 编译期依赖该接口）。「全站」或「按 id 找归属」只能由本层
// 枚举工程表后逐工程各设一次作用域完成 —— 绝不退回「不限工程」（换非超级角色后那是
// 静默 0 行，表现为「模板不存在」）。
func (s *Service) fanoutProjectIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.project == nil {
		return nil, errors.New(contenttemplateenums.ErrProjectRequired)
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for i := range list {
		if id := strings.TrimSpace(list[i].ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New(contenttemplateenums.ErrProjectRequired)
	}
	return ids, nil
}

// locateTemplate 按模板 id 定位模板（跨工程）：逐工程独立作用域探测，命中即返回。
//
// 为什么可以逐工程探测：content_templates.id 是主键，跨工程不会重复命中，所以结果确定。
// 为什么必须探测而不能直查：不设 app.project_id 的按 id 查询在非超级角色下静默
// ErrRecordNotFound —— 模板还在，接口却说它不存在。全部未命中返回 gorm.ErrRecordNotFound。
func (s *Service) locateTemplate(ctx context.Context, id string) (*contenttemplatemodel.TemplateEntity, error) {
	ids, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	var lastErr error = gorm.ErrRecordNotFound
	for _, projectID := range ids {
		if ctx.Err() != nil {
			break
		}
		e, gerr := s.m.Get(ctx, projectID, id)
		if gerr == nil {
			return e, nil
		}
		lastErr = gerr
		if !errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, gerr
		}
	}
	return nil, lastErr
}

// Create 创建模板（初始 draft_version=1 并写入 version=1 快照）。
func (s *Service) Create(ctx context.Context, req *contenttemplatedto.CreateReq) (res *contenttemplatedto.TemplateResp, err error) {
	if req == nil || !s.validEntityType(req.EntityType) || req.Name == "" {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if !json.Valid(req.DraftDocument) {
		return nil, errors.New(contenttemplateenums.ErrDataInvalid)
	}
	// 合入激活主题快照（EDT-003：与手工 Page 保存同口径）。
	merged, err := pipeline.MergeActiveThemeIntoDocument(ctx, s.project, projectID, req.DraftDocument)
	if err != nil {
		return nil, err
	}
	// 校验 DraftDocument 是合法 Page Document 并规范化为存储字节（含字段绑定的
	// 数据源白名单校验：越界绑定在保存时即拒绝，不等发布才炸）。
	doc, err := s.validateDocument(req.EntityType, merged)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	role := strings.TrimSpace(req.TemplateRole)
	if role == "" {
		role = contenttemplatemodel.TemplateRoleDetail
	}
	if !contenttemplatemodel.IsValidTemplateRole(role) {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	e := &contenttemplatemodel.TemplateEntity{
		ID: uuid.NewString(), ProjectID: projectID, Name: req.Name, EntityType: req.EntityType,
		TemplateRole:  role,
		DraftDocument: doc, DraftVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	// 模板行 + 首个不可变版本 + 当前版本指针回填，三者原子写入。
	// 分步写会在中途失败时留下「模板存在但无 LatestVersion」的死模板。
	verID := uuid.NewString()
	e.CurrentVersionID = &verID
	if err = s.m.CreateWithVersion(ctx, e, &contenttemplatemodel.VersionEntity{
		ID: verID, TemplateID: e.ID, Version: 1, Document: doc,
		SourceHash: hashDocument(doc), CreatedBy: systemCreator, CreatedAt: now,
	}); err != nil {
		return nil, err
	}
	return toResp(e), nil
}

// Update 更新模板（draft_version 递增并写新不可变版本）。
func (s *Service) Update(ctx context.Context, req *contenttemplatedto.UpdateReq) (res *contenttemplatedto.TemplateResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	// 工程作用域（DB-009 第二批起）：content_templates 带 FORCE 策略，按 id 取模板必须
	// 带上工程。第三批把这里的「取唯一工程」换成逐工程定位 —— 多工程部署下不再是
	// 「需要显式指定工程」，也不会退化成「不限工程」（那在换非超级角色后是静默不存在）。
	e, err := s.locateTemplate(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	if !json.Valid(req.DraftDocument) {
		return nil, errors.New(contenttemplateenums.ErrDataInvalid)
	}
	merged, err := pipeline.MergeActiveThemeIntoDocument(ctx, s.project, e.ProjectID, req.DraftDocument)
	if err != nil {
		return nil, err
	}
	doc, err := s.validateDocument(e.EntityType, merged)
	if err != nil {
		return nil, err
	}
	e.DraftDocument = doc
	e.DraftVersion++ // 单调递增（版本号即不可变快照序号）
	e.UpdatedAt = time.Now().UTC()
	verID := uuid.NewString()
	e.CurrentVersionID = &verID
	// 新版本行与草稿/指针更新必须原子：分步写时若 Save 失败，指针停在旧版本，
	// 新版本成为不可达孤儿，且 draft_version 已自增导致重试版本号错位。
	if err = s.m.SaveWithVersion(ctx, e.ProjectID, &contenttemplatemodel.VersionEntity{
		ID: verID, TemplateID: e.ID, Version: e.DraftVersion, Document: doc,
		SourceHash: hashDocument(doc), CreatedBy: systemCreator, CreatedAt: e.UpdatedAt,
	}, e); err != nil {
		return nil, err
	}
	// 模板产生新版本 = 引用它的产物过期（页面 / 自动发布实例）。
	s.notifyTemplateChanged(ctx, e.ID)
	return toResp(e), nil
}

// Activate 切换该（工程, 类型）的生效模板（多套存着、单套生效）。
//
// 生效的那套换了，引用它的产物同样过期，故与 Update 一样走一次失效扇出：
// 旧的那套的依赖键与新的一致（键是"这个类型的生效模板"这条绑定关系所在的页面登记了
// 两份键），所以标记覆盖得到。
func (s *Service) Activate(ctx context.Context, req *contenttemplatedto.ActivateReq) (res *contenttemplatedto.TemplateResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	e, err := s.locateTemplate(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	now := time.Now().UTC()
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		return s.m.SetDefaultTx(tx, e.ProjectID, e.EntityType, e.ID, now)
	}); err != nil {
		return nil, err
	}
	e.IsDefault = true
	e.UpdatedAt = now
	s.notifyTemplateChanged(ctx, e.ID)
	return toResp(e), nil
}

// Delete 删除模板（连带它的全部历史版本与内容模板级组件版本锁定行）。
//
// 逐工程定位模板（与 Update 同口径）：入口只带 id，而 content_templates 带 FORCE 策略，
// 不设作用域的按 id 操作在非超级角色下静默 0 行（表现为「模板不存在」）。
//
// 被自动发布实例引用的模板：presentation_instances.template_id 的外键会拒绝删除。
// 那是「不许删」这个业务语义本身，不是故障 —— 换成可判定的 ErrTemplateInUse，
// 既不把 SQL 原文透给调用方，也不顺手把实例一起删掉。
func (s *Service) Delete(ctx context.Context, req *contenttemplatedto.DeleteReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return errors.New(contenttemplateenums.ErrInvalidParam)
	}
	e, err := s.locateTemplate(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(contenttemplateenums.ErrNotFound)
		}
		return err
	}
	// 结构模板（页眉 / 页脚）的引用在 **JSONB 文档**里（settings.structure），数据库管不到：
	// 删掉之后绑定静默丢失，站点的页眉在某次重建后直接没了，而删除操作本身一路成功。
	// 因此这里显式拦截，并把引用者（模板名 + 槽位）写进错误 —— 打回给人，不静默删。
	if contenttemplatemodel.IsStructureTemplateType(e.EntityType) {
		refs, rerr := s.structureTemplateRefs(ctx, e.ProjectID, e.ID)
		if rerr != nil {
			// fail-closed：查不出引用就不删（宁可不删，也不静默丢绑定）。
			logger.Scene("contenttemplate").With("templateId", e.ID).Error(rerr, "结构模板引用检查失败，已拒绝删除")
			return rerr
		}
		if len(refs) > 0 {
			logger.Scene("contenttemplate").With("templateId", e.ID).With("refs", strings.Join(refs, "；")).
				Warn("结构模板删除被拒绝：仍被其它模板绑定为结构槽位")
			// 冒号前是可翻译 key（handler 的三件套按 key 取文案），冒号后是可定位数据。
			return fmt.Errorf("%s: %s", contenttemplateenums.ErrStructureTemplateInUse, strings.Join(refs, "；"))
		}
	}
	// 页面与自动发布实例的引用：页面文档的 settings.structure 绑定写在 JSONB 里，
	// 数据库外键管不到；实例那条虽有外键兜底，但外键只说得清「被引用」，说不出
	// 「是哪个实体、线上路径在哪」—— 一次问全，删除被拒时才能给出可定位的数据。
	//
	// 与上面的结构模板分支并列而不是合并：两者的**处置方式不同**
	//（去那个模板里解绑 vs 去那张页面 / 那个实例上解绑），错误 key 也不同。
	bindingRefs, berr := s.referencesOfTemplate(ctx, e.ProjectID, e.ID)
	if berr != nil {
		// fail-closed：查不出引用就不删（让运营看见「查不出来」，好过静默丢绑定）。
		logger.Scene("contenttemplate").With("templateId", e.ID).Error(berr, "模板引用检查失败，已拒绝删除")
		return berr
	}
	if len(bindingRefs) > 0 {
		logger.Scene("contenttemplate").With("templateId", e.ID).With("refs", strings.Join(bindingRefs, "；")).
			Warn("模板删除被拒绝：仍被页面或实例引用")
		return fmt.Errorf("%s: %s", contenttemplateenums.ErrTemplateInUse, strings.Join(bindingRefs, "；"))
	}
	if err = s.m.DeleteWithHistory(ctx, e.ProjectID, e.ID); err != nil {
		if isForeignKeyViolation(err) {
			return errors.New(contenttemplateenums.ErrTemplateInUse)
		}
		logger.Scene("contenttemplate").With("templateId", e.ID).Error(err, "删除内容模板失败")
		return err
	}
	return nil
}

// structureTemplateRefs 返回把 templateID 绑成结构槽位的**其它内容模板**（可定位描述）。
//
// 形状是给人看的（模板名 + 槽位），用于删除拦截的错误文案与日志：
// 「这套页眉被《商品详情模板》用作用页眉」比「ErrStructureTemplateInUse」可行动得多。
func (s *Service) structureTemplateRefs(ctx context.Context, projectID, templateID string) ([]string, error) {
	rows, err := s.m.ListStructureBindingRows(ctx, projectID, templateID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, row := range rows {
		var doc struct {
			Settings struct {
				Structure builder.StructureBindings `json:"structure"`
			} `json:"settings"`
		}
		if err := json.Unmarshal(row.DraftDocument, &doc); err != nil {
			// 解析不了就说「引用了」而不是跳过：跳过等于放行一次可能丢失绑定的删除。
			out = append(out, fmt.Sprintf("模板《%s》（文档无法解析，无法判定槽位）", row.Name))
			continue
		}
		var slots []string
		for slot, id := range doc.Settings.Structure.TemplateBindings() {
			if id == templateID {
				slots = append(slots, structureSlotLabel(slot))
			}
		}
		if len(slots) == 0 {
			// SQL 粗筛命中但解析不出槽位（例如绑定写在历史版本里）：仍然拦下来，
			// 因为「命中却说没引用」正是那种会静默丢绑定的判断。
			slots = append(slots, "结构绑定")
		}
		sort.Strings(slots)
		out = append(out, fmt.Sprintf("模板《%s》的%s", row.Name, strings.Join(slots, " / ")))
	}
	sort.Strings(out)
	return out, nil
}

// structureSlotLabel 槽位名 → 面向运营的说法。
func structureSlotLabel(slot string) string {
	switch slot {
	case builder.SlotHeader:
		return "页眉"
	case builder.SlotFooter:
		return "页脚"
	default:
		return "结构槽位 " + slot
	}
}

// isForeignKeyViolation 错误链里是否含 PostgreSQL 外键冲突（SQLSTATE 23503）。
//
// 用 SQLState() 接口判定而不是 import 具体 driver 包：contenttemplate 不该为了一个错误码
// 绑定数据库驱动的实现细节（pgx 属于 pkg/database）。gorm 的 TranslateError 开不开由部署
// 决定，这里只认错误链自身携带的 SQLSTATE —— 翻译与否都成立。
func isForeignKeyViolation(err error) bool {
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		return state.SQLState() == "23503"
	}
	return false
}

// Get 按 ID 查询。
//
// 逐工程定位（DB-009 第三批）：入口只带 id，而 content_templates 带 FORCE 策略。
// 已经持有工程 id 的调用方走 GetScoped —— 少一次跨工程探测，语义也更直白。
func (s *Service) Get(ctx context.Context, req *contenttemplatedto.GetReq) (res *contenttemplatedto.TemplateResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	e, err := s.locateTemplate(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	return toResp(e), nil
}

// GetScoped 在**显式工程作用域**内按 id 取模板（DB-009 第二批）。
//
// 给「手里已经有工程 id」的调用方用（如 presentation 构建链路）：不传工程时
// service 只能靠 resolveProjectID 取唯一工程，多工程下必须报参数错误 ——
// 与其让调用方撞上「需要显式指定工程」，不如在这里把 id 直接透下去。
func (s *Service) GetScoped(ctx context.Context, projectID, id string) (res *contenttemplatedto.TemplateResp, err error) {
	e, err := s.m.Get(ctx, projectID, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	return toResp(e), nil
}

// List 按类型列表。
//
// 多工程部署下必须由调用方给出工程（DB-009 第三批）：这里**不**逐工程扇出合并 ——
// 「列出模板」的结果是给后台管理页看的，把多个工程的模板并在一个列表里等于取消隔离
// （而且 updatedAt 排序会在工程之间交错，用户无法分辨哪些是自己的）。
// 现有调用方若撞上 ErrProjectRequired，补参数的落点是：本方法的 req 加 ProjectID
// （或改调 ResolveTemplateByRoleScoped 那组显式作用域入口）。
func (s *Service) List(ctx context.Context, req *contenttemplatedto.ListReq) (list []*contenttemplatedto.TemplateResp, err error) {
	if req == nil {
		req = &contenttemplatedto.ListReq{}
	}
	if req.EntityType != "" && !s.validEntityType(req.EntityType) {
		return nil, errors.New(contenttemplateenums.ErrInvalidType)
	}
	// 工程作用域优先用请求里显式给的那一个（后台页面手里就有），缺省才回落「唯一工程」。
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.List(ctx, projectID, req.EntityType)
	if err != nil {
		return nil, err
	}
	out := make([]*contenttemplatedto.TemplateResp, 0, len(rows))
	for _, r := range rows {
		out = append(out, toResp(r))
	}
	return out, nil
}

// ResolveTemplate 取 entityType 的当前激活模板版本（presentation 派生
// DocumentSnapshot 的唯一入口）。逻辑：
//  1. 优先取 is_default=true 的模板；无默认时回落 update_time 最新一条并记 warn（EDT-014）；
//  2. 取该模板最新版本（LatestVersion）的 document；
//  3. 组装 ResolvedTemplate{TemplateID, VersionID, Version, EntityType, Document}。
//
// 多工程部署下必须由调用方给出工程（DB-009 第三批）：**不**逐工程扇出 ——
// 「该类型的当前模板」只可能属于一个工程，扇出会得到多份互不一致的结果，
// 而调用方（构建链）没法从中挑一份。已有工程 id 的调用方走 ResolveTemplateScoped。
func (s *Service) ResolveTemplate(ctx context.Context, entityType string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	projectID, err := s.resolveProjectID(ctx, "")
	if err != nil {
		return nil, err
	}
	return s.ResolveTemplateScoped(ctx, projectID, entityType)
}

// ResolveTemplateScoped 在显式工程作用域内解析该类型的当前模板版本。
//
// 与 ResolveTemplate 的分工：后者没有工程参数，只能取「唯一工程」；
// 构建链路（presentation）手里本来就有工程 id，走这条不会在多工程部署下
// 撞上「需要显式指定工程」——而模板解析失败会让整次构建失败。
func (s *Service) ResolveTemplateScoped(ctx context.Context, projectID, entityType string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	if entityType == "" || !s.validEntityType(entityType) {
		return nil, errors.New(contenttemplateenums.ErrInvalidType)
	}
	// 只取详情角色（审计 EDT-004）：归档模板与详情模板可以同类型共存，
	// 不过滤就会把归档模板当成详情模板取用。
	return s.ResolveTemplateByRoleScoped(ctx, projectID, entityType, contenttemplatemodel.TemplateRoleDetail)
}

// ResolveTemplateByRole 按实体类型与角色解析模板（审计 EDT-004）。
//
// 归档型实例（分类页 / 标签页 / 品牌页）走这个入口取归档模板；没有配置时返回
// ErrNotFound，由调用方决定是「跳过」还是「报错」——不在这里替调用方做决定。
//
// 多工程部署下必须由调用方给出工程（DB-009 第三批）：同 ResolveTemplate，
// 扇出会得到多份结果而无法挑一份，所以这里保持显式失败，不做「不限工程」的兜底；
// 已持有工程 id 的调用方走 ResolveTemplateByRoleScoped。
func (s *Service) ResolveTemplateByRole(ctx context.Context, entityType, role string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	projectID, err := s.resolveProjectID(ctx, "")
	if err != nil {
		return nil, err
	}
	return s.ResolveTemplateByRoleScoped(ctx, projectID, entityType, role)
}

// ResolveTemplateByRoleScoped 在显式工程作用域内按实体类型与角色解析模板。
func (s *Service) ResolveTemplateByRoleScoped(ctx context.Context, projectID, entityType, role string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	if entityType == "" || !s.validEntityType(entityType) {
		return nil, errors.New(contenttemplateenums.ErrInvalidType)
	}
	if role != "" && !contenttemplatemodel.IsValidTemplateRole(role) {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	rows, err := s.m.ListByRole(ctx, projectID, entityType, role)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New(contenttemplateenums.ErrNotFound)
	}
	tpl := rows[0]
	if !tpl.IsDefault {
		logger.Scene("contenttemplate").With("entityType", entityType).With("templateId", tpl.ID).
			Warn("未标记默认模板，回落到最新更新的模板")
	}
	ver, err := s.m.LatestVersion(ctx, tpl.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	return s.resolvedTemplateFromVersion(tpl, ver)
}

// ResolveTemplateByID 按模板 ID 解析其当前版本（issue #14：同一实体类型下可建多套
// 命名模板，发布/预览按 ID 显式指定用哪一套）。
//
// 与 ResolveTemplate 共享同一条版本解析口径（都取该模板的 LatestVersion）：
// 「模板」与「模板版本」是两层——换一套模板是换 TemplateID，
// 同一套模板改版式则产生新版本，两条路径都不需要调用方区分。
func (s *Service) ResolveTemplateByID(ctx context.Context, templateID string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	if strings.TrimSpace(templateID) == "" {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	// 逐工程定位（DB-009 第三批）：模板 id 是主键，逐工程探测的结果唯一；
	// 已持有工程 id 的调用方（presentation 构建链）应走 ResolveTemplateByIDScoped。
	tpl, err := s.locateTemplate(ctx, templateID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	ver, err := s.m.LatestVersion(ctx, tpl.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	return s.resolvedTemplateFromVersion(tpl, ver)
}

// ResolveTemplateByIDScoped 在显式工程作用域内按模板 ID 解析其当前版本。
func (s *Service) ResolveTemplateByIDScoped(ctx context.Context, projectID, templateID string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	if strings.TrimSpace(templateID) == "" {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	tpl, err := s.m.Get(ctx, projectID, templateID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	ver, err := s.m.LatestVersion(ctx, tpl.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	return s.resolvedTemplateFromVersion(tpl, ver)
}

// resolvedTemplateFromVersion 组装 ResolvedTemplate；发布取用前走严格校验（EDT-013）。
func (s *Service) resolvedTemplateFromVersion(tpl *contenttemplatemodel.TemplateEntity, ver *contenttemplatemodel.VersionEntity) (*contenttemplatecontract.ResolvedTemplate, error) {
	doc, err := s.validateDocumentStrict(tpl.EntityType, ver.Document)
	if err != nil {
		return nil, err
	}
	return &contenttemplatecontract.ResolvedTemplate{
		TemplateID:   tpl.ID,
		TemplateName: tpl.Name,
		VersionID:    ver.ID,
		Version:      ver.Version,
		EntityType:   tpl.EntityType,
		Document:     doc,
	}, nil
}

// resolveProjectID 解析模板所属工程：显式传入优先（校验存在），
// 否则经 project 契约取唯一工程；无工程或多工程时要求显式指定。
//
// DB-009 第三批的边界：只用于**必须落在单一工程**的入口（Create 的落库工程、
// List / ResolveTemplate / ResolveTemplateByRole 的「哪个工程的模板」）——
// 这些入口扇出会得到互相冲突的多份结果，所以多工程下显式报 ErrProjectRequired，
// 而不是退到「不限工程」。按 id 定位的入口（Get / Update / ResolveTemplateByID）
// 已改为逐工程探测（locateTemplate），不再依赖「工程唯一」这个前提。
func (s *Service) resolveProjectID(ctx context.Context, explicit string) (string, error) {
	if id := strings.TrimSpace(explicit); id != "" {
		if s.project != nil {
			ok, err := s.project.Exists(ctx, id)
			if err != nil {
				return "", err
			}
			if !ok {
				return "", errors.New(contenttemplateenums.ErrProjectNotFound)
			}
		}
		return id, nil
	}
	if s.project == nil {
		return "", errors.New(contenttemplateenums.ErrProjectRequired)
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return "", err
	}
	if len(list) != 1 {
		return "", errors.New(contenttemplateenums.ErrProjectRequired)
	}
	return list[0].ID, nil
}

// hashDocument 版本文档内容哈希（content_template_versions.source_hash）。
func hashDocument(doc []byte) string {
	sum := sha256.Sum256(doc)
	return hex.EncodeToString(sum[:])
}

// validateDocument 草稿保存：ValidatePageTolerant + 字段绑定白名单（EDT-013）。
func (s *Service) validateDocument(entityType string, raw json.RawMessage) (json.RawMessage, error) {
	return s.validateDocumentMode(entityType, raw, true)
}

// validateDocumentStrict 发布/解析口径：完整 ValidatePage，非法模板在取用前拒绝。
func (s *Service) validateDocumentStrict(entityType string, raw json.RawMessage) (json.RawMessage, error) {
	return s.validateDocumentMode(entityType, raw, false)
}

func (s *Service) validateDocumentMode(entityType string, raw json.RawMessage, tolerant bool) (json.RawMessage, error) {
	page, err := builder.ParsePage(raw)
	if err != nil {
		return nil, errors.New(contenttemplateenums.ErrDataInvalid)
	}
	if tolerant {
		if _, err = builder.ValidatePageTolerant(page); err != nil {
			return nil, errors.New(contenttemplateenums.ErrDataInvalid)
		}
	} else if err = builder.ValidatePage(page); err != nil {
		return nil, errors.New(contenttemplateenums.ErrDataInvalid)
	}
	if contenttemplatemodel.IsStructureTemplateType(entityType) {
		// 结构模板（页眉 / 页脚）不是内容实体：字段绑定在构建期会按**引用它的那个页面**
		// 的实体上下文解析 —— 同一份页眉在商品页显示商品名、在文章页显示标题，
		// 那不是"全局结构"应有的行为。故这里**明确拒绝**，而不是跳过校验
		//（跳过要等线上才看得出页眉串了数据）。
		refs, rerr := builder.CollectFieldRefs(page)
		if rerr != nil {
			return nil, errors.New(contenttemplateenums.ErrDataInvalid)
		}
		if len(refs) > 0 {
			return nil, errors.New(contenttemplateenums.ErrFieldBindingInvalid)
		}
	} else if err = builder.ValidateFieldRefs(page, entityType, s.registry); err != nil {
		return nil, fmt.Errorf("%s: %w", contenttemplateenums.ErrFieldBindingInvalid, err)
	}
	doc, err := json.Marshal(page)
	if err != nil {
		return nil, errors.New(contenttemplateenums.ErrDataInvalid)
	}
	return doc, nil
}

// toResp 实体 → 响应。
func toResp(e *contenttemplatemodel.TemplateEntity) *contenttemplatedto.TemplateResp {
	return &contenttemplatedto.TemplateResp{
		ID:            e.ID,
		Name:          e.Name,
		EntityType:    e.EntityType,
		TemplateRole:  e.TemplateRole,
		IsDefault:     e.IsDefault,
		DraftVersion:  e.DraftVersion,
		DraftDocument: e.DraftDocument,
		UpdatedAt:     e.UpdatedAt.Format("2006-01-02 15:04"),
	}
}
