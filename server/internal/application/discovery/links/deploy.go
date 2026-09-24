package links

import (
	"encoding/json"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// deployExtractor reads every marker that says a component ships somewhere:
// provider config files, IaC resource blocks, and GitHub Actions workflow
// steps that run a deploy command.
type deployExtractor struct{}

func (deployExtractor) extract(ctx *scanCtx) {
	detectSimpleMarkers(ctx)
	detectVercelProject(ctx)
	detectFlyToml(ctx)
	detectFirebase(ctx)
	detectAppEngine(ctx)
	detectKnativeService(ctx)
	detectCloudBuild(ctx)
	detectServerless(ctx)
	detectSAMTemplate(ctx)
	detectECSTaskDef(ctx)
	detectFastlane(ctx)
	detectTerraform(ctx)
	detectWorkflows(ctx)
}

func addDeploy(ctx *scanCtx, componentPath, provider string, env domain.DeployEnvironment, ref map[string]string, confidence domain.Confidence, evidence ...domain.SourceEvidence) {
	ctx.collector.addDeploySignal(domain.DeploySignal{
		ComponentPath: componentPath,
		Provider:      provider,
		Environment:   env,
		Ref:           ref,
		Evidence:      evidence,
		Confidence:    confidence,
	})
}

// refConfidence: a Ref with at least one resolved (non-interpolated) value is
// an explicit identifier; an empty or all-unknown Ref is a guess.
func refConfidence(ref map[string]string) domain.Confidence {
	for _, v := range ref {
		if v != "" {
			return domain.ConfidenceHigh
		}
	}
	return domain.ConfidenceMedium
}

// --- single-file, no-ref markers ---

var simpleMarkers = []struct{ basename, provider string }{
	{"netlify.toml", "netlify"},
	{"render.yaml", "render"},
	{"railway.json", "railway"},
	{"railway.toml", "railway"},
	{"Procfile", "heroku"},
	{"app.json", "heroku"},
	{"apprunner.yaml", "aws_app_runner"},
	{"amplify.yml", "aws_amplify"},
	{"eas.json", "expo"},
	{"vercel.json", "vercel"},
}

func detectSimpleMarkers(ctx *scanCtx) {
	for _, m := range simpleMarkers {
		for _, file := range ctx.tree.ByBase(m.basename) {
			owner, ok := ctx.ownerOf(file)
			if !ok {
				continue
			}
			addDeploy(ctx, owner, m.provider, "", nil, domain.ConfidenceMedium, domain.SourceEvidence{Path: file})
		}
	}
}

// --- vercel: .vercel/project.json ---

type vercelProjectJSON struct {
	ProjectID string `json:"projectId"`
	OrgID     string `json:"orgId"`
}

func detectVercelProject(ctx *scanCtx) {
	for _, file := range ctx.tree.ByBase("project.json") {
		if !strings.Contains(file, ".vercel/") {
			continue
		}
		owner, ok := ctx.ownerOf(file)
		if !ok {
			continue
		}
		raw, err := ctx.tree.Read(file)
		if err != nil {
			continue
		}
		var doc vercelProjectJSON
		_ = json.Unmarshal(raw, &doc)
		ref := map[string]string{}
		if doc.ProjectID != "" {
			ref["project_id"] = doc.ProjectID
		}
		if doc.OrgID != "" {
			ref["org_id"] = doc.OrgID
		}
		addDeploy(ctx, owner, "vercel", "", ref, refConfidence(ref), domain.SourceEvidence{Path: file})
	}
}

// --- fly.toml ---

var flyAppRe = regexp.MustCompile(`(?m)^\s*app\s*=\s*"([^"]+)"`)

func detectFlyToml(ctx *scanCtx) {
	for _, file := range ctx.tree.ByBase("fly.toml") {
		owner, ok := ctx.ownerOf(file)
		if !ok {
			continue
		}
		ref := map[string]string{}
		if m := flyAppRe.FindStringSubmatch(ctx.tree.ReadString(file)); m != nil {
			ref["app"] = m[1]
		}
		addDeploy(ctx, owner, "fly", "", ref, refConfidence(ref), domain.SourceEvidence{Path: file})
	}
}

// --- firebase.json + .firebaserc ---

type firebasercJSON struct {
	Projects map[string]string `json:"projects"`
}

func detectFirebase(ctx *scanCtx) {
	for _, file := range ctx.tree.ByBase("firebase.json") {
		owner, ok := ctx.ownerOf(file)
		if !ok {
			continue
		}
		ref := map[string]string{}
		dir := dirName(file)
		for _, rcFile := range ctx.tree.ByBase(".firebaserc") {
			if dirName(rcFile) != dir {
				continue
			}
			raw, err := ctx.tree.Read(rcFile)
			if err != nil {
				continue
			}
			var doc firebasercJSON
			if json.Unmarshal(raw, &doc) == nil {
				if p := doc.Projects["default"]; p != "" {
					ref["project"] = p
				}
			}
		}
		addDeploy(ctx, owner, "firebase", "", ref, refConfidence(ref), domain.SourceEvidence{Path: file})
	}
}

// --- app.yaml (GAE) ---

var gaeRuntimeRe = regexp.MustCompile(`(?m)^\s*runtime\s*:\s*\S+`)
var gaeServiceRe = regexp.MustCompile(`(?m)^\s*service\s*:\s*["']?([A-Za-z0-9_-]+)["']?`)

func detectAppEngine(ctx *scanCtx) {
	for _, file := range ctx.tree.ByBase("app.yaml") {
		owner, ok := ctx.ownerOf(file)
		if !ok {
			continue
		}
		content := ctx.tree.ReadString(file)
		if !gaeRuntimeRe.MatchString(content) {
			continue
		}
		ref := map[string]string{}
		if m := gaeServiceRe.FindStringSubmatch(content); m != nil {
			ref["service"] = m[1]
		}
		addDeploy(ctx, owner, "gcp_app_engine", "", ref, refConfidence(ref), domain.SourceEvidence{Path: file})
	}
}

// --- Knative service.yaml ---

type knativeServiceYAML struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
}

