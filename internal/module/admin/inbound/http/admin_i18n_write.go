package adminhttp

// admin_i18n_write.go — 文案词条的 **JSON 写端点**（POST /api/i18n/save）。
//
// 为什么是「一个端点 + 显式 action」而不是四条 REST 路径：本仓权限模型里 sys_permission
// 每个 code 只有一列 api_path/api_method，声明式注册又要求「一个权限点一条路径」——
// 两条路径共用一个 code 时同步只能留下其中一条，另一条上的策略随即失配（超管 403、
// 角色静默失权，见 block_router.go 里 block:bulk_delete 的成文教训）。而词条页的写权限点
// 自迁移 178 起就只有 i18n:manage 一个，178 的注释还写明了它为什么**故意**不拆（拆开会长出
// 「给了保存、忘了给删除」这类无人能解释的配置错误）。于是：保持唯一 code/obj，
// 把增 / 改 / 删 / 批删四种动作收敛到同一个**真实端点**上，用显式 action 判别 ——
// 不再以「请求里有没有 id」这类形状去猜动作。
//
// 请求体是**严格 tagged union**：判据只有 action；未知字段、动作与字段混用、缺必需字段一律 400。
//
// 「缺省」与「显式 null」是两回事：可选字段省略 = 合法（category/remark 落到 ""），
// 显式 null = 形状错 400。依据是 encoding/json 把 null 解进 string / []string **不报错**，
// 于是 `{"ids":null}` 会变成一次静默空批量、`{"category":null}` 会静默清空已有分类 ——
// 两者都是「前端把没取到的值当成值发过来」，只能靠判原文（adminI18nWriteIsExplicitNull）
// 才能与合法省略区分开；数组元素也逐项判（`["a|zh-CN", null]` 同样是形状错）。
//
// 400 的形状错误与「请求体不是合法 JSON」走**同一个既有归口** adminBindFail（admin_err.go）：
// 两者都是客户端输入问题，处置逐字一致（warn 记 user_id + detail、响应只回受控文案）。
// 刻意不在这里另起一个 xxxFail 出口 helper —— scripts/check-shell-migration.sh 把「模块自建
// 出口 helper」计入基线（判据 2 的正则按函数名 + 签名匹配），同形重复的 helper 会让基线上升；
// 归口只有一处，形状细节则通过 err 的 detail 进日志。
//
//	{"action":"create","key":"…","lang":"…","value":"…","category":"…","remark":"…"}
//	{"action":"update","key":"…","lang":"…","value":"…","category":"…","remark":"…"}
//	{"action":"delete","key":"…","lang":"…"}
//	{"ids":["key|lang", …]}                       ← 批量删除档
//
// 批量档没有 action 字段，这是**基座契约**而不是本处的取舍：json_write.js 的 bulk 模式
// payload 恒为 {ids:[…]}（见其 runBulk），模板侧能改的只有收集用的 DOM 名（data-write-ids），
// 而 rowOfID 又硬编码 [name="ids"]（改名会让「已删掉的行」留在页面上）。所以批量档的 tag 是
// 「键集恰好 {ids}」—— 它与另外三档互斥，只有一种解释。基座将来若支持 bulk 透传 action，
// 这里只需给 adminI18nWriteAllowedKeys 的批量档加一条 "action"，其余不用动。
//
// 响应统一 pkg/response.Response{Code,Message,Data}（不自创 {ok,msg}）：
//   - create / update → data = {key,lang}（前端 create 模式成功后整页刷新）
//   - delete          → data = null
//   - 批量            → data = {deleted,skipped,items:[{id,deleted,message}]}，部分成功仍 200
//     （json_write 按 items[].deleted 移除行、用 items[].text|message 逐条说原因）
//
// 空值校验留在这里而不是交给 service：pkg/i18n 把「key/语言/内容为空」与「存储不可用 / 驱动
// 报错」都当同一条 error 返回，混在一起就没法在归口助手里区分 —— 运营少填一个字段会看到
// 「操作失败」这种通用提示。先拦之后，剩下能走到归口助手的只可能是基础设施错误。

