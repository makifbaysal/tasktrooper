package ci

import (
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var deployActionMarkers = []string{
	"google-github-actions/deploy-cloudrun", "google-github-actions/deploy-appengine",
	"aws-actions/amazon-ecs-deploy-task-definition", "amondnet/vercel-action",
	"firebaseextended/action-hosting-deploy", "superfly/flyctl-actions",
	"azure/webapps-deploy", "cloudflare/wrangler-action",
}

var deployRunMarkers = []string{
	"vercel deploy", "vercel --prod", "fly deploy", "flyctl deploy", "firebase deploy",
	"kubectl apply", "kubectl set image", "kubectl rollout", "helm upgrade",
	"gcloud run deploy", "gcloud app deploy", "serverless deploy", "sls deploy",
	"sam deploy", "cdk deploy", "eas submit", "fastlane", "wrangler deploy",
	"wrangler publish", "netlify deploy", "railway up", "aws ecs update-service",
	"aws lambda update-function-code",
}

var releaseActionMarkers = []string{"softprops/action-gh-release", "changesets/action"}

var securityActionMarkers = []string{
	"github/codeql-action", "snyk/actions", "aquasecurity/trivy-action",
	"zaproxy/action-baseline", "gitleaks/gitleaks-action", "actions/dependency-review-action",
}

var securityRunMarkers = []string{"trivy", "gitleaks", "npm audit", "govulncheck"}

var testMarkers = []string{
	"go test", "npm test", "npm run test", "pnpm test", "pnpm run test",
	"yarn test", "yarn run test", "vitest", "jest", "pytest", "cargo test",
	"flutter test", "gradlew test", "gradle test", "mvn test", "xcodebuild test",
	"swift test", "bundle exec rspec", "php artisan test", "phpunit",
}

var typecheckMarkers = []string{"tsc", "run typecheck", "run type-check", "mypy", "pyright", "vue-tsc"}

var lintMarkers = []string{
	"eslint", "run lint", "golangci-lint", "go vet", "ruff", "flake8",
	"cargo clippy", "flutter analyze", "swiftlint", "ktlint", "prettier --check", "biome check",
}

var buildMarkers = []string{
	"go build", "run build", "docker build", "cargo build", "gradlew assemble",
	"gradlew build", "gradle assemble", "gradle build", "xcodebuild build",
	"xcodebuild archive", "flutter build", "mvn package",
}

var exactJobNamePurpose = map[string]domain.CheckPurpose{
	"lint": domain.CheckLint, "test": domain.CheckTest, "build": domain.CheckBuild,
	"typecheck": domain.CheckTypecheck, "e2e": domain.CheckE2E,
}

func classifyPurpose(j job) domain.CheckPurpose {
	keyName := strings.ToLower(j.Key + " " + j.Name)
	runText := strings.ToLower(allStepRunText(j))

	if isDeploy(j, keyName, runText) {
		return domain.CheckDeploy
	}
	if isRelease(j, keyName, runText) {
		return domain.CheckRelease
	}
	if containsAny(runText, "playwright", "cypress", "test:e2e") || strings.Contains(keyName, "e2e") {
		return domain.CheckE2E
	}
	if isSecurity(j, runText) {
		return domain.CheckSecurity
	}
	if p, ok := exactJobNamePurpose[strings.ToLower(strings.TrimSpace(j.Key))]; ok {
		return p
	}
	if p, ok := exactJobNamePurpose[strings.ToLower(strings.TrimSpace(j.Name))]; ok {
		return p
	}
	if p, ok := commandPurpose(runText); ok {
		return p
	}
	return domain.CheckOther
}

// hasStaticEnvironment excludes a templated `environment: ${{ ... }}` value:
// it cannot be resolved without running the workflow, so it must not by
// itself make a job look like a deploy (see release.yml's platform jobs,
// which gate secrets on a conditional Release environment but are release
// builds, not deploys).
func hasStaticEnvironment(env string) bool {
	return env != "" && !strings.Contains(env, "${{")
}

func isDeploy(j job, keyName, runText string) bool {
	if hasStaticEnvironment(j.Environment) {
		return true
	}
	if containsAny(keyName, "deploy", "rollout", "ship", "promote") {
		return true
	}
	if usesAny(j, deployActionMarkers) {
		return true
	}
	return containsAny(runText, deployRunMarkers...)
}

func isRelease(j job, keyName, runText string) bool {
	if usesAny(j, releaseActionMarkers) {
		return true
	}
	if strings.Contains(runText, "electron-builder") && strings.Contains(runText, "publish") {
		return true
	}
	if containsAny(runText, "goreleaser", "npm publish", "pnpm publish") {
		return true
	}
	return containsAny(keyName, "release", "publish")
}

func isSecurity(j job, runText string) bool {
	if usesAny(j, securityActionMarkers) {
		return true
	}
	return containsAny(runText, securityRunMarkers...)
}

func commandPurpose(runText string) (domain.CheckPurpose, bool) {
	switch {
	case containsAny(runText, testMarkers...):
		return domain.CheckTest, true
	case containsAny(runText, typecheckMarkers...):
		return domain.CheckTypecheck, true
	case containsAny(runText, lintMarkers...):
		return domain.CheckLint, true
	case containsAny(runText, buildMarkers...):
		return domain.CheckBuild, true
	}
	return "", false
}

func containsAny(hay string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(hay, n) {
			return true
		}
	}
	return false
}