func detectKnativeService(ctx *scanCtx) {
	for _, file := range ctx.tree.ByBase("service.yaml") {
		owner, ok := ctx.ownerOf(file)
		if !ok {
			continue
		}
		var doc knativeServiceYAML
		if yaml.Unmarshal([]byte(ctx.tree.ReadString(file)), &doc) != nil {
			continue
		}
		if !strings.Contains(doc.APIVersion, "serving.knative.dev") {
			continue
		}
		ref := map[string]string{}
		if doc.Metadata.Name != "" {
			ref["service"] = doc.Metadata.Name
		}
		addDeploy(ctx, owner, "gcp_cloud_run", "", ref, refConfidence(ref), domain.SourceEvidence{Path: file})
	}
}

// --- cloudbuild.yaml ---

func detectCloudBuild(ctx *scanCtx) {
	for _, file := range ctx.tree.ByBase("cloudbuild.yaml") {
		owner, ok := ctx.ownerOf(file)
		if !ok {
			continue
		}
		service, region, _, ok := parseGcloudRunDeploy(normalizeForCommandScan(ctx.tree.ReadString(file)))
		if !ok {
			continue
		}
		ref := map[string]string{"service": service}
		if region != "" {
			ref["region"] = region
		}
		addDeploy(ctx, owner, "gcp_cloud_run", "", ref, domain.ConfidenceHigh, domain.SourceEvidence{Path: file})
	}
}

// --- serverless.yml ---

var serverlessServiceRe = regexp.MustCompile(`(?m)^\s*service\s*:\s*["']?([A-Za-z0-9_.-]+)["']?`)

