// Package lock 提供基础锁能力。
//
// 边界说明：
// - 只解决“互斥访问”问题，不负责业务幂等语义本身
// - 进程内场景优先使用 local 实现
// - 跨实例场景再使用 redis 实现
// - 不建议把锁滥用为数据库一致性的第一选择，唯一索引/事务/乐观锁仍应优先
package lock

import (
	"context"
	"time"
)

// Locker 定义统一锁接口。
//
// 使用建议：
// - 本地互斥优先使用 NewLocal()
// - 跨实例互斥再使用 NewRedis()
type Locker interface {
	Acquire(ctx context.Context, key string, ttl time.Duration) (Lease, error)
}

// Lease 表示一次成功获取到的锁租约。
//
// 约束：
// - 调用方获取到 Lease 后，应在任务完成后显式 Release
// - 不要依赖 TTL 到期作为正常释放路径
type Lease interface {
	Key() string
	Release(ctx context.Context) error
}
