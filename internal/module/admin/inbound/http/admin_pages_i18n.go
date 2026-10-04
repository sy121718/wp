package adminhttp

// admin_pages_i18n.go — 文案词条页（/admin/i18n）：列表与分页、编辑抽屉、新增 / 更新 / 删除、批量删除与站点待重建标记。

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	adminenums "go_wp/internal/module/admin/enums"
)

// --- 文案词条页（审计 I18N-003） ---
//
// 迁移 seed 是默认值来源（ON CONFLICT DO NOTHING，不覆盖这里的修改）；
// 本页是真相来源（运营改过之后，seed 不会再回来覆盖）。
// 保存/删除后立即重载缓存（pkg/i18n 内部完成）。

// adminI18nEntryPageSize 每页条数。
const adminI18nEntryPageSize = 50

// adminI18nEntryHandle 词条页处理器（读写走 pkg/i18n 的端口，失效走装配注入的页面标记）。
type adminI18nEntryHandle struct {
	// pages 词条变更后标记站点待重建（装配期注入）。
	//
	// 为什么必须接：sys_i18n 的词条在**构建期**取词并烘进 HTML 字节（组件固定文案），
	// 改了词条而没人标 stale，站点就永远输出旧文案 —— 而且没有任何报错。
	// 页面 / 商品 / 导航翻译工作台与站点设置这四条路径历来都调 page.MarkStaleForI18n，
	// 词条页此前漏了这一环（词条是全局的：sys_i18n 没有工程维度，所以没有"精确到某页"
	// 的选项，一律按 i18n:site 依赖做全站标记）。
	pages adminI18nPageMarker
	// dict 字典只读口（sysconfig 契约，装配期注入）：给语言筛选下拉与「新建词条」的
	// 语言下拉供数。此前两处各写死 zh-CN / en-US 两条 option —— 于是**已收录的其它语言
	// 在页面上完全不可选**，建词条只能靠手输 URL 里的参数。
	//
	// 用不用 ui_available 做区分：**用**，但只在「新建词条」那一处 —— 那一处的下一步
	// 问题是「我该给哪个语言补词条」，标记出哪些语种已有界面译文正好回答它；
	// 筛选下拉不加标记（筛的是已有词条，标记只是噪音）。
	dict sysconfigcontract.DictReader
}

// adminI18nPageMarker 页面侧的最小失效端口（消费者侧定义，跨模块只依赖这一条）。
type adminI18nPageMarker interface {
	MarkStaleForI18n(ctx context.Context) error
}

// SetPageMarker 注入页面失效端口（装配期）。
func (h *adminI18nEntryHandle) SetPageMarker(m adminI18nPageMarker) { h.pages = m }

// SetDictReader 注入字典只读口（装配期）：语言下拉的供数来源。
func (h *adminI18nEntryHandle) SetDictReader(r sysconfigcontract.DictReader) { h.dict = r }

// NewAdminI18nEntryHandle 构造。
func NewAdminI18nEntryHandle() *adminI18nEntryHandle { return &adminI18nEntryHandle{} }

// markI18nStale 词条变更后标记站点待重建。
//
// 失败只记日志、**不改变响应**：词条此刻已经写进库了，回报"保存失败"会让运营以为没保存
// 而反复重试；而站点停在旧文案是**可见**的降级（下次编辑 / 发布会自然覆盖）。
// 未注入端口时直接返回 —— 装配漏接的表现是"改了词条站点不更新"，由 wiring 端口清单兜底。
func (h *adminI18nEntryHandle) markI18nStale(c *gin.Context) {
	if h == nil || h.pages == nil {
		return
	}
	if err := h.pages.MarkStaleForI18n(c.Request.Context()); err != nil {
		logger.Scene(adminErrScene).
			With("user_id", shell.CurrentUserID(c)).
			Error(err, "词条变更后标记站点待重建失败")
	}
}

