package navigationcontract

// navigation_translation.go — 导航译文整表写入的**跨模块端口**与回执事实（owner A / BE-NAV-WRITE）。
//
// 为什么这三个端口住在这里：写事务里要同时改三张不属于 navigation 的表
// （pages / presentation_instances / build_jobs）。AGENTS.md 表隔离禁止 navigation 直连
// 别人的表，依赖方向也禁止 navigation import page / presentation / build 的 service 或 model ——
// 于是**消费者侧声明最小端口**，由装配层（routers）用真实服务实现。
//
// 每个端口**只有一个方法**，三端口分开而不是合成一个 interface：三条能力属于三个模块、
// 三个 owner，合成一个会把「一边已交付、另一边还没接线」这种半成品状态藏进同一个
// 断言里；分开后装配期自检能逐条点名缺哪一根。
//
// 层归属：contract 只放接口与不可变 DTO —— 不 import 别人的 service / model / enums，
// 不取词、不拼句、不判业务。
//
// 三个 Tx 端口的共同契约（逐条都是调用方的责任，实现方一件都不代做）：
//   - tx 必须是调用方已开启、已 scope 的真实事务；端口自己不开事务、不提交、不回滚；
//   - 只在**确有真实变化**的路径上调用（每次调用都会推进页/实例的 input_version，
//     空调用等于给一次不存在的改动发新代次，会让下一次构建白跑）；
//   - 失败即整笔回滚：译文、页标记、实例标记、队列任务四者同进同出，绝不留半截。

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// PageStalePort 手工页面侧的按位置失效（由 page 模块实现，装配层注入）。
//
// 为什么按 kind 而不是按 key：菜单依赖的键是 (`menu:<project>:<kind>`)，位置只有四个，
// 而 navigation 不该知道 page 的依赖键构造规则（那是 page / pipeline 的口径）。
// 按位置传参让「怎么查」留在 page 内部，键的构造不会出现第二份。
type PageStalePort interface {
	// MarkMenuStaleTx 在调用方事务内，把「产物声明了该菜单位置依赖」的页面标记为待重建，
	// 返回真正命中的行数（RETURNING 回读，不是入参回显）。
	//
	// 命中口径由 page 决定（活跃/暂存/账本产物声明过这条依赖），本端口只保证：
	// 同一 tx、同一批决定、失败即整笔回滚。
	MarkMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) (n int64, err error)
}

// PresentationStalePort 自动发布实例侧的按位置失效（由 presentation 模块实现，装配层注入）。
//
// 与 PageStalePort 同形但**不合并**：两个模块的命中口径不同（实例有语言账本，
// 页面有 stage/active 两指针），合成一个 interface 会让哪边实现错了都无法在装配期区分。
type PresentationStalePort interface {
	// MarkInstanceMenuStaleTx 在调用方事务内，把「活跃或暂存产物声明了该菜单位置依赖」的
	// 自动发布实例标记为待重建，返回真正命中的行数。
	MarkInstanceMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) (n int64, err error)
}

// MenuItemStalePort 手工页面侧的**按菜单项**失效（由 page 模块实现，装配层注入）。
//
// 为什么与 PageStalePort 分开：按位置引用（menu:{project}:{kind}）与按项引用
// （navigation:{itemID}）是两条不同的依赖键，谁也不能替谁命中 —— 只登记了某一支的页面
// 只有按项键才会被标记，而位置键在菜单项改动时命中不到它（pipeline.NavigationKey 的注释
// 写明了这个区别）。两端口形状相同但语义不同，合成一个会让「按位置」与「按项」的调用
// 在编译期无法区分。
//
// 与 MenuStaleEnqueuePort 配合构成菜单项 CRUD 的闭环：手工页面同步标 stale（下次构建
// 带走新菜单），自动发布实例靠同位置/同项的 menu_stale 任务重建（见 MenuStaleDispatcher）。
type MenuItemStalePort interface {
	// MarkNavigationStaleTx 在调用方事务内，把「产物声明了该菜单项依赖」的页面标记为待重建，
	// 返回真正命中的行数（RETURNING 回读，不是入参回显）。
	MarkNavigationStaleTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string) (n int64, err error)
}

