package mediaservice

// media_ref.go — 引用缓存（02-B 第 4 能力）：查询侧 + 删除保护 + 构建期写入侧。
//
// 写入时机：**构建期**（pipeline 构建产物落地前），不是每次编辑。
// 理由：
//   1. 引用关系是「产物事实」——页面文档里写的 URL 未必都进产物（条件渲染、块内联、
//      CMS 集合展开），只有编译后的 HTML 才是权威引用集；
//   2. 编辑期高频写库会与后台元数据更新（alt/title）互相放大写放大，且草稿态引用
//      在未发布时不应阻止删除；
//   3. 构建期天然幂等：同一文档重复构建写入同一集合（差集为空，零写入）。
// 落点：page 模块 assembleCompile → media.SyncReferences（见 page_media_refs.go）。
//
// 读取侧：删除前查 refs（ListRefs），非空即拒绝；详情页可展示引用来源。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	mediacontract "go_wp/internal/module/media/contract"
	mediato "go_wp/internal/module/media/dto"
	mediaenums "go_wp/internal/module/media/enums"
	mediamodel "go_wp/internal/module/media/model"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

// References 查询附件的引用缓存（引用来源清单）。
func (s *Service) References(ctx context.Context, id uint64) (res []mediato.AttachmentRefResp, err error) {
	if _, err := s.am.GetByID(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(mediaenums.ErrAttachmentNotFound)
		}
		return nil, err
	}
	refs, err := s.am.ListRefs(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]mediato.AttachmentRefResp, 0, len(refs))
	for _, r := range refs {
		out = append(out, mediato.AttachmentRefResp{Kind: r.Kind, ID: r.ID, Title: r.Title})
	}
	return out, nil
}

// SyncReferences 构建期全量同步：把某引用方（页面/块）产物中的媒体 URL 集合
// 写进对应附件的 extra_info.refs，并解除该引用方不再引用的附件。
// 返回本次该引用方实际命中的附件数。差集增删，重复构建零写入（幂等）。
func (s *Service) SyncReferences(ctx context.Context, req *mediacontract.SyncRefsInput) (n int, err error) {
	if req == nil {
		return 0, errors.New(mediaenums.MsgBadRequest)
	}
	kind := strings.TrimSpace(req.RefKind)
	refID := strings.TrimSpace(req.RefID)
	if kind == "" || refID == "" {
		return 0, errors.New(mediaenums.MsgBadRequest)
	}

	target := make(map[uint64]struct{}, len(req.URLs))
	for _, raw := range req.URLs {
		if id, ok := s.attachmentIDByURL(ctx, raw); ok {
			target[id] = struct{}{}
		}
	}

	current, err := s.am.ListIDsByRef(ctx, kind, refID)
	if err != nil {
		return 0, err
	}
	currentSet := make(map[uint64]struct{}, len(current))
	for _, id := range current {
		currentSet[id] = struct{}{}
	}

	ref := mediamodel.AttachmentRef{Kind: kind, ID: refID, Title: strings.TrimSpace(req.RefTitle)}
	for id := range target {
		if _, ok := currentSet[id]; ok {
			continue
		}
		if _, aerr := s.am.AddRef(ctx, id, ref); aerr != nil {
			return n, aerr
		}
	}
	for _, id := range current {
		if _, ok := target[id]; ok {
			continue
		}
		if _, rerr := s.am.RemoveRef(ctx, id, kind, refID); rerr != nil {
			return n, rerr
		}
	}
	return len(target), nil
}

// attachmentIDByURL 把产物中的媒体 URL 解析为附件 ID：
// 先按原图 file_path 反查，未命中再按变体 file_path 反查（页面可能直接引用 _thumb/_medium）。
// 非 /storage 前缀、路径含 ..、或查不到附件时返回 false（静默跳过，不影响构建）。
func (s *Service) attachmentIDByURL(ctx context.Context, url string) (uint64, bool) {
	const prefix = "/storage/"
	raw := strings.TrimSpace(url)
	if raw == "" {
		return 0, false
	}
	// 兼容绝对 URL（upload.base_url 配置了站点域名时的产物形态）。
	if idx := strings.Index(raw, prefix); idx >= 0 {
		raw = raw[idx+len(prefix):]
	} else if !strings.HasPrefix(raw, prefix) {
		return 0, false
	}
	// 去掉查询串与锚点（构建产物里可能带 ?v=xxx 之类的缓存参数）。
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimPrefix(raw, "/")
	if raw == "" || strings.Contains(raw, "..") {
		return 0, false
	}
	if att, err := s.am.GetByFilePath(ctx, raw); err == nil && att != nil {
		return att.ID, true
	}
	v, err := s.vm.GetByFilePath(ctx, raw)
	if err != nil || v == nil {
		return 0, false
	}
	return v.AttachmentID, true
}

// collectStorageURLs 从 HTML 产物中提取媒体 URL（/storage/...）。
// 去重后返回；非媒体地址一律丢弃。用于构建期收集引用集合。
func collectStorageURLs(html string) []string {
	const prefix = "/storage/"
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for {
		idx := strings.Index(html, prefix)
		if idx < 0 {
			break
		}
		html = html[idx:]
		end := 0
		for end < len(html) {
			c := html[end]
			if c == '"' || c == '\'' || c == ')' || c == '<' || c == '>' || c == ' ' || c == '\n' || c == '\r' || c == '\t' {
				break
			}
			end++
		}
		url := html[:end]
		html = html[end:]
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		out = append(out, url)
	}
	return out
}

// SyncReferencesFromHTML 构建期入口：从产物 HTML 收集 /storage 引用并全量同步到 refs。
// 调用方（page 构建链）只需给出「引用方 + 产物字节」，URL 形态与反查逻辑留在媒体域内。
// 返回命中的附件数；失败返回 error 由调用方决定是否降级（构建主链不应被阻断）。
func (s *Service) SyncReferencesFromHTML(ctx context.Context, refKind, refID, refTitle, html string) (int, error) {
	if strings.TrimSpace(refID) == "" || html == "" {
		return 0, nil
	}
	urls := collectStorageURLs(html)
	n, err := s.SyncReferences(ctx, &mediacontract.SyncRefsInput{
		RefKind: refKind, RefID: refID, RefTitle: refTitle, URLs: urls,
	})
	if err != nil {
		logger.Scene("build").With("ref_id", refID).Error(err, "媒体引用缓存同步失败")
		return n, err
	}
	return n, nil
}

// refKindPage 页面引用方类型（与 media_reference 表的 ref_kind 取值口径一致）。
const refKindPage = "page"

// summarizeRefs 生成删除拦截提示：按引用方类型聚合计数。
// 全为页面时输出「N 个页面引用」，混合类型时输出「N 处引用」。
func summarizeRefs(refs []mediamodel.AttachmentRef) string {
	if len(refs) == 0 {
		return ""
	}
	allPage := true
	titles := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.Kind != refKindPage {
			allPage = false
		}
		if r.Title != "" {
			titles = append(titles, r.Title)
		} else {
			titles = append(titles, r.Kind+":"+r.ID)
		}
	}
	head := fmt.Sprintf("%d 处引用", len(refs))
	if allPage {
		head = fmt.Sprintf("%d 个页面引用", len(refs))
	}
	if len(titles) > 3 {
		titles = append(titles[:3], "等")
	}
	return head + "（" + strings.Join(titles, "、") + "）"
}