func detectServerless(ctx *scanCtx) {
	for _, file := range ctx.tree.ByBase("serverless.yml") {
		owner, ok := ctx.ownerOf(file)
		if !ok {
			continue
		}
		ref := map[string]string{}
		if m := serverlessServiceRe.FindStringSubmatch(ctx.tree.ReadString(file)); m != nil {
			ref["service"] = m[1]
		}
		addDeploy(ctx, owner, "aws_lambda", "", ref, refConfidence(ref), domain.SourceEvidence{Path: file})
	}
}

// --- SAM template.yaml ---

func detectSAMTemplate(ctx *scanCtx) {
	for _, file := range ctx.tree.ByBase("template.yaml") {
		owner, ok := ctx.ownerOf(file)
		if !ok {
			continue
		}
		if !strings.Contains(ctx.tree.ReadString(file), "AWS::Serverless::Function") {
			continue
		}
		addDeploy(ctx, owner, "aws_lambda", "", nil, domain.ConfidenceMedium, domain.SourceEvidence{Path: file})
	}
}

// --- ECS task-definition.json / *taskdef*.json ---

type ecsTaskDefJSON struct {
	Family string `json:"family"`
}

func detectECSTaskDef(ctx *scanCtx) {
	for _, file := range ctx.tree.Files {
		base := strings.ToLower(baseName(file))
		if !strings.HasSuffix(base, ".json") {
			continue
		}
		if base != "task-definition.json" && !strings.Contains(base, "taskdef") {
			continue
		}
		owner, ok := ctx.ownerOf(file)
		if !ok {
			continue
		}
		raw, err := ctx.tree.Read(file)
		if err != nil {
			continue
		}
		var doc ecsTaskDefJSON
		_ = json.Unmarshal(raw, &doc)
		ref := map[string]string{}
		if doc.Family != "" {
			ref["family"] = doc.Family
		}
		addDeploy(ctx, owner, "aws_ecs", "", ref, refConfidence(ref), domain.SourceEvidence{Path: file})
	}
}

// --- fastlane Fastfile ---

func detectFastlane(ctx *scanCtx) {
	for _, file := range ctx.tree.ByBase("Fastfile") {
		owner, ok := ctx.ownerOf(file)
		if !ok {
			continue
		}
		content := ctx.tree.ReadString(file)
		if strings.Contains(content, "upload_to_app_store") || strings.Contains(content, "upload_to_testflight") {
			addDeploy(ctx, owner, "app_store", "", nil, domain.ConfidenceMedium, domain.SourceEvidence{Path: file})
		}
		if strings.Contains(content, "upload_to_play_store") || strings.Contains(content, "supply") {
			addDeploy(ctx, owner, "google_play", "", nil, domain.ConfidenceMedium, domain.SourceEvidence{Path: file})
		}
	}
}

// --- terraform ---

var tfResourceRe = regexp.MustCompile(`resource\s+"([a-z0-9_]+)"\s+"([A-Za-z0-9_-]+)"\s*\{`)
var tfNameRe = regexp.MustCompile(`(?m)^\s*name\s*=\s*"([^"$][^"]*)"`)

var tfResourceProviders = map[string]string{
	"google_cloud_run_service":    "gcp_cloud_run",
	"google_cloud_run_v2_service": "gcp_cloud_run",
	"aws_ecs_service":             "aws_ecs",
	"aws_lambda_function":         "aws_lambda",
	"aws_apprunner_service":       "aws_app_runner",
	"vercel_project":              "vercel",
}

