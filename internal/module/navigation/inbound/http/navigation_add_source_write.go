// navigation_add_source_write.go — 来源批量加入菜单的**严格形状 union 分支**。
//
// 「从已有内容添加」原来是导航列表页的一条页面 POST（`/admin/navigations/add-source`，借
// `/api/navigation/create` 的权限对象）。本批把它迁到真实端点 `/api/navigation/create`：
//
//   - **不新开路径、不新增权限点**：`sys_permission` 每个 code 只有一列 api_path/api_method，
//     给同一个能力再挂一条路径就得再定义一个 code，于是「加一个菜单项」在角色授权下出现两套
//     锚点（能加普通的、加不了来源的说法迟早出现）。改用 action 判别，权限面逐字不变。
//   - **判别键是 action**：与 `/api/navigation/delete` 的「按形状判别」不同，这里必须有 action
//     —— 普通 Create 的请求体里本来就有 sourceType/sourceId（合法的业务字段），
//     靠键集合无法与来源批量区分开。所以：**缺 action = 普通创建，行为逐字不变**；
//     `action:"addSource"` = 本分支；其它未知 action 一律 400。
//   - **批专属键只有 ids**：普通 CreateReq 没有 ids 字段，所以「无 action 却带 ids」是明确的
//     混形（400）。除此之外**不**收紧普通创建的未知字段容忍度 —— 那条契约是给外部调用方的，
//     本批只做加法。
//
// 业务语义与退役的页面版逐条一致：候选按 project + sourceType 取（SourceGroups），
// 不在候选里 / 没有公开路径 / Create 失败 → 该条计入 skipped 并带一条**本请求语言**的原因，
// 整批不中断；每条成功都走同一个 service.Create（「同一工程同一类型下路径唯一」「成功即派发
// 菜单与导航失效」这些规则由 service 决定，handler 不复制一套）。
//
// 与页面版的三处刻意差异（都是「接口出口」而非「业务规则」的差异）：
//   - 读候选失败（SourceGroups 报错）→ 500 归口文案且**零写入**（页面版是跳提示页）；
//   - 回执带 added/skipped/items[]，页面版只有一句「已完成」（用户看不到哪几条被跳过）；
//   - 不再经 shell.RenderJump 回列表，由 json_write 的批量档就地显示摘要 + 刷新入口。
package navigationhttp

