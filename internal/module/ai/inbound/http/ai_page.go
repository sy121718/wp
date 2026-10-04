// ai_page.go — 后台「设置 → 模型」页的处理器（整页 + HTMX 片段）。
//
// 分档口径（见 ai_page_util.go）：
//
//	· 供应商级写操作（保存 / 删除 / 启停）→ PRG 整页重定向：这些动作会改变卡片集合
//	  与版本号，整页重渲染最不容易出现「片段与页面状态不同步」；
//	· 模型目录级操作（保存 / 恢复默认 / 获取可用 / 增行 / 删行）→ HTMX 片段替换目录区：
//	  目录是页面上最高频的编辑区，每次整页刷新会丢掉用户其他输入。
//
// 失败时片段用**用户提交的行**重渲染（保留输入），成功时用库里的行（展示归一后的结果）。
//
// 提示的一律口径：页面内部只流转 **i18n key**（不是译文）——
//
//	· HTMX 片段：渲染时经 facingTextParams 翻成当前语言（含 {n}/{m} 填充）；
//	· PRG 回执：把 key（与计数参数）写进 query，由 facingQuery 在读侧翻译。
//
// 这样「关掉 JS 的原生 POST」与「HTMX 片段」两条路径共用同一条 key，提示不会在任一条上丢失。
package aihttp

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	aicontract "go_wp/internal/module/ai/contract"
	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/internal/web/shell"
)

// 模板名与页面路径。
const (
	// modelsTemplate 模型目录片段（每张供应商卡片里那块）。
	modelsTemplate = "admin/ai/provider_models"
	// pickerTemplate 候选弹窗片段（插进 #ai-picker-host）。
	pickerTemplate = "admin/ai/provider_picker"
	// pagePath 统一入口（532 合并后模型与会话共用一个页面），也是各写操作 PRG 的回跳地址。
	pagePath = "/admin/ai/sessions"
)

// PageHandle 后台页面的处理器。
type PageHandle struct {
	svc aicontract.AIService
}

// NewPageHandle 构造页面处理器。
func NewPageHandle(svc aicontract.AIService) *PageHandle { return &PageHandle{svc: svc} }

// ProvidersPage GET /admin/ai/providers → 302 到统一入口的模型标签。
//
// 两个菜单合成一个（532）之后这一页不再有独立模板：留着 302 是为了旧书签与外部链接，
// 而不是「还有第二条渲染路径」—— 页面本体只有 admin/ai/sessions.html 一份。
func (h *PageHandle) ProvidersPage(c *gin.Context) {
	c.Redirect(http.StatusFound, pagePath+"?tab="+sessionTabModels)
}

// ProviderSave POST /admin/ai/providers/save → 新建 / 更新供应商（PRG）。
func (h *PageHandle) ProviderSave(c *gin.Context) {
	req := &aidto.SaveProviderReq{
		ID:          parseInt64(c.PostForm("providerId")),
		ProviderKey: c.PostForm("providerKey"),
		DisplayName: c.PostForm("displayName"),
		BaseURL:     c.PostForm("baseUrl"),
		Protocol:    c.PostForm("protocol"),
		APIKey:      c.PostForm("apiKey"),
		Status:      statusPtr(c.PostForm("status")),
		Sort:        int(parseInt64(c.PostForm("sort"))),
		Version:     parseInt64(c.PostForm("version")),
		UpdateBy:    userID(c),
	}
	applyPresetDefaults(req)
	if _, err := h.svc.SaveProvider(c.Request.Context(), req); err != nil {
		h.redirectWith(c, "err", aiErrKey(err), nil)
		return
	}
	h.redirectWith(c, "done", aienums.MsgSaved, nil)
}

// applyPresetDefaults 用内置预设补齐「第三方模型提供商」tab 留空的字段。
//
// 只补展示名与协议两处：预设 tab 的表单刻意不强制用户填它们（下拉里选一家就够了），
// 未登记协议的家预设协议本身为空 → 兜底后仍为空，仍由 service 的协议校验拒绝，
// 不静默回落成默认协议。
//
// **不补 base_url**：留空是有含义的（「用该提供商的默认地址」，见卡片里的 hint），
// 在这里填死会把「跟随预设」变成「拷了一份快照」，预设更新再也影响不到它。
func applyPresetDefaults(req *aidto.SaveProviderReq) {
	if req == nil {
		return
	}
	preset, ok := aiservice.BuiltinPreset(req.ProviderKey)
	if !ok {
		return
	}
	if strings.TrimSpace(req.DisplayName) == "" {
		req.DisplayName = preset.DisplayName
	}
	if strings.TrimSpace(req.Protocol) == "" {
		req.Protocol = preset.Protocol
	}
}