import (
	"errors"
	"fmt"
	"go_wp/pkg/binding"
	json "go_wp/pkg/jsonx"
	"net/http"
	"strings"

	adminenums "go_wp/internal/module/admin/enums"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// adminI18nHandle 词条页的**唯一** handle 实例（页面装配与写端点注册共用）。
//
// 为什么必须是同一个实例：页面装配（SetupAdminPages）才拿得到 page 服务（词条变更后标记站点
// 待重建）与字典只读口，而 /api/i18n/save 的注册点在 SetupAdminRoutes —— 那里只有 /api 路由组
// 与 db。若两处各 new 一个 handle，写端点那份就没有失效端口，表现是「改了词条站点不更新、
// 而且没有任何报错」—— 正是这个端口存在的理由。装配期各跑一次、运行期只读。
var adminI18nHandleInst = NewAdminI18nEntryHandle()

// adminI18nHandle 取共用的词条 handle（装配期注入过的端口对两侧同时生效）。
func adminI18nHandle() *adminI18nEntryHandle { return adminI18nHandleInst }

// adminI18nWriteAction 写请求的动作标签（严格 tagged union 的 tag）。
type adminI18nWriteAction string

const (
	adminI18nActionCreate adminI18nWriteAction = "create"
	adminI18nActionUpdate adminI18nWriteAction = "update"
	adminI18nActionDelete adminI18nWriteAction = "delete"
	// adminI18nActionBulk 批量删除：请求体里**不出现** action，tag 是「键集恰好 {ids}」（见文件头）。
	adminI18nActionBulk adminI18nWriteAction = "bulkDelete"
)

// adminI18nWriteAllowedKeys 各档允许出现的键：未列出的键一律 400（严格 union 的「未知/混合字段」判据）。
var adminI18nWriteAllowedKeys = map[adminI18nWriteAction]map[string]bool{
	adminI18nActionCreate: {"action": true, "key": true, "lang": true, "value": true, "category": true, "remark": true},
	adminI18nActionUpdate: {"action": true, "key": true, "lang": true, "value": true, "category": true, "remark": true},
	adminI18nActionDelete: {"action": true, "key": true, "lang": true},
	adminI18nActionBulk:   {"ids": true},
}

// adminI18nWriteReq 解析后的写请求（四种动作归一；哪些字段有效由 Action 决定）。
type adminI18nWriteReq struct {
	Action   adminI18nWriteAction
	Key      string
	Lang     string
	Value    string
	Category string
	Remark   string
	IDs      []string
}

// adminI18nWriteResult 单条成功回执的数据体（前端 create 模式只用 data.id 跳转，这里给身份串）。
type adminI18nWriteResult struct {
	Key  string `json:"key"`
	Lang string `json:"lang"`
}

// adminI18nWriteBulkItem 批量逐条结果：deleted 决定前端是否移除该行，
// Message 是**已取词**的成品句（前端优先读 text，其次 message）。
type adminI18nWriteBulkItem struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
	Message string `json:"message,omitempty"`
}

// adminI18nWriteBulkResult 批量结果（json_write 的摘要句与逐条原因都从这里读）。
type adminI18nWriteBulkResult struct {
	Deleted int                      `json:"deleted"`
	Skipped int                      `json:"skipped"`
	Items   []adminI18nWriteBulkItem `json:"items"`
}

// adminI18nWriteIsExplicitNull 判断字段是不是**显式** null。
//
// 为什么必须单独判一次：encoding/json 把 null 解进 string / []string 都**不报错**
// （落到零值 ""、nil），于是 `{"ids":null}` 会静默变成一次空批量、`category:null`
// 会静默把已有分类清空 —— 契约上「类型错」与「省略」是两回事，缺省才是合法省略，
// null 是前端把「没取到值」当成值发过来了，属于形状错（400）。
func adminI18nWriteIsExplicitNull(body json.RawMessage) bool {
	return len(body) > 0 && strings.TrimSpace(string(body)) == "null"
}

// adminI18nWriteStringOf 取一个字符串字段（缺省为 ""；显式 null / 非字符串都是形状错）。
func adminI18nWriteStringOf(raw map[string]json.RawMessage, key string) (string, error) {
	body, ok := raw[key]
	if !ok {
		return "", nil
	}
	if adminI18nWriteIsExplicitNull(body) {
		return "", fmt.Errorf("字段 %s 是 null（应省略或给字符串）", key)
	}
	var value string
	if err := json.Unmarshal(body, &value); err != nil {
		return "", fmt.Errorf("字段 %s 不是字符串", key)
	}
	return value, nil
}

// adminI18nWriteIDsOf 取批量档的 ids：必须是**字符串数组**，且每一项都得是字符串。
//
// null / 数组里的 null / 非数组一律形状错 —— 逐项判类型是为了 `["a|zh-CN", null]`
// 这种半合法请求：静默把 null 当成空串再靠 Normalize 丢掉，等于替调用方猜意图。
func adminI18nWriteIDsOf(raw map[string]json.RawMessage, key string) ([]string, error) {
	body, ok := raw[key]
	if !ok {
		return nil, fmt.Errorf("批量档缺 %s", key)
	}
	if adminI18nWriteIsExplicitNull(body) {
		return nil, fmt.Errorf("字段 %s 是 null（应为字符串数组）", key)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("字段 %s 不是字符串数组", key)
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		if adminI18nWriteIsExplicitNull(item) {
			return nil, fmt.Errorf("字段 %s 第 %d 项是 null", key, i+1)
		}
		var value string
		if err := json.Unmarshal(item, &value); err != nil {
			return nil, fmt.Errorf("字段 %s 第 %d 项不是字符串", key, i+1)
		}
		out = append(out, value)
	}
	return out, nil
}

