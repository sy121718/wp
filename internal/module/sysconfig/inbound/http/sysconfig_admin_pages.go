package sysconfighttp

// sysconfig_admin_pages.go — 系统设置页（全局默认值）。
//
// 页面前缀用 /admin/system：/admin/settings 已被**站点设置**（工程级，projects.settings）占用，
// 两者是不同的东西 —— 站点设置改的是「这一个站点」，本页改的是「这台系统的全局默认」。
//
// 一个必须处理的陷阱：SetGroup 是**整组替换**，而页面上只编辑部分键。i18n 组里还有
// lang_url_codes（短码覆盖表，属于「短码撞车时才配」的专家项，刻意不暴露给运营）——
// 若把表单值直接整组写回，保存一次就会把这个键**冲掉**（表现是站点 URL 里语言短码
// 悄悄回到默认）。所以保存前先读出原组、只覆盖本页暴露的键、再整组写回（见 mergeGroup）。

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	sysconfigenums "go_wp/internal/module/sysconfig/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// 分组键与「本页负责的键」白名单。
//
// 键名与 pkg/i18n 的读取侧一致（default_lang / site_lang_url_mode），写进常量是为了
// 让「页面暴露了哪些键」这件事只有一个可读清单：mergeGroup 只覆盖这里列出的键，
// 其余原样带回 —— 增加一个可编辑键 = 在这里加一行 + 模板加一个字段。
const (
	groupI18N  = "i18n"
	groupTrade = "trade"
)

var i18nEditableKeys = []string{"default_lang", "site_lang_url_mode"}
var tradeEditableKeys = []string{"default_country", "default_currency"}

// 语言 URL 方案的三选一取值（与 pkg/i18n 的纯谓词一致）。
var siteLangURLModeValues = []string{"default_plain", "all_prefix", "off"}

// dictReader 本模块页面对字典的只读需求。
//
// 不并进 contract：契约只放**跨模块可见**的东西，而字典下拉只有本模块的页面用。
// 注册期断言一次（装配缺陷 fail-fast），运行期不再断言。
type dictReader interface {
	ListDictOptions(ctx context.Context, dictType string) ([]sysconfigdto.DictOption, error)
	ListCountryOptions(ctx context.Context, lang string) ([]sysconfigdto.CountryOption, error)
}

// AdminHandle 系统设置页处理器。
//
// 导出（而非包内小写）是为了让集成测试能在**不起路由**的前提下直接渲染与保存：
// 页面路由挂着 Casbin 中间件，而中间件要的是一套完整的策略环境；测试真正想验的是
// 「模板渲染 / 模板与数据的键是否对得上 / 整组替换有没有冲掉未暴露的键」，
// 与鉴权无关（鉴权由 permission 包的既有测试覆盖）。
type AdminHandle struct {
	svc  sysconfigcontract.Service
	dict dictReader
}

// NewAdminHandle 构造系统设置页处理器；svc 必须实现字典读取口（装配缺陷 fail-fast）。
func NewAdminHandle(svc sysconfigcontract.Service) *AdminHandle {
	dict, ok := svc.(dictReader)
	if !ok {
		panic("sysconfig: 注入的服务未实现字典读取口（ListDictOptions / ListCountryOptions）")
	}
	return &AdminHandle{svc: svc, dict: dict}
}

// groupView 页面上的一组配置（值 + 版本 + 可选键清单）。
type groupView struct {
	Key     string
	Version int64
	Data    map[string]any
}

