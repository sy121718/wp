package dashboardhttp

// article_view.go — 文章页的视图组装与工具函数（取数逻辑在 article_handle.go）。
//
// 视图组装全部做成纯函数：渲染键名与计数口径只在这里定义一次，真实渲染测试
// 直接喂数据走同一条组装路径，而不是在测试里手抄一份键名 —— 手抄的那份会随模板
// 演进静默失配，那正是「页面上少了一块、断言却通过」的成因。

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	contentdto "go_wp/internal/module/content/dto"
	dashboardenums "go_wp/internal/module/dashboard/enums"
	presentationdto "go_wp/internal/module/presentation/dto"
	seoscore "go_wp/internal/seo"
	"go_wp/internal/seo/scoring"
)

// —— 数据取值 ——

// articleStr 取内容实体字段里的字符串值（非字符串按空处理）。
func articleStr(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	s, _ := data[key].(string)
	return strings.TrimSpace(s)
}

// articleTextOrEmpty 空值统一显示成占位符（表格空白单元格读不出「没有值」）。
func articleTextOrEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return articleEmptyField
	}
	return value
}

// articleSlugOf / articleRevisionOf / articleUpdatedAtOf 实体 → 表单字段。
//
// item 为 nil（新建，或读取失败）时给零值：表单仍可渲染与提交。
func articleSlugOf(item *contentdto.ContentResp) string {
	if item == nil {
		return ""
	}
	return item.Slug
}

func articleRevisionOf(item *contentdto.ContentResp) int64 {
	if item == nil {
		return 0
	}
	return item.Revision
}

func articleUpdatedAtOf(item *contentdto.ContentResp) string {
	if item == nil {
		return ""
	}
	return item.UpdatedAt
}

// —— 路径 ——

// articleEditURL 文章编辑页地址。
func articleEditURL(id string) string {
	return "/admin/articles/edit?" + url.Values{"id": {id}}.Encode()
}

// articlePreviewURL 文章的线上路径（评分器的 URL 检查用）。
//
// 未发布时用默认博客前缀拼一个「将来会是什么样」的地址：URL 相关检查看的是
// 路径形态（层级、长度、是否含参数），不是它现在能否打开。
func articlePreviewURL(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	return articleBlogPathPrefix + slug
}

// articleDefaultURLPath 发布区块的路径默认值（可改）。
func articleDefaultURLPath(slug string) string {
	return articlePreviewURL(slug)
}

// articlePublicURL 站点内逻辑路径 → 浏览器可打开的访问面地址。
//
// 与系统页面槽位页同一口径：active_path 恒带前导 "/"，这里仍做一次归一 ——
// 拼出 "/siteblog/x" 这种地址的错法是静默的（链接能渲染、点了才 404）。
func articlePublicURL(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	return "/site/" + strings.TrimPrefix(p, "/")
}

// —— 发布状态 ——

// articlesPublished 批量取「文章 id → 线上路径」，只含真正已发布的。
//
// 逐条查而不是一次查全表：presentation 契约按实体查询（GetByEntity）是既有口径，
// 为列表页新增一个「按类型列实例」的批量入口会把只读端口撑宽。
// 列表已限 50 条，最多 50 次主键查询，换掉一次契约扩张是划算的。
//
// 查询失败按「未发布」处理：列表页不是排查页，不让一条查询失败把整页打成错误页。
func articlesPublished(ctx context.Context, port articlePublishPort, list []*contentdto.ContentResp) map[string]string {
	out := map[string]string{}
	if port == nil {
		return out
	}
	for _, it := range list {
		res, err := port.GetByEntity(ctx, &presentationdto.GetByEntityReq{
			EntityType: articleEntityType, EntityID: it.ID,
		})
		if err != nil || res == nil {
			continue
		}
		if strings.TrimSpace(res.URLPath) != "" {
			out[it.ID] = res.URLPath
		}
	}
	return out
}

// —— 评分视图 ——

