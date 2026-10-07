package embeddedpg

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessAliveTellsALiveProcessFromAnExitedOne(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Fatal("this test process reads as dead")
	}

	exited := exec.Command(os.Args[0], "-test.run=^$")
	if err := exited.Run(); err != nil {
		t.Fatal(err)
	}
	if processAlive(exited.Process.Pid) {
		t.Fatalf("pid %d reads as alive after it exited and was reaped", exited.Process.Pid)
	}

	for _, pid := range []int{0, -1} {
		if processAlive(pid) {
			t.Fatalf("pid %d reads as alive", pid)
		}
	}
}

func TestMarkExtractedOnlyStandsInForAWindowsPgCtl(t *testing.T) {
	cases := []struct {
		name       string
		existing   map[string]string
		wantMarker bool
		wantBody   string
	}{
		{name: "windows layout", existing: map[string]string{"pg_ctl.exe": "MZ"}, wantMarker: true, wantBody: ""},
		{name: "unix layout keeps the real pg_ctl", existing: map[string]string{"pg_ctl": "ELF"}, wantMarker: true, wantBody: "ELF"},
		{name: "nothing extracted yet", existing: map[string]string{}, wantMarker: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cacheDir := t.TempDir()
			bin := filepath.Join(cacheDir, "bin")
			if err := os.MkdirAll(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, body := range tc.existing {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
			}

			if err := markExtracted(cacheDir); err != nil {
				t.Fatal(err)
			}

			got, err := os.ReadFile(filepath.Join(bin, "pg_ctl"))
			if !tc.wantMarker {
				if !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("bin/pg_ctl was created with no extracted binaries to vouch for (err=%v)", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.wantBody {
				t.Fatalf("bin/pg_ctl = %q, want %q", got, tc.wantBody)
			}
		})
	}
}

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

func TestStopAdoptedRunsPgCtlFastStopOnTheDataDir(t *testing.T) {
	var gotName string
	var gotArgs []string
	stopAdopted("/cache", "/data/postgres", func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotName, gotArgs = name, args
		return nil, nil
	})
	if gotName != pgCtlPath("/cache") {
		t.Fatalf("name = %q", gotName)
	}
	want := "stop -D /data/postgres -m fast -w -t 30"
	if got := strings.Join(gotArgs, " "); got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestStopAdoptedSurvivesAFailingPgCtl(t *testing.T) {
	stopAdopted("/cache", "/data/postgres", func(context.Context, string, ...string) ([]byte, error) {
		return []byte("boom"), errors.New("exit 1")
	})
}