// ProviderDelete POST /admin/ai/providers/delete → 删除供应商（PRG）。
func (h *PageHandle) ProviderDelete(c *gin.Context) {
	id := parseInt64(c.PostForm("providerId"))
	if err := h.svc.DeleteProvider(c.Request.Context(), id); err != nil {
		h.redirectWith(c, "err", aiErrKey(err), nil)
		return
	}
	h.redirectWith(c, "done", aienums.MsgDeleted, nil)
}

// ProviderStatus POST /admin/ai/providers/status → 启停供应商（PRG）。
func (h *PageHandle) ProviderStatus(c *gin.Context) {
	req := &aidto.SetStatusReq{
		ID:       parseInt64(c.PostForm("providerId")),
		Status:   int(parseInt64(c.PostForm("status"))),
		Version:  parseInt64(c.PostForm("version")),
		UpdateBy: userID(c),
	}
	if _, err := h.svc.SetProviderStatus(c.Request.Context(), req); err != nil {
		h.redirectWith(c, "err", aiErrKey(err), nil)
		return
	}
	h.redirectWith(c, "done", aienums.MsgStatusChanged, nil)
}

// ModelsSave POST /admin/ai/providers/models/save → 整组保存模型目录（HTMX 片段）。
func (h *PageHandle) ModelsSave(c *gin.Context) {
	providerID := parseInt64(c.PostForm("providerId"))
	version := parseInt64(c.PostForm("version"))
	rows := parseModelRows(c)
	provider, err := h.svc.SaveModels(c.Request.Context(), &aidto.SaveModelsReq{
		ProviderID: providerID,
		Version:    version,
		Models:     rows,
		UpdateBy:   userID(c),
	})
	if err != nil {
		h.modelsFailure(c, providerID, rows, err)
		return
	}
	h.modelsSuccess(c, *provider, aienums.MsgModelsSaved, nil)
}

// ModelsRestore POST /admin/ai/providers/models/restore → 恢复内置默认模型（HTMX 片段）。
func (h *PageHandle) ModelsRestore(c *gin.Context) {
	providerID := parseInt64(c.PostForm("providerId"))
	provider, err := h.svc.RestoreDefaultModels(c.Request.Context(), &aidto.ProviderActionReq{
		ProviderID: providerID,
		Version:    parseInt64(c.PostForm("version")),
		UpdateBy:   userID(c),
	})
	if err != nil {
		h.modelsFailure(c, providerID, parseModelRows(c), err)
		return
	}
	h.modelsSuccess(c, *provider, aienums.MsgModelsRestored, nil)
}

// ModelsFetch POST /admin/ai/providers/models/fetch → 拉取可用模型并合并（HTMX 片段）。
//
// 回执带计数（拉回 {n} 个、新增 {m} 个）：计数走文案参数，不拼进中文串 ——
// 拼出来的串不在白名单里，关掉 JS 的 PRG 路径会把提示整条丢掉。
func (h *PageHandle) ModelsFetch(c *gin.Context) {
	providerID := parseInt64(c.PostForm("providerId"))
	result, err := h.svc.FetchAvailableModels(c.Request.Context(), &aidto.ProviderActionReq{
		ProviderID: providerID,
		Version:    parseInt64(c.PostForm("version")),
		UpdateBy:   userID(c),
	})
	if err != nil {
		h.modelsFailure(c, providerID, parseModelRows(c), err)
		return
	}
	if result == nil {
		h.modelsSuccess(c, aidto.Provider{}, aienums.MsgModelsFetched, nil)
		return
	}
	h.modelsSuccess(c, *result.Provider, aienums.MsgModelsFetchedDetail, map[string]string{
		"n": strconv.Itoa(result.Fetched),
		"m": strconv.Itoa(len(result.Added)),
	})
}

