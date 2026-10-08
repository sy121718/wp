package contenttemplateservice

// 写路径的原子性交给 model 侧的 CreateWithVersion / SaveWithVersion（模板行 + 不可变版本 +
// 当前版本指针同事务）；删除路径的引用检查一律 fail-closed —— 查不出引用就不删，
// 不让写在 JSONB 文档里的结构绑定随删除静默丢失。

// 两个口径：保存草稿走宽容校验（ValidatePageTolerant），发布/解析走严格校验（ValidatePage）；
// 两者都带字段绑定白名单校验（越界绑定在保存时即拒绝），结构模板（页眉/页脚）另行
// **明确拒绝**字段绑定 —— 它们在构建期按引用页面的实体解析，不是"全局结构"应有的行为。

// 「哪些模板文档引用了这个块」反查（ListBlockSourceRefs，逐工程扇出）。
//
// 按 id 的读取一律走逐工程定位（locateTemplate / GetScoped），不做「不限工程」兜底：
// content_templates 带 FORCE 策略，漏作用域时表现为「模板不存在」，而模板其实还在。

// 两条轴：按「实体类型 + 角色」解析生效模板（详情 / 归档分角色取用），或按模板 id 显式指定
// 用哪一套；每条轴都有「无工程参数（取唯一工程）」与「显式工程作用域」两个入口。
// 解析出的文档一律走严格校验（EDT-013）：非法模板在取用前拒绝，不等渲染期才炸。

// 与「结构模板被其它模板绑定」那条（contenttemplate_service.go 的 structureTemplateRefs，
// 读本模块自己的 content_templates 表）互补：这里回答的是**页面与自动发布实例**层面的引用 ——
// 页面草稿文档 settings.structure 的绑定、实例的 template_id 与覆盖文档绑定。
// 三者的共同点是都写在 JSONB 文档或跨模块的表里，数据库外键要么管不到（页面）、
// 要么只说得清「被引用」却说不清「是谁」（实例外键）。
//
// 数据来源由装配层经 TemplateImpactPort 注入（最窄只读接口）：本模块不认识页面表与实例表，
// 也不 import 对方的 service/model —— 依赖方向是 contenttemplate ← 装配。

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
	"go_wp/internal/module/block/contract"
	"go_wp/internal/module/contenttemplate/contract"
	"go_wp/internal/module/contenttemplate/dto"
	"go_wp/internal/module/contenttemplate/enums"
	"go_wp/internal/module/contenttemplate/model"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
)

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
		TemplateRole: tpl.TemplateRole,
		Document:     doc,
	}, nil
}

// SetImpactPort 注入引用反查端口（装配期调用；未注入时 Impact 回 Available=false，
// 删除路径退化为「只查本模块的表 + 外键兜底」，并各记一条日志让能力缺失可见）。
func (s *Service) SetImpactPort(p contenttemplatecontract.TemplateImpactPort) { s.impact = p }

// Impact 列出工程内引用了各模板的页面与实例（列表页整页渲染与删除保护共用一次扫描）。
func (s *Service) Impact(ctx context.Context, req *contenttemplatedto.ImpactReq) (res *contenttemplatedto.ImpactResp, err error) {
	if req == nil {
		req = &contenttemplatedto.ImpactReq{}
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if s.impact == nil {
		// Available=false 不是「没有引用」：调用方必须把它显示成「查不出来」而不是 0。
		return &contenttemplatedto.ImpactResp{Available: false, References: []contenttemplatedto.TemplateReference{}}, nil
	}
	ids, lerr := s.templateIDsOf(ctx, projectID)
	if lerr != nil {
		return nil, lerr
	}
	refs, unparsable, ierr := s.impact.ListTemplateReferences(ctx, projectID, ids)
	if ierr != nil {
		return nil, ierr
	}
	if refs == nil {
		refs = []contenttemplatedto.TemplateReference{}
	}
	return &contenttemplatedto.ImpactResp{Available: true, References: refs, Unparsable: unparsable}, nil
}

// referencesOfTemplate 汇总「引用某模板」的可定位描述（页面 + 实例），删除保护用。
//
// 端口未装配时返回 nil 且记日志：装配层必然注入（缺失是装配缺陷），但把「未装配」
// 当成「有引用」会让整个模块一条都删不掉 —— 那是在用故障换故障。
func (s *Service) referencesOfTemplate(ctx context.Context, projectID, templateID string) (refs []string, err error) {
	if s.impact == nil {
		logger.Scene("contenttemplate").With("templateId", templateID).
			Warn("模板引用反查未装配：本次删除未检查页面 / 实例绑定")
		return nil, nil
	}
	ids, lerr := s.templateIDsOf(ctx, projectID)
	if lerr != nil {
		return nil, lerr
	}
	all, _, ierr := s.impact.ListTemplateReferences(ctx, projectID, ids)
	if ierr != nil {
		return nil, ierr
	}
	for _, r := range all {
		if r.TemplateID != templateID {
			continue
		}
		refs = append(refs, templateReferenceText(r))
	}
	sort.Strings(refs)
	return refs, nil
}

// templateIDsOf 取工程内全部模板 id：引用扫描要认出文档里的绑定 id，
// 而「文档解析不了」时要退回字符串粗判，粗判同样需要这份集合。
func (s *Service) templateIDsOf(ctx context.Context, projectID string) ([]string, error) {
	rows, err := s.m.List(ctx, projectID, "")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r == nil {
			continue
		}
		if id := strings.TrimSpace(r.ID); id != "" {
			out = append(out, id)
		}
	}
	return out, nil
}

// templateReferenceText 一条引用记录 → 面向运营的一句话（删除拦截的错误明细 / 日志）。
//
// 文案里给出**可定位数据**（页面标题或路径 / 实例的实体与线上路径）：只写「被引用了」
// 会让人无从下手，而这一页的处置方式是「先到那个引用方解绑」。
func templateReferenceText(r contenttemplatedto.TemplateReference) string {
	switch r.Kind {
	case contenttemplatedto.ReferenceKindPage:
		name := strings.TrimSpace(r.PageTitle)
		if name == "" {
			name = strings.TrimSpace(r.PagePath)
		}
		if name == "" {
			name = r.PageID
		}
		if len(r.Slots) > 0 {
			return fmt.Sprintf("页面《%s》的%s", name, slotsLabelText(r.Slots))
		}
		return fmt.Sprintf("页面《%s》", name)
	case contenttemplatedto.ReferenceKindInstance:
		name := strings.TrimSpace(r.URLPath)
		if name == "" {
			name = r.EntityType + ":" + r.EntityID
		}
		if strings.TrimSpace(r.EntityType) != "" {
			return fmt.Sprintf("自动发布实例（%s %s，%s）", r.EntityType, r.EntityID, name)
		}
		return fmt.Sprintf("自动发布实例（%s）", name)
	default:
		return "未知来源的引用"
	}
}

// slotsLabelText 槽位列表 → 「页眉 / 页脚」这类面向运营的说法（排序后拼接）。
func slotsLabelText(slots []string) string {
	labels := make([]string, 0, len(slots))
	for _, slot := range slots {
		labels = append(labels, structureSlotLabel(slot))
	}
	sort.Strings(labels)
	return strings.Join(labels, " / ")
}