// scoreViewFromResult 评分结果 → 视图（不含 SERP 预览：那部分由调用方按自己的
// 标题/描述来源填）。页面设置面板与文章编辑页共用这一份转换 —— 两处各写一遍的话，
// 「哪些检查项算未达标」这种判断会分叉。
func scoreViewFromResult(res *scoring.Result) scoreView {
	if res == nil {
		return scoreView{}
	}
	sv := scoreView{OK: true, Total: res.Total, Grade: res.Grade}
	for _, sec := range res.Sections {
		item := scoreSectionView{
			Label: sec.Label, Color: sec.Color, Score: sec.Score, Max: sec.Max,
			ColorLabel: seoColorLabels[sec.Color],
		}
		for _, ck := range sec.Checks {
			if ck.Score >= ck.Max {
				continue
			}
			item.Issues = append(item.Issues, scoreIssueView{
				Text:   fmt.Sprintf("%s：%s（基准 %s）→ %s", ck.Label, ck.Actual, ck.Benchmark, ck.Hint),
				Target: ck.Target,
			})
		}
		sv.Sections = append(sv.Sections, item)
	}
	return sv
}

// articleScoreViewOf 文章字段 → 评分视图（编辑页初始渲染与评分片段共用）。
func articleScoreViewOf(data map[string]any, articleURL, lang string) scoreView {
	sv := scoreViewFromResult(seoscore.ScoreArticle(data, articleURL, lang))
	if !sv.OK {
		return sv
	}
	sv.SerpTitle = firstNonEmpty(articleStr(data, "seoTitle"), articleStr(data, "title"))
	if sv.SerpTitle == "" {
		sv.SerpTitle = "（未填写文章标题）"
	}
	sv.SerpDesc = firstNonEmpty(articleStr(data, "seoDescription"), articleStr(data, "excerpt"))
	if sv.SerpDesc == "" {
		sv.SerpDesc = "（未填写摘要 / SEO 描述）"
	}
	sv.SerpURL = articleURL
	if sv.SerpURL == "" {
		sv.SerpURL = articleBlogPathPrefix + "example"
	}
	return sv
}

// —— 文案与跳转 ——

// articleRedirectList 回列表页并回显结论（成功 ?ok=、失败 ?err=）。
func articleRedirectList(c *gin.Context, errText, okText string) {
	articleRedirect(c, "/admin/articles", "", errText, okText)
}

// articleRedirectEdit 回编辑页并回显结论。
func articleRedirectEdit(c *gin.Context, id, okText, errText string) {
	articleRedirect(c, "/admin/articles/edit", id, errText, okText)
}

// articleRedirect 统一跳转：只带白名单内的文案（查询参数是用户可编辑的）。
func articleRedirect(c *gin.Context, path, id, errText, okText string) {
	q := url.Values{}
	if strings.TrimSpace(id) != "" {
		q.Set("id", id)
	}
	if errText != "" {
		q.Set("err", errText)
	}
	if okText != "" {
		q.Set("ok", okText)
	}
	target := path
	if enc := q.Encode(); enc != "" {
		target += "?" + enc
	}
	c.Redirect(http.StatusFound, target)
}

// articleFacingError 把 content 契约的错误转成可展示文案。
func articleFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := articleFacingText(err.Error()); msg != "" {
		return msg
	}
	return articleInternalText(c)
}

// articleFacingText 白名单校验：命中返回可展示文案，未命中返回空串。
//
// content 契约有两类错误串：
//   - 裸常量（"ErrSlugTaken"）；
//   - 「常量: 明细」（"ErrInvalidField: "body""，validateData 拼的）。
//
// 第二类必须按前缀命中，只做精确匹配的话它们会全部落到统一内部错误 ——
// 运营看到「系统内部错误」而实际问题只是提交了一个不支持的字段。
func articleFacingText(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if msg, ok := articleFacingMessages[raw]; ok {
		return msg
	}
	if idx := strings.IndexByte(raw, ':'); idx > 0 {
		if msg, ok := articleFacingMessages[strings.TrimSpace(raw[:idx])]; ok {
			return msg
		}
	}
	return ""
}

// articleQueryText 查询参数回显（?err= / ?ok=）：同样过白名单，未命中时用 fallback。
func articleQueryText(c *gin.Context, raw, fallback string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	if msg := articleFacingText(raw); msg != "" {
		return msg
	}
	return fallback
}

// articleInternalText 统一内部错误文案（走当前语言的译文，缺词条回退中文原文）。
func articleInternalText(c *gin.Context) string {
	return translateFor(c)(dashboardenums.MsgInternalError, "系统内部错误，请稍后重试")
}
