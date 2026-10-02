// Package sysconfigservice 实现 sysconfig 模块业务用例（GetGroup / ListGroups / SetGroup）。
//
// 本模块的写路径只有「整组替换」一种，且**必须**带乐观锁版本号：整组读-改-写若不带
// version 条件，两个管理员改同一组的不同键时后写者会静默覆盖前者（迁移 484 文件头）。
// 冲突一律返回明确错误打回给人，不自动合并、不重试、不静默胜出。
package sysconfigservice

import (
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	sysconfigmodel "go_wp/internal/module/sysconfig/model"
	"go_wp/pkg/utils"
)

// Service sysconfig 模块业务实现。
type Service struct {
	m *sysconfigmodel.Model
	// onChanged 保存成功后的主动刷新回调（装配层注入；本批注入的是 pkg/i18n 的
	// Invalidate）。未注入 = 不刷新（测试装配），保存照样成功，消费方会在下一次
	// 定时刷新时看到新值 —— 刷新失败/缺失不改变「配置已落库」这个事实。
	onChanged func()
}

// 编译期断言：本模块实现对外两条契约（宽的服务面 + 只读窄口）。
var (
	_ sysconfigcontract.Service      = (*Service)(nil)
	_ sysconfigcontract.ConfigReader = (*Service)(nil)
)

// NewService 构造（model 与刷新回调注入，不持有 *gorm.DB）。
func NewService(m *sysconfigmodel.Model, onChanged func()) *Service {
	return &Service{m: m, onChanged: onChanged}
}

// groupOf 实体 → 对外形状。
func groupOf(e *sysconfigmodel.SysConfigEntity) *sysconfigdto.Group {
	if e == nil {
		return nil
	}
	data := map[string]any(nil)
	if len(e.ConfigData) > 0 {
		data = make(map[string]any, len(e.ConfigData))
		for k, v := range e.ConfigData {
			data[k] = v
		}
	}
	return &sysconfigdto.Group{
		Key:        e.GroupKey,
		Name:       e.GroupName,
		Data:       data,
		Remark:     e.Remark,
		Status:     e.Status,
		Version:    e.Version,
		UpdateTime: utils.NewJSONTime(e.UpdateTime),
	}
}