// adminI18nEntryFilterOf 读取筛选参数（GET，全部可选）。
func adminI18nEntryFilterOf(c *gin.Context) i18n.EntryFilter {
	page, _ := strconv.Atoi(strings.TrimSpace(c.Query("page")))
	if page < 1 {
		page = 1
	}
	return i18n.EntryFilter{
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		Lang:     strings.TrimSpace(c.Query("lang")),
		Category: strings.TrimSpace(c.Query("category")),
		Limit:    adminI18nEntryPageSize,
		Offset:   (page - 1) * adminI18nEntryPageSize,
	}
}

// I18nEntriesPage GET /admin/i18n —— 词条列表与编辑入口。
func (h *adminI18nEntryHandle) I18nEntriesPage(c *gin.Context) {
	filter := adminI18nEntryFilterOf(c)
	items, total, err := i18n.ListEntries(c.Request.Context(), filter)
	if err != nil {
		shell.PageError(c, "i18n_entry", err)
		return
	}
	categories, _ := i18n.Categories(c.Request.Context())
	page := filter.Offset/adminI18nEntryPageSize + 1
	if page < 1 {
		page = 1
	}
	editURLs := make([]string, len(items))
	for idx, entry := range items {
		query := url.Values{"key": {entry.Key}, "lang": {entry.Lang}}
		for _, field := range []string{"keyword", "category", "page"} {
			if value := strings.TrimSpace(c.Query(field)); value != "" {
				query.Set(field, value)
			}
		}
		if filterLang := strings.TrimSpace(c.Query("lang")); filterLang != "" {
			query.Set("filter_lang", filterLang)
		}
		editURLs[idx] = "/admin/i18n/edit?" + query.Encode()
	}
	data := gin.H{
		// title 是 i18n **key**（不是中文）：shell.Prepare → injectI18n 会按当前语言翻译它。
		// admin.i18n.title 是库内既有的词条（zh-CN「文案词条」/ 英文），所以这里复用它 ——
		// 写中文原文会让英文后台的页面标题恒为中文（其它页面标题都已经是 enums key）。
		"title":        "admin.i18n.title",
		"Entries":      items,
		"I18nEditURLs": editURLs,
		"Keyword":      filter.Keyword,
		"LangFilter":   filter.Lang,
		"CatFilter":    filter.Category,
		"Categories":   categories,
		"Saved":        adminPageSaved(c.Query("saved")),
		"Errored":      adminPageErrText(c, c.Query("errored")),
		// 批量删除的结果条（?done= / ?err=）：与全站列表页同一对键，文案由服务端拼装
		// （受控文本 + 计数）。读侧一律过受控出口 —— ?done= 走 adminPageDone（与写侧共用
		// 模板字面量、整体匹配），?err= / ?errored= 走 adminPageErrText —— 因为**页面不是
		// 可信边界**：手拼一个 ?done=任意文案 就能伪造一条顶着「成功」样式的消息。
		"Done": adminPageDone(c, c.Query("done")),
		"Err":  adminPageErrText(c, c.Query("err")),
	}
	// 语言下拉的供数（字典只读口）：读失败给空列表 —— 页面仍可筛「全部语言」，
	// 只是少了按语言筛的能力，不该因此整页打不开。
	if h.dict != nil {
		if opts, derr := h.dict.ListDictOptions(c.Request.Context(), "language"); derr == nil {
			data["LangOptions"] = opts
		}
	}
	// 分页条：原版只渲染「第 X / Y 页」文字，没有页码链接 —— Total 超过一页时第 2 页起
	// 完全不可达（列表页最要紧的缺陷）。链接与筛选同源，翻页不丢 keyword / lang / category。
	base := shell.FilterBaseURL("/admin/i18n", map[string]string{
		"keyword":  filter.Keyword,
		"lang":     filter.Lang,
		"category": filter.Category,
	})
	for k, v := range shell.BuildPagination(total, page, adminI18nEntryPageSize, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/system/i18n", shell.Prepare(c, data))
}

// adminI18nSaveMissingMsg 保存词条时的缺项判定：返回**具体**缺哪一项的 enums 文案
// （按表单从上到下的顺序报第一项），都不缺时返回空串。
//
// 为什么不继续共用 MsgFieldRequired：那句话只说「有必填项没填」，而这张表单有三行 ——
// 运营得逐个试才知道是哪一行。三条文案的判据相同（都是客户端输入问题），差别只在
// 「说得够不够具体」。空值校验留在 handler 是因为 i18n.SaveEntry 把「空值」与
// 「存储不可用」都当同一条 error 返回，混在一起就没法在归口助手里区分了。
func adminI18nSaveMissingMsg(key, lang, value string) string {
	switch {
	case key == "":
		return adminenums.ErrI18nKeyEmpty
	case lang == "":
		return adminenums.ErrI18nLangEmpty
	case strings.TrimSpace(value) == "":
		return adminenums.ErrI18nValueEmpty
	}
	return ""
}

// adminI18nDeleteMissingMsg 删除词条时的缺项判定（只有 key 与语言两个输入，没有内容项）。
func adminI18nDeleteMissingMsg(key, lang string) string {
	switch {
	case key == "":
		return adminenums.ErrI18nKeyEmpty
	case lang == "":
		return adminenums.ErrI18nLangEmpty
	}
	return ""
}

// I18nEntryEditFragment reads an exact (key, lang) pair for the edit drawer.
func (h *adminI18nEntryHandle) I18nEntryEditFragment(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	key, lang := strings.TrimSpace(c.Query("key")), strings.TrimSpace(c.Query("lang"))
	if key == "" || lang == "" {
		c.Status(http.StatusBadRequest)
		return
	}
	entry, err := i18n.GetEntry(c.Request.Context(), key, lang)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.Status(http.StatusNotFound)
		} else {
			adminErrParam(c, err)
			c.Status(http.StatusInternalServerError)
		}
		return
	}
	c.HTML(http.StatusOK, "admin/system/i18n_edit_form.html", shell.Prepare(c, gin.H{
		"I18nEdit": entry, "I18nEditBack": adminI18nEditBack(c),
	}))
}

