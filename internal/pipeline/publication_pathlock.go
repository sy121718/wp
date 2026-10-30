package pipeline

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

// PublicationPathLocker 访问面路径的**进程内**互斥（AGENTS.md 不变量 2：两条发布路径共用同一激活面）。
//
// 要解决的问题：一条访问路径的「切换」不是一次原子操作，而是「读现场 → 判断 → 写符号链接」三步。
// 归属保护（路由行条件写 / DeactivateReq 带 PageID）挡得住「删掉别人的 DB 行」，挡不住
// 「别人在这三步之间把同一条链接换成他的，我再 os.Remove」—— 那条链接会从线上消失，
// 而两边都认为自己是对的。Page 与 PresentationInstance 是两条独立的发布路径，它们的
// 实体锁互不相干，唯一能拦住这种交错的是一把**按访问路径**分的锁。
//
// **单实例约束**（与 LocalPublicationStore 的文件头同一前提）：锁只在本进程内生效。
// 激活状态就是本机磁盘上的一组符号链接，多实例下同一 URL 各有各的那组，锁无从协调；
// 那种部署需要 DB advisory lock 或「激活状态入库 + 版号 CAS」，不在此契约内。
//
// **不可重入**：同一 goroutine 重复取同一路径的锁会**死锁**（Go 的 sync.Mutex 没有属主概念）。
// 因此约定调用层级（自外向内，只允许外层持锁）：
//
//	实体行锁（page / presentation 各自的 LockForActivation）
//	→ WithPaths（本契约，唯一的路径锁入口）
//	  → 复核归属 / 复核账本身份（读 DB 或读链接）
//	  → LocalPublicationStore.Activate / Deactivate（**内部不再取本锁**）
//	→ 释放
//
// 这条层级是双向的：Store 侧永不取锁（它就是被锁保护的最内层副作用），调用方永远从
// WithPaths 进。这样「同一条路径」的并发写只可能有两处出现在同一锁期，不存在「一处持锁
// 一处绕过」的半保护状态。
type PublicationPathLocker interface {
	// WithPaths 持有 paths 上全部路径锁的前提下执行 fn；fn 返回前锁不释放。
	//
	// 语义约定（实现必须遵守，调用方按此使用）：
	//   - paths 里的元素是**规范化访问路径**（"" 忽略），调用方已按 pipeline.NormalizeURL 归一；
	//   - 多个路径按**锁分片索引**排序后依次加锁、逆序释放 —— 排序依据必须是分片而不是
	//     路径字符串：两条不同路径可能落在同一分片，按路径去重排不出稳定序，同一分片
	//     被取两次会自锁；
	//   - fn 内的 panic 也要释放锁（实现负责 defer）；
	//   - fn 执行期间不得再次调用 WithPaths（不可重入）。
	WithPaths(paths []string, fn func() error) error
}

// 默认共享实例：进程内所有会写访问面符号链接的服务**必须**用同一个指针。
//
// 为什么是包级单例而不是「各服务各自 new 一个」：锁的效力来自「同一把锁」，两份
// ProcessPathLocker 是两把互不相干的锁 —— 漏装配与不装配的后果完全相同，而漏装配
// 不会编译报错、不会测试失败，只会在线上以「偶尔消失的一条链接」出现。包级单例把
// 「必须共用」变成**默认成立**，调用方只有显式传入别的实例才会分叉。
//
// 生命周期：进程级、无 Close、无状态。测试要隔离时用 NewProcessPathLocker() 另起一个
// （此时它的互斥范围只覆盖该测试内共用它的那些服务）。
var defaultPathLocker = &ProcessPathLocker{}

// NewProcessPathLocker 新建一个独立的进程内路径锁（测试隔离用）。
//
// 生产代码一律用 DefaultPathLocker()：两份 ProcessPathLocker 是两把互不相干的锁，
// 各自 new 一个等于谁都没被保护。这里提供构造函数只为让测试能把"锁被调用了多少次、
// 锁了哪些路径"观察出来，而不必替换全局单例。
func NewProcessPathLocker() *ProcessPathLocker { return &ProcessPathLocker{} }

// WithPathLock 在 locker 上持锁执行 fn；**locker 为 nil 时 fail closed**。
//
// 为什么必须有这一层而不是让调用方直接 locker.WithPaths：nil **接口**上调用方法会在
// 任何实现代码之前就 panic（SIGSEGV），实现内部的 nil 判断根本没机会跑。凡是「可能没有
// 锁」的入口都必须先过这里 —— 生产侧由 page / publisher 的 withPaths 统一调用，测试侧
// 也用它证明 nil 接口的语义。
func WithPathLock(locker PublicationPathLocker, paths []string, fn func() error) error {
	if fn == nil {
		return nil
	}
	if locker == nil {
		return ErrPathLockerUnavailable
	}
	return locker.WithPaths(paths, fn)
}