// adminI18nWriteReqOf 解析并**严格校验**请求体：成功返回归一后的请求，
// 失败时已写出 400 响应（调用方直接 return）。
func adminI18nWriteReqOf(c *gin.Context) (adminI18nWriteReq, bool) {
	rawMap, err := binding.ParseJSON[map[string]json.RawMessage](c)
	if err != nil {
		adminBindFail(c, err)
		return adminI18nWriteReq{}, false
	}
	raw := *rawMap

	var req adminI18nWriteReq
	action := ""
	if body, ok := raw["action"]; ok {
		if adminI18nWriteIsExplicitNull(body) {
			adminBindFail(c, errors.New("action 是 null（应省略或给字符串）"))
			return adminI18nWriteReq{}, false
		}
		if err := json.Unmarshal(body, &action); err != nil {
			adminBindFail(c, errors.New("action 不是字符串"))
			return adminI18nWriteReq{}, false
		}
		req.Action = adminI18nWriteAction(action)
	} else {
		// 没有 action → 只剩批量删除一档（见文件头的基座契约）。
		req.Action = adminI18nActionBulk
	}

	allowed, ok := adminI18nWriteAllowedKeys[req.Action]
	if !ok {
		adminBindFail(c, fmt.Errorf("未知 action：%q", action))
		return adminI18nWriteReq{}, false
	}
	for key := range raw {
		if !allowed[key] {
			adminBindFail(c, fmt.Errorf("动作 %s 不接受字段 %q", req.Action, key))
			return adminI18nWriteReq{}, false
		}
	}

	if req.Action == adminI18nActionBulk {
		ids, err := adminI18nWriteIDsOf(raw, "ids")
		if err != nil {
			adminBindFail(c, err)
			return adminI18nWriteReq{}, false
		}
		req.IDs = ids
		return req, true
	}

	if req.Key, err = adminI18nWriteStringOf(raw, "key"); err != nil {
		adminBindFail(c, err)
		return adminI18nWriteReq{}, false
	}
	if req.Lang, err = adminI18nWriteStringOf(raw, "lang"); err != nil {
		adminBindFail(c, err)
		return adminI18nWriteReq{}, false
	}
	req.Key, req.Lang = strings.TrimSpace(req.Key), strings.TrimSpace(req.Lang)
	if req.Action == adminI18nActionDelete {
		if missing := adminI18nDeleteMissingMsg(req.Key, req.Lang); missing != "" {
			r.ErrorWithMessage(c, http.StatusBadRequest, missing)
			return adminI18nWriteReq{}, false
		}
		return req, true
	}
	// create / update：内容必填（空内容在构建期等同未命中，写它是纯噪声）。
	if req.Value, err = adminI18nWriteStringOf(raw, "value"); err != nil {
		adminBindFail(c, err)
		return adminI18nWriteReq{}, false
	}
	if missing := adminI18nSaveMissingMsg(req.Key, req.Lang, req.Value); missing != "" {
		r.ErrorWithMessage(c, http.StatusBadRequest, missing)
		return adminI18nWriteReq{}, false
	}
	if req.Category, err = adminI18nWriteStringOf(raw, "category"); err != nil {
		adminBindFail(c, err)
		return adminI18nWriteReq{}, false
	}
	if req.Remark, err = adminI18nWriteStringOf(raw, "remark"); err != nil {
		adminBindFail(c, err)
		return adminI18nWriteReq{}, false
	}
	return req, true
}

// I18nEntryWrite POST /api/i18n/save —— 词条写端点（增 / 改 / 删 / 批删，形状见文件头）。
//
// 权限点仍是 178 的 i18n:manage → 本路径 POST（路由注册即声明，逐字不变），
// 所以既有的超管与角色策略一行都不用改。
func (h *adminI18nEntryHandle) I18nEntryWrite(c *gin.Context) {
	req, ok := adminI18nWriteReqOf(c)
	if !ok {
		return
	}
	switch req.Action {
	case adminI18nActionCreate, adminI18nActionUpdate:
		h.i18nWriteUpsert(c, req)
	case adminI18nActionDelete:
		h.i18nWriteDelete(c, req)
	case adminI18nActionBulk:
		h.i18nWriteBulkDelete(c, req)
	}
}

