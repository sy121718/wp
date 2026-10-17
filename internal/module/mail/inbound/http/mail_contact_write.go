package mailhttp

// mail_contact_write.go — 联系人写操作的 **JSON 端点**（POST /api/mail/contact/{save,delete,tag}）。
//
// 为什么是这三条路径：权限点自迁移 525 起就以这三个 API 路径为 obj（页面写操作借用它们），
// 而本模块真实注册过的只有 /api/mail/contact/{list,import,status} —— 于是 save / delete / tag
// 是「有权限点、有策略、没有路由」的幽灵对象：AI 工具的权限判定（permission.RoutesOf）查不到
// 路由就 fail closed，而页面写操作却照常能用。本文件把三条路径变成**真实路由**，
// 判权对象与方法因此逐字不变（超管与角色策略一行都不用改，也不需要新迁移）。
//
// 一个端点两种形状（delete）：单条 {id} 与批量 {ids} 复用同一条 API 路径 —— 这是权限模型的
// 硬约束，不是省事：sys_permission 每个 code 只有一列 api_path/api_method，再开一条
// /api/mail/contact/bulk-delete 就必须新增权限点，而新增权限点 = 已授权角色静默缩权
//（见 521 / block_router.go 的成文教训）。判据因此是「id 与 ids 恰好给一个」，两个都给、
// 都不给都是 400。
//
// 严格形状：白名单外的字段、**显式 null**、类型不符一律 400。显式 null 必须单独判 ——
// encoding/json 把 null 解进 string / []string / uint64 都不报错（落到零值），不判原文就会
// 静默变成「一次空批量」或「把字段清空」。缺省（字段不出现）是合法省略，与显式 null 是两回事。
//
// 批量为什么逐条调用 service：json_write 的 bulk 模式要**逐条原因**（items[].deleted / message）
// 才能决定移除哪一行、并说清哪几个没成功。批删 service 是**单语句**（全成或全败，给不出逐条），
// 所以这里按条调用 DeleteContacts（各一条事务）；TagContacts 内部本来就是逐条事务循环，
// 按条调用与整批调用语义逐字相同。条数上限走 pkg/bulkids（200，超限整批 400，与页面路径同源）。
//
// 两个 bulk 动作的 items 形状**故意不同**，因为基座把「这一条成功了」与「把这一行从表格里删掉」
// 当成两件事，而两者的口径由一个属性分开（json_write 的 data-write-outcome）：
//   · 删除是破坏性动作 → 回 {deleted, skipped, items}，成功条目 deleted=true，前端据此移除该行；
//   · 打标签不是 → 回 {updated, skipped, items}（**成功条目也回**，updated=true），
//     前端读 data.updated、**不移除任何行**。若把成功条目回成 deleted=true，前端会把还在的
//     联系人行从列表里删掉（页面看着像删了人，刷新才发现没删）；若只回失败条目，
//     调用方（含 AI 工具）就无法从回执里知道「哪几条其实已经改好了」。
// 两个动作的摘要句用模板里的 data-msg-* 成品文案（按动作各写一套），计数口径因而无需基座特判。

import (
	"errors"
	"go_wp/pkg/binding"
	json "go_wp/pkg/jsonx"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/response"
)

// mailContactOperator 取操作人 ID。
//
// 这里用 middleware/builtin.GetUserID 而不是 builtin.CurrentUserID：后者只是它的
// 「>0 收口」封装，而本文件是纯 JSON 端点（没有页面壳、没有提示页），
// 多引一个 shell 依赖只会让本模块的生产代码多一条不必要的横向依赖
// （门禁 scripts/check-shell-migration.sh 也按「新增 shell 引用」计数）。
func mailContactOperator(c *gin.Context) uint64 {
	if id := builtin.GetUserID(c); id > 0 {
		return uint64(id)
	}
	return 0
}

// 三个端点的字段白名单（严格 union：多一个字段就 400，不做「忽略未知字段」的宽松解析）。
var (
	mailContactSaveKeys   = map[string]bool{"id": true, "email": true, "name": true, "tags": true, "source": true, "consent_source": true, "status": true}
	mailContactDeleteKeys = map[string]bool{"id": true, "ids": true}
	mailContactTagKeys    = map[string]bool{"ids": true, "add": true, "remove": true}
)

