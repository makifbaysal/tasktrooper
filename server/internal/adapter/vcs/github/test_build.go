package github

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	testBuildRunTitlePrefix = "tt-test-build "
	testBuildArtifactPrefix = "mobile-release-"

	defaultRunFindEvery  = 5 * time.Second
	defaultRunFindFor    = 2 * time.Minute
	defaultRunPollEvery  = 20 * time.Second
	defaultRunFinishFor  = 120 * time.Minute
	maxRunPollFailures   = 5
	failureLogTailLength = 80

	maxArtifactZipBytes      int64 = 2 << 30
	maxArtifactUnpackedBytes int64 = 4 << 30
	maxArtifactRedirects           = 10
)

// artifactHTTPClient is separate from httpClient because an artifact archive
// can take far longer than 15 seconds to stream.
var artifactHTTPClient = &http.Client{
	Timeout:       time.Hour,
	CheckRedirect: keepAuthOnOrigin,
}

// keepAuthOnOrigin exists because net/http's own redirect rule compares host
// names without the port and lets subdomains keep the header. The archive URL
// answers 302 to a signed storage URL that authenticates by its query string;
// the bearer token must never travel beside it to any other origin.
func keepAuthOnOrigin(req *http.Request, via []*http.Request) error {
	if len(via) >= maxArtifactRedirects {
		return fmt.Errorf("github: artifact download stopped after %d redirects", maxArtifactRedirects)
	}
	if !sameOrigin(req.URL, via[0].URL) {
		req.Header.Del("Authorization")
	}
	return nil
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func DispatchWorkflowInputs(ctx context.Context, token, owner, repo, workflowFile, ref string, inputs map[string]string) error {
	return dispatchWorkflowInputsAt(ctx, "", token, owner, repo, workflowFile, ref, inputs)
}

func dispatchWorkflowInputsAt(ctx context.Context, base, token, owner, repo, workflowFile, ref string, inputs map[string]string) error {
	p := repoPath(owner, repo) + "/actions/workflows/" + url.PathEscape(workflowFile) + "/dispatches"
	body := map[string]any{"ref": ref}
	if len(inputs) > 0 {
		body["inputs"] = inputs
	}
	return doJSONAt(ctx, base, token, http.MethodPost, p, body, nil)
}

type titledWorkflowRun struct {
	WorkflowRun
	DisplayTitle string `json:"display_title"`
}

// FindRunByTitle is how a dispatch is tied to its run: the dispatch endpoint
// answers 204 with no run id, so the workflow's run-name carries a unique title.
// When several runs carry the title, the newest wins.
func FindRunByTitle(ctx context.Context, token, owner, repo, workflowFile, title string, since time.Time) (WorkflowRun, bool, error) {
	return findRunByTitleAt(ctx, "", token, owner, repo, workflowFile, title, since)
}

func findRunByTitleAt(ctx context.Context, base, token, owner, repo, workflowFile, title string, since time.Time) (WorkflowRun, bool, error) {
	q := url.Values{}
	q.Set("event", "workflow_dispatch")
	q.Set("per_page", "50")
	if !since.IsZero() {
		q.Set("created", ">="+since.UTC().Format(time.RFC3339))
	}
	p := repoPath(owner, repo) + "/actions/workflows/" + url.PathEscape(workflowFile) + "/runs?" + q.Encode()
	var out struct {
		WorkflowRuns []titledWorkflowRun `json:"workflow_runs"`
	}
	if err := doJSONAt(ctx, base, token, http.MethodGet, p, nil, &out); err != nil {
		return WorkflowRun{}, false, err
	}
	var best WorkflowRun
	found := false
	for _, r := range out.WorkflowRuns {
		if r.DisplayTitle != title {
			continue
		}
		if !found || r.CreatedAt.After(best.CreatedAt) || (r.CreatedAt.Equal(best.CreatedAt) && r.ID > best.ID) {
			best = r.WorkflowRun
			found = true
		}
	}
	return best, found, nil
}

func GetRun(ctx context.Context, token, owner, repo string, runID int64) (WorkflowRun, error) {
	return getRunAt(ctx, "", token, owner, repo, runID)
}

func getRunAt(ctx context.Context, base, token, owner, repo string, runID int64) (WorkflowRun, error) {
	var out WorkflowRun
	p := repoPath(owner, repo) + "/actions/runs/" + strconv.FormatInt(runID, 10)
	if err := doJSONAt(ctx, base, token, http.MethodGet, p, nil, &out); err != nil {
		return WorkflowRun{}, err
	}
	return out, nil
}

type runArtifact struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	SizeInBytes        int64     `json:"size_in_bytes"`
	ArchiveDownloadURL string    `json:"archive_download_url"`
	Expired            bool      `json:"expired"`
	CreatedAt          time.Time `json:"created_at"`
}

