package embeddedpg

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartExplainsAnOSLevelExecFailure(t *testing.T) {
	dataDir := t.TempDir()
	cacheDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cacheDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "bin", "pg_ctl"), []byte("not a real binary"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stop, err := Start(context.Background(), dataDir, cacheDir)
	if err == nil {
		if stop != nil {
			stop()
		}
		t.Fatal("want an error when the postgres binaries cannot be launched")
	}

	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("error does not carry the underlying OS-level exec failure: %v", err)
	}
	if !strings.Contains(err.Error(), "antivirus") {
		t.Fatalf("error message does not explain the likely cause to a Windows user: %v", err)
	}
	if strings.TrimSpace(err.Error()) == "embedded postgres failed to start" {
		t.Fatalf("error message is only the generic text, no OS-level detail: %v", err)
	}
}
