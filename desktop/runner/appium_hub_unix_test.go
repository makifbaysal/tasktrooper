//go:build unix

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A runner killed before it could stop its hub leaves the hub running in a
// process group of its own. These tests stand in for that hub with the fake
// appium (this test binary, through a link named `appium` so its command line
// reads like the real one) started here rather than by the manager under test,
// and write the record a killed runner would have left.

type leftover struct {
	dir    string
	link   string
	base   string
	port   string
	record string
	pid    int
}

func newLeftoverDir(t *testing.T) (dir, link string) {
	t.Helper()
	dir = t.TempDir()
	t.Setenv(fakeAppiumDirEnv, dir)
	link = filepath.Join(dir, "appium")
	if err := os.Symlink(os.Args[0], link); err != nil {
		t.Fatal(err)
	}
	return dir, link
}

// startLeftover starts a hub on a free port the way a runner would have, and
// reaps it here — a killed runner's hub is reaped by init, never left a zombie.
func startLeftover(t *testing.T, dir, link string) leftover {
	t.Helper()
	port := freeLoopbackPort(t)
	cmd := exec.Command(link, "--address", "127.0.0.1", "--port", port, "--log-no-colors")
	group, err := startProcessGroup(cmd)
	if err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		group.release()
		close(exited)
	}()
	t.Cleanup(func() {
		killProcessGroup(group, time.Second, exited)
		<-exited
	})
	base := "http://127.0.0.1:" + port
	eventually(t, 10*time.Second, func() bool { return hubAnswers(base) }, "the leftover hub never answered")
	return leftover{dir: dir, link: link, base: base, port: port, record: filepath.Join(dir, "appium-hub.json"), pid: cmd.Process.Pid}
}

func (l leftover) writeRecord(t *testing.T, rec hubRecord) {
	t.Helper()
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeHubRecord(l.record, raw); err != nil {
		t.Fatal(err)
	}
}

func (l leftover) start(t *testing.T) string {
	t.Helper()
	start, _, err := processIdentity(context.Background(), l.pid)
	if err != nil {
		t.Fatalf("reading the leftover's start time: %v", err)
	}
	return start
}

func leftoverTimings() hubTimings {
	return hubTimings{
		idleAfter:    300 * time.Millisecond,
		readyTimeout: 15 * time.Second,
		reapEvery:    50 * time.Millisecond,
		cooldown:     time.Minute,
		stopGrace:    2 * time.Second,
	}
}

func spawnCount(t *testing.T, dir string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "spawns.log"))
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Split(strings.TrimSpace(string(raw)), "\n"))
}

func TestProcessIdentityReadsThisProcess(t *testing.T) {
	start, command, err := processIdentity(context.Background(), os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Fields(start)) != 5 || !strings.Contains(command, filepath.Base(os.Args[0])) {
		t.Fatalf("start %q command %q, want ps's lstart and this binary's command line", start, command)
	}
	again, _, err := processIdentity(context.Background(), os.Getpid())
	if err != nil || again != start {
		t.Fatalf("a second read of one process's start time = %q (%v), want %q", again, err, start)
	}
}

func TestTheHubThisRunnerStartedIsWrittenDownAndForgottenWhenStopped(t *testing.T) {
	f := newHubFixture(t, nil)
	mustEnsure(t, f.hub)

	raw, err := os.ReadFile(f.hub.recordPath)
	if err != nil {
		t.Fatalf("no record of the hub this runner started: %v", err)
	}
	var rec hubRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.Fields(f.spawns(t)[0])[0])
	if rec.PID != pid || rec.Hub != f.base || len(strings.Fields(rec.Start)) != 5 {
		t.Fatalf("record = %+v, want pid %d on %s with its start time", rec, pid, f.base)
	}

	f.hub.close()
	if _, err := os.Stat(f.hub.recordPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the record outlived the hub it names (%v)", err)
	}
}

