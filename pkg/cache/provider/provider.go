package cacheprovider

import (
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
)

type Provider interface {
	Init(v *viper.Viper) error
	Close() error
	Client() (redis.UniversalClient, error)
	IsInited() bool
}
