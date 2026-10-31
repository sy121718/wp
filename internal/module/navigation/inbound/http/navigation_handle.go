package navigationhttp

// navigation_handle.go — 模块 HTTP 处理器的构造与 Update 入口（严格 tagged union）。
//
// 三条写动作共用 POST /api/navigation/update：
//   - 缺 action            → 旧的更新请求（UpdateReq），行为逐字不变；
//   - action="translations" → 译文整表提交，**直接调 writerSvc**（导航译文 MVC 的写服务）；
//   - action="move"         → 同级上移 / 下移，仍走 moveNavigation 的既有白名单与校验。
//
// 绑定只有一处：binding.JSONLimited[UpdateEnvelope](c, 1<<20)。JSON 投放走
// UpdateEnvelope.UnmarshalJSON，表单投放（urlencoded / multipart）走 FormBinder.BindForm ——
// 两条路汇到同一份顶层键事实，请求体只读一次，本方法不再读 body、不 ParseForm。
//
// 译文分支**不再**把已解析的结构拼回 JSON 字节再交给旧 helper：dto 的
// TranslationUpdateReq 逐字段转成 contract 的 TranslationWriteReq（handler 是唯一转换点，
// contract 与 dto 因此各自独立），业务（来源校验、现值比对、写入 + 三处同事务失效）
// 全在 service 里；失败出口只做 response.ErrorStatusAuto —— 状态码与文案都由声明处给，
// 整批拒绝的明细由 facing.BlockedError 形状自动落进 data.items。成功出口是「现成结果直发」：
// 文案 key 是 enums.Saved（取词由 pkg/response 做），data 就是 contract.TranslationWriteResult
// 本身（自带 written / changedKinds tag），HTTP 层不拼成品句、不数条数、也不造第二种形状。

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// Handle navigation 模块 HTTP 处理器。
type Handle struct {
	svc navigationcontract.NavigationService
	// writerSvc 译文整表提交的写服务（装配期经 SetTranslationWriteService 注入）。
	//
	// 为什么是字段而不是包级句柄：写服务的依赖（三个跨模块端口 + 事务边界）在装配层
	// 才齐备，而路由注册更早 —— 由装配层在拿到 handle 后注入，未注入时译文分支
	// fail closed（500 + 「写入服务未就绪」），绝不退化成「接受请求但什么都没写」。
	writerSvc navigationcontract.TranslationWriteService
}

// NewHandle 构造（签名保持不变：调用点只关心导航服务）。
func NewHandle(svc navigationcontract.NavigationService) *Handle {
	return &Handle{svc: svc}
}

// SetTranslationWriteService 注入译文写服务（装配期；传 nil = 未装配）。
//
// 注入发生在装配完成之后、请求到达之前，不存在「请求期间被替换」的竞态。
func (h *Handle) SetTranslationWriteService(s navigationcontract.TranslationWriteService) {
	h.writerSvc = s
}

// Create 新建导航项；同时是**来源批量加入菜单的唯一写入口**（严格 tagged union，见
// navigation_add_source_handle.go）：缺 action 就是本来的创建请求，行为逐字不变
// （binding:"required"、指针字段、未知字段忽略都保持不变）。
func (h *Handle) Create(c *gin.Context) {
	body, raw, ok := navigationWriteBody(c)
	if !ok {
		return
	}
	action, hasAction, shapeOK := navigationWriteAction(raw)
	if !shapeOK {
		response.ParamError(c)
		return
	}
	if hasAction {
		if action != navigationAddSourceAction {
			response.ParamError(c)
			return
		}
		h.createFromSource(c, raw)
		return
	}
	if navigationDeleteHasKey(raw, "ids") {
		// ids 是来源批量档的专属键：普通创建契约里没有它，出现即混形（见该文件头）。
		response.ParamError(c)
		return
	}
	req := &navigationdto.CreateReq{}
	if err := ginbinding.JSON.BindBody(body, req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgCreateSuccess, res)
}

