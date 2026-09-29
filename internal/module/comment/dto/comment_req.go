package commentdto

// comment_req.go — comment 模块的请求结构（数据流 inbound -> service -> inbound）。
//
// 全部字段都是**不可变值**：dto 跨模块传递（runtimefragment 构造 ListReq / SubmitReq 传进来），
// 消费方不得持有并改写。刻意不放 gin / gorm 相关类型：dto 要能被片段层与后台页共用。

// ListReq 公开列表请求（片段 commentList 与后台页共用同一套分页口径）。
type ListReq struct {
	// ProjectID 工程 id（必填：评论按工程隔离）。
	ProjectID string `json:"projectId" form:"projectId"`
	// EntityType 实体类型（必填：白名单由拥有该实体的模块声明）。
	EntityType string `json:"entityType" form:"entityType"`
	// EntityID 实体 id（必填：字符串形态）。
	EntityID string `json:"entityId" form:"entityId"`
	// Page 页码，从 1 开始；≤0 视为 1。
	Page int `json:"page" form:"page"`
	// PageSize 每页条数；≤0 用默认值，超过上限被压到上限。
	PageSize int `json:"pageSize" form:"pageSize"`
}

// SubmitReq 提交评论请求（片段 commentSubmit 构造）。
//
// 不含 CSRF token：token 的校验属于**访问面协议**（片段端点那一层），
// service 只关心业务规则 —— 把协议细节塞进 dto 会让业务测试必须伪造 token。
type SubmitReq struct {
	ProjectID  string
	EntityType string
	EntityID   string
	// UserID 评论者（访客账号 id）。0 = 未登录 —— 提交入口据此拒绝（产品口径：匿名不可评）。
	UserID uint64
	// ParentID 回复目标（顶层评论 id）；0 = 顶层评论。
	ParentID int64
	// Body 正文原样（service 负责 trim / 长度 / 控制字符清洗，**不信任客户端**）。
	Body string
	// IPHash 来源 IP 的带盐哈希（已由调用方算好；空串 = 拿不到 IP）。
	//
	// 为什么传哈希而不是明文 IP：service 不需要明文（限流的 key 与落库的取证值
	// 都只用哈希），把明文交出去只会多一个能记日志泄漏的地方。
	IPHash string
}

// AdminListReq 后台审核队列查询（控制面）。
type AdminListReq struct {
	// ProjectID 工程 id（必填：后台页也要先选工程，见 AdminList 的说明）。
	ProjectID string `json:"projectId" form:"projectId"`
	// Status 状态筛选（空 = 全部）；非空时必须在白名单内。
	Status string `json:"status" form:"status"`
	// EntityType 实体类型筛选（空 = 全部）；非空时必须在注册表内。
	EntityType string `json:"entityType" form:"entityType"`
	// Keyword 正文关键词（空 = 不过滤）。
	Keyword  string `json:"keyword" form:"keyword"`
	Page     int    `json:"page" form:"page"`
	PageSize int    `json:"pageSize" form:"pageSize"`
}

// ReviewReq 批量审核请求。
type ReviewReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	// IDs 要处理的评论 id。
	//
	// 页面表单入口经 shell.BulkIDs 去空白 / 去重 / 限量（MaxBulkIDs）；JSON 入口的上限
	// 由 service 的 commentenums.MaxReviewIDs 兜住 —— 两条入口都必须有界，
	// 否则「一次提交多少 id」就交给请求方决定了。
	IDs []int64 `json:"ids" form:"ids"`
	// Status 目标状态；只接受 approved / rejected（见 enums.ReviewableStatuses）。
	Status string `json:"status" form:"status"`
	// ReviewerID 审核人（sys_admin.id）。**不从请求体读**：它由 handler 从会话取，
	// 让请求方能填操作人等于把审计留痕变成可伪造的字段。
	ReviewerID uint64 `json:"-" form:"-"`
}
