package navigationhttp

// navigation_translation_write.go — 导航译文保存的真实 JSON 写入口（POST /api/navigation/update）。
//
// 为什么并进 /api/navigation/update 而不是新开一条路径：sys_permission 里一个权限点只有一列
// api_path / api_method，新路径就得新增权限点 + sys_menus 按钮行 + 一份迁移；而译文本来就是
// 「导航项内容的一部分」，沿用 navigation:update 是既有的授权口径（一码一路径，不出现
// 「一码两 path 里只有一条能同步进策略表」那种状态）。
//
// 判别用**严格 tagged union**（同一段请求体只读一次，解析与绑定都用它）：
//   - 缺 action             → 旧的更新请求，行为逐字不变；binding:"required" 的校验仍在
//     （走 gin 的 binding 而不是裸 json.Unmarshal，否则 required 会被静默跳过），指针字段的
//     nil / 空语义、乐观锁、错误码映射全部照旧。
//   - action="translations"  → 译文整表提交。
//   - action 为 null / 空 / 非字符串 / 未知值 → 400。
//   - 旧请求里出现 rows / project / lang 任一 → 400（混形）；**其余未知字段仍按 JSON 语义忽略**
//     —— 不借这次迁移收紧旧契约。
//   - 译文分支顶层只认 action / project / lang / rows，行只认 context / hash / target，
//     多一个键就 400（形状错了就该在写之前拒绝，而不是猜）。

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

const (
	// navigationTranslationAction 译文提交的动作标签。
	navigationTranslationAction = "translations"
	// navigationTranslationMaxRows 单次提交的行数上限。
	//
	// 依据：工作台一屏就是「该工程全部菜单文字（去重后）」，正常量级是几十行；1000 是给
	// 菜单极多的大站留的余量，同时把「一次请求的最坏写入量」压在可控范围。超限直接 400
	// （走既有 ErrInvalidParams 归口，不为这个数字新增词条），也**不做截断** —— 截断会让
	// 「提交了 1500 行、只写了 1000 行」这种静默丢数据的状态出现。
	navigationTranslationMaxRows = 1000
	// navigationWriteMaxBodyBytes 请求体上限（1 MiB）。
	//
	// 与行数上限相互独立：行数判据在解析**之后**，这里挡的是解析**之前**的字节量
	//（否则一份很大的 body 会先进内存，再被行数判据拒绝）。
	navigationWriteMaxBodyBytes = 1 << 20
)

// navigationTranslationWriteRow 一行译文。
//
// 显式结构，不沿用页面上那种「rowTarget / rowContext / rowHash 三个数组按下标对齐」的隐式契约：
// 并行数组一旦有一处长度不一致，语义就整体错位，而错位的表现是「把 A 的译文写到 B 上」。
type navigationTranslationWriteRow struct {
	Context string `json:"context"`
	Hash    string `json:"hash"`
	Target  string `json:"target"`
}

// navigationTranslationWriteKeys 译文分支顶层允许出现的键。
var navigationTranslationWriteKeys = map[string]bool{
	"action": true, "project": true, "lang": true, "rows": true,
}

// navigationTranslationWriteRowKeys 译文行允许出现的键。
var navigationTranslationWriteRowKeys = map[string]bool{
	"context": true, "hash": true, "target": true,
}

// navigationTranslationWriteInst 译文写句柄。
//
// 端口（writer / pages / menus）都挂在 navigationTranslationHandle 上，而它们由
// SetupNavigationPages 在装配期注入（那里才拿得到 pageSvc 与导航服务）；API 侧的路由注册
// （SetupNavigationRoutes）拿不到这些端口，所以用一个包级句柄把两边接起来。写入发生在
// 装配完成之后，不存在「请求期间被替换」的竞态。
var navigationTranslationWriteInst *navigationTranslationHandle

// SetNavigationTranslationWriter 注入译文写句柄（装配期与测试用；传 nil = 未装配）。
//
// 未注入时译文分支 **fail closed**：500 + 归口文案（`ErrStorageUnavailable`），
// 而不是退化成「接受请求但什么都没写」。
func SetNavigationTranslationWriter(h *navigationTranslationHandle) {
	navigationTranslationWriteInst = h
}

// navigationWriteBody 读一次请求体并解析成顶层键值。
//
// 空体 / 超过 1 MiB / 非法 JSON / 不是对象（数组、字符串、null）→ 400 并返回 false。
func navigationWriteBody(c *gin.Context) ([]byte, map[string]json.RawMessage, bool) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return nil, nil, false
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, navigationWriteMaxBodyBytes+1))
	if err != nil {
		logger.Scene("navigation").Error(err, "读取导航写请求体失败")
		response.ParamError(c)
		return nil, nil, false
	}
	if len(body) > navigationWriteMaxBodyBytes {
		// 只记字节数：body 里可能有运营填的整表译文，原文不入日志。
		logger.Scene("navigation").With("bytes", len(body)).Warn("导航写请求体超过上限，已拒绝")
		response.ParamError(c)
		return nil, nil, false
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil {
		response.ParamError(c)
		return nil, nil, false
	}
	return body, raw, true
}