func (h *adminI18nEntryHandle) i18nEditFail(c *gin.Context, msg string) {
	if !adminDrawerHX(c) {
		c.Redirect(http.StatusFound, adminI18nBackURL(c, "errored", msg))
		return
	}
	key, lang := strings.TrimSpace(c.PostForm("key")), strings.TrimSpace(c.PostForm("lang"))
	entry, err := i18n.GetEntry(c.Request.Context(), key, lang)
	if err != nil || entry == nil {
		adminDrawerRedirect(c, adminI18nBackURL(c, "errored", msg))
		return
	}
	c.HTML(http.StatusOK, "admin/system/i18n_edit_form.html", shell.Prepare(c, gin.H{
		"I18nEdit": entry, "I18nEditBack": adminI18nEditBack(c), "I18nEditErr": msg, "I18nEditEcho": map[string]string{
			"value": c.PostForm("value"), "category": c.PostForm("category"), "remark": c.PostForm("remark"),
		},
	}))
}

// I18nEntryUpdate only updates an existing pair, unlike SaveEntry's create/upsert path.
func (h *adminI18nEntryHandle) I18nEntryUpdate(c *gin.Context) {
	key, lang := strings.TrimSpace(c.PostForm("key")), strings.TrimSpace(c.PostForm("lang"))
	value := c.PostForm("value")
	if missing := adminI18nSaveMissingMsg(key, lang, value); missing != "" {
		h.i18nEditFail(c, response.TranslateMessage(c, missing))
		return
	}
	err := i18n.UpdateEntry(c.Request.Context(), i18n.Entry{
		Key: key, Lang: lang, Value: value, Category: c.PostForm("category"), Remark: c.PostForm("remark"),
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			h.i18nEditFail(c, response.TranslateMessage(c, adminenums.MsgBadRequest))
		} else {
			h.i18nEditFail(c, adminErrParam(c, err))
		}
		return
	}
	h.markI18nStale(c)
	adminDrawerRedirect(c, adminI18nBackURL(c, "saved", adminI18nEntryIdentity(key, lang)))
}

