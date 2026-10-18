package ghrunner

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type tarEntry struct {
	name     string
	body     string
	mode     int64
	typeflag byte
	link     string
}

func writeTarGz(t *testing.T, path string, entries []tarEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typeflag
		if typ == 0 {
			typ = tar.TypeReg
		}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		hdr := &tar.Header{Name: e.name, Mode: mode, Typeflag: typ, Linkname: e.link, Size: int64(len(e.body))}
		if typ != tar.TypeReg {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestEveryPlatformGitHubShipsHasAPinnedDigest(t *testing.T) {
	for _, p := range []struct{ goos, goarch string }{
		{"darwin", "arm64"}, {"darwin", "amd64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"windows", "amd64"}, {"windows", "arm64"},
	} {
		d, ok := distributionFor(p.goos, p.goarch)
		if !ok || len(d.SHA256) != 64 || !strings.Contains(d.URL, "/v"+Version+"/") {
			t.Errorf("%s/%s: %+v %v", p.goos, p.goarch, d, ok)
		}
		if d.Zip != (p.goos == "windows") {
			t.Errorf("%s/%s: zip = %v", p.goos, p.goarch, d.Zip)
		}
	}
	if _, ok := distributionFor("freebsd", "amd64"); ok {
		t.Error("freebsd has no runner package, but got one")
	}
}

func TestInstallUnpacksWithModesAndInsideLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes and symlinks")
	}
	dir := t.TempDir()
	archive := filepath.Join(dir, "runner.tar.gz")
	writeTarGz(t, archive, []tarEntry{
		{name: "bin/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "run.sh", body: "#!/bin/sh\n", mode: 0o755},
		{name: "bin/Runner.Listener", body: "x", mode: 0o755},
		{name: "bin/listener", typeflag: tar.TypeSymlink, link: "Runner.Listener"},
	})
	dest := filepath.Join(dir, "install")
	if err := install(archive, false, dest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dest, "run.sh"))
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("run.sh not executable: %v %v", info, err)
	}
	if target, err := os.Readlink(filepath.Join(dest, "bin/listener")); err != nil || target != "Runner.Listener" {
		t.Fatalf("symlink: %q %v", target, err)
	}
	if _, err := os.Stat(filepath.Join(dest, installedMarker)); err != nil {
		t.Fatal("no installed marker")
	}
}

func TestInstallRefusesEntriesThatEscape(t *testing.T) {
	for name, entry := range map[string]tarEntry{
		"dotdot":        {name: "../evil", body: "x"},
		"absolute":      {name: "/tmp/evil", body: "x"},
		"link outside":  {name: "bin/out", typeflag: tar.TypeSymlink, link: "../../outside"},
		"link absolute": {name: "bin/abs", typeflag: tar.TypeSymlink, link: "/etc/passwd"},
		"dotdot inside": {name: "a/../../evil", body: "x"},
		"hard link":     {name: "bin/h", typeflag: tar.TypeLink, link: "run.sh"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			archive := filepath.Join(dir, "runner.tar.gz")
			writeTarGz(t, archive, []tarEntry{entry})
			dest := filepath.Join(dir, "install")
			if err := install(archive, false, dest); err == nil {
				t.Fatal("install accepted an escaping entry")
			}
			if _, err := os.Stat(dest); !os.IsNotExist(err) {
				t.Fatal("a refused archive left an install directory")
			}
		})
	}
}

// A link through a link: d -> . makes d/l land in the root, where its
// ../evil leaves the install. Checked against each name alone it looks fine.
func TestInstallRefusesALinkUnderAnotherLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	dir := t.TempDir()
	archive := filepath.Join(dir, "runner.tar.gz")
	writeTarGz(t, archive, []tarEntry{
		{name: "d", typeflag: tar.TypeSymlink, link: "."},
		{name: "d/l", typeflag: tar.TypeSymlink, link: "../evil"},
	})
	if err := os.WriteFile(filepath.Join(dir, "evil"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := install(archive, false, filepath.Join(dir, "install")); err == nil {
		t.Fatal("a link placed through another link was accepted")
	}
}

func TestFetchVerifiesTheDigestAndReusesAVerifiedCopy(t *testing.T) {
	body := []byte("runner package")
	sum := sha256.Sum256(body)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "old.tar.gz"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	bad := Distribution{URL: srv.URL + "/pkg.tar.gz", SHA256: strings.Repeat("0", 64)}
	if _, err := fetch(context.Background(), srv.Client(), bad, dir); err == nil {
		t.Fatal("a digest mismatch was accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "pkg.tar.gz")); !os.IsNotExist(err) {
		t.Fatal("a mismatched download was kept")
	}

	good := Distribution{URL: srv.URL + "/pkg.tar.gz", SHA256: hex.EncodeToString(sum[:])}
	for i := 0; i < 2; i++ {
		if _, err := fetch(context.Background(), srv.Client(), good, dir); err != nil {
			t.Fatal(err)
		}
	}
	if hits != 2 {
		t.Fatalf("downloads = %d, want 2 (the mismatch, then one verified copy reused)", hits)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.tar.gz")); !os.IsNotExist(err) {
		t.Fatal("an older package was left beside the new one")
	}
}

func TestRunnerHostIsAShortLabelSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"Akifs-MacBook-Pro.local": "akifs-macbook-pro",
		"":                        "computer",
		"weird_name!!":            "weird-name",
		strings.Repeat("a", 50):   strings.Repeat("a", 30),
	} {
		if got := runnerHost(in); got != want {
			t.Errorf("runnerHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseLineFollowsTheRunnersOwnWording(t *testing.T) {
	cases := map[string]Event{
		"2026-10-16 10:00:00Z: Listening for Jobs":                              {Kind: "listening"},
		"2026-10-16 10:00:05Z: Running job: build (macOS)":                      {Kind: "running", Job: "build (macOS)"},
		"2026-10-16 10:01:05Z: Job build (macOS) completed with result: Failed": {Kind: "completed", Job: "build (macOS)", Result: "Failed"},
	}
	for line, want := range cases {
		got, ok := parseLine(line)
		if !ok || got != want {
			t.Errorf("parseLine(%q) = %+v %v, want %+v", line, got, ok, want)
		}
	}
	if _, ok := parseLine("√ Connected to GitHub"); ok {
		t.Error("an unrelated line was parsed as an event")
	}
}
