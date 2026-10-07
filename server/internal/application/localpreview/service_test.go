package localpreview

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type stubTasks struct {
	task domain.BoardTask
}

func (s stubTasks) Get(context.Context, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return s.task, nil
}

type stubRepos struct {
	root string
}

func (s stubRepos) ResolveRootPath(context.Context, uuid.UUID) (string, error) {
	return s.root, nil
}

type stubGit struct {
	has bool
}

func (s stubGit) HasGit(string) bool { return s.has }
func (s stubGit) EnsureTaskWorkspace(_ context.Context, _, workspacePath, _ string) error {
	return os.MkdirAll(workspacePath, 0o755)
}

func newTestService(t *testing.T, root string) *Service {
	t.Helper()
	workspaceRoot := t.TempDir()
	return NewService(Deps{
		Tasks:         stubTasks{task: domain.BoardTask{ID: uuid.New(), Key: "T-9", TaskNumber: 9}},
		Repositories:  stubRepos{root: root},
		Git:           stubGit{has: true},
		WorkspaceRoot: workspaceRoot,
	})
}

func TestStartDetectsTheURLTheCommandPrints(t *testing.T) {
	svc := newTestService(t, t.TempDir())
	repositoryID, taskID := uuid.New(), uuid.New()

	preview, err := svc.Start(context.Background(), repositoryID, taskID, `echo "Local: http://localhost:4321/"; sleep 5`)
	require.NoError(t, err)

	assert.Contains(t, []domain.LocalPreviewStatus{domain.LocalPreviewStarting, domain.LocalPreviewRunning}, preview.Status)

	require.Eventually(t, func() bool {
		p, ok := svc.Status(repositoryID)
		return ok && p.Status == domain.LocalPreviewRunning
	}, 3*time.Second, 20*time.Millisecond)

	p, ok := svc.Status(repositoryID)
	require.True(t, ok)
	assert.Equal(t, "http://localhost:4321/", p.URL)
	assert.NotEmpty(t, p.LogTail)

	svc.Stop(repositoryID)
	_, ok = svc.Status(repositoryID)
	assert.False(t, ok, "a stopped preview is no longer the repository's active one")
}

func TestStartingASecondPreviewReplacesTheFirst(t *testing.T) {
	svc := newTestService(t, t.TempDir())
	repositoryID := uuid.New()
	secondTaskID := uuid.New()

	_, err := svc.Start(context.Background(), repositoryID, uuid.New(), "sleep 30")
	require.NoError(t, err)

	second, err := svc.Start(context.Background(), repositoryID, secondTaskID, "sleep 30")
	require.NoError(t, err)
	assert.Equal(t, secondTaskID, second.TaskID)

	p, ok := svc.Status(repositoryID)
	require.True(t, ok)
	assert.Equal(t, secondTaskID, p.TaskID, "only the second preview may be the repository's active one")

	svc.Stop(repositoryID)
	_, ok = svc.Status(repositoryID)
	assert.False(t, ok)
}

func TestAFailedPreviewStaysVisibleUntilCleared(t *testing.T) {
	svc := newTestService(t, t.TempDir())
	repositoryID, taskID := uuid.New(), uuid.New()

	_, err := svc.Start(context.Background(), repositoryID, taskID, `echo "Another dev server is already running"; exit 1`)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		p, ok := svc.Status(repositoryID)
		return ok && p.Status == domain.LocalPreviewFailed
	}, 3*time.Second, 20*time.Millisecond, "a preview that died must still be reported, not vanish")

	p, _ := svc.Status(repositoryID)
	assert.Equal(t, taskID, p.TaskID)
	assert.NotEmpty(t, p.Detail)
	assert.Contains(t, p.LogTail, "Another dev server is already running")
	assert.Empty(t, loadState(svc.workspaceRoot), "an exited process is never persisted for the next boot to signal")

	svc.Stop(repositoryID)
	_, ok := svc.Status(repositoryID)
	assert.False(t, ok)
}

func TestCommandMatchesOnlyTheCommandThatWasStarted(t *testing.T) {
	cases := []struct {
		name    string
		live    string
		started string
		want    bool
	}{
		{"still the shell running it", "sh -c npm run dev", "npm run dev", true},
		{"the shell exec'd it", "sleep 30", "sleep 30", true},
		{"npm through its node shebang", "node /usr/local/bin/npm run dev", "npm run dev", true},
		{"the pid now runs something else", "/usr/bin/vim notes.txt", "npm run dev", false},
		{"the pid is gone or unreadable", "", "npm run dev", false},
		{"nothing was recorded", "sh -c npm run dev", "", false},
		{"only whitespace was recorded", "sh -c npm run dev", "   ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, commandMatches(tc.live, tc.started))
		})
	}
}

// A Maven/Gradle wrapper execs java, so the pid's command line no longer
// contains the command that started it.
func TestJVMWrapperPreviewIsRecognisedByShape(t *testing.T) {
	cases := []struct {
		started string
		live    string
		want    bool
	}{
		{"./mvnw spring-boot:run", "/usr/lib/jvm/bin/java -classpath /x org.codehaus.plexus.classworlds.launcher.Launcher spring-boot:run", true},
		{"./gradlew bootRun", "java -Xmx64m -cp gradle-wrapper.jar org.gradle.wrapper.GradleWrapperMain bootRun", true},
		{"mvn quarkus:dev", "/opt/homebrew/opt/openjdk/bin/java -jar x.jar", true},
		{"npm run dev", "java -jar x.jar", false},
		{"./mvnw spring-boot:run", "/usr/bin/vim pom.xml", false},
		{"./mvnw-helper.sh", "java -jar x.jar", false},
	}
	for _, tc := range cases {
		got := jvmWrapper.MatchString(tc.started) && javaCommand.MatchString(tc.live)
		assert.Equal(t, tc.want, got, "%q / %q", tc.started, tc.live)
	}
}

