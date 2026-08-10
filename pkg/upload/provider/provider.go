package uploadprovider

import (
	"context"
	"io"

	"github.com/spf13/viper"
)

type File struct {
	Filename    string
	Reader      io.Reader
	Size        int64
	ContentType string
}

type Request struct {
	Route        string
	Directory    string
	ObjectKey    string
	PreserveName bool
}

type RuntimeConfig struct {
	Mark      string
	Provider  string
	Endpoint  string
	Bucket    string
	Region    string
	BaseURL   string
	AccessKey string
	SecretKey string
	Extra     map[string]string
}

type Result struct {
	Provider string
	Key      string
	URL      string
	Size     int64
}

type Provider interface {
	Name() string
	Init(v *viper.Viper) error
	Close() error
	Upload(ctx context.Context, cfg RuntimeConfig, file File, req Request) (Result, error)
}
