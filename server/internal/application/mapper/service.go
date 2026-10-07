package mapper

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const charsPerToken = 4

type Service struct {
	cfg domain.MappingConfig
}

func NewService(cfg domain.MappingConfig) *Service {
	return &Service{cfg: cfg}
}

func (s *Service) ExpandTree(root, prefix string, maxDepth int) (string, error) {
	if !s.cfg.Enabled {
		return "", nil
	}
	paths, err := Walk(root, WalkOptions{UseGitignore: true, Subdir: treePrefix(prefix)})
	if err != nil {
		return "", err
	}
	if maxDepth <= 0 {
		maxDepth = s.cfg.TreeMaxDepth
		if maxDepth <= 0 {
			maxDepth = 4
		}
	}
	return ExpandTree(prefix, paths, maxDepth), nil
}

// FileSkeleton parses one workspace file. relPath comes straight from a tool
// argument — i.e. from model output — and the extract below ends in os.ReadFile,
// so the containment check is the only thing standing between a "read code
// structure" tool and an arbitrary file reader. It lives here as well as in the
// calling tool because this is the sink: any future caller that forgets the
// check still cannot escape the root.
//
// The sibling render paths (BuildSkeleton, BuildPackageSummary) feed
// extractFileSkeleton with paths Walk produced from the root itself, so they
// are already confined by construction and pay nothing for this.
func (s *Service) FileSkeleton(root, relPath string) (FileSkeleton, error) {
	if !s.cfg.Enabled {
		return FileSkeleton{}, nil
	}
	fullPath, err := workspace.ResolveWithinRoot(root, relPath)
	if err != nil {
		return FileSkeleton{}, err
	}
	return s.extractFileSkeleton(fullPath, relPath)
}

func (s *Service) BuildTree(root string) (string, error) {
	if !s.cfg.Enabled {
		return "", nil
	}
	paths, err := Walk(root, WalkOptions{UseGitignore: true})
	if err != nil {
		return "", err
	}
	maxDepth := s.cfg.TreeMaxDepth
	if maxDepth <= 0 {
		maxDepth = 4
	}
	maxFiles := s.cfg.MaxFiles
	if maxFiles <= 0 {
		maxFiles = 500
	}
	return BuildTree(paths, maxDepth, maxFiles), nil
}

func (s *Service) BuildSkeleton(root string) (string, error) {
	return s.BuildSkeletonRanked(root, nil)
}

// BuildSkeletonRanked renders skeletons with the most-referenced files first
// (fan-in from the workspace symbol graph), so they survive the char limit.
func (s *Service) BuildSkeletonRanked(root string, fanIn map[string]int) (string, error) {
	if !s.cfg.Enabled {
		return "", nil
	}
	paths, err := Walk(root, WalkOptions{UseGitignore: true})
	if err != nil {
		return "", err
	}
	if len(fanIn) > 0 {
		sort.SliceStable(paths, func(i, j int) bool {
			return fanIn[paths[i]] > fanIn[paths[j]]
		})
	}
	return s.renderSkeletons(root, paths), nil
}

func (s *Service) BuildPackageSummary(root string) (string, error) {
	if !s.cfg.Enabled {
		return "", nil
	}
	paths, err := Walk(root, WalkOptions{UseGitignore: true})
	if err != nil {
		return "", err
	}
	maxDepth := s.cfg.TreeMaxDepth
	if maxDepth <= 0 {
		maxDepth = 4
	}
	maxFiles := s.cfg.MaxFiles
	if maxFiles <= 0 {
		maxFiles = 500
	}
	tree := BuildTree(paths, maxDepth, maxFiles)
	skeleton := s.renderSkeletons(root, paths)
	limit := s.skeletonCharLimit()

	var b strings.Builder
	b.WriteString("# Repository Structure\n")
	b.WriteString(tree)
	if !strings.HasSuffix(tree, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("\n# Code Skeleton\n")
	remaining := limit - b.Len()
	if remaining <= 0 {
		return truncateDoc(b.String(), limit), nil
	}
	if len(skeleton) > remaining {
		skeleton = truncateDoc(skeleton, remaining)
	}
	b.WriteString(skeleton)
	return truncateDoc(b.String(), limit), nil
}

func (s *Service) skeletonCharLimit() int {
	tokens := s.cfg.SkeletonMaxTokens
	if tokens <= 0 {
		tokens = 3000
	}
	return tokens * charsPerToken
}

func (s *Service) renderSkeletons(root string, paths []string) string {
	limit := s.skeletonCharLimit()
	var sections []string
	used := 0
	for _, rel := range paths {
		if !isSkeletonPath(rel) {
			continue
		}
		sk, err := s.extractFileSkeleton(filepath.Join(root, rel), rel)
		if err != nil || len(sk.Symbols) == 0 && sk.Doc == "" && sk.Package == "" {
			continue
		}
		section := FormatFileSkeleton(sk, defaultMaxDocChars)
		if section == "" {
			continue
		}
		if used+len(section)+1 > limit {
			break
		}
		sections = append(sections, section)
		used += len(section) + 1
	}
	return strings.Join(sections, "\n\n")
}

func (s *Service) extractFileSkeleton(fullPath, relPath string) (FileSkeleton, error) {
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return FileSkeleton{}, fmt.Errorf("read skeleton file: %w", err)
	}
	lower := strings.ToLower(relPath)
	switch {
	case strings.HasSuffix(lower, ".go"):
		sk, err := extractGoSkeleton(relPath, content)
		if err != nil {
			return FileSkeleton{}, err
		}
		return sk, nil
	case isTSPath(relPath):
		return extractTSSkeleton(relPath, content), nil
	case isPyPath(relPath):
		return extractPySkeleton(relPath, content), nil
	default:
		return FileSkeleton{}, nil
	}
}

func isSkeletonPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".go") ||
		isTSPath(path) ||
		isPyPath(path)
}
