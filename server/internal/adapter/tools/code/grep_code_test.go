package code_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/code"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// GrepCodeCaseSuite covers matching behaviour rather than containment. A model
// searching for UI copy types the casing from the ticket, and a case-sensitive
// miss reads back to it as "this string is not in the repository" — so the tool
// ignores case unless the caller explicitly asks for exact matching.
type GrepCodeCaseSuite struct {
	suite.Suite
	withoutRipgrep bool
	root           string
	ctx            context.Context
	kit            *code.ToolKit
	tool           port.ToolExecutor
}

func TestGrepCodeCaseSuite(t *testing.T) {
	suite.Run(t, new(GrepCodeCaseSuite))
}

// The same behaviour has to hold on a machine with no rg — a stock Windows
// install — where grep_code searches in-process instead.
func TestGrepCodeCaseSuiteWithoutRipgrep(t *testing.T) {
	suite.Run(t, &GrepCodeCaseSuite{withoutRipgrep: true})
}

func (s *GrepCodeCaseSuite) SetupTest() {
	if _, err := exec.LookPath("rg"); err != nil && !s.withoutRipgrep {
		s.T().Skip("ripgrep not installed")
	}

	root := s.T().TempDir()
	s.root = root
	s.Require().NoError(os.WriteFile(
		filepath.Join(root, "banner.tsx"),
		[]byte("export const Banner = () => <div>Coming Soon</div>\n"), 0o644))
	s.Require().NoError(os.WriteFile(
		filepath.Join(root, ".gitignore"), []byte("vendor\n"), 0o644))
	s.Require().NoError(os.MkdirAll(filepath.Join(root, "vendor"), 0o755))
	s.Require().NoError(os.WriteFile(
		filepath.Join(root, "vendor", "dep.tsx"),
		[]byte("// Coming Soon, vendored\n"), 0o644))

	s.ctx = registry.ContextWithWorkspaceDir(
		registry.ContextWithSessionID(context.Background(), uuid.New()),
		root,
	)
	mapperSvc := mapper.NewService(domain.MappingConfig{Enabled: true, TreeMaxDepth: 4, MaxFiles: 50})
	s.kit = code.NewToolKit(nil, nil, mapperSvc, domain.IndexerConfig{TopK: 3}, domain.GraphConfig{}, "embed-model")

	if s.withoutRipgrep {
		s.tool = code.NewGrepCodeToolWithoutRipgrep(s.kit)
		return
	}
	for _, executor := range code.NewExecutors(s.kit) {
		if executor.Name() == "grep_code" {
			s.tool = executor
		}
	}
	s.Require().NotNil(s.tool, "grep_code not registered")
}

func (s *GrepCodeCaseSuite) TestReportsLineNumberAndRelativePath() {
	s.Require().NoError(os.MkdirAll(filepath.Join(s.root, "src", "deep"), 0o755))
	s.Require().NoError(os.WriteFile(
		filepath.Join(s.root, "src", "deep", "copy.ts"),
		[]byte("first\nconst label = \"Coming Soon: v2\"\r\nlast\n"), 0o644))

	result := s.tool.Execute(s.ctx, `{"pattern":"coming soon:","path":"src"}`)

	s.False(result.IsError, result.Content)
	s.JSONEq(`{"matches":[{"file_path":"src/deep/copy.ts","line":2,"content":"const label = \"Coming Soon: v2\""}]}`, result.Content)
}

func (s *GrepCodeCaseSuite) TestPathCanNameASingleFile() {
	result := s.tool.Execute(s.ctx, `{"pattern":"coming soon","path":"banner.tsx"}`)

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, `"file_path":"banner.tsx"`)
	s.Contains(result.Content, `"line":1`)
}

func (s *GrepCodeCaseSuite) TestPathScopesTheSearch() {
	s.Require().NoError(os.MkdirAll(filepath.Join(s.root, "other"), 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(s.root, "other", "x.tsx"), []byte("Coming Soon\n"), 0o644))

	result := s.tool.Execute(s.ctx, `{"pattern":"Coming Soon","path":"other"}`)

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "other/x.tsx")
	s.NotContains(result.Content, "banner.tsx")
}

