package credprompt

import (
	"os"
	"testing"
)

// The test binary is also the helper git and ssh run in the integration
// tests, exactly as CommitTree's own binary is in the app.
func TestMain(m *testing.M) {
	if os.Getenv(EnvSock) != "" {
		os.Exit(RunHelper(os.Args[1:], os.Getenv, os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}
