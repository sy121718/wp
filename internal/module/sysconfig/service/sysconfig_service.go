// Package sysconfigservice 实现 sysconfig 模块业务用例（GetGroup / ListGroups / SetGroup）。
//
// 本模块的写路径只有「整组替换」一种，且**必须**带乐观锁版本号：整组读-改-写若不带
// version 条件，两个管理员改同一组的不同键时后写者会静默覆盖前者（迁移 484 文件头）。
// 冲突一律返回明确错误打回给人，不自动合并、不重试、不静默胜出。
package sysconfigservice

import (
	"sync"
	"time"

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
	// countryLabels 国家/地区「码 → 当前语言显示名」的进程内索引（键 = 归一语言）。
	//
	// 为什么在这里缓存而不是让每个消费方各建一层：sys_area 是参考数据字典，而订单详情
	// 一次渲染可能解析多个地址、列表页一屏几十行 —— 每个消费方各缓存一份，等于同一份
	// 数据在进程里存 N 份、各查一次库。Service 是装配期构造的单实例，缓存挂在它上面天然
	// 只有一份。
	//
	// 值是 countryLabelEntry（索引 + 过期时刻），按 TTL 失效；用 sync.Map 而不是
	// map + 互斥：读远多于写（每种语言每 TTL 才写一次），且条目之间互不影响。
	countryLabels sync.Map
}

// countryLabelEntry 一种语言的「码 → 显示名」索引及其过期时刻。
type countryLabelEntry struct {
	index   map[string]string
	expires time.Time
}

// countryLabelTTL 国家名索引的存活时长。
//
// 为什么要过期、而不是一直缓存到进程结束：sys_area 眼下是迁移 seed 的静态字典，但
// **字典一旦有了编辑入口**（后台字典 CRUD 尚在待办），永久缓存就意味着「改完必须重启
// 进程才生效」—— 那是最难查的一类问题（页面看着正常、就是不改）。TTL 把最坏延迟变成
// 有界值，且不必现在就给字典造一条失效广播链（编辑入口还不存在，广播无处触发）。
//
// 取值依据：参考数据低频变更，5 分钟对运营足够快；对数据库则意味着**每种语言每 5 分钟
// 最多一次全表读**（247 行）。
//
// 将来加字典编辑页时：在保存路径清掉 countryLabels（Range + Delete）即可即时生效，
// 本 TTL 退化为兜底 —— 那时它也仍应保留。
const countryLabelTTL = 5 * time.Minute

// 编译期断言：本模块实现对外两条契约（宽的服务面 + 只读窄口）。
var (
	_ sysconfigcontract.Service      = (*Service)(nil)
	_ sysconfigcontract.ConfigReader = (*Service)(nil)
	// 字典只读口（后台页面下拉供数）：形状对不上时在这里编译错，
	// 而不是等到页面渲染出一片空下拉才发现。
	_ sysconfigcontract.DictReader = (*Service)(nil)
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
