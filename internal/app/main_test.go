package app

import (
	"os"
	"testing"

	"git-ui/internal/credprompt"
)

// The test binary doubles as the credential helper (see credprompt).
func TestMain(m *testing.M) {
	if os.Getenv(credprompt.EnvSock) != "" {
		os.Exit(credprompt.RunHelper(os.Args[1:], os.Getenv, os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}
