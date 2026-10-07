package agentfs

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const excludeHeader = "# TaskTrooper agent catalog (materialised per run, not source)"

var excludePatterns = []string{
	claudeSkillsDir + "/" + ttPrefix + "*/",
	claudeAgentsDir + "/" + ttPrefix + "*.md",
	cursorRulesDir + "/" + ttPrefix + "*.mdc",
}

func Exclude(root string) error {
	if strings.TrimSpace(root) == "" {
		return errors.New("agentfs: root is empty")
	}

	commonDir, err := gitCommonDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}

	infoDir := filepath.Join(commonDir, "info")
	path := filepath.Join(infoDir, "exclude")
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("agentfs: read %s: %w", path, err)
	}

	have := make(map[string]struct{})
	scanner := bufio.NewScanner(strings.NewReader(string(existing)))
	for scanner.Scan() {
		have[strings.TrimSpace(scanner.Text())] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("agentfs: scan %s: %w", path, err)
	}

	missing := make([]string, 0, len(excludePatterns))
	for _, p := range excludePatterns {
		if _, ok := have[p]; !ok {
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	var b strings.Builder
	b.Write(existing)
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		b.WriteString("\n")
	}
	if _, ok := have[excludeHeader]; !ok {
		b.WriteString(excludeHeader + "\n")
	}
	for _, p := range missing {
		b.WriteString(p + "\n")
	}

	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return fmt.Errorf("agentfs: mkdir %s: %w", infoDir, err)
	}
	if _, err := writeIfChanged(path, b.String()); err != nil {
		return err
	}
	return nil
}

// gitCommonDir is the directory git reads info/exclude from, the same one
// `git rev-parse --git-path info/exclude` resolves. In a linked worktree or a
// submodule .git is a file ("gitdir: <path>"), not a directory: the gitdir it
// names belongs to that one worktree, and a worktree's gitdir records the
// shared repository in its commondir file — which is where info/ lives.
func gitCommonDir(root string) (string, error) {
	dotGit := filepath.Join(root, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		return "", fmt.Errorf("agentfs: stat .git in %s: %w", root, err)
	}
	if info.IsDir() {
		return dotGit, nil
	}

	raw, err := os.ReadFile(dotGit)
	if err != nil {
		return "", fmt.Errorf("agentfs: read %s: %w", dotGit, err)
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir:")
	if !ok {
		return "", fmt.Errorf("agentfs: %s is a file without a gitdir: line", dotGit)
	}
	gitDir = resolveFrom(root, strings.TrimSpace(gitDir))
	if _, err := os.Stat(gitDir); err != nil {
		return "", fmt.Errorf("agentfs: gitdir of %s: %w", root, err)
	}

	common, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if errors.Is(err, os.ErrNotExist) {
		return gitDir, nil
	} else if err != nil {
		return "", fmt.Errorf("agentfs: read %s: %w", filepath.Join(gitDir, "commondir"), err)
	}
	return resolveFrom(gitDir, strings.TrimSpace(string(common))), nil
}

func resolveFrom(base, path string) string {
	path = filepath.FromSlash(path)
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}
