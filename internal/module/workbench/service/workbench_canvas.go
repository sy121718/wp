package service

// workbench_canvas.go — 画布装配的取数与纯投影。
//
// 这一组是「HTTP 之外的那一半」：读页面 / 主题 / 块 / 模板、把契约响应压成画布要的轻量形状、
// 拼预览查询串、算静态资源版本、算画布标题。它们原来散在 inbound/http 的三个文件里，
// 由 handler 直接编排（router.go 的 pageOf、workbench_handle.go 的 blockSummaries 等）。
// 搬到这里的判据是：**换一个入口（CLI / 后台任务 / 将来的画布 v2）也要用同一份口径** ——
// 尤其是 pageOf 的「先问归属再取详情」，它是一条越权防护，不该有两份实现。
//
// 本文件不认识 gin：ctx 由调用方传，取词由调用方给（Translate）。

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	pagecontract "go_wp/internal/module/page/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	workbenchenums "go_wp/internal/module/workbench/enums"
)

// PageByID 按 id 读取页面（画布 / 预览共用同一取数口径）。
//
// Detail 把 projectID 当**必填的越权防护 scope**（少它只会得到「参数缺失」，
// 看起来像「页面不存在」）。画布路由手上只有 pageId，所以先用只读的
// ProjectOfPage 问「这个页面属于谁」，再按 scope 取详情。
func (s *Service) PageByID(ctx context.Context, pageID string) (*pagecontract.PageResp, error) {
	if s == nil || s.pages == nil {
		return nil, errors.New("页面服务未装配")
	}
	projectID, err := s.pages.ProjectOfPage(ctx, pageID)
	if err != nil {
		return nil, err
	}
	return s.pages.Detail(ctx, &pagecontract.DetailReq{ProjectID: projectID, ID: pageID})
}

// ThemeIDOf 页面挂接的主题 ID（未挂接返回空串）。
func ThemeIDOf(page *pagecontract.PageResp) string {
	if page.ThemeID == "" {
		return ""
	}
	return page.ThemeID
}

// ThemeSettingsOf 页面挂接主题的 settings（colors/fontFamily 等），未挂接或查询失败返回 nil。
func (s *Service) ThemeSettingsOf(ctx context.Context, page *pagecontract.PageResp) json.RawMessage {
	if page.ThemeID == "" {
		return nil
	}
	theme, err := s.projects.GetTheme(ctx, page.ThemeID)
	if err != nil || theme == nil {
		return nil
	}
	return theme.Settings
}

// BlockSummaries 工程块列表的轻量投影（id/name/kind/category/reuseMode，不含文档大字段）。
// category 供 workbench 全局块按分类分组；reuseMode 供「引用/复制」双动作分流（docs/02-D §5.3）。
//
// 返回 []map[string]any 而不是 gin.H：这是契约无关的形状投影，service 不认识 gin。
// 值进 gin.H 后再 json.Marshal，字节与原实现一致。
func (s *Service) BlockSummaries(ctx context.Context, projectID string) []map[string]any {
	blocks, err := s.blocks.List(ctx, &blockcontract.ListReq{ProjectID: projectID})
	if err != nil {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, map[string]any{"id": b.ID, "name": b.Name, "kind": b.Kind, "category": b.Category, "reuseMode": b.ReuseMode})
	}
	return out
}

// TemplateByID 按模板 id 取预览目标：优先用画布自己带过来的工程作用域
// （content_templates 带 FORCE 策略，作用域缺省时只能靠「工程唯一」解析）。
//
// 调用方负责先确认模板端口已装配（原 handler 的 previewTemplateTarget 就做这件事）。
func (s *Service) TemplateByID(ctx context.Context, templateID, projectID string) (*contenttemplatedto.TemplateResp, error) {
	if pid := strings.TrimSpace(projectID); pid != "" {
		return s.contentTemplates.GetScoped(ctx, pid, templateID)
	}
	return s.contentTemplates.Get(ctx, &contenttemplatedto.GetReq{ID: templateID})
}

