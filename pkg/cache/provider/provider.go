package cacheprovider

import (
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
)

// Provider 缓存实现接口。
type Provider interface {
	Init(v *viper.Viper) error
	Close() error
	Client() (redis.UniversalClient, error)
	IsInited() bool
}
