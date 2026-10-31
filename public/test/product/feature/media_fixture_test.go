package feature

import (
	"fmt"
	"os"
	"testing"

	"github.com/spf13/viper"
	"go_wp/pkg/upload"
)

const testMediaBaseURL = "https://media.example.test/storage"

func initFixtureMedia() error {
	cfg := viper.New()
	cfg.Set("upload.default_provider", "local")
	cfg.Set("upload.base_url", testMediaBaseURL)
	return upload.Init(cfg)
}

func TestMain(m *testing.M) {
	if err := initFixtureMedia(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	if err := upload.Close(); err != nil && code == 0 {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}