// navigationWriteAction 读 action 键。返回 (动作, 是否出现该键, 形状是否合法)。
//
// 形状判据刻意严格：null / 空串 / 数字 / 布尔 / 对象 / 数组一律非法 —— 「判别键写得模棱两可」
// 时唯一安全的动作是拒绝，而不是替调用方猜他想走哪个分支。
func navigationWriteAction(raw map[string]json.RawMessage) (string, bool, bool) {
	value, exists := raw["action"]
	if !exists {
		return "", false, true
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return "", true, false
	}
	var action string
	if err := json.Unmarshal(value, &action); err != nil {
		return "", true, false
	}
	action = strings.TrimSpace(action)
	if action == "" {
		return "", true, false
	}
	return action, true, true
}

// navigationWriteHasTranslationKeys 旧分支的混形守卫：出现译文分支专属键即拒。
//
// 只认这三个键，不顺手收紧别的未知字段：旧契约里多出来的字段从来是被忽略的，
// 借迁移把它们变成 400 会让既有客户端在无感知的情况下开始报错。
func navigationWriteHasTranslationKeys(raw map[string]json.RawMessage) bool {
	for _, key := range []string{"rows", "project", "lang"} {
		if _, ok := raw[key]; ok {
			return true
		}
	}
	return false
}

// navigationWriteRequiredString 读一个必需的字符串字段（缺键 / null / 非字符串 / 空 → 拒）。
func navigationWriteRequiredString(raw map[string]json.RawMessage, key string) (string, bool) {
	value, ok := raw[key]
	if !ok {
		return "", false
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return "", false
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return "", false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	return text, true
}

// navigationTranslationRowsOf 严格解析 rows：必须是数组，每项必须是对象，键只认
// context / hash / target 且值必须是 JSON 字符串。
//
// 三个键必须**都出现**：缺 target 的行若被放行，下游会把它当成「显式空译文」直接跳过
// （target=="" 是「本次不改动这一行」的契约表达），于是**形状错误被静默吞成
// 200 + written:0** —— 客户端以为保存成功，实际一个字段都没提交。缺键与显式空串是
// 两件事，只有在形状层分开，静默吞掉才不会发生。
func navigationTranslationRowsOf(raw json.RawMessage) ([]navigationTranslationWriteRow, bool) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, false
	}
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, false
	}
	out := make([]navigationTranslationWriteRow, 0, len(list))
	for _, item := range list {
		for _, required := range []string{"context", "hash", "target"} {
			if _, ok := item[required]; !ok {
				return nil, false
			}
		}
		row := navigationTranslationWriteRow{}
		for key, value := range item {
			if !navigationTranslationWriteRowKeys[key] {
				return nil, false
			}
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return nil, false
			}
			var text string
			if err := json.Unmarshal(value, &text); err != nil {
				return nil, false
			}
			switch key {
			case "context":
				row.Context = text
			case "hash":
				row.Hash = text
			case "target":
				row.Target = text
			}
		}
		out = append(out, row)
	}
	return out, true
}

// updateTranslations 译文分支（action="translations"）的入口。
//
// 形状与上限全部在**写之前**判完：形状错了就 400，不去猜调用方的意图，也不做部分写入。
func (h *Handle) updateTranslations(c *gin.Context, raw map[string]json.RawMessage) {
	for key := range raw {
		if !navigationTranslationWriteKeys[key] {
			response.ParamError(c)
			return
		}
	}
	projectID, ok := navigationWriteRequiredString(raw, "project")
	if !ok {
		response.ParamError(c)
		return
	}
	lang, ok := navigationWriteRequiredString(raw, "lang")
	if !ok {
		response.ParamError(c)
		return
	}
	if !navigationLangSupported(lang) {
		response.ParamError(c)
		return
	}
	rowsRaw, ok := raw["rows"]
	if !ok {
		response.ParamError(c)
		return
	}
	rows, ok := navigationTranslationRowsOf(rowsRaw)
	if !ok {
		response.ParamError(c)
		return
	}
	if len(rows) > navigationTranslationMaxRows {
		response.ParamError(c)
		return
	}
	handle := navigationTranslationWriteInst
	if handle == nil {
		// 未装配（正常情况下不会发生）：fail closed，绝不静默丢弃。
		logger.Scene("navigation").Warn("导航译文写句柄未装配，拒绝译文保存")
		response.ErrorWithMessage(c, http.StatusInternalServerError, navigationenums.ErrStorageUnavailable)
		return
	}
	handle.writeTranslations(c, projectID, lang, rows)
}

