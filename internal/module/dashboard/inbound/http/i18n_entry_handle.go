// i18n_entry_handle.go — 后台「文案词条」页（审计 I18N-003）。
//
// 此前词条只能靠迁移 seed：改一句文案要写迁移文件、重跑迁移、重新部署。
// 结果是能改文案的人只有写代码的人 —— 运营遇到错别字只能等下一次发版。
//
// 职责边界（写在页面正文里，避免后来者搞混）：
//
//	· 迁移 seed 是**默认值来源**（ON CONFLICT DO NOTHING，不覆盖这里的修改）；
//	· 本页是**真相来源**（运营改过之后，seed 不会再回来覆盖）。
//
// 保存/删除后立即重载缓存（pkg/i18n 内部完成）：否则要等下一次定时刷新才生效，
// 运营会以为「保存失败」然后反复保存，而每次保存都推进 revision、标记全站页面待重建。
package dashboardhttp

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/i18n"
)

// i18nEntryPageSize 每页条数。
const i18nEntryPageSize = 50

// i18nEntryPageHandle 词条页处理器（无依赖：读写都走 pkg/i18n 的端口）。
type i18nEntryPageHandle struct{}

// NewI18nEntryPageHandle 构造。
func NewI18nEntryPageHandle() *i18nEntryPageHandle { return &i18nEntryPageHandle{} }

// i18nEntryFilterOf 读取筛选参数（GET，全部可选）。
func i18nEntryFilterOf(c *gin.Context) i18n.EntryFilter {
	page, _ := strconv.Atoi(strings.TrimSpace(c.Query("page")))
	if page < 1 {
		page = 1
	}
	return i18n.EntryFilter{
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		Lang:     strings.TrimSpace(c.Query("lang")),
		Category: strings.TrimSpace(c.Query("category")),
		Limit:    i18nEntryPageSize,
		Offset:   (page - 1) * i18nEntryPageSize,
	}
}

// I18nEntriesPage GET /admin/i18n —— 词条列表与编辑入口。
func (h *i18nEntryPageHandle) I18nEntriesPage(c *gin.Context) {
	filter := i18nEntryFilterOf(c)
	items, total, err := i18n.ListEntries(c.Request.Context(), filter)
	if err != nil {
		pageError(c, "i18n_entry", err)
		return
	}
	categories, _ := i18n.Categories(c.Request.Context())
	page := filter.Offset/i18nEntryPageSize + 1
	if page < 1 {
		page = 1
	}
	pages := int((total + i18nEntryPageSize - 1) / i18nEntryPageSize)
	data := gin.H{
		"title":      "文案词条",
		"Entries":    items,
		"Total":      total,
		"Page":       page,
		"Pages":      pages,
		"Keyword":    filter.Keyword,
		"LangFilter": filter.Lang,
		"CatFilter":  filter.Category,
		"Categories": categories,
		"Saved":      strings.TrimSpace(c.Query("saved")),
		"Errored":    strings.TrimSpace(c.Query("errored")),
	}
	c.HTML(http.StatusOK, "admin/i18n", withCSRF(c, withI18n(c, data)))
}

// I18nEntrySave POST /admin/i18n/save —— 新增或更新一条词条。
//
// 用表单 + 302 回列表（而不是 JSON API）：这一页的操作只有「改一条字」这一种，
// 表单能把「保存成功了吗」直接反馈在页面上，不需要前端状态机。
func (h *i18nEntryPageHandle) I18nEntrySave(c *gin.Context) {
	entry := i18n.Entry{
		Key:      c.PostForm("key"),
		Lang:     c.PostForm("lang"),
		Value:    c.PostForm("value"),
		Category: c.PostForm("category"),
		Remark:   c.PostForm("remark"),
		Status:   1,
	}
	if err := i18n.SaveEntry(c.Request.Context(), entry); err != nil {
		c.Redirect(http.StatusFound, i18nBackURL(c, "errored", err.Error()))
		return
	}
	c.Redirect(http.StatusFound, i18nBackURL(c, "saved", strings.TrimSpace(entry.Key)+" · "+strings.TrimSpace(entry.Lang)))
}

// I18nEntryDelete POST /admin/i18n/delete —— 删除一条词条。
//
// 删除后构建期回退到组件包内的中文兜底（可见降级，不是空白），所以不做「不许删」的保护 ——
// 但运营需要知道这个后果，页面上写明了。
func (h *i18nEntryPageHandle) I18nEntryDelete(c *gin.Context) {
	key := c.PostForm("key")
	lang := c.PostForm("lang")
	if err := i18n.DeleteEntry(c.Request.Context(), key, lang); err != nil {
		c.Redirect(http.StatusFound, i18nBackURL(c, "errored", err.Error()))
		return
	}
	c.Redirect(http.StatusFound, i18nBackURL(c, "saved", "已删除 "+strings.TrimSpace(key)+" · "+strings.TrimSpace(lang)))
}

// i18nBackURL 回列表并带上筛选与提示（只回填站内相对路径，避免开放重定向）。
func i18nBackURL(c *gin.Context, hintKey, hintValue string) string {
	q := []string{}
	for _, k := range []string{"keyword", "lang", "category", "page"} {
		value := strings.TrimSpace(c.PostForm("_" + k))
		if value == "" {
			value = strings.TrimSpace(c.Query(k))
		}
		if value != "" {
			q = append(q, k+"="+url.QueryEscape(value))
		}
	}
	q = append(q, hintKey+"="+url.QueryEscape(hintValue))
	return "/admin/i18n?" + strings.Join(q, "&")
}