// PluginAssembly 启用插件装配素材（无插件模块契约或无启用插件时返回 nil）。
//
// 与原实现的差别：**去掉了单请求内缓存**（原 handler 把结果挂在 gin.Context 的
// "pluginAssembly" 键上）。全仓只有画布入口一处调用，一次请求最多取一次，
// 缓存从来没有命中过；而它把「一次取数」的语义藏进了 HTTP 上下文里。
// 真正需要复用的调用方自己存返回值即可。
func (s *Service) PluginAssembly(ctx context.Context) *plugincontract.Assembly {
	if s.plugins == nil {
		return nil
	}
	asm, err := s.plugins.EnabledAssembly(ctx)
	if err != nil {
		return nil
	}
	if asm != nil && (len(asm.PluginFS) > 0 || len(asm.Specs) > 0) {
		return asm
	}
	return nil
}

// TemplatePreviewQuery 模板画布 iframe 与「新标签预览」共用的查询串。
//
// entityId 为空（结构模板的无实体模式）时不带该参数：空串参数与服务端「缺参数」在
// 日志与排查里长得一样，少一个无意义的空参数省一次误判。
func TemplatePreviewQuery(templateID, entityType, entityID, projectID string) string {
	q := url.Values{}
	q.Set("template", templateID)
	q.Set("entityType", entityType)
	if entityID != "" {
		q.Set("entityId", entityID)
	}
	q.Set("editor", "1")
	if projectID != "" {
		q.Set("projectId", projectID)
	}
	return q.Encode()
}

// BlockByID 按 id 取全局块详情（块画布与块预览共用同一取数口径）。
//
// 与原 handler 内联调用等价：Detail 的越权防护 scope 由块的 ProjectID 承担，
// 这里没有额外的 `pageOf` 式两跳（block 契约的 Detail 只收 id）。
func (s *Service) BlockByID(ctx context.Context, blockID string) (*blockdto.BlockResp, error) {
	return s.blocks.Detail(ctx, &blockdto.DetailReq{ID: blockID})
}

// StaticJSVersion 工作台脚本缓存版本：取拆分后模块目录（static/js/workbench/**）
// 下所有 .js 的最新 mtime。任一模块改动都会让入口 URL 的 ?v= 变化，配合
// StaticCacheMiddleware 的协商缓存，浏览器不会再执行旧模块。
//
// 读的是工作目录下的相对路径 —— 版本值只影响浏览器缓存键，与运行环境无关。
func StaticJSVersion() string {
	root := filepath.Join("internal", "templates", "static", "js", "workbench")
	var latest int64
	err := filepath.Walk(root, func(_ string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // 目录缺失/权限问题不阻断渲染，版本退化为 0
		}
		if fi.IsDir() || !strings.HasSuffix(fi.Name(), ".js") {
			return nil
		}
		if m := fi.ModTime().Unix(); m > latest {
			latest = m
		}
		return nil
	})
	if err != nil || latest == 0 {
		return "0"
	}
	return strconv.FormatInt(latest, 10)
}

// WorkbenchTitle 画布标题：作者自己的 SEO 标题优先，否则「前缀 + 草稿路径」。
//
// 取词在 Go 侧完成（tr 参与签名）：这个值会作为 data.title 交给 shell.Prepare，
// 而 injectI18n 对 title 的处理是 `t(title, title)` —— 拼接过的句子不是 key，
// 只会原样返回，所以前缀必须先在这里翻译好（词条 workbench.title.*）。
func WorkbenchTitle(page *pagecontract.PageResp, tr Translate) string {
	if page == nil || strings.TrimSpace(page.ID) == "" {
		return tr(workbenchenums.TitleEditor)
	}
	var doc struct {
		Settings struct {
			SEO struct {
				Title string `json:"title"`
			} `json:"seo"`
		} `json:"settings"`
	}
	_ = json.Unmarshal(page.DraftDocument, &doc)
	if doc.Settings.SEO.Title != "" {
		return doc.Settings.SEO.Title
	}
	return tr(workbenchenums.TitleEditorPrefix) + page.DraftPath
}