import (
	"bytes"
	json "go_wp/pkg/jsonx"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// navigationAddSourceAction action 键的来源批量档取值。
const navigationAddSourceAction = "addSource"

// navigationAddSourceKeys 来源批量档顶层允许出现的键（**严格白名单**）。
//
// ids 由 json_write 的批量档最后写入（页面复选框 name 已由 sourceIds 改为 ids），
// projectId/kind/sourceType 由表单具名声明（data-write-extra）。
var navigationAddSourceKeys = map[string]bool{
	"action": true, "projectId": true, "kind": true, "sourceType": true, "ids": true,
}

// navigationAddSourceNoPathKey 候选「暂无公开路径」的原因词条。
//
// 刻意复用模板里那条 key（迁移 192，中英成对）：同一件事在管理页抽屉与接口回执里
// 说的是同一句话，不给它再抄一份文案 —— 两份迟早会有一边改一边忘。
// 它是**业务回执**的文案（随请求语言解析），不是模板预渲染的 UI 文案。
const navigationAddSourceNoPathKey = navigationenums.NoPublicPath

// navigationAddSourceItem 逐条结果。
//
// 字段名与 block / 导航批量删除同形（added + items[].message）：json_write 的批量档按
// `data.okKey`（outcome=add → added）计数、按 items[].message 列逐条原因，
// 三个模块给不同的字段名就等于让基座维护三套读法。
type navigationAddSourceItem struct {
	ID      string `json:"id"`
	Added   bool   `json:"added"`
	Message string `json:"message,omitempty"`
}

// navigationAddSourceResult 来源批量的整体回执。
type navigationAddSourceResult struct {
	Added   int                       `json:"added"`
	Skipped int                       `json:"skipped"`
	Items   []navigationAddSourceItem `json:"items"`
}

// navigationAddSourceOptionalString 读**可选**字符串键。
//
// 与 navigationWriteRequiredString 的唯一差别是「缺省」不再算错，因为 kind 在页面语义里
// 本就是「空值回落 header」（normalizeNavKind）。显式 null 与非字符串仍然拒 ——
// 「这一格没填」与「这一格填了个不是类型值的东西」是两件事。
func navigationAddSourceOptionalString(raw map[string]json.RawMessage, key string) (string, bool) {
	value, exists := raw[key]
	if !exists {
		return "", true
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return "", false
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return "", false
	}
	return strings.TrimSpace(text), true
}

// createFromSource 处理 action=addSource 档：来源实体批量加入菜单。
func (h *Handle) createFromSource(c *gin.Context, raw map[string]json.RawMessage) {
	if !navigationDeleteShapeOK(raw, navigationAddSourceKeys) {
		response.ParamError(c)
		return
	}
	projectID, ok := navigationWriteRequiredString(raw, "projectId")
	if !ok {
		response.ParamError(c)
		return
	}
	sourceType, ok := navigationWriteRequiredString(raw, "sourceType")
	if !ok {
		response.ParamError(c)
		return
	}
	kindText, ok := navigationAddSourceOptionalString(raw, "kind")
	if !ok {
		response.ParamError(c)
		return
	}
	kind := normalizeNavKind(kindText)
	params, ok := navigationDeleteBulkIDsOf(raw["ids"])
	if !ok {
		response.ParamError(c)
		return
	}
	ids, nerr := bulkids.Normalize(params)
	if nerr != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, navigationBulkTooManyMessage(nerr))
		return
	}
	if len(ids) == 0 {
		// 一条都没勾：空数组是**合法**形状（与 `{"ids":null}` 的区别是「说了没有」而不是
		// 「没说清楚」），回执零计数、由页面显示「请至少勾选一项要加入菜单的内容」。
		// 这一档**不读候选**：空批量不该产生一次库查询。
		response.Success(c, navigationAddSourceResult{Items: []navigationAddSourceItem{}})
		return
	}

	ctx := c.Request.Context()
	groups, gerr := h.svc.SourceGroups(ctx, projectID)
	if gerr != nil {
		// 读候选失败与「候选为空」必须分开：前者零写入并归口 500（否则用户会看到
		// 「已加入 0 项」以为勾错了），后者是合法的空结果，逐条计入 skipped。
		logger.Scene(navigationErrScene).With("project", projectID).Error(gerr, "加载导航来源候选失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, navigationInternalKey)
		return
	}

	candidates := make(map[string]navigationcontract.SourceCandidate, len(ids))
	for _, g := range groups {
		if g.Type != sourceType {
			continue
		}
		for _, it := range g.Items {
			candidates[it.ID] = it
		}
	}

	res := navigationAddSourceResult{Items: []navigationAddSourceItem{}}
	for _, id := range ids {
		item := navigationAddSourceItem{ID: id}
		candidate, found := candidates[id]
		if !found {
			// 该 id 不属于本工程 + 本来源类型（伪造的 id、或换了来源类型的请求）。
			item.Message = navigationReasonKeyText(c, navigationenums.ErrNotFound)
			res.Skipped++
			res.Items = append(res.Items, item)
			continue
		}
		if candidate.URL == "" {
			item.Message = navigationReasonKeyText(c, navigationAddSourceNoPathKey)
			res.Skipped++
			res.Items = append(res.Items, item)
			continue
		}
		title := candidate.Title
		if title == "" {
			title = candidate.Label
		}
		sourceID := candidate.ID
		if _, cerr := h.svc.Create(ctx, &navigationdto.CreateReq{
			ProjectID: projectID, Title: title, Path: candidate.URL, Kind: kind,
			SourceType: sourceType, SourceID: &sourceID,
		}); cerr != nil {
			// 逐条原因走与单条接口同一份判据（未命中 → 归口 key + 一条日志），
			// 再按请求语言转成品句、词条缺席时回落到受控中文兜底：回执是给人看的，
			// 裸 key 就是「漏译」（与删除端点共用 navigationReasonText，见 navigation_err.go）。
			logger.Scene(navigationErrScene).With("sourceType", sourceType).With("sourceId", id).
				Error(cerr, "来源实体加入菜单失败")
			item.Message = navigationReasonText(c, cerr)
			res.Skipped++
			res.Items = append(res.Items, item)
			continue
		}
		item.Added = true
		res.Added++
		res.Items = append(res.Items, item)
	}
	logger.Scene(navigationErrScene).With("project", projectID).With("kind", kind).
		With("sourceType", sourceType).With("added", res.Added).With("skipped", res.Skipped).
		Info("来源实体加入导航菜单")
	response.Success(c, res)
}