// Update 更新导航项；同时是**译文整表提交**与**同级上移 / 下移**的唯一写入口。
//
//   - RouteLegacy       → 本来的更新请求，DecodeLegacy() 复现迁移前的 binding:"required"
//     与指针字段 nil / 空语义，service 调用与成功响应逐字不变；
//   - RouteTranslations → 直接调 writerSvc.WriteTranslations（见 updateTranslations）；
//   - RouteMove         → 解析后调 moveNavigation 的既有校验与 SwapMove；
//   - RouteRejected 或任何解码失败 → 400（未知标签 / 混形不猜分支）。
func (h *Handle) Update(c *gin.Context) {
	env, ok := binding.JSONLimited[navigationdto.UpdateEnvelope](c, 1<<20)
	if !ok {
		return // binding 已写 400
	}
	// 形状校验在这里统一做一次，而不是散进各分支：三种投放（JSON / urlencoded / multipart）
	// 共用同一个端点，只有一处判据时才不会出现「一种投放 400、另一种 200」。
	// 放在分流之前：Route 只回判别结果，形状不可用的请求不该进入任何分支。
	if err := env.Validate(); err != nil {
		response.ParamError(c)
		return
	}
	switch env.Route() {
	case navigationdto.RouteTranslations:
		h.updateTranslations(c, env)
	case navigationdto.RouteMove:
		h.moveNavigationEnvelope(c, env)
	case navigationdto.RouteLegacy:
		req, err := env.DecodeLegacy()
		if err != nil {
			response.ParamError(c)
			return
		}
		res, err := h.svc.Update(c.Request.Context(), req)
		if err != nil {
			response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
			return
		}
		response.SuccessWithMessage(c, navigationenums.MsgUpdateSuccess, res)
	default:
		// RouteRejected：action 形状非法、未知标签、缺 action 却带任一条分支的专属键（混形）。
		response.ParamError(c)
	}
}

// updateTranslations 译文分支的入口：dto → contract 的一次显式转换，然后交给写服务。
//
// 转换只有一处：contract 不能 import dto（那是别人模块的请求形状），而 service 只依赖
// contract 的形状才能被单测调用。这里逐字段搬运，**不判业务**（语境 / 未知 hash /
// 可翻译性 / 重复行 / 行数语义全部在 service）。
//
// 绑定期已判掉的形状（dto 侧，仍旧 400）：顶层白名单（action / project / lang / rows /
// csrf_token）、project 与 lang 非空、lang 受支持、rows 是数组且每项三键齐全且值为字符串、
// 行数 ≤ 1000。service 侧再判一次语义层的越界（语境 / 重复 / 来源），两处都不是重复实现：
// 一处管形状、一处管库内事实。
func (h *Handle) updateTranslations(c *gin.Context, env *navigationdto.UpdateEnvelope) {
	req, err := env.DecodeTranslation()
	if err != nil {
		response.ParamError(c)
		return
	}
	if h.writerSvc == nil {
		// 装配缺陷（正常情况下不会发生）：fail closed，绝不静默丢弃。
		logger.Scene("navigation").Warn("导航译文写服务未装配，拒绝译文保存")
		response.ErrorStatusAuto(c, "navigation", navigationenums.ErrTranslationWriteUnavailableError())
		return
	}
	rows := make([]navigationcontract.TranslationWriteRow, 0, len(req.Rows))
	for _, row := range req.Rows {
		rows = append(rows, navigationcontract.TranslationWriteRow{
			Context: row.Context, Hash: row.Hash, Target: row.Target,
		})
	}
	result, err := h.writerSvc.WriteTranslations(c.Request.Context(), &navigationcontract.TranslationWriteReq{
		ProjectID: req.ProjectID, Lang: req.Lang, Rows: rows,
	})
	if err != nil {
		// 状态码与文案都由声明处给；整批拒绝的逐条明细（facing.BlockedError）进 data.items。
		response.ErrorStatusAuto(c, "navigation", err)
		return
	}
	if result == nil {
		// 服务没交回执 = 装配/实现缺陷，不是「一行都没写」：此时是否写入根本无从得知，
		// 报成成功 written:0 会让运营以为「提交了但没变化」而不再重试。
		// 走与「写服务未装配」同一份错误事实（500），不伪造成功的空结果。
		logger.Scene("navigation").Warn("导航译文写服务返回空回执，按失败处理")
		response.ErrorStatusAuto(c, "navigation", navigationenums.ErrTranslationWriteUnavailableError())
		return
	}
	// 回执**就是**服务交回的那个事实对象，原样直发、不投影：
	// contract.TranslationWriteResult 自带 JSON tag（written / changedKinds），字段名即 HTTP 契约，
	// 成功路径由 service 保证 ChangedKinds 非 nil（空切片 → []，不是 null）。
	// 因此这里既不需要 dto 的第二形状，也不需要在 handler 里归一 nil —— 那两步都是
	// 「为同一份事实造第二个形状」，会让字段名漂移无处可查。
	//
	// 文案交**事实**（key + 中文兜底 + 数字参数），不在这里取词拼句：
	// enums.Saved 是一条带 `%s` 的整句（英文语序与中文不同，拼接得不到），
	// 取词与填参在 SuccessMsg 里一次完成（response.ArgsText）。
	// 直接把 key 当 message 传的后果是 `%s` 原样留在响应里 —— 运营看到的是一句占位符没填的句子。
	response.SuccessMsg(c, navigationenums.SavedMsg(strconv.Itoa(result.Written)), result)
}

