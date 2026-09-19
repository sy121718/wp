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
	"time"
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