// mailContactWriteResult 单条成功回执的数据体（前端 create 模式失败时才读 data，成功即刷新；
// 给 id 是为了让脚本 / AI 工具拿到刚写入的身份）。
type mailContactWriteResult struct {
	ID uint64 `json:"id"`
}

// mailContactBulkItem 批量逐条结果：deleted 决定前端是否移除该行，
// Message 是**已取词**的成品句（前端读 items[].text，其次 items[].message）。
type mailContactBulkItem struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
	Message string `json:"message,omitempty"`
}

// mailContactBulkResult 批量结果（json_write 的摘要句与逐条原因都从这里读）。
type mailContactBulkResult struct {
	Deleted int                   `json:"deleted"`
	Skipped int                   `json:"skipped"`
	Items   []mailContactBulkItem `json:"items"`
}

// mailContactTagItem / mailContactTagResult 是**非破坏动作**的逐条回执：键名是 updated 而不是
// deleted（同一个键不能既表示「成功了」又表示「把它从列表里删掉」，见文件头）。
type mailContactTagItem struct {
	ID      string `json:"id"`
	Updated bool   `json:"updated"`
	Message string `json:"message,omitempty"`
}

type mailContactTagResult struct {
	Updated int                  `json:"updated"`
	Skipped int                  `json:"skipped"`
	Items   []mailContactTagItem `json:"items"`
}

// mailContactIsExplicitNull 判断字段是不是**显式** null（json.RawMessage 保留原文）。
func mailContactIsExplicitNull(body json.RawMessage) bool {
	return len(body) > 0 && strings.TrimSpace(string(body)) == "null"
}

// mailContactBody 解析并严格校验请求体：顶层必须是对象、字段必须在白名单内、不得出现显式 null。
// 失败时已写出 400（受控文案），调用方直接 return。
func mailContactBody(c *gin.Context, allowed map[string]bool) (map[string]json.RawMessage, bool) {
	rawMap, err := binding.ParseJSON[map[string]json.RawMessage](c)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return nil, false
	}
	raw := *rawMap
	for key, body := range raw {
		if !allowed[key] || mailContactIsExplicitNull(body) {
			response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
			return nil, false
		}
	}
	return raw, true
}

// mailContactStringOf 取一个字符串字段（**缺省为 ""** —— 省略是合法形状；显式 null 已被
// mailContactBody 拦掉，这里只需处理类型）。
func mailContactStringOf(raw map[string]json.RawMessage, key string) (string, error) {
	body, ok := raw[key]
	if !ok {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(body, &value); err != nil {
		return "", errors.New(key + " 不是字符串")
	}
	return value, nil
}

// mailContactUintOf 取一个无符号整数字段（缺省为 0）。JSON 字符串 "1" 不算数字 ——
// 客户端要么给数字、要么省略，别让两种形态都能表达同一个值。
func mailContactUintOf(raw map[string]json.RawMessage, key string) (uint64, error) {
	body, ok := raw[key]
	if !ok {
		return 0, nil
	}
	// 判原文而不是判解出来的值：json.Number 的底层是 string，把 JSON 字符串 "5" 解进去
	// **不会报错** —— 于是 {"id":"5"} 会被当成 id=5 静默接受。两种形态（数字与字符串数字）
	// 表达同一个值只会让客户端各自猜一种，所以这里只收字面上以数字开头的 JSON 数字：
	// 字符串 / 布尔 / 对象 / 数组 / 小数 / 科学计数法 / 负数一律 400。
	text := strings.TrimSpace(string(body))
	if text == "" || text[0] < '0' || text[0] > '9' {
		return 0, errors.New(key + " 不是非负整数")
	}
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0, errors.New(key + " 不是非负整数")
	}
	return value, nil
}

// mailContactIDsOf 取批量档的 ids：必须是**字符串数组**且逐项都是字符串。
//
// 逐项判类型是为了 ["1", null] 这种半合法请求：静默把 null 当成空串再靠归一丢掉，
// 等于替调用方猜意图（去重与上限由调用方紧接着走 pkg/bulkids）。
func mailContactIDsOf(raw map[string]json.RawMessage) ([]string, error) {
	body, ok := raw["ids"]
	if !ok {
		return nil, errors.New("缺 ids")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, errors.New("ids 不是数组")
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		if mailContactIsExplicitNull(item) {
			return nil, errors.New("ids 第 " + strconv.Itoa(i+1) + " 项是 null")
		}
		var value string
		if err := json.Unmarshal(item, &value); err != nil {
			return nil, errors.New("ids 第 " + strconv.Itoa(i+1) + " 项不是字符串")
		}
		out = append(out, value)
	}
	return out, nil
}