// i18nWriteUpsert 新增（upsert）或更新一条词条。
//
// create 与 update 的差别是**是否允许无中生有**：create 走 i18n.SaveEntry（OnConflict upsert，
// 新建与改都成功），update 走 i18n.UpdateEntry（只改已存在的 key×lang，缺席返回 ErrRecordNotFound）。
// 两条语义都保留 —— 抽屉里的「编辑」不该因为词条已被别人删掉就悄悄新建一条。
func (h *adminI18nEntryHandle) i18nWriteUpsert(c *gin.Context, req adminI18nWriteReq) {
	entry := i18n.Entry{
		Key: req.Key, Lang: req.Lang, Value: req.Value,
		Category: strings.TrimSpace(req.Category), Remark: strings.TrimSpace(req.Remark), Status: 1,
	}
	var err error
	if req.Action == adminI18nActionUpdate {
		err = i18n.UpdateEntry(c.Request.Context(), entry)
	} else {
		err = i18n.SaveEntry(c.Request.Context(), entry)
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 编辑期间词条被别处删掉：如实回 404（前端就地显示原因），不新建、不静默成功。
			r.ErrorWithMessage(c, http.StatusNotFound, adminenums.MsgBadRequest)
			return
		}
		adminFail(c, err)
		return
	}
	// 词条在构建期取词烘进产物字节：改完必须标记站点待重建（否则站点停在旧文案且无报错）。
	h.markI18nStale(c)
	r.Success(c, adminI18nWriteResult{Key: req.Key, Lang: req.Lang})
}

// i18nWriteDelete 删除一条词条。
//
// pkg/i18n.DeleteEntry 对「本来就不存在」不报错（幂等），只有输入非法与基础设施错误才回错 ——
// 保持既有语义：重复删除不是失败。
func (h *adminI18nEntryHandle) i18nWriteDelete(c *gin.Context, req adminI18nWriteReq) {
	if err := i18n.DeleteEntry(c.Request.Context(), req.Key, req.Lang); err != nil {
		adminFail(c, err)
		return
	}
	// 删词条同样会改变构建期取词结果（回退到组件包内的中文兜底），必须标记待重建。
	h.markI18nStale(c)
	r.Success(c)
}

// i18nWriteBulkDelete 批量删除：逐条走同一条单条删除路径，某一条失败只计入跳过，整批不中断。
//
// 上限与去重由 pkg/bulkids.Normalize 在服务端强制（客户端那次同口径拦截只是快速反馈）；
// 结果里每条都带 id，前端据此移除行并逐条说明原因。
func (h *adminI18nEntryHandle) i18nWriteBulkDelete(c *gin.Context, req adminI18nWriteReq) {
	ids, nerr := bulkids.Normalize(req.IDs)
	if nerr != nil {
		r.ErrorWithMessage(c, http.StatusBadRequest, adminI18nBulkTooManyMessage(nerr))
		return
	}
	res := adminI18nWriteBulkResult{Items: []adminI18nWriteBulkItem{}}
	for _, raw := range ids {
		item := adminI18nWriteBulkItem{ID: raw}
		key, lang, ok := strings.Cut(strings.TrimSpace(raw), adminI18nEntryKeySeparator)
		key, lang = strings.TrimSpace(key), strings.TrimSpace(lang)
		if !ok || key == "" || lang == "" {
			// 复合键不成形状（"key|lang"）：跳过而不是猜 —— 猜错会删掉别的行。
			item.Message = r.TranslateMessage(c, adminenums.ErrI18nKeyEmpty)
			res.Skipped++
			res.Items = append(res.Items, item)
			continue
		}
		if err := i18n.DeleteEntry(c.Request.Context(), key, lang); err != nil {
			item.Message = adminErrParam(c, err)
			res.Skipped++
			res.Items = append(res.Items, item)
			continue
		}
		item.Deleted = true
		res.Deleted++
		res.Items = append(res.Items, item)
	}
	// 单条删除的失效按批只发一次：逐条调会把「全站标记」重复 N 遍，而它标记的是同一批页面
	// （结果等价，代价随批量线性放大）。
	if res.Deleted > 0 {
		h.markI18nStale(c)
	}
	r.Success(c, res)
}

// adminI18nBulkTooManyMessage 批量超限文案的**带参协议形态**（key|max|count）。
//
// 与 block 侧同源：词条与两个数字都归 pkg/bulkids 所有（bulkids.MsgTooMany 的两个 %s 依次是
// 上限与本次条数），response 的 translate 按 | 后的参数依次填参。
func adminI18nBulkTooManyMessage(err error) string {
	max, count := bulkids.MaxIDs, 0
	var tooMany *bulkids.IDsError
	if errors.As(err, &tooMany) {
		max, count = tooMany.Max, tooMany.Count
	}
	return fmt.Sprintf("%s|%d|%d", bulkids.MsgTooMany, max, count)
}
