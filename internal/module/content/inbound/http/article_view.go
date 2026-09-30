package contenthttp

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
	contentenums "go_wp/internal/module/content/enums"
	presentationdto "go_wp/internal/module/presentation/dto"
	projectenums "go_wp/internal/module/project/enums"
	seoscore "go_wp/internal/seo"
	"go_wp/internal/seo/scoring"
	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// articleErrScene 日志场景名（与 content 模块其它 logger.Scene("content") 一致）。
const articleErrScene = "content"

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
func scoreViewFromResult(res *scoring.Result, trs ...func(key, fallback string) string) scoreView {
	tr := articlePublishTr(trs)
	if res == nil {
		return scoreView{}
	}
	sv := scoreView{OK: true, Total: res.Total, Grade: res.Grade}
	for _, sec := range res.Sections {
		item := scoreSectionView{
			Label: sec.Label, Color: sec.Color, Score: sec.Score, Max: sec.Max,
			ColorLabel: seoscore.ScoreGradeText(tr, sec.Color),
		}
		for _, ck := range sec.Checks {
			if ck.Score >= ck.Max {
				continue
			}
			item.Issues = append(item.Issues, scoreIssueView{
				Text: i18n.FillTranslate(tr, projectenums.SEOScoreIssueFormat, "{label}：{actual}（基准 {benchmark}）→ {hint}",
					map[string]string{"label": ck.Label, "actual": ck.Actual, "benchmark": ck.Benchmark, "hint": ck.Hint}),
				Target: ck.Target,
			})
		}
		sv.Sections = append(sv.Sections, item)
	}
	return sv
}

// articleScoreViewOf 文章字段 → 评分视图（编辑页初始渲染与评分片段共用）。
func articleScoreViewOf(data map[string]any, articleURL, lang string,
	trs ...func(key, fallback string) string) scoreView {
	tr := articlePublishTr(trs)
	sv := scoreViewFromResult(seoscore.ScoreArticle(data, articleURL, lang), tr)
	if !sv.OK {
		return sv
	}
	// SERP 预览取的就是标题与摘要（2026-09-30 字段合并）：seoTitle / seoDescription
	// 已与它们合并，这里不再做「SEO 字段优先」的二段取值。
	sv.SerpTitle = articleStr(data, "title")
	if sv.SerpTitle == "" {
		sv.SerpTitle = tr(contentenums.ScoreSerpTitleEmpty, "（未填写文章标题）")
	}
	sv.SerpDesc = articleStr(data, "excerpt")
	if sv.SerpDesc == "" {
		sv.SerpDesc = tr(contentenums.ScoreSerpDescEmpty, "（未填写摘要 / SEO 描述）")
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

// articleInternalText 未命中任何白名单时的统一出口（错误文案三件套的第三件）：
// 原文只进日志（场景 + user_id + 原始错误），对外给归口文案。
//
// 页面上出现 "pq: duplicate key value violates unique constraint" 或
// `relation "contents" does not exist` 既看不懂，也把库表结构泄了出去 ——
// ?err= 回带与模板数据 Errors 都会被原样渲染，和 JSON body 一样不是可信边界。
func articleInternalText(c *gin.Context, err error) string {
	if err != nil {
		logger.Scene(articleErrScene).
			With("user_id", shell.CurrentUserID(c)).
			Error(err, "content 后台页操作失败（非业务错误，只对外给归口文案）")
	}
	return shell.PageInternalText(c)
}

// articleFacingOrInternal 不记日志的文案出口：调用点已经记过一条更具体的日志
// （带 lang）时用它，免得同一个错误在日志里出现两遍。
func articleFacingOrInternal(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := articleFacingText(c, err.Error()); msg != "" {
		return msg
	}
	return shell.PageInternalText(c)
}

// articleFacingError 把 content 契约的错误转成可展示文案（未命中 → 日志 + 归口文案）。
func articleFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := articleFacingText(c, err.Error()); msg != "" {
		return msg
	}
	return articleInternalText(c, err)
}

// articleErrControlledPrefixes 受控提示的前缀白名单（**按当前语言生成**）。
//
// 它们不是 enums key（因此进不了 articleFacingMessages），但整句都由本仓库自己拼出：
// 不含表名 / SQLSTATE / 路径，且带着运营照着做的数字。目前只有一条 ——
// shell.BulkIDs 的上限拒绝（internal/web/shell/bulk.go 的 BulkIDsFacingText）。
//
// 前缀必须跟着语言算：shell 那条提示是**按请求语言**取词渲染的，写死中文前缀会让
// 英文后台下的这条受控提示被判成未命中 → 回落归口文案（用户看不到「分批做」这句可行动的话）。
// 取词用**同一个 key 与同一个兜底模板**（shell.MsgBulkIDsTooMany），
// 与 shell 的写侧同源，不另抄一份措辞。
//
// 按**前缀**判而不是按来源直接透出：上游将来改成上抛别的错误时前缀不再命中，
// 会自动退回归口文案 / 回显 fallback，不会把不认识的原文顺出去。
func articleErrControlledPrefixes(c *gin.Context) []string {
	tpl := shell.TranslateFor(c)(shell.MsgBulkIDsTooMany, "一次最多操作 %s 项")
	return []string{fmt.Sprintf(strings.ReplaceAll(tpl, "%s", "%d"), shell.MaxBulkIDs)}
}

// articleControlledText 受控提示 → 原样透出（保留可行动信息）；未命中返回空串。
func articleControlledText(c *gin.Context, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, prefix := range articleErrControlledPrefixes(c) {
		if strings.HasPrefix(raw, prefix) {
			return raw
		}
	}
	return ""
}

// articleFacingText 白名单校验：命中返回可展示文案，未命中返回空串。
//
// content 契约有两类错误串：
//   - 裸常量（"ErrSlugTaken"）；
//   - 「常量: 明细」（"ErrInvalidField: "body""，validateData 拼的）。
//
// 第二类必须按前缀命中，只做精确匹配的话它们会全部落到统一内部错误 ——
// 运营看到「系统内部错误」而实际问题只是提交了一个不支持的字段。
func articleFacingText(c *gin.Context, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// 受控提示（shell.BulkIDs 的上限拒绝）放行：不是 enums key，但整句由本仓库拼出。
	// 写入侧（articleRedirectList）与回显侧（articleQueryText）共用这一份判据 ——
	// 少了它，「一次最多操作 10 项」会在回显时被自己的白名单吞掉（用户看不到任何提示）。
	if msg := articleControlledText(c, raw); msg != "" {
		return msg
	}
	tr := shell.TranslateFor(c)
	if fallback, ok := articleFacingMessages[raw]; ok {
		return tr(raw, fallback)
	}
	if idx := strings.IndexByte(raw, ':'); idx > 0 {
		if key := strings.TrimSpace(raw[:idx]); key != "" {
			if fallback, ok := articleFacingMessages[key]; ok {
				return tr(key, fallback)
			}
		}
	}
	return ""
}

// articleQueryText 查询参数回显（?err= / ?ok=）：同样过白名单，未命中时用 fallback。
func articleQueryText(c *gin.Context, raw, fallback string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	if msg := articleFacingText(c, raw); msg != "" {
		return msg
	}
	return fallback
}
