package presentationcontract

// publish_record.go — 「这批实体现在发布成什么样了」的收窄只读端口（B9 发布记录页）。
//
// 为什么单开一条端口而不是直接用 PresentationService.List：那个 List 返回的是
// **实例**（InstanceResp：模板 / 渲染模式 / 文档…），而发布记录页要的是「每个实体在每个
// 语言上：访问路径 / 发布时间 / 产物版本 / 是否激活」—— 这些事实分散在三张表上
//（presentation_instances 的 active 指针与 stale、presentation_publications 的每语言
// 激活行、presentation_artifacts 的版本号），拼装只该在 presentation 模块内做一次。
// 让调用方（商品页）自己去列表 + 逐行查产物，等于把三张表的读法复制到别的模块里。
//
// **诚实边界**：库里没有「每次发布尝试成功/失败」的历史明细表（回执
// publication_receipts 只覆盖访问面切换那一段，且完成后只留终态）。所以本端口回答的是
// **当前**发布状态，不是发布历史 —— 名字叫 Record 指「一行一条当前登记」，不是审计流水。

import (
	"context"
	"time"
)

// PublishRecordRow 一个实体在一个语言上的一条当前发布登记。
//
// 同一实体多语言多行；**从未发布过的实例也给一行**（Lang/ActivePath/PublishedAt 空，
// Status=draft），否则「建了详情页实例但从没发出去」的实体会从页面上消失 —— 而那正是
// 最需要被看见的一种状态。
type PublishRecordRow struct {
	InstanceID string
	EntityType string
	EntityID   string
	// InstanceRole detail / archive（同一个实体可能两张页面）。
	InstanceRole string
	// URLPath 实例登记的路径（未激活时也有值：它是「如果发布会在哪」）。
	URLPath string
	// Lang 构建语言；空 = 该实例还没有任何语言登记（从未发布）。
	Lang string
	// ActivePath 线上真实访问路径（含语言前缀）；空 = 该语言未激活。
	ActivePath string
	// ArtifactVersion 当前激活产物的版本号（>0）；0 = 没有产物。
	ArtifactVersion int64
	ArtifactHash    string
	// PublishedAt 该语言激活时刻；零值 = 未发布。
	PublishedAt time.Time
	// Active 该语言是否已上线（= 实例的 active_artifact_id 指针非空）。
	//
	// 给布尔而不是把 presentationenums 的取值（"active"/"draft"）放进行里：调用方
	// （商品页）要判断的是「线上有没有」，不是背下这个模块的枚举叫什么。要显示成
	// 文案的那一步在各自的模板里按这个布尔分支。
	Active bool
	// Stale 依赖已变更、待重建。
	Stale bool
}

// PublishRecordPort 按实体批量读「当前发布登记」。
type PublishRecordPort interface {
	// PublishRecords 返回本工程内 entityType 这些实体的发布登记。
	//
	// entityIDs 为空 = 本工程该类型全部；projectID 为空取唯一工程（多工程时必须显式给，
	// 与 PresentationService 的其它方法同口径）。**读不到就是错误**：这一屏的每一行都是
	// 发布事实，静默返回空表会让运营以为「一个都没发布」。
	PublishRecords(ctx context.Context, projectID, entityType string, entityIDs []string) ([]PublishRecordRow, error)
}
