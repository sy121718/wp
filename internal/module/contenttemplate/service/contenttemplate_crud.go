package contenttemplateservice

// contenttemplate_crud.go — 模板增删改与生效切换（写用例）。
//
// 写路径的原子性交给 model 侧的 CreateWithVersion / SaveWithVersion（模板行 + 不可变版本 +
// 当前版本指针同事务）；删除路径的引用检查一律 fail-closed —— 查不出引用就不删，
// 不让写在 JSONB 文档里的结构绑定随删除静默丢失。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
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
