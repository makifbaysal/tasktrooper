package simrun

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFindXcodeProjectPrefersTheWorkspaceOverTheProjectsOwn(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "App.xcodeproj/project.xcworkspace", "App.xcworkspace", "Pods/Pods.xcodeproj")

	flag, path, err := findXcodeProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if flag != "-workspace" || filepath.Base(path) != "App.xcworkspace" {
		t.Fatalf("got %s %s, want -workspace App.xcworkspace", flag, path)
	}
}

func TestFindXcodeProjectFallsBackToTheProjectOneLevelDown(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "ios/App.xcodeproj/project.xcworkspace")

	flag, path, err := findXcodeProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if flag != "-project" || filepath.Base(path) != "App.xcodeproj" {
		t.Fatalf("got %s %s, want -project ios/App.xcodeproj", flag, path)
	}
}

func TestFindXcodeProjectSaysWhenThereIsNone(t *testing.T) {
	if _, _, err := findXcodeProject(t.TempDir()); err == nil {
		t.Fatal("want an error for a directory with no Xcode project")
	}
}

func TestNewestMatchPrefersTheNamedOneThenTheNewest(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.apk")
	newer := filepath.Join(dir, "new.apk")
	for _, p := range []string{old, newer} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	if got, _ := newestMatch(dir, ".apk", ""); got != newer {
		t.Fatalf("newest = %s, want %s", got, newer)
	}
	if got, _ := newestMatch(dir, ".apk", "old.apk"); got != old {
		t.Fatalf("preferred = %s, want %s", got, old)
	}
	if _, err := newestMatch(dir, ".app", ""); err == nil {
		t.Fatal("want an error when nothing matches")
	}
}

func TestValidDeviceSerialRefusesAnythingThatIsNotOne(t *testing.T) {
	for _, s := range []string{"R58M123ABC", "192.168.1.5:5555", "emulator-5554"} {
		if !validDeviceSerial(s) {
			t.Errorf("%q refused", s)
		}
	}
	for _, s := range []string{"", "a b", "x;rm -rf /", "$(id)"} {
		if validDeviceSerial(s) {
			t.Errorf("%q accepted", s)
		}
	}
}