func TestStartRefusesAnEmptyCommand(t *testing.T) {
	svc := newTestService(t, t.TempDir())
	_, err := svc.Start(context.Background(), uuid.New(), uuid.New(), "   ")
	require.Error(t, err)
}

func TestDetectRunCommandPrefersNpmDevScript(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"dev":"vite","start":"node server.js"}}`), 0o644))
	cmd, detail := DetectRunCommand(dir)
	assert.Equal(t, "npm run dev", cmd)
	assert.Empty(t, detail)
}

func TestDetectRunCommandFallsBackToNpmStart(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"start":"node server.js"}}`), 0o644))
	cmd, _ := DetectRunCommand(dir)
	assert.Equal(t, "npm start", cmd)
}

func TestDetectRunCommandFallsBackToGoRun(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n"), 0o644))
	cmd, detail := DetectRunCommand(dir)
	assert.Equal(t, "go run .", cmd)
	assert.Empty(t, detail)
}

func TestDetectRunCommandFindsNothingItRecognises(t *testing.T) {
	cmd, detail := DetectRunCommand(t.TempDir())
	assert.Empty(t, cmd)
	assert.Empty(t, detail)
}

func TestDetectRunCommandUsesRootMainPackageOverCmd(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "worker"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd", "worker", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))

	cmd, _ := DetectRunCommand(dir)
	assert.Equal(t, "go run .", cmd)
}

func TestDetectRunCommandFindsTheSingleCmdMainPackage(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "api"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd", "api", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))

	cmd, detail := DetectRunCommand(dir)
	assert.Equal(t, "go run ./cmd/api", cmd)
	assert.Empty(t, detail)
}

func TestDetectRunCommandNamesSeveralCmdMainCandidates(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n"), 0o644))
	for _, name := range []string{"api", "worker"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", name), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd", name, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
	}

	cmd, detail := DetectRunCommand(dir)
	assert.Empty(t, cmd)
	assert.Contains(t, detail, "api, worker")
	assert.Contains(t, detail, "commandOverride")
}

func TestDetectRunCommandUsesMakefileRunTargetWhenNoDevTarget(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Makefile"), []byte("run:\n\tgo run .\n"), 0o644))
	cmd, _ := DetectRunCommand(dir)
	assert.Equal(t, "make run", cmd)
}

func TestDetectRunCommandPrefersMakefileDevOverRunTarget(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Makefile"), []byte("dev:\n\tnpm run dev\nrun:\n\tnpm start\n"), 0o644))
	cmd, _ := DetectRunCommand(dir)
	assert.Equal(t, "make dev", cmd)
}

func TestDetectRunCommandMavenSpringBoot(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mvnw"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project><dependencies><dependency><artifactId>spring-boot-starter-web</artifactId></dependency></dependencies></project>"), 0o644))
	cmd, _ := DetectRunCommand(dir)
	assert.Equal(t, "./mvnw spring-boot:run", cmd)
}

func TestDetectRunCommandMavenQuarkus(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mvnw"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project><dependencies><dependency><groupId>io.quarkus</groupId></dependency></dependencies></project>"), 0o644))
	cmd, _ := DetectRunCommand(dir)
	assert.Equal(t, "./mvnw quarkus:dev", cmd)
}

func TestDetectRunCommandGradleSpringBoot(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gradlew"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build.gradle"), []byte("plugins { id 'org.springframework.boot' version '3.3.0' }\n"), 0o644))
	cmd, _ := DetectRunCommand(dir)
	assert.Equal(t, "./gradlew bootRun", cmd)
}

func TestDetectRunCommandGradleQuarkusKotlinDSL(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gradlew"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build.gradle.kts"), []byte("plugins {\n    id(\"io.quarkus\")\n}\n"), 0o644))
	cmd, _ := DetectRunCommand(dir)
	assert.Equal(t, "./gradlew quarkusDev", cmd)
}

func TestDetectRunCommandSkipsMavenWithoutWrapper(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project><dependencies><dependency><artifactId>spring-boot-starter-web</artifactId></dependency></dependencies></project>"), 0o644))
	cmd, _ := DetectRunCommand(dir)
	assert.Empty(t, cmd)
}

func TestAppendLineSynthesizesURLFromBarePort(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{"go default log line", "2026/10/01 12:00:00 listening on :8080", "http://127.0.0.1:8080"},
		{"generic phrasing", "Listening on port 8080", "http://127.0.0.1:8080"},
		{"spring boot tomcat banner", "Tomcat started on port 8080 (http) with context path ''", "http://127.0.0.1:8080"},
		{"bare ports line", "port(s): 8080", "http://127.0.0.1:8080"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &process{done: make(chan struct{})}
			p.appendLine(tc.line)
			assert.Equal(t, tc.want, p.url)
			assert.Equal(t, domain.LocalPreviewRunning, p.status)
		})
	}
}

func TestAppendLinePrefersExplicitURLOverPort(t *testing.T) {
	p := &process{done: make(chan struct{})}
	p.appendLine("listening on :8080")
	p.appendLine("Local: http://localhost:3000/")
	assert.Equal(t, "http://127.0.0.1:8080", p.url, "the first match wins; a later line must not override it")
}

func TestCloseIsSafeWithNothingActive(t *testing.T) {
	s := NewService(Deps{})
	s.Close()
	s.Close()
	if len(s.active) != 0 {
		t.Fatalf("active = %d after Close", len(s.active))
	}
}