// navigationLangSupported 目标语言必须在受支持的语言集合里。
//
// 只查非空是不够的：写进一个不存在的语言码会得到一行永远不参与构建的译文 ——
// 库里看得到、页面上看不到，排查成本极高。
func navigationLangSupported(lang string) bool {
	for _, code := range i18n.AvailableLangs() {
		if code == lang {
			return true
		}
	}
	return false
}

// writeTranslations 译文写核心：真实验证来源 → 去重 → 与库内现值对比 → 只派发真变化的失效。
//
// 关键不变量（顺序即判据）：
//  1. 来源一律取自本次 build 的结果（`sourceOf` 按原文哈希回查），**不信请求里带的任何原文**：
//     客户端能发的只有「哈希 + 译文」，原文是服务端自己算出来的。
//  2. 空译文 = 这一行不改动（整表提交里未填的行就该被跳过，不是报错）。
//  3. 已变化的才派发 stale：没变化的提交不该把产物标一遍（那会让下次构建白跑，也查不出是谁改的）。
//  4. 写失败不标 stale；标记失败只记日志 —— 译文本身已经写好，不该让运营以为保存失败。
func (h *navigationTranslationHandle) writeTranslations(c *gin.Context, projectID, lang string,
	rows []navigationTranslationWriteRow) {
	ctx := c.Request.Context()
	// tr 是 **Go 侧成品句**那一档（见 navTr 的注释）：本端点的回执与逐条原因都经
	// pkg/response 直发，成品句在那里不再被翻译，所以这里要把词条取成当前语言。
	// 它的取词入口是共享的 adminui.TranslateArgs，不是 shell.TranslateFor。
	tr := navTr(c)

	if h.projects != nil {
		// 工程必须真实存在：否则 build 会拿一个空集合渲染，每一行的哈希都解析不到，
		// 用户看到的会是「菜单文字已变化，请刷新后重试」——错误提示指向了错误的原因。
		projects, perr := h.projects.List(ctx)
		if perr != nil {
			logger.Scene("navigation").Error(perr, "读取导航译文工程列表失败")
			response.ErrorInternal(c, "navigation", perr)
			return
		}
		found := false
		for _, project := range projects {
			if project.ID == projectID {
				found = true
				break
			}
		}
		if !found {
			response.ParamError(c)
			return
		}
	}
	if h.writer == nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, navigationenums.ErrStorageUnavailable)
		return
	}

	data := h.build(ctx, projectID, lang, tr)
	var rowErrors []string
	items := make([]i18n.ContentWriteItem, 0, len(rows))
	keys := make([]string, 0, len(rows))
	// kinds 与 items / keys 同下标：这条译文来自被提交的那一行所在的菜单位置。
	// 跨位置的同名文字在列表里只出现一行，因此它只是**回退值** ——
	// 权威的「哪些位置受影响」按原文哈希回查（见 kindsForHashes）。
	kinds := make([]string, 0, len(rows))
	queued := map[string]bool{}
	for _, row := range rows {
		contextName := strings.TrimSpace(row.Context)
		if contextName != navigationTranslationContext() {
			rowErrors = append(rowErrors, navigationPlaceholderText(
				tr(navigationenums.ErrContextInvalid, "语境非法：导航标签只接受 %s"), navigationTranslationContext()))
			continue
		}
		source, ok := data.sourceOf(strings.TrimSpace(row.Hash))
		if !ok {
			rowErrors = append(rowErrors, tr(navigationenums.ErrSourceChanged, "菜单文字已变化，请刷新后重试"))
			continue
		}
		target := strings.TrimSpace(row.Target)
		if target == "" {
			continue
		}
		key := i18n.ContentIndexKey(source.SourceHash, contextName)
		if queued[key] {
			continue
		}
		queued[key] = true
		if !i18n.ShouldTranslateContent(source.Source) {
			rowErrors = append(rowErrors, navigationPlaceholderText(
				tr(navigationenums.ErrNotTranslatable, "%s：该菜单文字不参与翻译（纯数字或纯符号）"), source.Source))
			continue
		}
		items = append(items, i18n.ContentWriteItem{
			SourceHash: source.SourceHash, Context: contextName, Lang: lang,
			SourceText: source.Source, TargetText: target, Engine: i18n.ContentEngineManual,
		})
		keys = append(keys, key)
		kinds = append(kinds, source.Kind)
	}
	if len(rowErrors) > 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, navigationWriteErrorsText(c, rowErrors))
		return
	}
	if len(items) == 0 {
		response.SuccessWithMessage(c, tr(navigationenums.SavedNone, "没有需要写入的变化。"), gin.H{"written": 0})
		return
	}

	hashesAll := make([]string, 0, len(items))
	for _, it := range items {
		hashesAll = append(hashesAll, it.SourceHash)
	}
	before, berr := h.writer.LoadDetails(ctx, lang, hashesAll)
	if berr != nil {
		logger.Scene("navigation").With("lang", lang).Error(berr, "读取现有导航译文失败，按全部变更处理")
		before = map[string]i18n.ContentTargetInfo{}
	}
	pending := make([]i18n.ContentWriteItem, 0, len(items))
	changedHashes := map[string]bool{}
	fallbackKinds := make([]string, 0, 2)
	seenKind := map[string]bool{}
	for i, item := range items {
		prev := before[keys[i]]
		if prev.TargetText == item.TargetText && prev.Engine == i18n.ContentEngineManual {
			continue
		}
		pending = append(pending, item)
		changedHashes[item.SourceHash] = true
		if k := strings.TrimSpace(kinds[i]); k != "" && !seenKind[k] {
			seenKind[k] = true
			fallbackKinds = append(fallbackKinds, k)
		}
	}
	changedKinds := data.kindsForHashes(changedHashes, fallbackKinds)
	written := 0
	if len(pending) > 0 {
		var uerr error
		written, uerr = h.writer.Upsert(ctx, pending)
		if uerr != nil {
			logger.Scene("navigation").With("lang", lang).Error(uerr, "写入导航译文失败")
			// 归口文案：命中 enums 白名单的业务文案原样透出，其余（数据库原文：
			// 表名 / 约束名 / SQLSTATE）只进上面那条日志。
			response.ErrorWithMessage(c, http.StatusInternalServerError,
				navigationPlaceholderText(tr(navigationenums.ErrSaveFailed, "保存失败：%s"), adminui.MsgText(c, navigationErrMsg(c, uerr))))
			return
		}
		// 译文已落库 → 标记待重建。失败只记日志：译文本身已经写好了，
		// 标记失败只影响「下次构建会不会主动带上这些页」，不该让运营以为保存失败。
		if h.pages != nil {
			if merr := h.pages.MarkStaleForI18n(ctx); merr != nil {
				logger.Scene("navigation").With("lang", lang).Error(merr, "导航译文保存后标记页面待重建失败")
			}
		}
		// 自动发布实例侧同批派发（menu:{projectID}:{kind}）：只标手工页面会让
		// 详情页 / 产品页停在旧菜单标签上，而线上与库里都看不出任何异常。
		if h.menus != nil && len(changedKinds) > 0 {
			h.menus.InvalidateMenuLabels(ctx, scopeProjectID(data, projectID), changedKinds)
		}
	}
	response.SuccessWithMessage(c,
		navigationPlaceholderText(tr(navigationenums.Saved, "已保存 %d 条译文；已标记受影响页面待重建（下次构建生效）。"),
			strconv.Itoa(written)),
		gin.H{"written": written})
}

