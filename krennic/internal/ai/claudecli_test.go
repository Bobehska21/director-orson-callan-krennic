package ai

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestClaudeCLIUsesSupportedPermissionMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake is Unix-only")
	}

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	binPath := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(argsPath) + "\nprintf '%s\\n' '{\"result\":\"ok\",\"total_cost_usd\":0.01}'\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	provider := &ClaudeCLIProvider{Bin: binPath}
	if _, err := provider.Complete(context.Background(), CompletionRequest{User: "review this"}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read captured args: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(data)), "\n")
	if !containsSequence(args, "--permission-mode", claudePermissionMode) {
		t.Fatalf("expected --permission-mode %q in args, got %q", claudePermissionMode, args)
	}
	if contains(args, "dontAsk") {
		t.Fatalf("legacy dontAsk permission mode must not be used: %q", args)
	}
}

func containsSequence(items []string, first, second string) bool {
	for i := 0; i+1 < len(items); i++ {
		if items[i] == first && items[i+1] == second {
			return true
		}
	}
	return false
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