// DownloadRunArtifact unpacks the run's artifact called name into destDir and
// returns the paths of the regular files it wrote. Symlinks in the archive are
// skipped; an absolute or escaping entry fails the whole download before
// anything is written.
func DownloadRunArtifact(ctx context.Context, token, owner, repo string, runID int64, name, destDir string) ([]string, error) {
	return downloadRunArtifactAt(ctx, "", token, owner, repo, runID, name, destDir)
}

func downloadRunArtifactAt(ctx context.Context, base, token, owner, repo string, runID int64, name, destDir string) ([]string, error) {
	if strings.TrimSpace(destDir) == "" {
		return nil, fmt.Errorf("github: unpacking artifact %q needs a destination directory", name)
	}
	var list struct {
		Artifacts []runArtifact `json:"artifacts"`
	}
	p := repoPath(owner, repo) + "/actions/runs/" + strconv.FormatInt(runID, 10) + "/artifacts?per_page=100"
	if err := doJSONAt(ctx, base, token, http.MethodGet, p, nil, &list); err != nil {
		return nil, fmt.Errorf("github: listing the artifacts of run %d: %w", runID, err)
	}
	var art runArtifact
	found := false
	for _, a := range list.Artifacts {
		if a.Name != name {
			continue
		}
		if !found || a.ID > art.ID {
			art = a
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("github: run %d uploaded no artifact named %q", runID, name)
	}
	if art.Expired {
		return nil, fmt.Errorf("github: artifact %q of run %d has expired", name, runID)
	}
	if art.SizeInBytes > maxArtifactZipBytes {
		return nil, fmt.Errorf("github: artifact %q is %d bytes, over the %d byte limit", name, art.SizeInBytes, maxArtifactZipBytes)
	}

	zipPath, err := fetchArtifactZip(ctx, artifactDownloadURL(base, owner, repo, art), token)
	if err != nil {
		return nil, fmt.Errorf("github: downloading artifact %q of run %d: %w", name, runID, err)
	}
	defer os.Remove(zipPath)

	files, err := extractZip(zipPath, destDir)
	if err != nil {
		return nil, fmt.Errorf("github: unpacking artifact %q of run %d: %w", name, runID, err)
	}
	return files, nil
}

// artifactDownloadURL only trusts archive_download_url when it points at the
// API origin the token belongs to; anything else is rebuilt from the id.
func artifactDownloadURL(base, owner, repo string, art runArtifact) string {
	api := resolveBase(base)
	if apiURL, err := url.Parse(api); err == nil {
		if u, err := url.Parse(art.ArchiveDownloadURL); err == nil && art.ArchiveDownloadURL != "" && sameOrigin(u, apiURL) {
			return art.ArchiveDownloadURL
		}
	}
	return api + repoPath(owner, repo) + "/actions/artifacts/" + strconv.FormatInt(art.ID, 10) + "/zip"
}

func fetchArtifactZip(ctx context.Context, endpoint, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := artifactHTTPClient.Do(req)
	if err != nil {
		return "", withoutRequestURL(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", &apiError{Status: resp.StatusCode, Message: "artifact download: " + strings.TrimSpace(string(data))}
	}
	if resp.ContentLength > maxArtifactZipBytes {
		return "", fmt.Errorf("the archive is %d bytes, over the %d byte limit", resp.ContentLength, maxArtifactZipBytes)
	}
	f, err := os.CreateTemp("", "tt-artifact-*.zip")
	if err != nil {
		return "", err
	}
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, maxArtifactZipBytes+1))
	closeErr := f.Close()
	switch {
	case copyErr != nil:
		_ = os.Remove(f.Name())
		return "", withoutRequestURL(copyErr)
	case closeErr != nil:
		_ = os.Remove(f.Name())
		return "", closeErr
	case n > maxArtifactZipBytes:
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("the archive is over the %d byte limit", maxArtifactZipBytes)
	}
	return f.Name(), nil
}