// valueOf 读组内某个键的字符串值（缺失返回空串）。
func (g groupView) valueOf(key string) string {
	if v, ok := g.Data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// SystemPage GET /admin/system：渲染系统设置页。
func (h *AdminHandle) SystemPage(c *gin.Context) {
	ctx := c.Request.Context()
	i18nGroup, errText := h.loadGroup(c, groupI18N)
	tradeGroup, terr := h.loadGroup(c, groupTrade)
	if errText == "" && terr != "" {
		errText = terr
	}
	langs, lerr := h.dict.ListDictOptions(ctx, "language")
	currencies, cerr := h.dict.ListDictOptions(ctx, "currency")
	countries, coerr := h.dict.ListCountryOptions(ctx, response.RequestLanguage(c))
	if errText == "" {
		switch {
		case lerr != nil:
			errText = sysconfigErrText(c, lerr)
		case cerr != nil:
			errText = sysconfigErrText(c, cerr)
		case coerr != nil:
			errText = sysconfigErrText(c, coerr)
		}
	}
	doneText := ""
	if c.Query("done") == "1" {
		doneText = shell.TranslateFor(c)("admin.system.saved", "系统设置已保存（全局默认值立即对读取方生效）")
	}
	c.HTML(http.StatusOK, "admin/system/settings", shell.Prepare(c, gin.H{
		"I18N":            i18nGroup,
		"Trade":           tradeGroup,
		"LangOptions":     langs,
		"CurrencyOptions": currencies,
		"CountryOptions":  countries,
		"ModeOptions":     siteLangURLModeValues,
		"Err":             errText,
		"Done":            doneText,
	}))
}

// SystemSave POST /admin/system/save：保存两组（PRG 回本页）。
func (h *AdminHandle) SystemSave(c *gin.Context) {
	ctx := c.Request.Context()
	lang := strings.TrimSpace(c.PostForm("defaultLang"))
	mode := strings.TrimSpace(c.PostForm("siteLangURLMode"))
	country := strings.TrimSpace(c.PostForm("defaultCountry"))
	currency := strings.TrimSpace(c.PostForm("defaultCurrency"))
	i18nVersion := parseVersion(c.PostForm("i18nVersion"))
	tradeVersion := parseVersion(c.PostForm("tradeVersion"))

	if msg := h.validateOptions(c, lang, mode, country, currency); msg != "" {
		sysconfigRedirect(c, msg)
		return
	}

	i18nGroup, errText := h.loadGroup(c, groupI18N)
	if errText != "" {
		sysconfigRedirect(c, errText)
		return
	}
	tradeGroup, errText := h.loadGroup(c, groupTrade)
	if errText != "" {
		sysconfigRedirect(c, errText)
		return
	}
	// 版本以**页面提交的**为准（乐观锁前置条件），而不是刚读出来的当前值 ——
	// 后者等于每次都拿最新版本去写，冲突永远不会被发现。
	if _, err := h.svc.SetGroups(ctx, &sysconfigdto.SetGroupsReq{
		Groups: []sysconfigdto.SetGroupReq{
			{GroupKey: groupI18N, Version: i18nVersion, Data: mergeGroup(i18nGroup.Data, i18nEditableKeys, map[string]string{
				"default_lang":       lang,
				"site_lang_url_mode": mode,
			})},
			{GroupKey: groupTrade, Version: tradeVersion, Data: mergeGroup(tradeGroup.Data, tradeEditableKeys, map[string]string{
				"default_country":  country,
				"default_currency": currency,
			})},
		},
		UpdateBy: int64(shell.CurrentUserID(c)),
	}); err != nil {
		logger.Scene("sysconfig").With("path", c.Request.URL.Path).Error(err, "系统设置保存失败")
		sysconfigRedirect(c, sysconfigErrText(c, err))
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/system?done=1")
}

// loadGroup 读一组配置；组不存在返回可读提示（本页不凭空建组）。
func (h *AdminHandle) loadGroup(c *gin.Context, key string) (groupView, string) {
	ctx := c.Request.Context()
	g, err := h.svc.GetGroup(ctx, key)
	if err != nil {
		if errors.Is(err, sysconfigcontract.ErrGroupNotFound) {
			return groupView{Key: key, Data: map[string]any{}},
				shell.TranslateFor(c)("admin.system.group_missing", "配置分组缺失，请先执行数据库迁移（make migrate）后再打开本页")
		}
		return groupView{Key: key, Data: map[string]any{}}, sysconfigErrText(c, err)
	}
	return groupView{Key: g.Key, Version: g.Version, Data: g.Data}, ""
}

// mergeGroup 合并：以**原组数据**为底，只覆盖本页暴露的键。
//
// 这是「整组替换 + 部分编辑」这个陷阱的正面解法（见文件头）：未暴露的键
// （i18n 组的 lang_url_codes、trade 组将来可能加的键）原样带回，不会被保存冲掉。
// 副作用是「页面上删不掉某个键」—— 那本就该由迁移或专门的专家界面做。
func mergeGroup(current map[string]any, editable []string, values map[string]string) map[string]any {
	merged := make(map[string]any, len(current)+len(editable))
	for k, v := range current {
		merged[k] = v
	}
	for _, k := range editable {
		if v, ok := values[k]; ok {
			merged[k] = v
		}
	}
	return merged
}

// validateOptions 校验提交值：必须在字典/枚举里（防伪造，不靠前端下拉）。
func (h *AdminHandle) validateOptions(c *gin.Context, lang, mode, country, currency string) string {
	ctx := c.Request.Context()
	if lang == "" || country == "" || currency == "" {
		return shell.TranslateFor(c)(sysconfigenums.ErrInvalidParam, "请把四个设置项都选上")
	}
	if !contains(siteLangURLModeValues, mode) {
		return shell.TranslateFor(c)(sysconfigenums.ErrInvalidParam, "请把四个设置项都选上")
	}
	if opts, err := h.dict.ListDictOptions(ctx, "language"); err == nil && !hasCode(opts, lang) {
		return shell.TranslateFor(c)("admin.system.err.lang_unknown", "选择的默认语言不在字典里（可能已被停用），请重新选择")
	}
	if opts, err := h.dict.ListDictOptions(ctx, "currency"); err == nil && !hasCode(opts, currency) {
		return shell.TranslateFor(c)("admin.system.err.currency_unknown", "选择的默认货币不在字典里（可能已被停用），请重新选择")
	}
	if opts, err := h.dict.ListCountryOptions(ctx, response.RequestLanguage(c)); err == nil && !hasCode2(opts, country) {
		return shell.TranslateFor(c)("admin.system.err.country_unknown", "选择的默认国家不在字典里（可能已被停用），请重新选择")
	}
	return ""
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func hasCode(opts []sysconfigdto.DictOption, code string) bool {
	for _, o := range opts {
		if o.Code == code {
			return true
		}
	}
	return false
}

func hasCode2(opts []sysconfigdto.CountryOption, code string) bool {
	for _, o := range opts {
		if o.Code == code {
			return true
		}
	}
	return false
}