// navigationPlaceholderText 把词条成品里的占位符替换成实际值。
//
// 为什么不是 fmt.Sprintf：同一批词条的中英占位符**风格不同** —— zh 写 `%s` / `%d`，
// en 写 `{source}` / `{detail}` / `{count}`（见迁移 451）。对 `{...}` 形态用 Sprintf 只会
// 得到 `Save failed: {detail}%!(EXTRA string=…)`，而那串东西会原样出现在响应里。
// 位置参数按占位符出现的先后替换（这批词条每条最多一个占位符）。
func navigationPlaceholderText(tpl string, values ...string) string {
	out := tpl
	for _, value := range values {
		idx, width := navigationFirstPlaceholder(out)
		if idx < 0 {
			break
		}
		out = out[:idx] + value + out[idx+width:]
	}
	return out
}

// navigationFirstPlaceholder 找最靠前的占位符，返回它的起点与长度（找不到返回 -1, 0）。
func navigationFirstPlaceholder(tpl string) (int, int) {
	best, width := -1, 0
	for _, marker := range []string{"%s", "%d"} {
		if i := strings.Index(tpl, marker); i >= 0 && (best < 0 || i < best) {
			best, width = i, len(marker)
		}
	}
	if i := strings.Index(tpl, "{"); i >= 0 {
		if j := strings.Index(tpl[i:], "}"); j > 0 && (best < 0 || i < best) {
			best, width = i, j+1
		}
	}
	return best, width
}

// navigationWriteErrorsText 把逐行错误并成一句话（每句都已按请求语言取好词）。
//
// 分号跟语言走：中文用全角「；」，其余语言用 "; "（与 navigation_err.go 的 facingDetailSep
// 同一口径）。
func navigationWriteErrorsText(c *gin.Context, errs []string) string {
	sep := "；"
	if !strings.HasPrefix(response.RequestLanguage(c), "zh") {
		sep = "; "
	}
	return strings.Join(errs, sep)
}