// Get 导航项详情。
func (h *Handle) Get(c *gin.Context) {
	req, ok := binding.Form[navigationdto.GetReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgDetailSuccess, res)
}

// List 导航项列表。
func (h *Handle) List(c *gin.Context) {
	req, ok := binding.Form[navigationdto.ListReq](c)
	if !ok {
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgListSuccess, list)
}

// Delete 删除导航项。
func (h *Handle) Delete(c *gin.Context) {
	body, raw, ok := navigationWriteBody(c)
	if !ok {
		return
	}
	// 判别键是**形状**而不是 action 标签：单档只认 id（列表行内删除）、批档只认 ids
	// （勾选后批量删除）。见 navigation_delete_handle.go 开头「为什么不拆成两条路径」。
	hasID := navigationDeleteHasKey(raw, "id")
	hasIDs := navigationDeleteHasKey(raw, "ids")
	if hasID && hasIDs {
		response.ParamError(c)
		return
	}
	if hasIDs {
		if !navigationDeleteShapeOK(raw, navigationDeleteBulkKeys) {
			response.ParamError(c)
			return
		}
		h.deleteBulk(c, raw)
		return
	}
	if !hasID || !navigationDeleteShapeOK(raw, navigationDeleteSingleKeys) {
		response.ParamError(c)
		return
	}
	req := &navigationdto.DeleteReq{}
	// 走 gin 的 binding 而不是裸 json.Unmarshal：binding:"required" 的校验必须与迁移前一致。
	if err := ginbinding.JSON.BindBody(body, req); err != nil {
		response.ParamError(c)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgDeleteSuccess, nil)
}

// navigationErrorStatus 把业务错误映射为 HTTP 状态码。
//
// 判档读的是 pkg/facing 的**事实**（key 及其键集形状），不是 err.Error() 里有没有出现某个
// 字面量：错误文案由声明处给（enums.Err / ErrDetail），service 加一句上下文不该改变状态码。
func navigationErrorStatus(err error) int {
	fact, business := navigationFact(err)
	if !business {
		return http.StatusInternalServerError
	}
	// 带参形态的 key 是 key|param，判档只看键本身。
	key, _, _ := strings.Cut(fact.Key, "|")
	switch key {
	case navigationenums.ErrNotFound:
		return http.StatusNotFound
	// 乐观锁冲突用 409：与「你填错了」（400）和「系统坏了」（500）都不同 ——
	// 这一次请求本身没问题，是数据在这期间被别人改过，客户端该刷新后重做。
	case navigationenums.ErrStaleVersion:
		return http.StatusConflict
	case navigationenums.ErrInvalidParam,
		navigationenums.ErrInvalidKind,
		navigationenums.ErrPathTaken,
		// 同级排序并列同样是「你这次的请求有问题、但不是 500」：交换两行取值等于没换，
		// service 显式早退（零写）。落进 default 会把它报成系统故障，让用户以为重试能好。
		navigationenums.ErrSortTied:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