// DefaultPathLocker 返回进程内共享的访问面路径锁。所有 page / presentation /
// Publisher 入口都必须使用它（或装配层显式传入的同一个替换实例），不得各自 new。
func DefaultPathLocker() PublicationPathLocker { return defaultPathLocker }

// ErrPathLockerUnavailable 未提供路径锁且无默认可用（注入 nil 时）。
//
// 访问面激活的「读现场 → 判断 → 改符号链接」三步必须有互斥才正确，缺锁时**不执行**
// 副作用（fail closed）：宁可这次处置失败留待收敛重放，也不能在无互斥下删掉/覆盖
// 另一实体刚上线的链接。
var ErrPathLockerUnavailable = errors.New("访问面路径锁不可用：未注入且无进程默认实例")

// pathLockShardCount 分片数：一次加锁退化为一次 sync.Mutex，无系统调用；
// 64 片让无关路径（含不同工程的同名路径）并行，同一条路径恒定落在同一片。
const pathLockShardCount = 64

// ProcessPathLocker 进程内分片互斥实现。零值可用。
type ProcessPathLocker struct {
	shards [pathLockShardCount]sync.Mutex
}

// 编译期断言：ProcessPathLocker 满足 PublicationPathLocker。
var _ PublicationPathLocker = (*ProcessPathLocker)(nil)

// WithPaths 实现 PublicationPathLocker。
func (l *ProcessPathLocker) WithPaths(paths []string, fn func() error) error {
	if fn == nil {
		return nil
	}
	if l == nil {
		// typed nil：方法可被调用（接收者是指针），必须在这里挡住，否则下标访问 nil 数组 panic。
		return ErrPathLockerUnavailable
	}
	shards := l.shardIndexes(paths)
	for _, i := range shards {
		l.shards[i].Lock()
	}
	defer func() {
		for i := len(shards) - 1; i >= 0; i-- {
			l.shards[shards[i]].Unlock()
		}
	}()
	return fn()
}

// shardIndexes 归一 → 去重 → 按**分片索引**升序。
//
// 排序键是分片索引而不是路径：分片是真正被加锁的对象，按它排序才能保证
// 「不同路径同分片」只加一次、「不同分片」按固定全序加锁（不会 A→B / B→A 互锁）。
func (l *ProcessPathLocker) shardIndexes(paths []string) []int {
	seen := make(map[int]bool, len(paths))
	out := make([]int, 0, len(paths))
	for _, p := range paths {
		key := pathLockKey(p)
		if key == "" {
			continue
		}
		i := shardIndexOf(key)
		if seen[i] {
			continue
		}
		seen[i] = true
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

// pathLockKey 锁键：**归一后的物理发布路径**，不含工程与语言前缀。
//
// 为什么键必须落在物理路径上：锁保护的是「public/active 下那一个符号链接」。
// 两条不同的逻辑标识（不同工程 / 不同语言的同一路径）如果映射到同一个物理链接，
// 用逻辑键就会得到两把互不相干的锁，交错照样发生。反过来，同一逻辑路径在不同
// 语言下物理不同（/{lang}/path 与 /path），用同一个物理键也不会互相阻塞。
// 物理等价性（源码证据，非推测）：LocalPublicationStore.Activate/Deactivate/Inspect
// 三者第一行都是 p, err := NormalizeURL(path)，再 filepath.Join(ActiveRoot, relActivePath(p))
// 落到链接（publication.go:78/167/183 与 relActivePath:332）。也就是说「同一个
// NormalizeURL 结果 ⇔ 同一个符号链接」，所以用 NormalizeURL 结果当锁键与物理路径一一对应。
// 语言维度不需要额外并入键：多语言访问路径由 LangURLRule.Path 产出（lang.go:182-205），
// 非默认语言已在**路径字符串里**带 /{短码} 前缀（/en/about），默认语言无前缀（/about），
// 因此 lang 不同的两个访问路径本来就归一成不同字符串、落到不同的链接上；把 lang 再拼进
// 键反而会把「同一物理链接」拆成两把锁。
//
// 归一失败时退化为修剪后的原串：store 侧同样会拒绝该路径（isError → 不落盘），
// 这里只需要一个稳定键，不需要在这里重复一份校验策略。
func pathLockKey(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if norm, err := NormalizeURL(p); err == nil {
		return norm
	}
	return p
}

// shardIndexOf FNV-1a（与 store 的 locatorHash 同族，但独立实现：锁键的哈希与产物
// 定位器的哈希是两件事，共用一个函数会让「改定位器哈希」意外移动锁的分布）。
func shardIndexOf(key string) int {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(key); i++ {
		h ^= uint64(key[i])
		h *= prime64
	}
	return int(h % pathLockShardCount)
}