// MenuItemPresentationStalePort presentation 侧按**具体菜单项**失效（由 presentation 模块实现）。
//
// 为什么与 PresentationStalePort 分开：那个按**位置**键（menu:{project}:{kind}）命中，
// 本接口按**项**键（navigation:{itemID}）命中。构建期 core 的两种绑定模式各记各的
// （见 internal/builder/core/render.go 的 UseMenu / UseNavigation），只登记了某一条菜单项
// 的实例产物不会被位置键命中。
//
// 缺这条路径的后果：菜单项写成功、按位置引用的实例重编了，但「只引用这一支」的实例
// 既没有同事务的版本屏障（旧编译晚到会覆盖新 HTML 并清 stale），也不在位置键的反查里。
// 所以它必须与写同事务，且**缺失即 fail closed**（可选接口缺席时不降级跳过）。
type MenuItemPresentationStalePort interface {
	// MarkInstanceNavigationStaleTx 在调用方事务内，把「活跃或暂存产物声明了该菜单项依赖」
	// 的自动发布实例标记为待重建，返回真正命中的行数（零命中是合法结果，但必须调用）。
	MarkInstanceNavigationStaleTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string, at time.Time) (n int64, err error)
}

// MenuStaleEnqueuePort 可靠重建任务的入队（由 build 模块实现，装配层注入）。
//
// 为什么必须与译文同事务：「译文写成功、任务丢了」→ 菜单标签永远停在旧字节；
// 「任务在、译文回滚」→ worker 去重建一份不存在的变化。两种半成品都只能靠同事务消除。
type MenuStaleEnqueuePort interface {
	// EnqueueMenuStaleTx 在调用方事务里为 (`工程, 菜单位置`) 可靠入队一条 menu_stale 任务，
	// 返回本次分配的任务版本号（单调递增）。
	//
	// inputVersion 是**任务序列号**：navigation 只负责「已入队」，不把它写进任何实体列、
	// 不参与业务判断、不向调用方回显（它是队列内部的分量）。
	EnqueueMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string) (inputVersion int64, err error)
}

// TranslationWriteResult 译文整表写入的事实回执（不可变，值语义）。
//
// 只有事实、没有成品句：回执文案由 pkg/response 在出口按请求语言取词，
// 这里给数字与位置名 —— 服务层不拼「已保存 %d 条译文」。
type TranslationWriteResult struct {
	// Written 本次**真正发生变化**的译文条数（同值且已是 manual 的行不计）。
	Written int `json:"written"`
	// ChangedKinds 本次真正变化涉及的菜单位置，按 header → header_mobile → footer →
	// footer_mobile 的规范顺序排列（去重、与提交顺序无关 → 回执确定）。
	//
	// 它是「哪些位置受影响」的权威事实：同一段原文可以同时出现在多个位置，
	// 而整表提交里那段文字只有一行，行级信息不足以还原位置。
	//
	// 恒为非 nil 切片（无变化时是 []string{}）：出口直接序列化成 [] 而不是 null，
	// 前端不必区分「空」与「缺」。
	ChangedKinds []string `json:"changedKinds"`
}

// TranslationWriteService 译文整表提交（由 navigation service 实现，handler 只依赖这个形状）。
//
// 为什么在 contract 而不是直接调 Service：HTTP 层不该知道事务边界与端口集合；
// 这条接口把「一次提交」收敛成一个方法，装配与替换点都只有一处。
type TranslationWriteService interface {
	// WriteTranslations 整表提交译文：全成或全败。
	//
	// 成功返回写入条数与真正变化的菜单位置；任何一行被拒绝（未知 hash / 不可翻译 /
	// 语境非法 / 重复行冲突）→ 整批拒绝且**零写入**（错误由 enums 声明，
	// 冲突明细走 facing.BlockedError 的 data.items）。
	WriteTranslations(ctx context.Context, req *TranslationWriteReq) (*TranslationWriteResult, error)
}

// TranslationWriteReq 译文整表提交的业务入参（不可变，值语义）。
//
// 与 dto 的 TranslationUpdateReq 同形但**不别名**：contract 不能 import 别人的 dto，
// 而请求形状的 owner 是 dto（绑定与校验属它）。handler 负责一次显式转换 ——
// 这层转换是唯一的地方，service 因此可以只依赖 contract 的形状被单测调用。
type TranslationWriteReq struct {
	// ProjectID 目标工程（任意大小写形态；service 内部归一成 uuid 规范形式）。
	ProjectID string
	// Lang 目标语言（必须属于 i18n.AvailableLangs()）。
	Lang string
	// Rows 整表提交的行（空 target = 本次不改这一行）。
	Rows []TranslationWriteRow
}

// TranslationWriteRow 一行译文（只是形状，业务规则在 service）。
//
// Target 是普通字符串：空串是合法值（= 本次不动这一行），缺键在 dto 绑定期就已拒绝。
type TranslationWriteRow struct {
	// Context 语境（导航标签固定为 `navigation.label`，由 dto / service 共同保证）。
	Context string
	// Hash 原文指纹（服务端算，客户端只回带；未知 hash 整批拒绝）。
	Hash string
	// Target 译文（已 TrimSpace 之前的值；service 负责归一）。
	Target string
}
