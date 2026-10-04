package navigationservice

// navigation_validate.go — 导航项字段校验与归一化。
// 含 kind/path 基础规则、父引用合法性（自引用 / 成环 / 跨工程 / 跨类型）与
// 来源、打开方式、悬浮面板三组白名单归一化，取值口径与迁移里的 CHECK 约束对齐。

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	navigationenums "go_wp/internal/module/navigation/enums"
)

// normalizePanel 归一化悬浮面板字段（超级菜单，迁移 285）。
//
// 空白块 id 一律归一成 nil（"没选块" 与 "选了空" 是同一个状态，留空串会让
// 渲染期误以为有面板、去解析一个不存在的块 id）。宽度白名单与 DDL CHECK 对齐，
// 越界在这里显式拒绝，而不是让库层约束报错（那会变成 500 而不是可读的参数错误）。
func normalizePanel(blockID *string, width string) (outID *string, outWidth string, err error) {
	outWidth, err = normalizePanelWidth(width)
	if err != nil {
		return nil, "", err
	}
	return normalizePanelBlockID(blockID), outWidth, nil
}

func normalizePanelBlockID(blockID *string) *string {
	if blockID == nil {
		return nil
	}
	id := strings.TrimSpace(*blockID)
	if id == "" {
		return nil
	}
	return &id
}

func normalizePanelWidth(width string) (string, error) {
	switch strings.TrimSpace(width) {
	case "", "auto":
		return "auto", nil
	case "full":
		return "full", nil
	}
	return "", errors.New(navigationenums.ErrInvalidParam)
}

// validateField 校验 title/path/kind 基础规则（不含唯一性）。
func validateField(title, path, kind string) error {
	if title == "" {
		return errors.New(navigationenums.ErrInvalidParam)
	}
	if !strings.HasPrefix(path, "/") {
		return errors.New(navigationenums.ErrInvalidParam)
	}
	if !isValidKind(kind) {
		return errors.New(navigationenums.ErrInvalidKind)
	}
	return nil
}

// isValidKind 判断导航类型是否为受支持的位置（桌面 / 移动端各自的页眉与页脚）。
func isValidKind(kind string) bool {
	return kind == kindHeader || kind == kindHeaderMobile ||
		kind == kindFooter || kind == kindFooterMobile
}

// maxParentDepth 父链上溯深度上限：兜底历史脏数据形成的环（正常菜单不超过 3~4 层）。
const maxParentDepth = 64

// validateParent 校验父引用合法：自引用、成环、跨工程、跨类型、父项不存在一律拒绝。
//
// 此前 parent_id 直接落库、零校验：一旦把节点挂到自己或自己的后代下就形成环，
// 构建期树装配从根节点出发递归，环上的节点永远到不了根 —— 整棵子树在产物里
// 静默消失（后台列表照常显示），排查成本极高。
func (s *Service) validateParent(ctx context.Context, projectID, kind, selfID string, parentID *string) error {
	if parentID == nil {
		return nil
	}
	cur := strings.TrimSpace(*parentID)
	if cur == "" {
		return nil
	}
	if selfID != "" && cur == selfID {
		return errors.New(navigationenums.ErrInvalidParent)
	}
	for depth := 0; depth < maxParentDepth; depth++ {
		// 父链上溯带工程作用域：跨工程父引用本来就要拒绝，作用域让它同时受策略约束。
		parent, err := s.m.Get(ctx, projectID, cur)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New(navigationenums.ErrInvalidParent)
			}
			return err
		}
		if parent.ProjectID != projectID {
			return errors.New(navigationenums.ErrInvalidParent)
		}
		if kind != "" && parent.Kind != kind {
			return errors.New(navigationenums.ErrInvalidParent)
		}
		if parent.ParentID == nil || strings.TrimSpace(*parent.ParentID) == "" {
			return nil // 到达根节点，链路合法
		}
		next := strings.TrimSpace(*parent.ParentID)
		if selfID != "" && next == selfID {
			return errors.New(navigationenums.ErrInvalidParent)
		}
		cur = next
	}
	return errors.New(navigationenums.ErrInvalidParent)
}

// normalizeParentID 把空字符串父 ID 规范为 nil（表示根导航项）。
func normalizeParentID(p *string) *string {
	if p == nil {
		return nil
	}
	if strings.TrimSpace(*p) == "" {
		return nil
	}
	v := strings.TrimSpace(*p)
	return &v
}

// normalizeSource 规范化并校验菜单项来源三件套（sourceType/sourceID/target）。
// 规则：空值按 custom/self 处理；custom 来源忽略来源实体；非 custom 必须给出来源实体。
func normalizeSource(sourceType string, sourceID *string, target string) (st string, sid *string, tg string, err error) {
	st = normalizeSourceType(sourceType)
	if !isValidSourceType(st) {
		return "", nil, "", errors.New(navigationenums.ErrInvalidSource)
	}
	tg = normalizeTarget(target)
	if !isValidTarget(tg) {
		return "", nil, "", errors.New(navigationenums.ErrInvalidTarget)
	}
	sid = normalizeSourceID(sourceID)
	if st == sourceCustom {
		return st, nil, tg, nil
	}
	if sid == nil {
		return "", nil, "", errors.New(navigationenums.ErrInvalidSource)
	}
	return st, sid, tg, nil
}

// isValidSourceType 判断菜单项来源是否在白名单内。
func isValidSourceType(v string) bool {
	switch v {
	case sourceCustom, sourcePage, sourceArticle, sourceProduct, sourceCategory, sourceBlock:
		return true
	}
	return false
}

// isValidTarget 判断打开方式是否在白名单内。
func isValidTarget(v string) bool { return v == targetSelf || v == targetBlank }

// normalizeSourceType 空值规范为 custom。
func normalizeSourceType(v string) string {
	if v = strings.TrimSpace(v); v == "" {
		return sourceCustom
	}
	return v
}

// normalizeTarget 空值规范为 self。
func normalizeTarget(v string) string {
	if v = strings.TrimSpace(v); v == "" {
		return targetSelf
	}
	return v
}

// normalizeSourceID 空字符串来源实体规范为 nil。
func normalizeSourceID(p *string) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	if v == "" {
		return nil
	}
	return &v
}