func detectTerraform(ctx *scanCtx) {
	for _, file := range ctx.tree.WithSuffix(".tf") {
		owner, ok := ctx.ownerOf(file)
		if !ok {
			continue
		}
		content := ctx.tree.ReadString(file)
		for _, loc := range tfResourceRe.FindAllStringSubmatchIndex(content, -1) {
			resourceType := content[loc[2]:loc[3]]
			resourceName := content[loc[4]:loc[5]]
			provider, ok := tfResourceProviders[resourceType]
			if !ok {
				continue
			}
			blockStart := loc[1] - 1
			blockEnd := matchBrace(content, blockStart)
			ref := map[string]string{"resource": resourceName}
			conf := domain.ConfidenceMedium
			if blockEnd > blockStart {
				if m := tfNameRe.FindStringSubmatch(content[blockStart:blockEnd]); m != nil {
					ref["name"] = m[1]
					conf = domain.ConfidenceHigh
				}
			}
			line := 1 + strings.Count(content[:loc[0]], "\n")
			addDeploy(ctx, owner, provider, "", ref, conf, domain.SourceEvidence{Path: file, Line: line})
		}
	}
}

func matchBrace(content string, openIdx int) int {
	depth := 0
	for i := openIdx; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(content)
}

// --- GitHub Actions workflows ---

type workflowStepDoc struct {
	Name             string               `yaml:"name"`
	Uses             string               `yaml:"uses"`
	Run              string               `yaml:"run"`
	With             map[string]yaml.Node `yaml:"with"`
	WorkingDirectory string               `yaml:"working-directory"`
}

type workflowJobDoc struct {
	Environment yaml.Node `yaml:"environment"`
	Defaults    struct {
		Run struct {
			WorkingDirectory string `yaml:"working-directory"`
		} `yaml:"run"`
	} `yaml:"defaults"`
	Steps []workflowStepDoc `yaml:"steps"`
}

type workflowDoc struct {
	Jobs map[string]workflowJobDoc `yaml:"jobs"`
}

func detectWorkflows(ctx *scanCtx) {
	for _, file := range ctx.tree.Files {
		if !strings.HasPrefix(file, ".github/workflows/") {
			continue
		}
		ext := strings.ToLower(extOf(file))
		if ext != ".yml" && ext != ".yaml" {
			continue
		}
		content := ctx.tree.ReadString(file)
		if content == "" {
			continue
		}
		var doc workflowDoc
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			ctx.collector.warn(file + " is not parseable YAML")
			continue
		}
		for _, job := range doc.Jobs {
			jobEnv := normalizeEnvironmentName(workflowEnvironmentName(job.Environment))
			for _, step := range job.Steps {
				dir := stepWorkingDir(job, step)
				owner, ok := resolveWorkflowOwner(ctx, dir)
				if !ok {
					ctx.collector.warn(file + ": could not resolve a component for a deploy step")
					continue
				}
				detectWorkflowStep(ctx, file, owner, jobEnv, step)
			}
		}
	}
}

var cdPrefixRe = regexp.MustCompile(`^\s*cd\s+([^\s&]+)`)

func stepWorkingDir(job workflowJobDoc, step workflowStepDoc) string {
	if step.WorkingDirectory != "" {
		return step.WorkingDirectory
	}
	if job.Defaults.Run.WorkingDirectory != "" {
		return job.Defaults.Run.WorkingDirectory
	}
	if m := cdPrefixRe.FindStringSubmatch(step.Run); m != nil {
		return m[1]
	}
	return ""
}

func resolveWorkflowOwner(ctx *scanCtx, dir string) (string, bool) {
	if dir != "" {
		clean := strings.TrimSuffix(strings.TrimPrefix(dir, "./"), "/")
		if clean == "" {
			clean = "."
		}
		if owner, ok := ctx.ownerOf(clean); ok {
			return owner, true
		}
	}
	if root, ok := ctx.rootComponent(); ok {
		return root, true
	}
	if only, ok := ctx.onlyComponent(); ok {
		return only, true
	}
	return "", false
}

func workflowEnvironmentName(node yaml.Node) string {
	switch node.Kind {
	case yaml.ScalarNode:
		return node.Value
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == "name" {
				return node.Content[i+1].Value
			}
		}
	}
	return ""
}

