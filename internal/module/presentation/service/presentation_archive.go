package presentationservice

// presentation_archive.go — 归档型实例（审计 EDT-004）。
//
// 详情页与归档页的区别不在模板语法，而在**页面身份**：详情页讲一个实体，
// 归档页列这个实体下的内容。两者由同一个实体驱动，所以实例表需要角色维度
//（迁移 175），模板侧也需要按角色取（contenttemplate 的 template_role）。
//
// 本文件只负责一件事：给定一个实体，确保它的归档页存在。
// 没有归档模板时**静默跳过** —— 那是正常状态（不是每个站点都要归档页），
// 报错会让「新建一个分类」变成一件会失败的事。

import (
	"context"
	"errors"
	"strings"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
)

// ArchivePathFor 归档页的访问路径。
//
// 规则：/{entityType}/{slug}，如 /category/electronics。
// 用实体类型做前缀而不是可配置前缀：两个类型配成同一个前缀时，它们在访问面上
// 无法区分，而冲突要到实际请求 404 或串页才会被发现。
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
	tpl, terr := s.templates.ResolveTemplateByRole(ctx, req.EntityType, contenttemplatecontract.TemplateRoleArchive)
	if terr != nil {
		// 没有归档模板是最常见的情况，按「跳过」处理；其它错误照常上报。
		if strings.Contains(terr.Error(), "not found") || strings.Contains(terr.Error(), "ErrNotFound") {
			return &presentationdto.EnsureArchiveResp{Skipped: "该实体类型未配置归档模板"}, nil
		}
		return nil, terr
	}

	wantPath := ArchivePathFor(req.EntityType, slug)
	inst, cerr := s.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: req.ProjectID, EntityType: req.EntityType, EntityID: req.EntityID,
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
			ID: inst.ID, NewPath: wantPath, WithRedirect: true,
		})
		if uerr != nil {
			return nil, uerr
		}
		inst = updated
	}
	return &presentationdto.EnsureArchiveResp{InstanceID: inst.ID, Created: true}, nil
}