// ModelsCandidates POST /admin/ai/providers/models/candidates → 只拉候选，整页渲染 + 候选弹窗。
//
// 为什么回整页而不是 htmx 片段：候选弹窗挂在页面上，片段换入后还得再触发打开（多一层时序）；
// 整页渲染里弹窗自带 data-modal-auto-open，控件扫描时自动打开。
// 这次请求**不写库**，卡片状态不因它变化，所以整页重渲染没有副作用。
func (h *PageHandle) ModelsCandidates(c *gin.Context) {
	providerID := parseInt64(c.PostForm("providerId"))
	result, err := h.svc.FetchModelCandidates(c.Request.Context(), &aidto.ProviderActionReq{
		ProviderID: providerID,
		Version:    parseInt64(c.PostForm("version")),
		UpdateBy:   userID(c),
	})
	if err != nil {
		h.redirectWith(c, "err", aiErrKey(err), nil)
		return
	}
	if result == nil || result.Provider == nil {
		h.redirectWith(c, "err", aienums.ErrProviderNotFound, nil)
		return
	}
	picker := gin.H{
		"Provider":   result.Provider,
		"Candidates": result.Candidates,
		"Existing":   idSet(result.Existing),
	}
	// 两个页面合成一个（532 的菜单合并）之后，弹窗不再靠「重渲染整页 + data-modal-auto-open」，
	// 而是作为片段插进模型标签底部的 #ai-picker-host —— 整页重渲染会把用户在同一页其它卡片里
	// 刚填的密钥冲掉。非 htmx 请求（没有 JS）退回整页入口，至少不丢功能。
	page := shell.Prepare(c, gin.H{
		"title":  shell.TranslateFor(c)(aienums.AdminLLMTitle, "大模型管理"),
		"Picker": picker,
	})
	if hxFragment(c, pickerTemplate, page) {
		return
	}
	c.Redirect(http.StatusFound, pagePath+"?tab="+sessionTabModels+"&picker="+strconv.FormatInt(providerID, 10))
}

// ModelsAppend POST /admin/ai/providers/models/append → 把弹窗勾中的模型追加进目录（PRG）。
//
// 追加语义而不是整组覆盖：弹窗只交回「这次勾中的 id」，用覆盖会把这些之外的行全删掉。
// 已存在的 id 跳过 —— 弹窗里那几行是禁用状态，但请求可以被绕过，服务端仍然要判。
func (h *PageHandle) ModelsAppend(c *gin.Context) {
	providerID := parseInt64(c.PostForm("providerId"))
	selected := normalizeSelectedIDs(c.PostFormArray("modelIds"))
	if len(selected) == 0 {
		h.redirectWith(c, "err", aienums.ErrNoModelSelected, nil)
		return
	}
	if len(selected) > maxModelAppend {
		h.redirectWith(c, "err", aienums.ErrInvalidParam, nil)
		return
	}
	provider, err := h.svc.GetProvider(c.Request.Context(), providerID)
	if err != nil || provider == nil {
		h.redirectWith(c, "err", aiErrKey(err), nil)
		return
	}
	have := idSet(modelIDs(provider.Models))
	rows := append([]aidto.ModelEntry{}, provider.Models...)
	added := 0
	for _, id := range selected {
		if have[id] {
			continue
		}
		have[id] = true
		rows = append(rows, aidto.ModelEntry{ID: id, DisplayName: id, InputTypes: []string{aienums.InputTypeText}})
		added++
	}
	if added == 0 {
		// 勾中的全在目录里（或与库里重复）：没有产生任何新增，回一条能看懂的提示，
		// 而不是「操作成功」—— 那会让用户以为目录多了一批模型。
		h.redirectWith(c, "err", aienums.ErrNoModelSelected, nil)
		return
	}
	if _, serr := h.svc.SaveModels(c.Request.Context(), &aidto.SaveModelsReq{
		ProviderID: providerID,
		Version:    provider.Version,
		Models:     rows,
		UpdateBy:   userID(c),
	}); serr != nil {
		h.redirectWith(c, "err", aiErrKey(serr), nil)
		return
	}
	h.redirectWith(c, "done", aienums.MsgModelsAppended, map[string]string{"n": strconv.Itoa(added)})
}

// maxModelAppend 一次可追加的模型上限：请求方不该决定服务端的循环次数
// （候选最多几百条，2000 是「正常用量不可能碰到、畸形请求早失败」的界线）。
const maxModelAppend = 2000

// normalizeSelectedIDs 归一提交的模型 id：去空白、丢空串、按首次出现去重（保持提交顺序）。
func normalizeSelectedIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// modelIDs 取目录里各行的 id。
func modelIDs(models []aidto.ModelEntry) []string {
	out := make([]string, 0, len(models))
	for i := range models {
		out = append(out, models[i].ID)
	}
	return out
}

