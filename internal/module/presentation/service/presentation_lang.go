package presentationservice

// presentation_lang.go — 多语言变更后的实例失效入口（文案词条 sys_i18n / 内容译文 sys_translation）。
//
// 与 page 模块同名方法同义（page_lang.go §MarkStaleForI18n）：两者都是「构建期取词注入
// 字节」的产物，词条或译文一变，已发布产物就过期。缺这条的表现是「改了译文，商品页
// 还是旧字节」，而且日志里什么都没有（本项目反复吃过这类静默失效）。
//
// 逐工程扇出（DB-009 第三批）：presentation_instances 带 FORCE 策略，调用方（后台
// 翻译页）没有工程上下文，因此「全站」必须由 service 层逐工程拼出来；不做无作用域的全表 UPDATE。

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// MarkStaleForI18n 把各工程内的全部自动发布实例标记为待重建。
func (s *Service) MarkStaleForI18n(ctx context.Context) (err error) {
	if s.project == nil {
		return nil
	}
	projects, err := s.project.List(ctx)
	if err != nil {
		return err
	}
	at := time.Now().UTC()
	for _, p := range projects {
		if ctx.Err() != nil {
			break
		}
		if _, merr := s.m.MarkStaleForI18n(ctx, p.ID, at); merr != nil {
			return merr
		}
	}
	return nil
}

// MarkStaleForI18nTx 在**调用方的事务**内标记该工程的实例（page 侧 I18nStalePeer 的 Tx 变体）。
//
// 与 MarkStaleForI18n 是同一批失效判定，差别是事务边界：page 的 MarkStaleForI18n 把
// 「pages 标记 + 本来源标记」放进同一个事务（同库跨模块的写，AGENTS.md「写操作的事务与
// 回滚」），任一步失败整体回滚 —— 不再停在「页面已标、实例未标」的半截状态上等下一次
// 词条保存。本方法**不枚举工程**：工程由调用方给定（它自己逐工程扇出），作用域由 model
// 在传入的 tx 上设置。
//
// 与 MarkStaleForI18n 的另一个差别：不检查 s.project —— 那个检查存在的原因是自足版本
// 要靠 project 契约枚举工程；这里工程已是入参，缺契约不该把标记静默变成 no-op。
// tx 为 nil / 工程非法交给 model 报错（rls.ScopeTx 会拒非事务句柄），不在这一层吞掉。
func (s *Service) MarkStaleForI18nTx(ctx context.Context, tx *gorm.DB, projectID string) (err error) {
	if s == nil || s.m == nil {
		return errors.New("presentation: 实例仓储未装配，无法标记实例失效")
	}
	if _, merr := s.m.MarkStaleForI18nTx(ctx, tx, projectID, time.Now().UTC()); merr != nil {
		return merr
	}
	return nil
}
