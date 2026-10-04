package contenttemplateservice

// contenttemplate_query.go — 模板读取入口：按 id / 按类型取列表，以及给 block 模块的
// 「哪些模板文档引用了这个块」反查（ListBlockSourceRefs，逐工程扇出）。
//
// 按 id 的读取一律走逐工程定位（locateTemplate / GetScoped），不做「不限工程」兜底：
// content_templates 带 FORCE 策略，漏作用域时表现为「模板不存在」，而模板其实还在。

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
)

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
