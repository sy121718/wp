package feature

import (
	"os"
	"testing"

	"go_wp/public/test/support"
)

func TestMain(m *testing.M) {
	os.Exit(support.RunTestMainWithLeakCheck(m))
}