// mailContactNormalizeIDs 形状通过后的去重与上限（与页面路径同源：pkg/bulkids）。
func mailContactNormalizeIDs(c *gin.Context, raw map[string]json.RawMessage) ([]string, bool) {
	ids, err := mailContactIDsOf(raw)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return nil, false
	}
	normalized, nerr := bulkids.Normalize(ids)
	if nerr != nil {
		// shell.BulkIDs 的受控出口：超限文案带 (上限, 去重后条数)，按当前语言生成。
		response.ErrorWithMessage(c, http.StatusBadRequest, mailBulkIDsText(c, nerr))
		return nil, false
	}
	return normalized, true
}

// ContactSave 新建 / 编辑联系人（POST /api/mail/contact/save）。
//
// 与页面表单**同一份字段清单**（id=0 新建、否则编辑，语义在 service 里，见 SaveContactReq）：
// 分开两个端点必然出现「新建加了字段、编辑忘了加」。编辑时缺省的 source 不会把已有来源抹掉
// （service 的既有语义）。
func (h *Handle) ContactSave(c *gin.Context) {
	raw, ok := mailContactBody(c, mailContactSaveKeys)
	if !ok {
		return
	}
	id, err := mailContactUintOf(raw, "id")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	email, err := mailContactStringOf(raw, "email")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	name, err := mailContactStringOf(raw, "name")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	tags, err := mailContactStringOf(raw, "tags")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	source, err := mailContactStringOf(raw, "source")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	consentSource, err := mailContactStringOf(raw, "consent_source")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	status, err := mailContactStringOf(raw, "status")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}

	req := &maildto.SaveContactReq{
		ID:            id,
		Email:         email,
		Name:          name,
		Tags:          splitTagInput(tags),
		Source:        source,
		ConsentSource: consentSource,
		Status:        status,
		OperatorID:    mailContactOperator(c),
	}
	if req.ID > 0 {
		if err := h.svc.UpdateContact(c.Request.Context(), req); err != nil {
			response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
			return
		}
		response.SuccessWithMessage(c, mailenums.MsgSaveSuccess, mailContactWriteResult{ID: req.ID})
		return
	}
	created, err := h.svc.CreateContact(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgSaveSuccess, mailContactWriteResult{ID: created})
}

// ContactDelete 删除联系人：单条 {id} 或批量 {ids}（POST /api/mail/contact/delete）。
//
// 批量逐条结果只报数、不猜：id 非法的条目在**没有库**时也能判出来（形状先行），
// 所以它同样能给出「跳过 + 原因」。
//
// 「删不存在的 id」不算失败、但**也不能算成功**：service 返回的是真实删除行数，
// 0 行意味着这一条本来就不在（或已被别人删掉）。逐条调用时若忽略这个计数，
// 回执就会说「已删除 3 位」而实际只删了 2 位 —— 运营据此以为整批都干净了，
// 而列表刷新后才发现有一条还在（或者从没在过）。所以按 0 行计跳过并给原因。
//
// 单条档没有「部分成功」这个channel，所以 0 行不能像批量那样计跳过：直接按
// **这一条不存在**回 400 + 联系人不存在（与 UpdateContact / 本模块其它删除端点同一口径，
// 业务错误走 ErrorAuto 的 400/500 分档）。两条路给出的都是同一个事实，
// 只是载体不同：批量是逐条 skipped，单条是整请求失败。
func (h *Handle) ContactDelete(c *gin.Context) {
	raw, ok := mailContactBody(c, mailContactDeleteKeys)
	if !ok {
		return
	}
	_, hasIDs := raw["ids"]
	_, hasID := raw["id"]
	if hasIDs == hasID { // 两个都给或都不给：形状错，不替调用方选一个
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	operator := mailContactOperator(c)

	if hasID {
		id, err := mailContactUintOf(raw, "id")
		if err != nil || id == 0 {
			response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
			return
		}
		deleted, err := h.svc.DeleteContacts(c.Request.Context(), &maildto.DeleteContactsReq{
			IDs: []uint64{id}, OperatorID: operator,
		})
		if err != nil {
			response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
			return
		}
		if deleted == 0 {
			// 0 行 = 这一条本来就不在：说「删除成功」是假话，而且用户会以为自己
			// 点掉的行确实存在过（实际可能是自己看错了 id、或已被同事删掉）。
			response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrContactNotFound)
			return
		}
		response.SuccessWithMessage(c, mailenums.MsgDeleteSuccess, nil)
		return
	}

	ids, ok := mailContactNormalizeIDs(c, raw)
	if !ok {
		return
	}
	res := mailContactBulkResult{Items: make([]mailContactBulkItem, 0, len(ids))}
	ctx := c.Request.Context()
	for _, rawID := range ids {
		item := mailContactBulkItem{ID: rawID}
		contactID, perr := strconv.ParseUint(rawID, 10, 64)
		switch {
		case perr != nil || contactID == 0:
			item.Message = response.TranslateMessage(c, mailenums.ErrInvalidParam)
		default:
			deleted, derr := h.svc.DeleteContacts(ctx, &maildto.DeleteContactsReq{
				IDs: []uint64{contactID}, OperatorID: operator,
			})
			switch {
			case derr != nil:
				item.Message = mailErrPageText(c, derr)
			case deleted == 0:
				// 真实删除 0 行：这一条本来就不存在（幂等语义下不是错误，但报成成功就是假话）。
				item.Message = response.TranslateMessage(c, mailenums.ErrContactNotFound)
			default:
				item.Deleted = true
			}
		}
		if item.Deleted {
			res.Deleted++
		} else {
			res.Skipped++
		}
		res.Items = append(res.Items, item)
	}
	response.Success(c, res)
}

