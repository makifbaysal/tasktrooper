package board

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// coberturaReports finds what `dotnet test --collect:"XPlat Code Coverage"`
// wrote: one coverage.cobertura.xml per test project, each in its own
// run-id folder under the results directory.
func coberturaReports(dir string) []string {
	matches, _ := filepath.Glob(filepath.Join(dir, dotnetCoverageDir, "*", "coverage.cobertura.xml"))
	return matches
}

// parseCoberturaCoverage scores the merged line hits rather than summing each
// report's lines-valid: two test projects covering one library would count
// its lines twice.
func parseCoberturaCoverage(dir, _ string) (float64, bool) {
	hits, ok := coberturaLines(dir)
	if !ok {
		return 0, false
	}
	var total, covered int
	for _, lines := range hits {
		for _, count := range lines {
			total++
			if count > 0 {
				covered++
			}
		}
	}
	if total == 0 {
		return 0, false
	}
	return float64(covered) * 100 / float64(total), true
}

func coberturaLines(dir string) (lineHits, bool) {
	hits := make(lineHits)
	for _, report := range coberturaReports(dir) {
		readCoberturaReport(dir, report, hits)
	}
	if len(hits) == 0 {
		return nil, false
	}
	return hits, true
}

func readCoberturaReport(dir, report string, hits lineHits) {
	f, err := os.Open(report)
	if err != nil {
		return
	}
	defer f.Close()

	var sources []string
	current := ""
	inSource := false
	dec := xml.NewDecoder(f)
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "source":
				inSource = true
			case "class":
				current = coberturaFile(dir, sources, xmlAttr(el, "filename"))
			case "line":
				if current == "" {
					continue
				}
				line, lerr := strconv.Atoi(xmlAttr(el, "number"))
				count, cerr := strconv.Atoi(xmlAttr(el, "hits"))
				if lerr == nil && cerr == nil {
					hits.record(current, line, count)
				}
			}
		case xml.CharData:
			if inSource {
				if src := strings.TrimSpace(string(el)); src != "" {
					sources = append(sources, src)
				}
			}
		case xml.EndElement:
			switch el.Name.Local {
			case "source":
				inSource = false
			case "class":
				current = ""
			}
		}
	}
}

// coberturaFile turns a class's filename into the repository-relative path a
// diff names. coverlet writes it relative to one of <sources>, which are
// absolute paths of the machine that ran the tests.
func coberturaFile(dir string, sources []string, filename string) string {
	if filename == "" {
		return ""
	}
	filename = filepath.FromSlash(strings.ReplaceAll(filename, "\\", "/"))
	candidates := []string{filename}
	if !filepath.IsAbs(filename) {
		candidates = candidates[:0]
		for _, src := range sources {
			candidates = append(candidates, filepath.Join(filepath.FromSlash(strings.ReplaceAll(src, "\\", "/")), filename))
		}
	}
	roots := []string{dir}
	if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
		roots = append(roots, real)
	}
	for _, c := range candidates {
		for _, root := range roots {
			if rel, err := filepath.Rel(root, c); err == nil && !strings.HasPrefix(rel, "..") {
				return filepath.ToSlash(rel)
			}
		}
	}
	if !filepath.IsAbs(filename) && len(sources) == 0 {
		return filepath.ToSlash(filename)
	}
	return ""
}

func xmlAttr(el xml.StartElement, name string) string {
	for _, a := range el.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