// I18nEntrySave POST /admin/i18n/save —— 新增或更新一条词条。
//
// 空值校验留在 handler（与同文件其它表单一致：administrators/roles/… 都先查必填再进 service）：
// i18n.SaveEntry 把「key/语言/内容为空」与「存储不可用 / 驱动报错」都当同一条 error 返回，
// 而下游的归口助手（adminErrParam）只放行 admin enums 白名单 —— 两类混在一起没法在那儿区分。
// 不在这里先拦，运营少填一个字段就会看到「操作失败」这种通用提示（任务 1 那种反向缺陷）。
// 拦掉之后，剩下能走到归口助手的只可能是基础设施错误。
func (h *adminI18nEntryHandle) I18nEntrySave(c *gin.Context) {
	key := strings.TrimSpace(c.PostForm("key"))
	lang := strings.TrimSpace(c.PostForm("lang"))
	value := c.PostForm("value")
	if missing := adminI18nSaveMissingMsg(key, lang, value); missing != "" {
		c.Redirect(http.StatusFound, adminI18nBackURL(c, "errored",
			response.TranslateMessage(c, missing)))
		return
	}
	entry := i18n.Entry{
		Key:      key,
		Lang:     lang,
		Value:    value,
		Category: c.PostForm("category"),
		Remark:   c.PostForm("remark"),
		Status:   1,
	}
	if err := i18n.SaveEntry(c.Request.Context(), entry); err != nil {
		// 只到这里才可能是基础设施错误：走页面路径归口（业务文案原样，原文进日志）。
		c.Redirect(http.StatusFound, adminI18nBackURL(c, "errored", adminErrParam(c, err)))
		return
	}
	// 词条烘在产物字节里，改完必须标记站点待重建（否则站点停在旧文案且无报错）。
	h.markI18nStale(c)
	c.Redirect(http.StatusFound, adminI18nBackURL(c, "saved", adminI18nEntryIdentity(key, lang)))
}

// I18nEntryDelete POST /admin/i18n/delete —— 删除一条词条。
// 删除后构建期回退到组件包内的中文兜底（可见降级，不是空白）。
//
// 空值校验同 I18nEntrySave：DeleteEntry 的空 key/语言与存储不可用是两类错误，
// 前者必须让运营看见「哪个字段没填」，后者才走归口文案。
func (h *adminI18nEntryHandle) I18nEntryDelete(c *gin.Context) {
	key := strings.TrimSpace(c.PostForm("key"))
	lang := strings.TrimSpace(c.PostForm("lang"))
	if missing := adminI18nDeleteMissingMsg(key, lang); missing != "" {
		c.Redirect(http.StatusFound, adminI18nBackURL(c, "errored",
			response.TranslateMessage(c, missing)))
		return
	}
	if err := i18n.DeleteEntry(c.Request.Context(), key, lang); err != nil {
		c.Redirect(http.StatusFound, adminI18nBackURL(c, "errored", adminErrParam(c, err)))
		return
	}
	// 删词条会让构建期回退到组件包内的中文兜底 —— 产物字节同样变了，必须标记待重建。
	h.markI18nStale(c)
	c.Redirect(http.StatusFound, adminI18nBackURL(c, "saved", adminI18nDeletedIdentity(key, lang)))
}