// ContactTag 批量打标签（POST /api/mail/contact/tag）：给这批人加上 add 里的标签、去掉 remove 里的。
//
// 增与减在同一条请求里（拆成两个端点必然出现「加成功、减失败」的半截状态，见 TagContactsReq）。
// 逐条调用 service：TagContacts 内部就是「逐条加锁 → 算新值 → 写回」的循环，按条调用与整批
// 调用的语义逐字相同，而按条调用能给出逐条原因（批量结果区要说明「哪几个没打上」）。
//
// 回执形状是 {updated, skipped, items}（**成功条目也在 items 里**）：这是非破坏动作，
// 键名不能说 deleted（前端按它决定是否移除行，见文件头），而只回失败条目又会让调用方
// 拿不到「哪几条已经改好了」。
func (h *Handle) ContactTag(c *gin.Context) {
	raw, ok := mailContactBody(c, mailContactTagKeys)
	if !ok {
		return
	}
	add, err := mailContactStringOf(raw, "add")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	remove, err := mailContactStringOf(raw, "remove")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	ids, ok := mailContactNormalizeIDs(c, raw)
	if !ok {
		return
	}
	addTags, removeTags := splitTagInput(add), splitTagInput(remove)
	if len(addTags) == 0 && len(removeTags) == 0 {
		// 空差量是「没说要改什么」：既不是成功也不是失败，直接说清（与 service 同一条文案）。
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrContactTagEmpty)
		return
	}
	operator := mailContactOperator(c)
	res := mailContactTagResult{Items: make([]mailContactTagItem, 0, len(ids))}
	ctx := c.Request.Context()
	for _, rawID := range ids {
		contactID, perr := strconv.ParseUint(rawID, 10, 64)
		if perr != nil || contactID == 0 {
			res.Skipped++
			res.Items = append(res.Items, mailContactTagItem{
				ID: rawID, Message: response.TranslateMessage(c, mailenums.ErrInvalidParam),
			})
			continue
		}
		changed, skipped, terr := h.svc.TagContacts(ctx, &maildto.TagContactsReq{
			IDs:        []uint64{contactID},
			Add:        addTags,
			Remove:     removeTags,
			OperatorID: operator,
		})
		switch {
		case terr != nil:
			res.Skipped++
			res.Items = append(res.Items, mailContactTagItem{ID: rawID, Message: mailErrPageText(c, terr)})
		case skipped > 0 || changed == 0:
			// 逐条调用时 changed=0 且无错只可能是该 id 不存在（锁读不到行）——
			// service 的 changed 计「已处理」，不区分「标签真的变了」。
			res.Skipped++
			res.Items = append(res.Items, mailContactTagItem{
				ID: rawID, Message: response.TranslateMessage(c, mailenums.ErrContactNotFound),
			})
		default:
			res.Updated++
			res.Items = append(res.Items, mailContactTagItem{ID: rawID, Updated: true})
		}
	}
	response.Success(c, res)
}
