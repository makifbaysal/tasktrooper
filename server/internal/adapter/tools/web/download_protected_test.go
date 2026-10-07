package web_test

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/web"
)

// download_file shares the file writers' guard, so .git is refused under every
// spelling the host filesystem resolves to it, not only the literal ".git".
func (s *DownloadToolSuite) TestRefusesEveryAliasOfGit() {
	s.Require().NoError(os.MkdirAll(filepath.Join(s.workspace, ".git", "hooks"), 0o755))
	s.Require().NoError(os.Symlink(filepath.Join(s.workspace, ".git"), filepath.Join(s.workspace, "gitlink")))
	paths := []string{"gitlink/hooks/pre-commit", "src/../.git/hooks/pre-commit"}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		paths = append(paths, ".GIT/hooks/pre-commit")
	}
	tool := web.NewDownloadTool(web.WithURLPolicy(localPolicy()))

	for _, path := range paths {
		result := tool.Execute(s.ctx, `{"url":"http://example.com/a.png","path":"`+path+`"}`)
		s.True(result.IsError, path)
		s.Contains(result.Content, "protected", path)
	}
	s.NoFileExists(filepath.Join(s.workspace, ".git", "hooks", "pre-commit"))
}
