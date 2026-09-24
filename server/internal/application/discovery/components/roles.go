package components

import (
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// classifyRole applies the role rules in priority order — the first rule
// that fires wins, matching how a human would eyeball a directory: a Flutter
// app with a stray Express dev-proxy script is still a mobile app.
func classifyRole(tree *inventory.Tree, dir string, info *ManifestInfo) (domain.ComponentRole, domain.Confidence, []domain.SourceEvidence) {
	if info == nil {
		return classifyManifestlessRole(tree, dir)
	}

	var goSig *goSignals
	if info.Ecosystem == "go" {
		s := scanGoFiles(tree, dir)
		goSig = &s
	}

	if ev, ok := mobileEvidence(info); ok {
		return domain.ComponentRoleMobile, domain.ConfidenceHigh, []domain.SourceEvidence{ev}
	}
	if ev, ok := desktopEvidence(tree, dir, info); ok {
		return domain.ComponentRoleDesktop, domain.ConfidenceHigh, []domain.SourceEvidence{ev}
	}
	if role, conf, ev, ok := frontendEvidence(info); ok {
		return role, conf, []domain.SourceEvidence{ev}
	}
	if role, conf, ev, ok := backendEvidence(info, goSig); ok {
		return role, conf, []domain.SourceEvidence{ev}
	}
	if ev, ok := workerEvidence(dir, info, goSig); ok {
		return domain.ComponentRoleWorker, domain.ConfidenceMedium, []domain.SourceEvidence{ev}
	}
	if ev, ok := cliEvidence(info); ok {
		return domain.ComponentRoleCLI, domain.ConfidenceMedium, []domain.SourceEvidence{ev}
	}
	if role, conf, ev, ok := libraryEvidence(dir, info, goSig); ok {
		return role, conf, []domain.SourceEvidence{ev}
	}
	return domain.ComponentRoleOther, domain.ConfidenceLow, nil
}

func mobileEvidence(info *ManifestInfo) (domain.SourceEvidence, bool) {
	switch info.Ecosystem {
	case "dart":
		if _, ok := info.hasDep("flutter"); ok {
			return domain.SourceEvidence{Path: info.Path, Note: "flutter dependency"}, true
		}
	case "node":
		if name, _, ok := info.anyDep("react-native", "expo"); ok {
			return domain.SourceEvidence{Path: info.Path, Note: name + " dependency"}, true
		}
	case "swift":
		if info.Bin {
			return domain.SourceEvidence{Path: info.Path, Note: "Xcode app target"}, true
		}
	case "gradle":
		if _, ok := info.hasDep("com.android.application"); ok {
			return domain.SourceEvidence{Path: info.Path, Note: "com.android.application plugin"}, true
		}
	}
	return domain.SourceEvidence{}, false
}

func desktopEvidence(tree *inventory.Tree, dir string, info *ManifestInfo) (domain.SourceEvidence, bool) {
	if info.Ecosystem != "node" {
		return domain.SourceEvidence{}, false
	}
	if name, _, ok := info.anyDep("electron"); ok {
		return domain.SourceEvidence{Path: info.Path, Note: name + " dependency"}, true
	}
	if name, ok := info.hasDepPrefix("@tauri-apps/"); ok {
		return domain.SourceEvidence{Path: info.Path, Note: name + " dependency"}, true
	}
	if tree.HasDir(inventory.Join(dir, "src-tauri")) {
		return domain.SourceEvidence{Path: inventory.Join(dir, "src-tauri")}, true
	}
	return domain.SourceEvidence{}, false
}

var frontendHighDeps = []string{
	"next", "nuxt", "@remix-run/react", "@remix-run/node", "gatsby", "@sveltejs/kit",
	"@angular/core", "astro", "solid-js", "vue", "svelte", "preact",
}

func frontendEvidence(info *ManifestInfo) (domain.ComponentRole, domain.Confidence, domain.SourceEvidence, bool) {
	if info.Ecosystem != "node" {
		return "", "", domain.SourceEvidence{}, false
	}
	if name, _, ok := info.anyDep(frontendHighDeps...); ok {
		return domain.ComponentRoleFrontend, domain.ConfidenceHigh, domain.SourceEvidence{Path: info.Path, Note: name + " dependency"}, true
	}
	if _, ok := info.hasDep("react-dom"); ok {
		if name, _, ok := info.anyDep("vite", "webpack", "parcel"); ok {
			return domain.ComponentRoleFrontend, domain.ConfidenceHigh, domain.SourceEvidence{Path: info.Path, Note: "react-dom + " + name}, true
		}
		return domain.ComponentRoleFrontend, domain.ConfidenceMedium, domain.SourceEvidence{Path: info.Path, Note: "react-dom dependency"}, true
	}
	return "", "", domain.SourceEvidence{}, false
}

var goBackendModules = []string{
	"go-chi/chi", "gin-gonic/gin", "labstack/echo", "gofiber/fiber", "gorilla/mux",
	"connectrpc.com/connect", "google.golang.org/grpc", "danielgtaylor/huma", "go-chi/huma",
}

var nodeBackendDeps = []string{"express", "fastify", "@nestjs/core", "koa", "@hapi/hapi", "hono", "@trpc/server", "apollo-server", "apollo-server-express"}

var jvmBackendKeywords = []string{"org.springframework.boot", "spring-boot", "io.ktor", "ktor", "quarkus", "micronaut"}

func backendEvidence(info *ManifestInfo, goSig *goSignals) (domain.ComponentRole, domain.Confidence, domain.SourceEvidence, bool) {
	switch info.Ecosystem {
	case "go":
		if mod, ok := anyDepContains(info, goBackendModules...); ok {
			return domain.ComponentRoleBackend, domain.ConfidenceHigh, domain.SourceEvidence{Path: info.Path, Note: mod + " dependency"}, true
		}
		if goSig != nil && goSig.ListensHTTP {
			return domain.ComponentRoleBackend, domain.ConfidenceMedium, goSig.ListenerEvidence, true
		}
	case "node":
		if name, _, ok := info.anyDep(nodeBackendDeps...); ok {
			return domain.ComponentRoleBackend, domain.ConfidenceHigh, domain.SourceEvidence{Path: info.Path, Note: name + " dependency"}, true
		}
	case "python":
		if name, _, ok := info.anyDep("fastapi", "django", "flask", "starlette", "aiohttp", "sanic"); ok {
			return domain.ComponentRoleBackend, domain.ConfidenceHigh, domain.SourceEvidence{Path: info.Path, Note: name + " dependency"}, true
		}
	case "gradle", "maven":
		if kw, ok := anyDepContains(info, jvmBackendKeywords...); ok {
			return domain.ComponentRoleBackend, domain.ConfidenceHigh, domain.SourceEvidence{Path: info.Path, Note: kw}, true
		}
	case "ruby":
		if name, _, ok := info.anyDep("rails", "sinatra"); ok {
			return domain.ComponentRoleBackend, domain.ConfidenceHigh, domain.SourceEvidence{Path: info.Path, Note: name + " dependency"}, true
		}
	case "php":
		if mod, ok := anyDepContains(info, "laravel/framework", "symfony/"); ok {
			return domain.ComponentRoleBackend, domain.ConfidenceHigh, domain.SourceEvidence{Path: info.Path, Note: mod + " dependency"}, true
		}
	case "rust":
		if name, _, ok := info.anyDep("axum", "actix-web", "rocket", "warp"); ok {
			return domain.ComponentRoleBackend, domain.ConfidenceHigh, domain.SourceEvidence{Path: info.Path, Note: name + " dependency"}, true
		}
	case "dotnet":
		if mod, ok := anyDepContains(info, "Microsoft.AspNetCore"); ok {
			return domain.ComponentRoleBackend, domain.ConfidenceHigh, domain.SourceEvidence{Path: info.Path, Note: mod + " dependency"}, true
		}
	}
	return "", "", domain.SourceEvidence{}, false
}

var nodeWorkerDeps = []string{"bullmq", "bull", "bee-queue", "@temporalio/worker"}
var pyWorkerDeps = []string{"celery", "rq", "dramatiq"}
var workerDirHints = []string{"worker", "jobs", "consumer", "queue"}

func workerEvidence(dir string, info *ManifestInfo, goSig *goSignals) (domain.SourceEvidence, bool) {
	switch info.Ecosystem {
	case "node":
		if name, _, ok := info.anyDep(nodeWorkerDeps...); ok {
			return domain.SourceEvidence{Path: info.Path, Note: name + " dependency"}, true
		}
	case "python":
		if name, _, ok := info.anyDep(pyWorkerDeps...); ok {
			return domain.SourceEvidence{Path: info.Path, Note: name + " dependency"}, true
		}
	case "ruby":
		if name, _, ok := info.anyDep("sidekiq"); ok {
			return domain.SourceEvidence{Path: info.Path, Note: name + " dependency"}, true
		}
	case "go":
		if goSig != nil && goSig.HasMain && !goSig.ListensHTTP && goSig.UsesQueue {
			return goSig.QueueEvidence, true
		}
	}
	dirName := baseName(dir)
	for _, hint := range workerDirHints {
		if strings.Contains(strings.ToLower(dirName), hint) {
			return domain.SourceEvidence{Path: dir, Note: "directory name suggests a " + hint}, true
		}
	}
	return domain.SourceEvidence{}, false
}

func cliEvidence(info *ManifestInfo) (domain.SourceEvidence, bool) {
	switch info.Ecosystem {
	case "node":
		if info.Bin {
			return domain.SourceEvidence{Path: info.Path, Note: "bin entry"}, true
		}
	case "go":
		if mod, ok := anyDepContains(info, "spf13/cobra", "urfave/cli"); ok {
			if _, hasServer := anyDepContains(info, goBackendModules...); !hasServer {
				return domain.SourceEvidence{Path: info.Path, Note: mod + " dependency"}, true
			}
		}
	case "rust":
		if name, _, ok := info.anyDep("clap"); ok {
			return domain.SourceEvidence{Path: info.Path, Note: name + " dependency"}, true
		}
	}
	return domain.SourceEvidence{}, false
}

func libraryEvidence(dir string, info *ManifestInfo, goSig *goSignals) (domain.ComponentRole, domain.Confidence, domain.SourceEvidence, bool) {
	switch info.Ecosystem {
	case "node":
		if !info.NoOwnSource {
			return "", "", domain.SourceEvidence{}, false
		}
		conf := domain.ConfidenceMedium
		if underAppsServicesPackages(dir) || strings.HasPrefix(dir, "libs/") || dir == "libs" {
			conf = domain.ConfidenceHigh
		}
		return domain.ComponentRoleLibrary, conf, domain.SourceEvidence{Path: info.Path, Note: "no start/dev/serve script; publishes an entry point"}, true
	case "go":
		if goSig == nil || !goSig.HasMain {
			return domain.ComponentRoleLibrary, domain.ConfidenceHigh, domain.SourceEvidence{Path: info.Path, Note: "module has no package main"}, true
		}
	case "rust":
		if info.NoOwnSource {
			return domain.ComponentRoleLibrary, domain.ConfidenceHigh, domain.SourceEvidence{Path: info.Path, Note: "src/lib.rs with no src/main.rs"}, true
		}
	}
	return "", "", domain.SourceEvidence{}, false
}

func classifyManifestlessRole(tree *inventory.Tree, dir string) (domain.ComponentRole, domain.Confidence, []domain.SourceEvidence) {
	files := tree.Under(dir)
	iacCount, other := 0, 0
	var iacEvidence string
	for _, f := range files {
		switch {
		case strings.HasSuffix(f, ".tf"):
			iacCount++
			if iacEvidence == "" {
				iacEvidence = f
			}
		case strings.HasSuffix(f, "Chart.yaml"):
			iacCount++
			if iacEvidence == "" {
				iacEvidence = f
			}
		case (strings.HasSuffix(f, ".yaml") || strings.HasSuffix(f, ".yml")) && looksLikeK8sManifest(tree, f):
			iacCount++
			if iacEvidence == "" {
				iacEvidence = f
			}
		default:
			other++
		}
	}
	if iacCount > 0 && iacCount >= other {
		return domain.ComponentRoleInfra, domain.ConfidenceLow, []domain.SourceEvidence{{Path: iacEvidence}}
	}
	return domain.ComponentRoleOther, domain.ConfidenceLow, nil
}

func looksLikeK8sManifest(tree *inventory.Tree, f string) bool {
	body := tree.ReadString(f)
	return strings.Contains(body, "apiVersion:") && strings.Contains(body, "kind:")
}

func anyDepContains(info *ManifestInfo, substrs ...string) (string, bool) {
	for _, s := range substrs {
		for key := range info.Dependencies {
			if strings.Contains(key, s) {
				return s, true
			}
		}
	}
	return "", false
}

const maxGoFilesScanned = 400

var queueKeywords = []string{"pubsub", "sqs", "kafka", "amqp", "rabbitmq"}

type goSignals struct {
	HasMain          bool
	ListensHTTP      bool
	ListenerEvidence domain.SourceEvidence
	UsesQueue        bool
	QueueEvidence    domain.SourceEvidence
}

// scanGoFiles is the one pass over a Go component's non-test source used by
// backend, worker and library role detection, capped so a vendored blob
// cannot turn shape detection into a full-tree read.
func scanGoFiles(tree *inventory.Tree, dir string) goSignals {
	var sig goSignals
	scanned := 0
	for _, f := range tree.Under(dir) {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		if scanned >= maxGoFilesScanned {
			break
		}
		scanned++
		body := tree.ReadString(f)
		if !strings.Contains(body, "package main") {
			continue
		}
		sig.HasMain = true
		if !sig.ListensHTTP {
			if idx := strings.Index(body, "ListenAndServe("); idx >= 0 {
				sig.ListensHTTP = true
				sig.ListenerEvidence = domain.SourceEvidence{Path: f, Line: lineOf(body, idx)}
			} else if idx := strings.Index(body, "net.Listen("); idx >= 0 {
				sig.ListensHTTP = true
				sig.ListenerEvidence = domain.SourceEvidence{Path: f, Line: lineOf(body, idx)}
			}
		}
		if !sig.UsesQueue {
			lower := strings.ToLower(body)
			for _, kw := range queueKeywords {
				if strings.Contains(lower, kw) {
					sig.UsesQueue = true
					sig.QueueEvidence = domain.SourceEvidence{Path: f}
					break
				}
			}
		}
	}
	return sig
}

func lineOf(body string, idx int) int {
	if idx < 0 || idx > len(body) {
		return 0
	}
	return strings.Count(body[:idx], "\n") + 1
}
