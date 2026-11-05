package sysconfighttp

import (
	"context"
	"errors"
	"go_wp/internal/middleware/builtin"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	sysconfigenums "go_wp/internal/module/sysconfig/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

const (
	groupI18N  = "i18n"
	groupTrade = "trade"
)

var i18nEditableKeys = []string{"default_lang", "site_lang_url_mode"}
var tradeEditableKeys = []string{"default_country", "default_currency"}

var siteLangURLModeValues = []string{"default_plain", "all_prefix", "off"}

type dictReader interface {
	ListDictOptions(ctx context.Context, dictType string) ([]sysconfigdto.DictOption, error)
	ListCountryOptions(ctx context.Context, lang string) ([]sysconfigdto.CountryOption, error)
}

type AdminHandle struct {
	svc  sysconfigcontract.Service
	dict dictReader
}

func NewAdminHandle(svc sysconfigcontract.Service) *AdminHandle {
	dict, ok := svc.(dictReader)
	if !ok {
		panic("sysconfig: 注入的服务未实现字典读取口（ListDictOptions / ListCountryOptions）")
	}
	return &AdminHandle{svc: svc, dict: dict}
}

type groupView struct {
	Key     string
	Version int64
	Data    map[string]any
}

func (g groupView) valueOf(key string) string {
	if v, ok := g.Data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

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
			errText = response.MessageOf(c, "sysconfig", lerr)
		case cerr != nil:
			errText = response.MessageOf(c, "sysconfig", cerr)
		case coerr != nil:
			errText = response.MessageOf(c, "sysconfig", coerr)
		}
	}
	pageview.Render(c, &sysconfigdto.SystemSettingsPageResp{
		Title:           i18n.TranslateFunc(response.RequestLanguage(c))(sysconfigenums.AdminSystemTitle, "系统设置"),
		I18N:            i18nGroup,
		Trade:           tradeGroup,
		LangOptions:     toAnySlice(langs),
		CurrencyOptions: toAnySlice(currencies),
		CountryOptions:  toAnySlice(countries),
		ModeOptions:     toAnySlice(siteLangURLModeValues),
		LoadErr:         errText,
	})
}

// APISystemSave 接收标准表单，成功后由 Jet 返回统一成功片段。
func (h *AdminHandle) APISystemSave(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req sysconfigdto.SystemSaveReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, "sysconfig", err)
		return
	}
	ctx := c.Request.Context()
	lang := strings.TrimSpace(req.DefaultLang)
	mode := strings.TrimSpace(req.SiteLangURLMode)
	country := strings.TrimSpace(req.DefaultCountry)
	currency := strings.TrimSpace(req.DefaultCurrency)
	i18nVersion := req.I18NVersion
	tradeVersion := req.TradeVersion

	if status, msg := h.validateOptions(c, lang, mode, country, currency); msg != "" {
		pageview.FailStatus(c, status, "sysconfig", errors.New(msg))
		return
	}

	i18nGroup, errText := h.loadGroup(c, groupI18N)
	if errText != "" {
		pageview.FailStatus(c, http.StatusInternalServerError, "sysconfig", errors.New(errText))
		return
	}
	tradeGroup, errText := h.loadGroup(c, groupTrade)
	if errText != "" {
		pageview.FailStatus(c, http.StatusInternalServerError, "sysconfig", errors.New(errText))
		return
	}

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
		UpdateBy: int64(builtin.CurrentUserID(c)),
	}); err != nil {
		status := http.StatusInternalServerError
		if response.IsBusinessError(err) {
			status = http.StatusConflict
		}
		pageview.FailStatus(c, status, "sysconfig", err)
		return
	}
	pageview.Success(c)
}

func (h *AdminHandle) loadGroup(c *gin.Context, key string) (groupView, string) {
	ctx := c.Request.Context()
	g, err := h.svc.GetGroup(ctx, key)
	if err != nil {
		if errors.Is(err, sysconfigcontract.ErrGroupNotFound) {
			return groupView{Key: key, Data: map[string]any{}},
				i18n.TranslateFunc(response.RequestLanguage(c))(sysconfigenums.AdminSystemGroupMissing, "配置分组缺失，请先执行数据库迁移（make migrate）后再打开本页")
		}
		return groupView{Key: key, Data: map[string]any{}}, response.MessageOf(c, "sysconfig", err)
	}
	return groupView{Key: g.Key, Version: g.Version, Data: g.Data}, ""
}

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

func (h *AdminHandle) validateOptions(c *gin.Context, lang, mode, country, currency string) (int, string) {
	ctx := c.Request.Context()
	t := i18n.TranslateFunc(response.RequestLanguage(c))
	if lang == "" || country == "" || currency == "" || !contains(siteLangURLModeValues, mode) {
		return http.StatusBadRequest, t(sysconfigenums.ErrInvalidParam, "请把四个设置项都选上")
	}
	langs, err := h.dict.ListDictOptions(ctx, "language")
	if err != nil {
		return http.StatusInternalServerError, response.MessageOf(c, "sysconfig", err)
	}
	if !hasCode(langs, lang) {
		return http.StatusBadRequest, t(sysconfigenums.AdminSystemErrLangUnknown, "选择的默认语言不在字典里（可能已被停用），请重新选择")
	}
	currencies, err := h.dict.ListDictOptions(ctx, "currency")
	if err != nil {
		return http.StatusInternalServerError, response.MessageOf(c, "sysconfig", err)
	}
	if !hasCode(currencies, currency) {
		return http.StatusBadRequest, t(sysconfigenums.AdminSystemErrCurrencyUnknown, "选择的默认货币不在字典里（可能已被停用），请重新选择")
	}
	countries, err := h.dict.ListCountryOptions(ctx, response.RequestLanguage(c))
	if err != nil {
		return http.StatusInternalServerError, response.MessageOf(c, "sysconfig", err)
	}
	if !hasCode2(countries, country) {
		return http.StatusBadRequest, t(sysconfigenums.AdminSystemErrCountryUnknown, "选择的默认国家不在字典里（可能已被停用），请重新选择")
	}
	return http.StatusOK, ""
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

func toAnySlice[T any](in []T) []any {
	out := make([]any, 0, len(in))
	for i := range in {
		out = append(out, in[i])
	}
	return out
}
