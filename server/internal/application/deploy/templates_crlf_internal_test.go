package deploy

import (
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

type TemplateLineEndingSuite struct{ suite.Suite }

func TestTemplateLineEndingSuite(t *testing.T) { suite.Run(t, new(TemplateLineEndingSuite)) }

func (s *TemplateLineEndingSuite) TestACRLFCheckoutParsesToTheSameRecipe() {
	entries, err := templateFS.ReadDir("templates")
	s.Require().NoError(err)
	s.Require().NotEmpty(entries)
	for _, entry := range entries {
		raw, err := templateFS.ReadFile(path.Join("templates", entry.Name()))
		s.Require().NoError(err)
		lf := strings.ReplaceAll(string(raw), "\r\n", "\n")

		want, err := parseTemplate(lf)
		s.Require().NoError(err, entry.Name())
		got, err := parseTemplate(strings.ReplaceAll(lf, "\n", "\r\n"))
		s.Require().NoError(err, entry.Name())

		s.Equal(want, got, entry.Name())
		s.NotContains(got.Body, "\r", entry.Name())
	}
}

func (s *TemplateLineEndingSuite) TestABOMAndCRLFTogetherStillParse() {
	tpl, err := parseTemplate("\ufeff---\r\nid: x\r\nprovider: vercel\r\nname: X\r\nworkflow_file: deploy.yml\r\n---\r\nbody line\r\n")

	s.Require().NoError(err)
	s.Equal("x", tpl.ID)
	s.Equal("body line", tpl.Body)
}
