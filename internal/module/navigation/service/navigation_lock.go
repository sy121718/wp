package navigationservice

// navigation_lock.go — 乐观锁与版本冲突文案（两个写入口的并发保护）。
// 解析调用方回带的 update_time、必要时取库内当前标题做定位，产出可展示的冲突提示。

import (
	"context"
	"errors"
	"strings"
	"time"

	"go_wp/pkg/utils"

	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
)

// —— 乐观锁辅助（本批新增）——

// parseExpectedUpdatedAt 解析调用方回带的 update_time（乐观锁）。
//
// 主形态是 RFC3339Nano（Go 的 time.Time 默认格式，微秒精度）：管理页表单原样回带它，
// 时区偏移一并带着，解析回来是同一时刻（库列是 timestamptz，比较的是时刻不是字符串）。
// 另接受空格分隔的秒级格式（utils.LayoutSecond）：外部脚本按项目既有口径传值时不必转换
// —— 秒级串只在 update_time 恰好落在整秒时命中，属于调用方的责任。
// 空 / nil = 不做校验（既有调用方与内部路径的兼容形态）；给了却解析不了 = 参数错误
// （不能静默忽略：那样并发保护会被一个坏参数悄悄绕过）。
func parseExpectedUpdatedAt(raw *string) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}
	s := strings.TrimSpace(*raw)
	if s == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339Nano, utils.LayoutSecond} {
		if t, perr := time.ParseInLocation(layout, s, time.Local); perr == nil {
			return &t, nil
		}
	}
	return nil, errors.New(navigationenums.ErrInvalidParam)
}

// staleVersionMessage 乐观锁冲突的对外文案：白名单 key + 「：<定位>」。
//
// 用「：」而不是带参的 key|param 形态：定位信息是菜单项标题（任意文本），
// 而页面路径经 ?err= 回带后要过 shell.FacingNotice 的受控文案判定 —— 那里认的是
// 「候选文案 + ：」前缀（形态 3），不是「模板 + 任意参数」。走带参形态的话，
// 读侧只有带 %s 的词条模板，永远匹配不上填好参的整句，提示会静默消失。
func staleVersionMessage(title string) string {
	return navigationenums.ErrStaleVersion + "：" + compactLocation(title)
}

// compactLocation 定位信息（菜单项标题）的收敛：去控制字符、限长。
//
// 标题来自用户输入，而页面提示条有长度上限（shell.NoticeMaxBytes = 512 字节）：
// 撑破上限的提示会被判定为伪造而整条丢弃 —— 等于没有提示。
func compactLocation(title string) string {
	title = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, title))
	if title == "" {
		return "-"
	}
	const maxRunes = 40
	if rs := []rune(title); len(rs) > maxRunes {
		return string(rs[:maxRunes]) + "…"
	}
	return title
}

// staleTitle 冲突提示里的定位信息：优先取库内**当前**标题（冲突之后它才是真相），
// 读不到（已被删除 / 作用域异常）时回退调用方手上的旧标题。
func (s *Service) staleTitle(ctx context.Context, e *navigationmodel.NavigationEntity) string {
	if cur, err := s.m.Get(ctx, e.ProjectID, e.ID); err == nil && cur != nil && strings.TrimSpace(cur.Title) != "" {
		return cur.Title
	}
	return e.Title
}
