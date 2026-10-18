// Package presentationenums presentation 模块响应消息与错误兜底。
//
// 错误的对外文案跟**声明处**走：这里同时给出 i18n key（常量值）与中文兜底（errFallbacks），
// 出口只调一次共享判据 pkg/facing.FactOf。原先只有 key、没有兜底，页面拿到的事实兜底就是
// key 本身，只能显示 ErrPathOccupied 这种标识符 —— 于是每个消费方各补一张补丁表，
// 同一个 key 在不同页面显示不同的话（product 详情模板页就补过两张，现已删除）。
package presentationenums

import (
	"fmt"
	"strings"

	"go_wp/pkg/facing"
)

// 响应消息（未接 i18n 前中文常量）。
const (
	MsgCreateSuccess  = "MsgCreateSuccess"  // 自动发布实例创建成功
	MsgRebuildSuccess = "MsgRebuildSuccess" // 实例重建成功
	MsgListSuccess    = "MsgListSuccess"    // 实例列表获取成功
	MsgDetailSuccess  = "MsgDetailSuccess"  // 实例详情获取成功
	MsgDeleteSuccess  = "MsgDeleteSuccess"  // 实例已删除
	// MsgPreviewSuccess 预览渲染成功（未落库、未激活，issue #14）。
	MsgPreviewSuccess = "MsgPreviewSuccess"
	// MsgUpdateURLSuccess 实例 URL 修改成功（新路径已激活、旧路径已按策略处置）。
	MsgUpdateURLSuccess = "MsgUpdateURLSuccess"

	ErrInvalidParam  = "ErrInvalidParam"  // 参数错误
	ErrNotFound      = "ErrNotFound"      // 实例不存在
	ErrNoTemplate    = "ErrNoTemplate"    // 该类型无可用内容模板
	ErrEntityMissing = "ErrEntityMissing" // 内容实体不存在
	ErrBuildFailed   = "ErrBuildFailed"   // 实例构建失败
	// ErrProjectRequired 未指定工程且无法从唯一工程推导（0 个或多个工程）。
	ErrProjectRequired = "ErrProjectRequired"
	// ErrProjectNotFound 显式指定的工程不存在。
	ErrProjectNotFound = "ErrProjectNotFound"
	// ErrRegistryMissing 实体类型注册表未装配（装配缺陷，构建期无法解析实体字段）。
	ErrRegistryMissing = "ErrRegistryMissing"
	// ErrTemplateTypeMismatch 指定的模板与内容实体类型不匹配（issue #14）：
	// 多套命名模板之间不允许串用（拿商品模板渲染文章会在构建期产出错误数据）。
	ErrTemplateTypeMismatch = "ErrTemplateTypeMismatch"
	// ErrInvalidPath 目标路径不合法（归一化失败：缺前导斜杠 / 含非法段 / 超长等）。
	ErrInvalidPath = "ErrInvalidPath"
	// ErrSamePath 新路径与实例当前路径相同（改 URL 的空操作）。
	ErrSamePath = "ErrSamePath"
	// ErrPathOccupied 目标路径已被其他页面或展示实例占用。
	// 与 page 侧 ErrPathOccupied 同一文案口径（改 URL 抢路径的失败原因对用户是同一件事）。
	ErrPathOccupied = "ErrPathOccupied"

	// 以下四条是双轨写动作的业务错误（迁移 282）。**值即 i18n key**（本轮 285 接词），
	// 刻意带模块前缀：sys_i18n 主键是 (item_key, lang)，裸名 "ErrRollbackTargetMiss"
	// 在 page 模块已被占用（page/enums 里有一个同名哨兵）—— 裸 key 会互相顶掉词条，
	// 页面上两条不同来源的错误显示同一句话（与 navigation.err.internal 同一理由）。
	//
	// ErrDetachConfirmRequired 转入独立文档需要用户确认（会放弃模板同步）。
	// 服务层在未确认时返回它，前端据它弹确认并带 confirmDetach 重试 ——
	// 不用「先弹窗再请求」是因为判据（文档结构是否真的变了）只有服务端算得准。
	ErrDetachConfirmRequired = "presentation.err.detachConfirmRequired"
	// ErrRollbackTargetMiss 回滚目标不存在（hash / 快照找不到或产物文件已缺失）。
	ErrRollbackTargetMiss = "presentation.err.rollbackTargetMiss"
	// ErrRollbackFailed 回滚激活失败（线上版本保持不变）。
	ErrRollbackFailed = "presentation.err.rollbackFailed"
	// ErrSnapshotMismatch 目标快照不属于该实例（防止拿别人的快照回滚自己）。
	ErrSnapshotMismatch = "presentation.err.snapshotMismatch"
)

// errFallbacks 每个错误常量的中文兜底（与声明的 key 一起交给 pkg/facing.New）。
//
// 兜底是**声明处**的话：写模块自己的口径，不写某个页面的口径 —— 同一个错误在列表页、
// 详情页、AI 工具里的措辞应该一致，页面若需要更具体的话，由页面自己的文案补，不经错误事实。
var errFallbacks = map[string]string{
	ErrInvalidParam:          "参数不合法：请检查后重试。",
	ErrNotFound:              "实例不存在，可能已被删除。",
	ErrNoTemplate:            "该内容类型没有可用的模板。",
	ErrEntityMissing:         "内容实体不存在。",
	ErrBuildFailed:           "实例构建失败，请稍后重试。",
	ErrProjectRequired:       "请先选择站点工程。",
	ErrProjectNotFound:       "选择的站点工程不存在。",
	ErrRegistryMissing:       "内容实体类型未注册，无法构建。",
	ErrTemplateTypeMismatch:  "这套模板与该内容类型不匹配，请换一套。",
	ErrInvalidPath:           "访问路径不合法：必须以 / 开头，且不含空格与 .. 路径段。",
	ErrSamePath:              "新旧路径相同，没有需要修改的地方。",
	ErrPathOccupied:          "这个路径已被占用，换一个再试。",
	ErrDetachConfirmRequired: "转入独立文档需要确认（会放弃模板同步）。",
	ErrRollbackTargetMiss:    "回滚目标不存在，或产物文件已缺失。",
	ErrRollbackFailed:        "回滚激活失败，线上版本保持不变。",
	ErrSnapshotMismatch:      "目标快照不属于这个实例，不能用来回滚。",
}

// Err 声明一条自带对外文案的业务错误（pkg/facing.New：key + 中文兜底）。
func Err(key string) error {
	return facing.New(key, errFallbacks[key])
}

// ErrDetail 声明一条带明细的业务错误：明细拼在半角冒号之后。
//
// **必须半角**：pkg/facing.FactOf 的复合形态只按 ": " 切明细段，写全角会让整条过不了判据，
// 退化成内部错误（对外只剩归口文案）。
func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}

// FallbackFor 取一个 key 的中文兜底（调用方只有 key、没有事实时用）。
func FallbackFor(key string) string {
	return errFallbacks[key]
}

// 实例发布状态（由指针列推导，非表列）。
const (
	// StatusActive 已上线（active_artifact_id 非空）。
	StatusActive = "active"
	// StatusDraft 仅有草稿/未上线。
	StatusDraft = "draft"
)