// adminI18nEntryKeySeparator 批量删除的复合键分隔符（"key|lang"）。
//
// 词条的唯一标识是 (key, lang) 二元组 —— 只带 key 会把其它语言下的同名词条
// 一起删掉（删 zh-CN 顺手删了 en-US）。分隔符选 "|"：key 是点分标识、
// lang 是 BCP-47（zh-CN），两者都不含它。万一真有人建了带 "|" 的 key，
// Cut 出来 lang 为空 → 计跳过，绝不会误删别的行（fail-safe，不是 fail-open）。
const adminI18nEntryKeySeparator = "|"

// I18nEntriesBulkDelete POST /admin/i18n/bulk-delete —— 批量删除词条。
//
// 逐条走同一条单条删除路径（同一个 i18n.DeleteEntry）：某一条失败（已不存在、
// key/lang 非法）只计入跳过数，整批不中断 —— 整批回滚会让用户以为「一条都没删」，
// 然后反复重试。结果按「已删除 N 条 / 跳过 M 条」回带，筛选与页码原样保留。
func (h *adminI18nEntryHandle) I18nEntriesBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是受控错误（理由见 adminBulkResultURL 上方）：文案走 shell 的受控出口，不直传原文。
		c.Redirect(http.StatusFound, adminI18nBackURL(c, "err", shell.BulkIDsFacingText(c, berr)))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		key, lang, ok := strings.Cut(strings.TrimSpace(raw), adminI18nEntryKeySeparator)
		key, lang = strings.TrimSpace(key), strings.TrimSpace(lang)
		if !ok || key == "" || lang == "" {
			skipped++
			continue
		}
		if err := i18n.DeleteEntry(c.Request.Context(), key, lang); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	// 单条删除的失效在这里按批只发一次：逐条调会把"全站标记"重复 N 遍，
	// 而它标记的对象是同一批页面（结果等价，代价随批量线性放大）。
	if deleted > 0 {
		h.markI18nStale(c)
	}
	// 有跳过就进 ?err=（警告条更显眼，用户下次会去看剩下那些）；全成功才进 ?done=。
	msg := adminI18nBulkDeleteResult(c, deleted, skipped)
	if skipped > 0 {
		c.Redirect(http.StatusFound, adminI18nBackURL(c, "err", msg))
		return
	}
	c.Redirect(http.StatusFound, adminI18nBackURL(c, "done", msg))
}

// adminI18nBulkDeleteResult 批量删除的结果文案：成功几个、跳过几个都要说清楚
// （只报「操作完成」会把部分成功静默成全部成功，用户不会再去看剩下那几条）。
func adminI18nBulkDeleteResult(c *gin.Context, deleted, skipped int) string {
	switch {
	case deleted == 0 && skipped == 0:
		return adminBulkTextOf(c, adminI18nBulkNoneSelected)
	case skipped == 0:
		return fmt.Sprintf(adminBulkTextOf(c, adminI18nBulkAllDeleted), strconv.Itoa(deleted))
	case deleted == 0:
		return fmt.Sprintf(adminBulkTextOf(c, adminI18nBulkAllSkipped), strconv.Itoa(skipped))
	default:
		return fmt.Sprintf(adminBulkTextOf(c, adminI18nBulkPartial), strconv.Itoa(deleted), strconv.Itoa(skipped))
	}
}

// adminI18nEditBack carries the list filters from the edit GET into the update POST.
func adminI18nEditBack(c *gin.Context) map[string]string {
	back := map[string]string{
		"_keyword": c.PostForm("_keyword"), "_lang": c.PostForm("_lang"),
		"_category": c.PostForm("_category"), "_page": c.PostForm("_page"),
	}
	if c.Request.Method == http.MethodGet {
		back["_keyword"] = c.Query("keyword")
		back["_lang"] = c.Query("filter_lang")
		back["_category"] = c.Query("category")
		back["_page"] = c.Query("page")
	}
	return back
}

// adminI18nBackURL 回列表并带上筛选与提示（只回填站内相对路径，避免开放重定向）。
func adminI18nBackURL(c *gin.Context, hintKey, hintValue string) string {
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
