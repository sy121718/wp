// detail_template_mode.go — 实例「双轨」能力端口（迁移 281 / 282，docs/04-C-instance-override.md）。
//
// 双轨 = 同一实体的详情页有两种文档来源：跟随共享模板（template 模式）与实例自带独立文档
// （document 模式）。本端口是这条能力域对外的全部动作：切换模式（写入独立文档 / 重新套用
// 预设）、两类回滚（文档级重编译、产物级切指针），外加后台要用的两个只读面
//（历史快照清单、按模板统计影响面）。
//
// 为什么单开一条收窄接口（而不是把六条方法摊进主服务的消费面上）：后台商品详情页只消费
// 这六条，引用类型收敛到本端口后，消费方在类型上就够不着创建 / 重建 / 删除 —— 形状即越权
// 防护（与 published_locator.go 同一手法）。反过来 PresentationService 嵌入本端口，
// 装配层持有的 presentationSvc 静态类型因此直接满足它：「presentation 提不提供这个能力」
// 是编译期问题，不再靠运行期类型断言 + 静默降级来回答。

package presentationcontract

import (
	"context"

	presentationdto "go_wp/internal/module/presentation/dto"
)

// DetailTemplateModePort 实例双轨能力的收窄端口（后台商品详情页消费）。
//
// 由 presentation 的 service 实现（service 侧有编译期断言：
// var _ presentationcontract.DetailTemplateModePort = (*Service)(nil)）。
type DetailTemplateModePort interface {
	// SaveOverrideDocument 保存实例级文档覆盖并按其重建发布（迁移 281）：
	// workbench 实例模式的保存通道；只改本实例，不影响共享模板与同模板的其他实例。
	SaveOverrideDocument(ctx context.Context, req *presentationdto.SaveOverrideReq) (res *presentationdto.InstanceResp, err error)
	// ReapplyPreset 重新套用预设（迁移 282）：放弃实例的独立文档、回到跟随模板并重建；
	// req.TemplateID 非空时同时换一套底稿。与「转入独立」对称，是可反悔的另一半
	//（主服务上的 ClearOverride 语义等同本方法，为既有调用点保留）。
	ReapplyPreset(ctx context.Context, req *presentationdto.ReapplyPresetReq) (res *presentationdto.InstanceResp, err error)
	// RollbackDocument 取某份历史快照的文档重发（重新编译，实体数据取最新）：
	// 语义上等于「把这份历史文档作为该实例当前文档」，实例进入 document 模式
	//（此后模板更新不再覆盖它）。
	RollbackDocument(ctx context.Context, req *presentationdto.RollbackDocumentReq) (res *presentationdto.InstanceResp, err error)
	// RollbackArtifact 产物指针回滚：把实例的线上指针切回历史产物（不重新编译，秒级）。
	// 目标产物文件不在位时**拒绝**回滚 —— 切了就是路由指向不存在的文件。
	RollbackArtifact(ctx context.Context, req *presentationdto.RollbackArtifactReq) (res *presentationdto.InstanceResp, err error)
	// ListSnapshots 实例的历史快照清单（新→旧），供后台选择文档回滚目标。
	ListSnapshots(ctx context.Context, req *presentationdto.ListSnapshotsReq) (list []*presentationdto.SnapshotSummary, err error)
	// CountByTemplate 按绑定模板统计实例数（template 模式 / document 模式分开），
	// 供后台「编辑模板（影响 N 个商品）」的影响面提示：转入独立的实例不受模板更新影响，
	// 把它们算进影响面会吓退用户（数字必须是真的）。
	CountByTemplate(ctx context.Context, req *presentationdto.CountByTemplateReq) (res *presentationdto.CountByTemplateResp, err error)
}