func TestAHubAKilledRunnerLeftIsTakenBackAndStoppedWhenIdle(t *testing.T) {
	dir, link := newLeftoverDir(t)
	l := startLeftover(t, dir, link)
	l.writeRecord(t, hubRecord{PID: l.pid, Start: l.start(t), Hub: l.base})
	h := testHub(t, config{appiumBin: link, appiumBaseURL: l.base}, leftoverTimings(), dir)

	mustEnsure(t, h)
	if got := spawnCount(t, dir); got != 1 {
		t.Fatalf("%d spawns, want only the leftover: a hub this runner can prove it started is not started again", got)
	}
	eventually(t, 10*time.Second, func() bool { return !hubAnswers(l.base) }, "the taken-back hub outlived its idle time")
	eventually(t, 5*time.Second, func() bool { return !processAlive(l.pid) }, "the taken-back hub's process is still running")
	eventually(t, 5*time.Second, func() bool {
		_, err := os.Stat(l.record)
		return errors.Is(err, os.ErrNotExist)
	}, "the record outlived the hub it names")
}

func TestAtStartupALeftoverIsTakenBackWithoutACall(t *testing.T) {
	dir, link := newLeftoverDir(t)
	l := startLeftover(t, dir, link)
	l.writeRecord(t, hubRecord{PID: l.pid, Start: l.start(t), Hub: l.base})
	h := testHub(t, config{appiumBin: link, appiumBaseURL: l.base}, leftoverTimings(), dir)

	h.resume()

	eventually(t, 10*time.Second, func() bool { return !processAlive(l.pid) },
		"a leftover nothing calls for was left running")
}

func TestCloseStopsATakenBackHub(t *testing.T) {
	dir, link := newLeftoverDir(t)
	l := startLeftover(t, dir, link)
	l.writeRecord(t, hubRecord{PID: l.pid, Start: l.start(t), Hub: l.base})
	timings := leftoverTimings()
	timings.idleAfter = time.Hour
	h := testHub(t, config{appiumBin: link, appiumBaseURL: l.base}, timings, dir)

	mustEnsure(t, h)
	h.close()

	if processAlive(l.pid) || hubAnswers(l.base) {
		t.Fatal("the taken-back hub outlived close")
	}
}

// Anything short of the whole proof is somebody else's hub: adopted, used, and
// never signalled — not by the idle stop, and not by close.
func TestARecordThatDoesNotProveItIsAdoptedAndNeverStopped(t *testing.T) {
	dir, link := newLeftoverDir(t)
	l := startLeftover(t, dir, link)

	theirs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"value":{"ready":true}}`))
	}))
	t.Cleanup(theirs.Close)

	thisStart, _, err := processIdentity(context.Background(), os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		base string
		rec  hubRecord
	}{
		"another start time": {l.base, hubRecord{PID: l.pid, Start: "Thu Jan  1 00:00:00 1970", Hub: l.base}},
		"another address":    {l.base, hubRecord{PID: l.pid, Start: l.start(t), Hub: "http://127.0.0.1:1"}},
		"not on our port":    {theirs.URL, hubRecord{PID: l.pid, Start: l.start(t), Hub: theirs.URL}},
		"not appium":         {theirs.URL, hubRecord{PID: os.Getpid(), Start: thisStart, Hub: theirs.URL}},
		"a dead pid":         {l.base, hubRecord{PID: 999_999, Start: l.start(t), Hub: l.base}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			l.writeRecord(t, tc.rec)
			timings := leftoverTimings()
			timings.idleAfter = 50 * time.Millisecond
			h := testHub(t, config{appiumBin: link, appiumBaseURL: tc.base}, timings, dir)

			mustEnsure(t, h)
			time.Sleep(400 * time.Millisecond)
			h.close()

			if !hubAnswers(tc.base) || !processAlive(l.pid) {
				t.Fatal("a hub the record did not prove was ours was stopped")
			}
			if got := spawnCount(t, dir); got != 1 {
				t.Fatalf("%d spawns; an answering hub was started again", got)
			}
		})
	}
}

func TestStopLeftoverNeverSignalsAProcessWhoseStartTimeMoved(t *testing.T) {
	dir, link := newLeftoverDir(t)
	l := startLeftover(t, dir, link)
	p := &hubProcess{pid: l.pid, start: "Thu Jan  1 00:00:00 1970", done: make(chan struct{})}

	stopLeftover(p, time.Second)

	if !processAlive(l.pid) {
		t.Fatal("a process that is not the recorded one was signalled")
	}
	if p.running() {
		t.Fatal("stopLeftover did not let go of a process that is not the recorded one")
	}
}
