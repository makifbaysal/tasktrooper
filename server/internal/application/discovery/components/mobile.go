package components

import (
	"regexp"
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// buildMobileFacts is the mobile-only half of a component: platform, store
// identity and release build targets, ported from
// application/repository.DetectAppIdentity/DetectBuildTargets/DetectMobilePlatform
// so an import through the old path and a scan through this one agree.
func buildMobileFacts(tree *inventory.Tree, dir string, info *ManifestInfo) *domain.MobileFacts {
	platform := detectMobilePlatform(tree, dir, info)
	identity := detectAppIdentity(tree, dir)
	targets := detectBuildTargets(tree, dir)
	if platform == "" && identity.IsZero() && targets.IsZero() {
		return nil
	}
	return &domain.MobileFacts{Platform: platform, Identity: identity, BuildTargets: targets}
}

func detectMobilePlatform(tree *inventory.Tree, dir string, info *ManifestInfo) string {
	hasAndroidDir := tree.HasDir(inventory.Join(dir, "android"))
	hasIOSDir := tree.HasDir(inventory.Join(dir, "ios"))
	isFlutter := (info != nil && info.Ecosystem == "dart") || looksFlutterAppTree(tree, dir)
	if isFlutter || (hasAndroidDir && hasIOSDir) {
		return domain.MobilePlatformCross
	}
	ios := hasIOSDir || hasAppleProjectTree(tree, dir)
	android := hasAndroidDir || hasGradleProjectTree(tree, dir)
	switch {
	case ios && android:
		return domain.MobilePlatformCross
	case ios:
		return domain.MobilePlatformIOS
	case android:
		return domain.MobilePlatformAndroid
	}
	return ""
}

func looksFlutterAppTree(tree *inventory.Tree, dir string) bool {
	return strings.Contains(tree.ReadString(inventory.Join(dir, "pubspec.yaml")), "flutter:")
}

func hasAppleProjectTree(tree *inventory.Tree, dir string) bool {
	if tree.Has(inventory.Join(dir, "project.yml")) || tree.Has(inventory.Join(dir, "Package.swift")) {
		return true
	}
	for _, name := range childDirs(tree, dir) {
		if strings.HasSuffix(name, ".xcodeproj") || strings.HasSuffix(name, ".xcworkspace") {
			return true
		}
	}
	return false
}

func hasAndroidManifestTree(tree *inventory.Tree, dir string) bool {
	return tree.Has(inventory.Join(dir, "AndroidManifest.xml")) || tree.Has(inventory.Join(dir, "app/src/main/AndroidManifest.xml"))
}

func hasGradleProjectTree(tree *inventory.Tree, dir string) bool {
	if hasAndroidManifestTree(tree, dir) {
		return true
	}
	for _, m := range []string{"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "gradlew"} {
		if tree.Has(inventory.Join(dir, m)) {
			return true
		}
	}
	return false
}

// childDirs lists the immediate subdirectory names the tree's file list
// implies under dir — there is no real directory listing over a git
// ls-files-derived Tree, only the paths of the files it kept.
func childDirs(tree *inventory.Tree, dir string) []string {
	prefix := dir + "/"
	seen := map[string]bool{}
	for _, f := range tree.Files {
		rest := f
		if dir != "." {
			if !strings.HasPrefix(f, prefix) {
				continue
			}
			rest = strings.TrimPrefix(f, prefix)
		}
		i := strings.IndexByte(rest, '/')
		if i < 0 {
			continue
		}
		name := rest[:i]
		if !skipDirName(name) {
			seen[name] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// childFiles lists the file basenames directly under dir (not recursively)
// ending with suffix.
func childFiles(tree *inventory.Tree, dir, suffix string) []string {
	prefix := dir + "/"
	var out []string
	for _, f := range tree.Files {
		if !strings.HasPrefix(f, prefix) {
			continue
		}
		rest := strings.TrimPrefix(f, prefix)
		if strings.Contains(rest, "/") || !strings.HasSuffix(rest, suffix) {
			continue
		}
		out = append(out, rest)
	}
	return out
}

func skipDirName(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "build", ".venv", "target":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// --- app identity (ported from application/repository/appidentity.go) ---

func detectAppIdentity(tree *inventory.Tree, dir string) domain.AppIdentity {
	return domain.AppIdentity{
		BundleID:    detectBundleID(tree, dir),
		PackageName: detectPackageName(tree, dir),
	}
}

func detectPackageName(tree *inventory.Tree, dir string) string {
	for _, rel := range []string{
		"android/app/build.gradle", "android/app/build.gradle.kts",
		"app/build.gradle", "app/build.gradle.kts",
		"android/build.gradle", "android/build.gradle.kts",
		"build.gradle", "build.gradle.kts",
	} {
		body := tree.ReadString(inventory.Join(dir, rel))
		if body == "" {
			continue
		}
		if id := gradleApplicationID(body); id != "" {
			return id
		}
	}
	for _, rel := range []string{
		"android/app/src/main/AndroidManifest.xml",
		"app/src/main/AndroidManifest.xml",
		"src/main/AndroidManifest.xml",
		"AndroidManifest.xml",
	} {
		body := tree.ReadString(inventory.Join(dir, rel))
		if body == "" {
			continue
		}
		if id := manifestPackage(body); id != "" {
			return id
		}
	}
	return ""
}

func detectBundleID(tree *inventory.Tree, dir string) string {
	for _, base := range []string{dir, inventory.Join(dir, "ios")} {
		for _, proj := range xcodeProjectsIn(tree, base) {
			body := tree.ReadString(inventory.Join(proj, "project.pbxproj"))
			if body == "" {
				continue
			}
			if id := pickBundleID(pbxBundleIDs(body)); id != "" {
				return id
			}
		}
	}
	for _, p := range infoPlistPaths(tree, dir) {
		body := tree.ReadString(p)
		if body == "" {
			continue
		}
		if id := plistBundleID(body); id != "" {
			return id
		}
	}
	return ""
}

func xcodeProjectsIn(tree *inventory.Tree, base string) []string {
	var out []string
	for _, name := range childDirs(tree, base) {
		if strings.HasSuffix(name, ".xcodeproj") {
			out = append(out, inventory.Join(base, name))
		}
	}
	return out
}

func infoPlistPaths(tree *inventory.Tree, dir string) []string {
	paths := []string{inventory.Join(dir, "ios/Runner/Info.plist")}
	for _, base := range []string{inventory.Join(dir, "ios"), dir} {
		for _, child := range childDirs(tree, base) {
			paths = append(paths, inventory.Join(base, child+"/Info.plist"))
		}
	}
	return append(paths, inventory.Join(dir, "Info.plist"))
}

var applicationIDPattern = regexp.MustCompile(`applicationId\s*=?\s*["']([^"']*)["']`)

func gradleApplicationID(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		m := applicationIDPattern.FindStringSubmatch(trimmed)
		if m != nil && plausibleAppID(m[1]) {
			return m[1]
		}
	}
	return ""
}

var (
	manifestTagPattern     = regexp.MustCompile(`(?s)<manifest\b[^>]*>`)
	manifestPackagePattern = regexp.MustCompile(`(?:^|\s)package\s*=\s*"([^"]*)"`)
)

func manifestPackage(body string) string {
	tag := manifestTagPattern.FindString(body)
	if tag == "" {
		return ""
	}
	m := manifestPackagePattern.FindStringSubmatch(tag)
	if m == nil || !plausibleAppID(m[1]) {
		return ""
	}
	return m[1]
}

var pbxBundleIDPattern = regexp.MustCompile(`PRODUCT_BUNDLE_IDENTIFIER\s*=\s*("[^"\n]*"|[^;\n]*);`)

func pbxBundleIDs(body string) []string {
	var out []string
	for _, m := range pbxBundleIDPattern.FindAllStringSubmatch(body, -1) {
		value := strings.Trim(strings.TrimSpace(m[1]), `"`)
		if plausibleAppID(value) {
			out = append(out, value)
		}
	}
	return out
}

func pickBundleID(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	primary := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if extendsAnother(c, candidates) || auxiliaryTarget(c) {
			continue
		}
		primary = append(primary, c)
	}
	if len(primary) == 0 {
		primary = candidates
	}
	best := primary[0]
	for _, c := range primary[1:] {
		if len(c) < len(best) {
			best = c
		}
	}
	return best
}

func extendsAnother(candidate string, all []string) bool {
	for _, other := range all {
		if other != candidate && strings.HasPrefix(candidate, other+".") {
			return true
		}
	}
	return false
}

var auxiliarySuffixes = []string{
	"test", "tests", "uitest", "uitests", "testing",
	"widget", "widgets", "widgetextension", "extension",
	"notificationservice", "notificationcontent", "shareextension", "todayextension",
	"watchkitapp", "watchkitextension", "intents", "intentsui", "clip",
}

func auxiliaryTarget(id string) bool {
	last := id
	if i := strings.LastIndex(id, "."); i >= 0 {
		last = id[i+1:]
	}
	last = strings.ToLower(last)
	for _, suffix := range auxiliarySuffixes {
		if strings.HasSuffix(last, suffix) {
			return true
		}
	}
	return false
}

var plistStringPattern = regexp.MustCompile(`(?s)<string>(.*?)</string>`)

func plistBundleID(body string) string {
	idx := strings.Index(body, "<key>CFBundleIdentifier</key>")
	if idx < 0 {
		return ""
	}
	m := plistStringPattern.FindStringSubmatch(body[idx:])
	if m == nil {
		return ""
	}
	value := strings.TrimSpace(m[1])
	if !plausibleAppID(value) {
		return ""
	}
	return value
}

func plausibleAppID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 3 || !strings.Contains(value, ".") {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return !strings.HasPrefix(value, ".") && !strings.HasSuffix(value, ".")
}

// --- build targets (ported from application/repository/buildtargets.go) ---

func detectBuildTargets(tree *inventory.Tree, dir string) domain.BuildTargets {
	return domain.BuildTargets{
		XcodeScheme:  detectXcodeScheme(tree, dir),
		GradleModule: detectGradleModule(tree, dir),
	}
}

func platformDirs(dir, platform string) []string {
	return []string{dir, inventory.Join(dir, platform)}
}

var (
	schemeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,99}$`)
	moduleRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(:[A-Za-z0-9][A-Za-z0-9._-]*)*$`)
)

func detectXcodeScheme(tree *inventory.Tree, dir string) string {
	for _, base := range platformDirs(dir, "ios") {
		containers := xcodeContainers(tree, base)
		if len(containers) == 0 {
			continue
		}
		if shared := sharedSchemes(tree, containers); len(shared) > 0 {
			return pickScheme(shared)
		}
		return pickScheme(containerNames(containers))
	}
	return ""
}

func xcodeContainers(tree *inventory.Tree, dir string) []string {
	var out []string
	for _, name := range childDirs(tree, dir) {
		if strings.HasSuffix(name, ".xcworkspace") || strings.HasSuffix(name, ".xcodeproj") {
			out = append(out, inventory.Join(dir, name))
		}
	}
	return out
}

func sharedSchemes(tree *inventory.Tree, containers []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, container := range containers {
		for _, name := range childFiles(tree, inventory.Join(container, "xcshareddata/xcschemes"), ".xcscheme") {
			schemeName := strings.TrimSuffix(name, ".xcscheme")
			if !seen[schemeName] {
				seen[schemeName] = true
				out = append(out, schemeName)
			}
		}
	}
	return out
}

func containerNames(containers []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, container := range containers {
		name := baseName(container)
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			name = name[:i]
		}
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func pickScheme(candidates []string) string {
	var primary []string
	for _, c := range candidates {
		name := strings.TrimSpace(c)
		if name == "" || auxiliaryTarget(name) || !schemeRe.MatchString(name) {
			continue
		}
		primary = append(primary, name)
	}
	if len(primary) != 1 {
		return ""
	}
	return primary[0]
}

func detectGradleModule(tree *inventory.Tree, dir string) string {
	for _, base := range platformDirs(dir, "android") {
		var apps []string
		for _, module := range includedModules(tree, base) {
			if appliesAndroidApplication(tree, moduleDir(base, module)) {
				apps = append(apps, module)
			}
		}
		if len(apps) == 1 {
			return apps[0]
		}
		if len(apps) > 1 {
			return ""
		}
	}
	return ""
}

func includedModules(tree *inventory.Tree, dir string) []string {
	body := ""
	for _, name := range []string{"settings.gradle", "settings.gradle.kts"} {
		if text := tree.ReadString(inventory.Join(dir, name)); text != "" {
			body += "\n" + text
		}
	}
	if strings.TrimSpace(body) == "" {
		return []string{"app"}
	}
	modules := parseGradleModuleIncludes(body)
	if len(modules) == 0 {
		return []string{"app"}
	}
	return modules
}

var (
	blockCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)
	includeRe      = regexp.MustCompile(`(?m)include\s*\(([^)]*)\)|^\s*include\s+([^\n]*)`)
	quotedRe       = regexp.MustCompile(`["']([^"']*)["']`)
)

func parseGradleModuleIncludes(body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range includeRe.FindAllStringSubmatch(stripGradleComments(body), -1) {
		args := m[1] + m[2]
		for _, q := range quotedRe.FindAllStringSubmatch(args, -1) {
			module := strings.Trim(strings.TrimSpace(q[1]), ":")
			if module == "" || seen[module] || !moduleRe.MatchString(module) {
				continue
			}
			seen[module] = true
			out = append(out, module)
		}
	}
	return out
}

func stripGradleComments(body string) string {
	body = blockCommentRe.ReplaceAllString(body, " ")
	var b strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func moduleDir(dir, module string) string {
	return inventory.Join(dir, strings.ReplaceAll(module, ":", "/"))
}

var androidAppPluginRe = regexp.MustCompile(`com\.android\.application\b|plugins\.android[._]?[Aa]pplication\b`)

func appliesAndroidApplication(tree *inventory.Tree, dir string) bool {
	for _, name := range []string{"build.gradle", "build.gradle.kts"} {
		body := tree.ReadString(inventory.Join(dir, name))
		if body == "" {
			continue
		}
		if androidAppPluginRe.MatchString(stripGradleComments(body)) {
			return true
		}
	}
	return false
}
