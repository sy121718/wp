package contenttemplateservice

// contenttemplate_scope.go — 工程作用域解析（DB-009 第三批）。
//
// content_templates 带 FORCE 策略：不设作用域的查询在非超级角色下静默返回 0 行
// （表现为「模板不存在」，而模板其实还在）。三个来源分工：
// resolveProjectID（显式传入 → 校验存在 → 回落唯一工程）、
// fanoutProjectIDs（只给得出「全站」语义的入口逐工程枚举）、
// locateTemplate（只带模板 id 的入口逐工程探测定位，命中即返回）。

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
)

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