func usesAny(j job, markers []string) bool {
	for _, s := range j.Steps {
		u := strings.ToLower(s.Uses)
		if u == "" {
			continue
		}
		for _, m := range markers {
			if strings.HasPrefix(u, m) {
				return true
			}
		}
	}
	return false
}

func allStepRunText(j job) string {
	var b strings.Builder
	for _, s := range j.Steps {
		b.WriteString(s.Run)
		b.WriteString("\n")
	}
	return b.String()
}

// deployEnvironment normalizes a deploy check's environment: the job's own
// environment first, then key/name keywords, then a push-to-main/master
// trigger as a last resort. A wrong guess is worse than none, so anything
// that matches nothing stays "".
func deployEnvironment(wf workflow, j job) domain.DeployEnvironment {
	if env := normalizeEnvironment(j.Environment); env != "" {
		return env
	}
	keyName := strings.ToLower(j.Key + " " + j.Name)
	if env := envFromKeywords(keyName); env != "" {
		return env
	}
	if pushToMain(wf.Triggers) {
		return domain.EnvironmentProduction
	}
	return ""
}

func normalizeEnvironment(raw string) domain.DeployEnvironment {
	if !hasStaticEnvironment(raw) {
		return ""
	}
	low := strings.ToLower(strings.TrimSpace(raw))
	switch low {
	case "prod", "production", "live":
		return domain.EnvironmentProduction
	case "stage", "staging":
		return domain.EnvironmentStaging
	case "preview", "pr":
		return domain.EnvironmentPreview
	case "dev", "development":
		return domain.EnvironmentDevelopment
	}
	if strings.Contains(low, "prod") {
		return domain.EnvironmentProduction
	}
	return domain.EnvironmentStaging
}

func envFromKeywords(keyName string) domain.DeployEnvironment {
	switch {
	case containsAny(keyName, "prod", "production", "live"):
		return domain.EnvironmentProduction
	case containsAny(keyName, "staging", "stage"):
		return domain.EnvironmentStaging
	case containsAny(keyName, "preview"):
		return domain.EnvironmentPreview
	case containsAny(keyName, "development", "dev"):
		return domain.EnvironmentDevelopment
	}
	return ""
}

func pushToMain(triggers []string) bool {
	for _, t := range triggers {
		rest, ok := strings.CutPrefix(t, "push:")
		if !ok {
			continue
		}
		for _, b := range strings.Split(rest, ",") {
			if b == "main" || b == "master" {
				return true
			}
		}
	}
	return false
}