func (s *GrepCodeCaseSuite) TestMaxResultsCapsTheMatches() {
	s.Require().NoError(os.WriteFile(filepath.Join(s.root, "many.txt"), []byte("hit\nhit\nhit\nhit\n"), 0o644))

	result := s.tool.Execute(s.ctx, `{"pattern":"^hit$","max_results":2}`)

	s.False(result.IsError, result.Content)
	s.Equal(2, strings.Count(result.Content, `"many.txt"`))
}

func (s *GrepCodeCaseSuite) TestNoMatchIsAnEmptyListNotAnError() {
	result := s.tool.Execute(s.ctx, `{"pattern":"definitely-not-in-the-repo"}`)

	s.False(result.IsError, result.Content)
	s.JSONEq(`{"matches":[]}`, result.Content)
}

func (s *GrepCodeCaseSuite) TestIgnoresCaseByDefault() {
	for _, pattern := range []string{"coming soon", "COMING SOON", "Coming Soon"} {
		result := s.tool.Execute(s.ctx, `{"pattern":"`+pattern+`"}`)

		s.False(result.IsError, result.Content)
		s.Contains(result.Content, "banner.tsx", "pattern %q found nothing", pattern)
	}
}

func (s *GrepCodeCaseSuite) TestGlobIgnoresCaseByDefault() {
	for _, glob := range []string{"*.tsx", "*.TSX", "*.TsX"} {
		result := s.tool.Execute(s.ctx, `{"pattern":"Coming Soon","glob":"`+glob+`"}`)

		s.False(result.IsError, result.Content)
		s.Contains(result.Content, "banner.tsx", "glob %q filtered everything out", glob)
	}
}

// Case-insensitive globbing also loosens the "!pattern" exclusions built from
// .gitignore, so assert those still keep ignored trees out of the results.
func (s *GrepCodeCaseSuite) TestGitignoreExclusionsStillHold() {
	result := s.tool.Execute(s.ctx, `{"pattern":"Coming Soon"}`)

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "banner.tsx")
	s.NotContains(result.Content, "vendor")
}

func (s *GrepCodeCaseSuite) TestCaseSensitiveOptIn() {
	exact := s.tool.Execute(s.ctx, `{"pattern":"Coming Soon","case_sensitive":true}`)
	s.False(exact.IsError, exact.Content)
	s.Contains(exact.Content, "banner.tsx")

	wrongCase := s.tool.Execute(s.ctx, `{"pattern":"coming soon","case_sensitive":true}`)
	s.False(wrongCase.IsError, wrongCase.Content)
	s.NotContains(wrongCase.Content, "banner.tsx")
}

func (s *GrepCodeCaseSuite) TestMaxResultsCapsTheTotalAcrossFiles() {
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		s.Require().NoError(os.WriteFile(filepath.Join(s.root, name), []byte("hit\nhit\n"), 0o644))
	}

	result := s.tool.Execute(s.ctx, `{"pattern":"^hit$","max_results":3}`)

	s.False(result.IsError, result.Content)
	s.Equal(3, strings.Count(result.Content, `"file_path"`))
}

func (s *GrepCodeCaseSuite) TestLongLinesComeBackAsAPreview() {
	long := "needle " + strings.Repeat("x", 5000)
	s.Require().NoError(os.WriteFile(filepath.Join(s.root, "bundle.min.js"), []byte(long+"\n"), 0o644))

	result := s.tool.Execute(s.ctx, `{"pattern":"needle"}`)

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "needle xxx")
	s.Contains(result.Content, "[... omitted end of long line]")
	s.Less(len(result.Content), 600, "the 5000-byte line must not come back whole")
}

func (s *GrepCodeCaseSuite) TestGitignoreExclusionsHoldInsideAGitRepository() {
	if _, err := exec.LookPath("git"); err != nil {
		s.T().Skip("git not installed")
	}
	s.Require().NoError(exec.Command("git", "-C", s.root, "init", "-q").Run())

	result := s.tool.Execute(s.ctx, `{"pattern":"Coming Soon"}`)

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "banner.tsx")
	s.NotContains(result.Content, "vendor")
}