func normalizeEnvironmentName(name string) domain.DeployEnvironment {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "prod", "production", "live":
		return domain.EnvironmentProduction
	case "stage", "staging":
		return domain.EnvironmentStaging
	case "preview":
		return domain.EnvironmentPreview
	}
	return ""
}

func withValue(with map[string]yaml.Node, key string) string {
	node, ok := with[key]
	if !ok {
		return ""
	}
	v := strings.TrimSpace(node.Value)
	if strings.HasPrefix(v, "${{") {
		return ""
	}
	return v
}

func detectWorkflowStep(ctx *scanCtx, file, owner string, jobEnv domain.DeployEnvironment, step workflowStepDoc) {
	uses := strings.ToLower(step.Uses)
	switch {
	case strings.HasPrefix(uses, "google-github-actions/deploy-cloudrun"):
		ref := map[string]string{
			"service":    withValue(step.With, "service"),
			"region":     withValue(step.With, "region"),
			"project_id": withValue(step.With, "project_id"),
		}
		addDeploy(ctx, owner, "gcp_cloud_run", jobEnv, ref, refConfidence(ref), domain.SourceEvidence{Path: file})
		return
	case strings.HasPrefix(uses, "aws-actions/amazon-ecs-deploy-task-definition"):
		ref := map[string]string{
			"service": withValue(step.With, "service"),
			"cluster": withValue(step.With, "cluster"),
		}
		addDeploy(ctx, owner, "aws_ecs", jobEnv, ref, refConfidence(ref), domain.SourceEvidence{Path: file})
		return
	case strings.HasPrefix(uses, "amondnet/vercel-action"):
		ref := map[string]string{"vercel-project-id": withValue(step.With, "vercel-project-id")}
		addDeploy(ctx, owner, "vercel", jobEnv, ref, refConfidence(ref), domain.SourceEvidence{Path: file})
		return
	case strings.HasPrefix(uses, "firebaseextended/action-hosting-deploy"):
		addDeploy(ctx, owner, "firebase", jobEnv, nil, domain.ConfidenceMedium, domain.SourceEvidence{Path: file})
		return
	case strings.HasPrefix(uses, "superfly/flyctl-actions"):
		ref := map[string]string{}
		if v := withValue(step.With, "app"); v != "" {
			ref["app"] = v
		}
		addDeploy(ctx, owner, "fly", jobEnv, ref, refConfidence(ref), domain.SourceEvidence{Path: file})
		return
	}

	if step.Run != "" {
		detectWorkflowRunLine(ctx, file, owner, jobEnv, step.Run)
	}
}

var (
	awsLambdaFuncRe   = regexp.MustCompile(`--function-name (\S+)`)
	awsEcsClusterRe   = regexp.MustCompile(`--cluster (\S+)`)
	awsEcsServiceRe   = regexp.MustCompile(`--service (\S+)`)
	kubectlDeployRe   = regexp.MustCompile(`deployment/(\S+)`)
	helmUpgradeRe     = regexp.MustCompile(`helm upgrade --install (\S+)`)
	gcloudRunDeployRe = regexp.MustCompile(`gcloud run deploy (\S+)`)
	gcloudRegionRe    = regexp.MustCompile(`--region[= ](\S+)`)
	gcloudProjectRe   = regexp.MustCompile(`--project[= ](\S+)`)
)

func parseGcloudRunDeploy(text string) (service, region, project string, ok bool) {
	m := gcloudRunDeployRe.FindStringSubmatch(text)
	if m == nil {
		return "", "", "", false
	}
	service = m[1]
	if rm := gcloudRegionRe.FindStringSubmatch(text); rm != nil {
		region = rm[1]
	}
	if pm := gcloudProjectRe.FindStringSubmatch(text); pm != nil {
		project = pm[1]
	}
	return service, region, project, true
}