// idSet 把 id 列表转成集合（候选弹窗标「已在目录」、追加时判重都用它）。
func idSet(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// ModelsRowAdd POST /admin/ai/providers/models/row/add → 追加一个空行（HTMX 片段，不落库）。
func (h *PageHandle) ModelsRowAdd(c *gin.Context) {
	providerID := parseInt64(c.PostForm("providerId"))
	rows := append(parseModelRows(c), aidto.ModelEntry{InputTypes: []string{aienums.InputTypeText}})
	h.renderRows(c, providerID, rows, "", nil, false)
}

// ModelsRowDelete POST /admin/ai/providers/models/row/delete → 删除第 rowIndex 行
// （HTMX 片段，不落库；行序号是渲染时编的，服务端按提交内容整体重渲染）。
func (h *PageHandle) ModelsRowDelete(c *gin.Context) {
	providerID := parseInt64(c.PostForm("providerId"))
	rows := parseModelRows(c)
	index := int(parseInt64(c.PostForm("rowIndex")))
	if index >= 0 && index < len(rows) {
		rows = append(rows[:index], rows[index+1:]...)
	}
	h.renderRows(c, providerID, rows, "", nil, false)
}

// renderRows 用给定行重渲染目录区（增删行路径；失败则整页重定向）。
func (h *PageHandle) renderRows(c *gin.Context, providerID int64, rows []aidto.ModelEntry, noticeKey string, params map[string]string, isErr bool) {
	provider, err := h.svc.GetProvider(c.Request.Context(), providerID)
	if err != nil || provider == nil {
		h.redirectWith(c, "err", aiErrKey(err), nil)
		return
	}
	if hxFragment(c, modelsTemplate, h.modelsData(c, *provider, rows, noticeKey, params, isErr)) {
		return
	}
	h.redirectWith(c, redirectSlot(isErr), noticeKey, params)
}

// modelsSuccess 成功路径：用库里的行（归一后的结果）+ 提示渲染目录区。
func (h *PageHandle) modelsSuccess(c *gin.Context, provider aidto.Provider, noticeKey string, params map[string]string) {
	if hxFragment(c, modelsTemplate, h.modelsData(c, provider, provider.Models, noticeKey, params, false)) {
		return
	}
	h.redirectWith(c, "done", noticeKey, params)
}

// modelsFailure 失败路径：用**用户提交的行**渲染（保留输入）+ 错误提示。
//
// 版本冲突时也要走到这里：把用户刚填的内容丢掉换成库里的旧值，是最让人恼火的失败方式。
func (h *PageHandle) modelsFailure(c *gin.Context, providerID int64, rows []aidto.ModelEntry, err error) {
	key := aiErrKey(err)
	provider, gerr := h.svc.GetProvider(c.Request.Context(), providerID)
	if gerr != nil || provider == nil {
		h.redirectWith(c, "err", key, nil)
		return
	}
	if hxFragment(c, modelsTemplate, h.modelsData(c, *provider, rows, key, nil, true)) {
		return
	}
	h.redirectWith(c, "err", key, nil)
}

// modelsData 目录区的模板数据：Provider 提供版本号等库内事实，Rows 提供要渲染的行。
//
// noticeKey 是 i18n key（不是译文）；这里翻成当前语言并填充 {n}/{m} 后才交给模板。
func (h *PageHandle) modelsData(c *gin.Context, provider aidto.Provider, rows []aidto.ModelEntry, noticeKey string, params map[string]string, isErr bool) gin.H {
	provider.Models = rows
	return shell.Prepare(c, gin.H{
		"Provider":        provider,
		"Rows":            modelRows(rows),
		"ModelsNotice":    facingTextParams(c, noticeKey, params),
		"ModelsNoticeErr": isErr,
	})
}

// redirectWith 带一次性提示的 PRG 重定向。
//
// query 里放的是 **key**（外加 `{slot}N` / `{slot}M` 计数参数），由 facingQuery 在读侧
// 按白名单翻译 + 填充 —— 这样语言切换不会让提示对不上白名单而消失。
func (h *PageHandle) redirectWith(c *gin.Context, slot, key string, params map[string]string) {
	q := url.Values{}
	if k := strings.TrimSpace(key); k != "" {
		q.Set(slot, k)
		for name, value := range params {
			if name == "" || strings.TrimSpace(value) == "" {
				continue
			}
			q.Set(slot+upperFirst(name), value)
		}
	}
	target := pagePath
	if encoded := q.Encode(); encoded != "" {
		target = pagePath + "?" + encoded
	}
	redirectWhere(c, target)
}

// redirectSlot PRG 回执落在哪个槽位（错误 → err，其余 → done）。
func redirectSlot(isErr bool) string {
	if isErr {
		return "err"
	}
	return "done"
}

// facingTextParams 按 key 取当前语言的文案并填充占位符（未登记的 key 原样返回）。
func facingTextParams(c *gin.Context, key string, params map[string]string) string {
	if strings.TrimSpace(key) == "" {
		return ""
	}
	text, ok := aienums.FacingText(key)
	if !ok {
		return key
	}
	return aienums.FormatFacing(shell.TranslateFor(c)(key, text), params)
}

// facingText 按 key 取当前语言的文案（未登记的 key 原样返回）。
func facingText(c *gin.Context, key string) string { return facingTextParams(c, key, nil) }

// upperFirst 把参数名首字母大写（`n` → `N`，拼 query 名用）。
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// builtinProviderKeys 内置供应商键（模板 datalist 用）。
func builtinProviderKeys() []string { return aiservice.BuiltinProviderKeys() }