// withoutRequestURL drops the URL net/http puts in its errors: after the
// redirect it is the signed storage URL, which is a credential while it lives
// and must not reach a log or a person.
func withoutRequestURL(err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return fmt.Errorf("%s artifact archive: %w", uerr.Op, uerr.Err)
	}
	return err
}

type zipEntryPlan struct {
	file   *zip.File
	target string
}

func extractZip(zipPath, destDir string) ([]string, error) {
	root, err := filepath.Abs(destDir)
	if err != nil {
		return nil, err
	}
	zr, err := zip.OpenReader(zipPath)
	insecure := errors.Is(err, zip.ErrInsecurePath)
	if err != nil && !insecure {
		return nil, err
	}
	defer zr.Close()

	plan := make([]zipEntryPlan, 0, len(zr.File))
	for _, f := range zr.File {
		target, skip, err := zipEntryTarget(root, f)
		if err != nil {
			return nil, err
		}
		if !skip {
			plan = append(plan, zipEntryPlan{file: f, target: target})
		}
	}
	if insecure {
		return nil, fmt.Errorf("the archive holds an unsafe entry name: %w", zip.ErrInsecurePath)
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	var written int64
	out := make([]string, 0, len(plan))
	for _, e := range plan {
		if e.file.FileInfo().IsDir() {
			if err := os.MkdirAll(e.target, 0o755); err != nil {
				return nil, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(e.target), 0o755); err != nil {
			return nil, err
		}
		n, err := writeZipEntry(e.file, e.target, maxArtifactUnpackedBytes-written)
		written += n
		if err != nil {
			return nil, err
		}
		out = append(out, e.target)
	}
	return out, nil
}

// zipEntryTarget is the zip-slip guard: skip is true for symlinks, devices and
// the archive root; an absolute name or one that climbs out of root is an
// error rather than a skip, since no honest artifact holds one.
func zipEntryTarget(root string, f *zip.File) (target string, skip bool, err error) {
	mode := f.Mode()
	if mode&os.ModeSymlink != 0 || (!mode.IsRegular() && !mode.IsDir()) {
		return "", true, nil
	}
	name := strings.ReplaceAll(f.Name, `\`, "/")
	if strings.HasPrefix(name, "/") || filepath.IsAbs(name) || filepath.VolumeName(name) != "" ||
		(len(name) >= 2 && name[1] == ':') {
		return "", false, fmt.Errorf("archive entry %q is an absolute path", f.Name)
	}
	clean := path.Clean(name)
	if clean == "." {
		return "", true, nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false, fmt.Errorf("archive entry %q escapes the destination directory", f.Name)
	}
	target = filepath.Join(root, filepath.FromSlash(clean))
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false, fmt.Errorf("archive entry %q escapes the destination directory", f.Name)
	}
	return target, false, nil
}

func writeZipEntry(f *zip.File, target string, budget int64) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, fmt.Errorf("opening archive entry %q: %w", f.Name, err)
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	n, copyErr := io.Copy(out, io.LimitReader(rc, budget+1))
	closeErr := out.Close()
	if copyErr != nil {
		return n, fmt.Errorf("unpacking archive entry %q: %w", f.Name, copyErr)
	}
	if closeErr != nil {
		return n, closeErr
	}
	if n > budget {
		return n, fmt.Errorf("the archive unpacks to more than %d bytes", maxArtifactUnpackedBytes)
	}
	return n, nil
}

// RepoAccessResolver answers which GitHub repository a domain.Repository is,
// and the token to reach it with.
type RepoAccessResolver func(ctx context.Context, repo domain.Repository) (token, owner, name, defaultBranch string, err error)

// MobileTestBuilder is the Actions engine for per-task test builds: it commits
// the generated pipeline, dispatches its stage channel and waits for the run.
type MobileTestBuilder struct {
	resolve   RepoAccessResolver
	baseURL   string
	findEvery time.Duration
	findFor   time.Duration
	pollEvery time.Duration
	finishFor time.Duration
}

var _ port.MobileBuilder = (*MobileTestBuilder)(nil)

func NewMobileTestBuilder(resolve func(ctx context.Context, repo domain.Repository) (token, owner, name, defaultBranch string, err error)) *MobileTestBuilder {
	return &MobileTestBuilder{
		resolve:   resolve,
		findEvery: defaultRunFindEvery,
		findFor:   defaultRunFindFor,
		pollEvery: defaultRunPollEvery,
		finishFor: defaultRunFinishFor,
	}
}

func (b *MobileTestBuilder) SetBaseURL(u string) { b.baseURL = u }

// SetTimings overrides how often and how long the run is looked for and then
// polled; a zero duration keeps the default.
func (b *MobileTestBuilder) SetTimings(findEvery, findFor, pollEvery, finishFor time.Duration) {
	if findEvery > 0 {
		b.findEvery = findEvery
	}
	if findFor > 0 {
		b.findFor = findFor
	}
	if pollEvery > 0 {
		b.pollEvery = pollEvery
	}
	if finishFor > 0 {
		b.finishFor = finishFor
	}
}

func testBuildRunTitle(buildID string) string { return testBuildRunTitlePrefix + buildID }

func testBuildArtifactName(identifier, buildID string) string {
	return testBuildArtifactPrefix + identifier + "-" + buildID
}

func (b *MobileTestBuilder) Run(ctx context.Context, req port.MobileBuildRequest) (port.MobileBuildResult, error) {
	logf := func(format string, args ...any) {
		if req.Log != nil {
			req.Log(fmt.Sprintf(format, args...))
		}
	}
	platform, workflowFile, inputs, err := testBuildDispatch(req)
	if err != nil {
		return port.MobileBuildResult{}, err
	}
	if b.resolve == nil {
		return port.MobileBuildResult{}, errors.New("github actions test build: no repository resolver configured")
	}
	token, owner, name, branch, err := b.resolve(ctx, req.Repository)
	if err != nil {
		return port.MobileBuildResult{}, fmt.Errorf("github actions test build: resolving the repository: %w", err)
	}
	if strings.TrimSpace(branch) == "" {
		var repo Repo
		if err := doJSONAt(ctx, b.baseURL, token, http.MethodGet, repoPath(owner, name), nil, &repo); err != nil {
			return port.MobileBuildResult{}, fmt.Errorf("github actions test build: reading the default branch of %s/%s: %w", owner, name, err)
		}
		branch = repo.DefaultBranch
	}

	// CommitFiles makes no commit when the tree it builds equals the tip's, so
	// an unchanged pipeline never produces an empty commit.
	files := make([]FileChange, 0, len(req.Files))
	for _, f := range req.Files {
		files = append(files, FileChange{Path: f.Path, Body: f.Body, Mode: f.Mode})
	}
	sha, committed, err := commitFilesAt(ctx, b.baseURL, token, owner, name, branch,
		fmt.Sprintf("chore(release): generate the %s release pipeline for %s", platform, req.Identifier), files)
	if err != nil {
		return port.MobileBuildResult{}, fmt.Errorf("github actions test build: writing the release pipeline to %s: %w", branch, err)
	}
	if committed {
		logf("committed the %s release pipeline to %s (%s)", platform, branch, shortSHA(sha))
	}

	// A minute of slack absorbs clock skew between this machine and GitHub,
	// whose created filter is evaluated on GitHub's clock.
	started := time.Now().Add(-1 * time.Minute)
	if err := dispatchWorkflowInputsAt(ctx, b.baseURL, token, owner, name, workflowFile, branch, inputs); err != nil {
		return port.MobileBuildResult{}, fmt.Errorf("github actions test build: dispatching %s on %s: %w", workflowFile, branch, err)
	}
	logf("dispatched %s on %s to build %s", workflowFile, branch, req.Ref)

	title := testBuildRunTitle(req.BuildID.String())
	run, err := b.awaitRun(ctx, token, owner, name, workflowFile, title, started)
	if err != nil {
		return port.MobileBuildResult{}, err
	}
	if req.OnRunURL != nil && run.HTMLURL != "" {
		req.OnRunURL(run.HTMLURL)
	}
	logf("GitHub Actions run: %s", run.HTMLURL)

	run, err = b.awaitCompletion(ctx, token, owner, name, run, logf)
	result := port.MobileBuildResult{RunURL: run.HTMLURL}
	if err != nil {
		return result, err
	}
	if run.Conclusion != "success" {
		return result, b.runFailure(ctx, token, owner, name, run)
	}
	if platform != domain.MobileStorePlatformAndroid {
		return result, nil
	}

	artifact := testBuildArtifactName(req.Identifier, req.BuildID.String())
	paths, err := downloadRunArtifactAt(ctx, b.baseURL, token, owner, name, run.ID, artifact, req.ArtifactDir)
	if err != nil {
		return result, fmt.Errorf("github actions test build: %w", err)
	}
	aab := largestFileWithExt(paths, ".aab")
	if aab == "" {
		return result, fmt.Errorf("github actions test build: artifact %q holds no .aab (%s)", artifact, run.HTMLURL)
	}
	logf("downloaded %s", filepath.Base(aab))
	result.Artifact = aab
	return result, nil
}

// testBuildDispatch sends exactly the inputs the generated workflow declares:
// GitHub rejects a dispatch that carries an undeclared input.
func testBuildDispatch(req port.MobileBuildRequest) (platform, workflowFile string, inputs map[string]string, err error) {
	platform = strings.ToLower(strings.TrimSpace(req.Platform))
	if platform != domain.MobileStorePlatformIOS && platform != domain.MobileStorePlatformAndroid {
		return "", "", nil, fmt.Errorf("github actions test build: unknown platform %q", req.Platform)
	}
	workflowFile = path.Base(strings.TrimSpace(req.Workflow))
	if workflowFile == "" || workflowFile == "." || workflowFile == "/" {
		return "", "", nil, errors.New("github actions test build: no workflow to dispatch")
	}
	if req.BuildID == uuid.Nil {
		return "", "", nil, errors.New("github actions test build: no build id")
	}
	if strings.TrimSpace(req.Identifier) == "" {
		return "", "", nil, errors.New("github actions test build: no app identifier")
	}
	buildNumber := strings.TrimSpace(req.Env["BUILD_NUMBER"])
	if buildNumber == "" {
		return "", "", nil, errors.New("github actions test build: no BUILD_NUMBER")
	}
	if strings.TrimSpace(req.Ref) == "" {
		return "", "", nil, errors.New("github actions test build: no commit to build")
	}
	if platform == domain.MobileStorePlatformAndroid && strings.TrimSpace(req.ArtifactDir) == "" {
		return "", "", nil, errors.New("github actions test build: no directory to unpack the android bundle into")
	}
	inputs = map[string]string{
		"channel":      "stage",
		"build_number": buildNumber,
		"ref":          req.Ref,
		"build_id":     req.BuildID.String(),
	}
	// Android uploads from this server through internal app sharing, so the
	// run must not push the bundle to the internal track itself.
	if platform == domain.MobileStorePlatformAndroid {
		inputs["upload"] = "false"
	}
	return platform, workflowFile, inputs, nil
}

func (b *MobileTestBuilder) awaitRun(ctx context.Context, token, owner, name, workflowFile, title string, since time.Time) (WorkflowRun, error) {
	deadline := time.Now().Add(b.findFor)
	var lastErr error
	for {
		run, found, err := findRunByTitleAt(ctx, b.baseURL, token, owner, name, workflowFile, title, since)
		if err == nil && found {
			return run, nil
		}
		if err != nil {
			lastErr = err
		}
		if !time.Now().Before(deadline) {
			if lastErr != nil {
				return WorkflowRun{}, fmt.Errorf("github actions test build: looking for the run titled %q: %w", title, lastErr)
			}
			return WorkflowRun{}, fmt.Errorf(
				"github actions test build: GitHub accepted the dispatch of %s but no run titled %q appeared within %s — the workflow's run-name must be \"tt-test-build ${{ inputs.build_id }}\"",
				workflowFile, title, b.findFor)
		}
		if err := sleepContext(ctx, b.findEvery); err != nil {
			return WorkflowRun{}, err
		}
	}
}

func (b *MobileTestBuilder) awaitCompletion(parent context.Context, token, owner, name string, run WorkflowRun, logf func(string, ...any)) (WorkflowRun, error) {
	ctx, cancel := context.WithTimeout(parent, b.finishFor)
	defer cancel()
	lastStatus := ""
	failures := 0
	for {
		if run.Status != lastStatus {
			if run.Status == "completed" {
				logf("run %d: completed (%s)", run.ID, run.Conclusion)
			} else {
				logf("run %d: %s", run.ID, run.Status)
			}
			lastStatus = run.Status
		}
		if run.Status == "completed" {
			return run, nil
		}
		if err := sleepContext(ctx, b.pollEvery); err != nil {
			if parent.Err() != nil {
				return run, parent.Err()
			}
			return run, fmt.Errorf("github actions test build: the run did not finish within %s: %s", b.finishFor, run.HTMLURL)
		}
		next, err := getRunAt(ctx, b.baseURL, token, owner, name, run.ID)
		if err != nil {
			if ctx.Err() != nil {
				continue
			}
			failures++
			if failures >= maxRunPollFailures {
				return run, fmt.Errorf("github actions test build: polling %s: %w", run.HTMLURL, err)
			}
			continue
		}
		failures = 0
		if next.HTMLURL == "" {
			next.HTMLURL = run.HTMLURL
		}
		if next.ID == 0 {
			next.ID = run.ID
		}
		run = next
	}
}

func (b *MobileTestBuilder) runFailure(ctx context.Context, token, owner, name string, run WorkflowRun) error {
	conclusion := run.Conclusion
	if conclusion == "" {
		conclusion = "without a conclusion"
	}
	head := fmt.Sprintf("github actions test build: the run concluded %s: %s", conclusion, run.HTMLURL)
	jobs, err := listRunJobsAt(ctx, b.baseURL, token, owner, name, run.ID)
	if err != nil {
		return fmt.Errorf("%s (its jobs could not be listed: %v)", head, err)
	}
	job, ok := failedJob(jobs)
	if !ok {
		return errors.New(head)
	}
	logs, err := getJobLogsAt(ctx, b.baseURL, token, owner, name, job.ID)
	if err != nil {
		return fmt.Errorf("%s (the log of job %q could not be read: %v)", head, job.Name, err)
	}
	return fmt.Errorf("%s\nlast lines of job %q:\n%s", head, job.Name, logTail(logs, failureLogTailLength))
}

func failedJob(jobs []RunJob) (RunJob, bool) {
	for _, j := range jobs {
		if j.Conclusion == "failure" {
			return j, true
		}
	}
	for _, j := range jobs {
		switch j.Conclusion {
		case "", "success", "skipped", "neutral":
		default:
			return j, true
		}
	}
	return RunJob{}, false
}

func logTail(logs string, n int) string {
	lines := strings.Split(strings.TrimRight(logs, "\r\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	for i, line := range lines {
		lines[i] = stripLogTimestamp(strings.TrimRight(line, "\r"))
	}
	return strings.Join(lines, "\n")
}

func stripLogTimestamp(line string) string {
	idx := strings.IndexByte(line, ' ')
	if idx <= 0 {
		return line
	}
	if _, err := time.Parse(time.RFC3339Nano, line[:idx]); err != nil {
		return line
	}
	return line[idx+1:]
}

func largestFileWithExt(paths []string, ext string) string {
	best := ""
	var bestSize int64 = -1
	for _, p := range paths {
		if !strings.EqualFold(filepath.Ext(p), ext) {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.Size() > bestSize {
			best, bestSize = p, info.Size()
		}
	}
	return best
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