func detectWorkflowRunLine(ctx *scanCtx, file, owner string, jobEnv domain.DeployEnvironment, run string) {
	normalized := normalizeForCommandScan(run)
	lower := strings.ToLower(normalized)

	if service, region, project, ok := parseGcloudRunDeploy(normalized); ok {
		ref := map[string]string{"service": service}
		if region != "" {
			ref["region"] = region
		}
		if project != "" {
			ref["project"] = project
		}
		addDeploy(ctx, owner, "gcp_cloud_run", jobEnv, ref, domain.ConfidenceHigh, domain.SourceEvidence{Path: file})
	}

	if m := awsLambdaFuncRe.FindStringSubmatch(normalized); m != nil {
		addDeploy(ctx, owner, "aws_lambda", jobEnv, map[string]string{"function": m[1]}, domain.ConfidenceHigh, domain.SourceEvidence{Path: file})
	}

	if strings.Contains(lower, "aws ecs update-service") {
		ref := map[string]string{}
		if m := awsEcsClusterRe.FindStringSubmatch(normalized); m != nil {
			ref["cluster"] = m[1]
		}
		if m := awsEcsServiceRe.FindStringSubmatch(normalized); m != nil {
			ref["service"] = m[1]
		}
		addDeploy(ctx, owner, "aws_ecs", jobEnv, ref, refConfidence(ref), domain.SourceEvidence{Path: file})
	}

	switch {
	case strings.Contains(lower, "vercel deploy --prod"), strings.Contains(lower, "vercel --prod"):
		env := jobEnv
		if env == "" {
			env = domain.EnvironmentProduction
		}
		addDeploy(ctx, owner, "vercel", env, nil, domain.ConfidenceMedium, domain.SourceEvidence{Path: file})
	case strings.Contains(lower, "vercel deploy"):
		addDeploy(ctx, owner, "vercel", jobEnv, nil, domain.ConfidenceMedium, domain.SourceEvidence{Path: file})
	}

	if strings.Contains(lower, "fly deploy") || strings.Contains(lower, "flyctl deploy") {
		addDeploy(ctx, owner, "fly", jobEnv, nil, domain.ConfidenceMedium, domain.SourceEvidence{Path: file})
	}

	if strings.Contains(lower, "firebase deploy") {
		addDeploy(ctx, owner, "firebase", jobEnv, nil, domain.ConfidenceMedium, domain.SourceEvidence{Path: file})
	}

	if strings.Contains(lower, "kubectl set image") || strings.Contains(lower, "kubectl apply") {
		ref := map[string]string{}
		if m := kubectlDeployRe.FindStringSubmatch(normalized); m != nil {
			ref["deployment"] = m[1]
		}
		addDeploy(ctx, owner, "kubernetes", jobEnv, ref, refConfidence(ref), domain.SourceEvidence{Path: file})
	}
	if m := helmUpgradeRe.FindStringSubmatch(normalized); m != nil {
		addDeploy(ctx, owner, "kubernetes", jobEnv, map[string]string{"release": m[1]}, domain.ConfidenceHigh, domain.SourceEvidence{Path: file})
	}

	if strings.Contains(lower, "wrangler deploy") {
		addDeploy(ctx, owner, "cloudflare", jobEnv, nil, domain.ConfidenceMedium, domain.SourceEvidence{Path: file})
	}
}

var cmdWhitespaceRe = regexp.MustCompile(`\s+`)

// normalizeForCommandScan flattens a shell run: block or a YAML args list
// into one space-separated line, so the same "gcloud run deploy X --region Y"
// pattern matches whichever form the workflow or cloudbuild file used.
func normalizeForCommandScan(content string) string {
	replacer := strings.NewReplacer("'", " ", `"`, " ", ",", " ")
	s := replacer.Replace(content)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		trimmed := strings.TrimLeft(l, " \t")
		if strings.HasPrefix(trimmed, "- ") {
			lines[i] = strings.Replace(l, "- ", "  ", 1)
		}
	}
	s = strings.Join(lines, " ")
	return strings.TrimSpace(cmdWhitespaceRe.ReplaceAllString(s, " "))
}
