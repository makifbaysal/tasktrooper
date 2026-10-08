package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// DesignLintFinding is one problem in a design system version. It carries
// codes and values only — the UI and the tool docs say what each code means.
type DesignLintFinding struct {
	Code        string  `json:"code"`
	Severity    string  `json:"severity"`
	Path        string  `json:"path,omitempty"`
	RelatedPath string  `json:"related_path,omitempty"`
	Value       string  `json:"value,omitempty"`
	Ratio       float64 `json:"ratio,omitempty"`
}

const (
	DesignLintError   = "error"
	DesignLintWarning = "warning"

	DesignLintUnresolvedAlias = "unresolved_alias"
	DesignLintInvalidColor    = "invalid_color"
	DesignLintContrastBelowAA = "contrast_below_aa"
	DesignLintEmptyGroup      = "empty_group"
	DesignLintMissingSection  = "missing_section"

	minTextContrast = 4.5
)

// designMDSections are the DESIGN.md sections a project base must carry; each
// entry lists the headings that count for it.
var designMDSections = []struct {
	name    string
	matches []string
}{
	{"Overview", []string{"overview"}},
	{"Colors", []string{"color", "colour"}},
	{"Typography", []string{"typography", "type"}},
	{"Components", []string{"component"}},
	{"Do's and Don'ts", []string{"do's", "dos", "do and don", "don't", "donts"}},
}

type flatToken struct {
	path  string
	typ   string
	value any
}

// LintDesignSystem checks a version's tokens — and, for a project base, its
// DESIGN.md — for what an agent building from it would trip over.
func LintDesignSystem(scope DesignSystemScope, designMD string, tokens json.RawMessage) []DesignLintFinding {
	findings := []DesignLintFinding{}
	tree, err := tokenTree(tokens)
	if err != nil {
		return findings
	}
	flat := map[string]flatToken{}
	var empty []string
	flattenTokens(tree, "", "", flat, &empty)
	for _, p := range empty {
		findings = append(findings, DesignLintFinding{Code: DesignLintEmptyGroup, Severity: DesignLintWarning, Path: p})
	}

	paths := make([]string, 0, len(flat))
	for p := range flat {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	colors := map[string]rgb{}
	for _, p := range paths {
		tok := flat[p]
		resolved, ok := resolveTokenValue(flat, tok.value, 0)
		if !ok {
			findings = append(findings, DesignLintFinding{
				Code: DesignLintUnresolvedAlias, Severity: DesignLintError, Path: p, Value: fmt.Sprint(tok.value),
			})
			continue
		}
		if !isColorToken(tok.typ, resolved) {
			continue
		}
		c, ok := parseColor(resolved)
		if !ok {
			findings = append(findings, DesignLintFinding{
				Code: DesignLintInvalidColor, Severity: DesignLintError, Path: p, Value: colorText(resolved),
			})
			continue
		}
		colors[p] = c
	}

	for _, p := range paths {
		bg, ok := colors[p]
		if !ok {
			continue
		}
		for _, fgPath := range foregroundPaths(p) {
			fg, ok := colors[fgPath]
			if !ok {
				continue
			}
			ratio := contrastRatio(bg, fg)
			if ratio < minTextContrast {
				findings = append(findings, DesignLintFinding{
					Code: DesignLintContrastBelowAA, Severity: DesignLintError, Path: p, RelatedPath: fgPath,
					Ratio: math.Round(ratio*100) / 100,
				})
			}
		}
	}

	if scope == DesignSystemScopeProject {
		headings := markdownHeadings(designMD)
		for _, section := range designMDSections {
			if !anyHeadingMatches(headings, section.matches) {
				findings = append(findings, DesignLintFinding{Code: DesignLintMissingSection, Severity: DesignLintWarning, Value: section.name})
			}
		}
	}
	return findings
}

func flattenTokens(node map[string]any, prefix, inheritedType string, out map[string]flatToken, empty *[]string) {
	groupType := inheritedType
	if t, ok := node["$type"].(string); ok {
		groupType = t
	}
	children := 0
	for k, v := range node {
		if strings.HasPrefix(k, "$") {
			continue
		}
		child, ok := v.(map[string]any)
		if !ok {
			continue
		}
		children++
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if value, isToken := child[designTokenValueKey]; isToken {
			typ := groupType
			if t, ok := child["$type"].(string); ok {
				typ = t
			}
			out[path] = flatToken{path: path, typ: typ, value: value}
			continue
		}
		flattenTokens(child, path, groupType, out, empty)
	}
	if children == 0 && prefix != "" {
		*empty = append(*empty, prefix)
	}
}

var aliasPattern = regexp.MustCompile(`^\{([^{}]+)\}$`)

func resolveTokenValue(flat map[string]flatToken, value any, depth int) (any, bool) {
	s, ok := value.(string)
	if !ok {
		return value, true
	}
	m := aliasPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return value, true
	}
	if depth > 16 {
		return nil, false
	}
	target, ok := flat[m[1]]
	if !ok {
		return nil, false
	}
	return resolveTokenValue(flat, target.value, depth+1)
}

func isColorToken(typ string, value any) bool {
	if typ == "color" {
		return true
	}
	if typ != "" {
		return false
	}
	switch v := value.(type) {
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		return strings.HasPrefix(s, "#") || strings.HasPrefix(s, "rgb") || strings.HasPrefix(s, "hsl") ||
			strings.HasPrefix(s, "oklch") || strings.HasPrefix(s, "oklab")
	case map[string]any:
		_, hasSpace := v["colorSpace"]
		return hasSpace
	}
	return false
}

// foregroundPaths are the tokens that name the text color drawn on p:
// primary → primary-foreground, primaryForeground, on-primary, onPrimary.
func foregroundPaths(p string) []string {
	dir, last := "", p
	if i := strings.LastIndex(p, "."); i >= 0 {
		dir, last = p[:i+1], p[i+1:]
	}
	if strings.HasSuffix(last, "-foreground") || strings.HasSuffix(last, "Foreground") ||
		strings.HasPrefix(last, "on-") || (strings.HasPrefix(last, "on") && len(last) > 2 && last[2] >= 'A' && last[2] <= 'Z') {
		return nil
	}
	upper := strings.ToUpper(last[:1]) + last[1:]
	return []string{
		p + "-foreground", p + "Foreground", dir + "on-" + last, dir + "on" + upper, p + ".foreground",
	}
}

func markdownHeadings(md string) []string {
	var out []string
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "#") {
			out = append(out, strings.ToLower(strings.TrimSpace(strings.TrimLeft(t, "#"))))
		}
	}
	return out
}

func anyHeadingMatches(headings, matches []string) bool {
	for _, h := range headings {
		h = strings.ReplaceAll(h, "’", "'")
		for _, m := range matches {
			if strings.Contains(h, m) {
				return true
			}
		}
	}
	return false
}

type rgb struct{ r, g, b float64 }

func colorText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}

// parseColor reads the CSS color forms a token tree carries — hex, rgb(),
// hsl(), oklch(), oklab() — and DTCG 2025.10 color objects, into sRGB 0..1.
func parseColor(v any) (rgb, bool) {
	switch c := v.(type) {
	case string:
		return parseCSSColor(c)
	case map[string]any:
		if hex, ok := c["hex"].(string); ok {
			return parseCSSColor(hex)
		}
		space, _ := c["colorSpace"].(string)
		comps, ok := c["components"].([]any)
		if !ok || len(comps) < 3 {
			return rgb{}, false
		}
		nums := make([]float64, 3)
		for i := 0; i < 3; i++ {
			f, ok := comps[i].(float64)
			if !ok {
				return rgb{}, false
			}
			nums[i] = f
		}
		switch space {
		case "srgb":
			return rgb{nums[0], nums[1], nums[2]}, true
		case "oklch":
			return oklchToSRGB(nums[0], nums[1], nums[2]), true
		case "oklab":
			return oklabToSRGB(nums[0], nums[1], nums[2]), true
		case "hsl":
			return hslToSRGB(nums[0], nums[1]/100, nums[2]/100), true
		}
	}
	return rgb{}, false
}

var cssFunc = regexp.MustCompile(`^(rgba?|hsla?|oklch|oklab)\(\s*([^)]*)\)$`)

func parseCSSColor(s string) (rgb, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(s, "#") {
		return parseHex(s[1:])
	}
	m := cssFunc.FindStringSubmatch(s)
	if m == nil {
		return rgb{}, false
	}
	args := strings.FieldsFunc(m[2], func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
	if len(args) < 3 {
		return rgb{}, false
	}
	switch m[1] {
	case "rgb", "rgba":
		var out [3]float64
		for i := 0; i < 3; i++ {
			f, pct, ok := cssNumber(args[i])
			if !ok {
				return rgb{}, false
			}
			if pct {
				out[i] = f / 100
			} else {
				out[i] = f / 255
			}
		}
		return rgb{out[0], out[1], out[2]}, true
	case "hsl", "hsla":
		h, _, ok1 := cssNumber(strings.TrimSuffix(args[0], "deg"))
		sat, _, ok2 := cssNumber(args[1])
		l, _, ok3 := cssNumber(args[2])
		if !ok1 || !ok2 || !ok3 {
			return rgb{}, false
		}
		return hslToSRGB(h, sat/100, l/100), true
	case "oklch", "oklab":
		l, lpct, ok1 := cssNumber(args[0])
		a, apct, ok2 := cssNumber(strings.TrimSuffix(args[1], "deg"))
		b, bpct, ok3 := cssNumber(strings.TrimSuffix(args[2], "deg"))
		if !ok1 || !ok2 || !ok3 {
			return rgb{}, false
		}
		if lpct {
			l /= 100
		}
		if m[1] == "oklch" {
			if apct {
				a = a / 100 * 0.4
			}
			return oklchToSRGB(l, a, b), true
		}
		if apct {
			a = a / 100 * 0.4
		}
		if bpct {
			b = b / 100 * 0.4
		}
		return oklabToSRGB(l, a, b), true
	}
	return rgb{}, false
}

func cssNumber(s string) (float64, bool, bool) {
	s = strings.TrimSpace(s)
	pct := strings.HasSuffix(s, "%")
	f, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
	return f, pct, err == nil
}

func parseHex(h string) (rgb, bool) {
	switch len(h) {
	case 3, 4:
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	case 6, 8:
		h = h[:6]
	default:
		return rgb{}, false
	}
	n, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return rgb{}, false
	}
	return rgb{float64(n>>16&0xff) / 255, float64(n>>8&0xff) / 255, float64(n&0xff) / 255}, true
}

func hslToSRGB(h, s, l float64) rgb {
	h = math.Mod(math.Mod(h, 360)+360, 360) / 360
	if s == 0 {
		return rgb{l, l, l}
	}
	q := l * (1 + s)
	if l >= 0.5 {
		q = l + s - l*s
	}
	p := 2*l - q
	hue := func(t float64) float64 {
		if t < 0 {
			t++
		}
		if t > 1 {
			t--
		}
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 0.5:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		}
		return p
	}
	return rgb{hue(h + 1.0/3), hue(h), hue(h - 1.0/3)}
}

func oklchToSRGB(l, c, h float64) rgb {
	rad := h * math.Pi / 180
	return oklabToSRGB(l, c*math.Cos(rad), c*math.Sin(rad))
}

func oklabToSRGB(l, a, b float64) rgb {
	l_ := l + 0.3963377774*a + 0.2158037573*b
	m_ := l - 0.1055613458*a - 0.0638541728*b
	s_ := l - 0.0894841775*a - 1.2914855480*b
	lc, mc, sc := l_*l_*l_, m_*m_*m_, s_*s_*s_
	lin := rgb{
		+4.0767416621*lc - 3.3077115913*mc + 0.2309699292*sc,
		-1.2684380046*lc + 2.6097574011*mc - 0.3413193965*sc,
		-0.0041960863*lc - 0.7034186147*mc + 1.7076147010*sc,
	}
	return rgb{encodeSRGB(lin.r), encodeSRGB(lin.g), encodeSRGB(lin.b)}
}

func encodeSRGB(v float64) float64 {
	v = math.Max(0, math.Min(1, v))
	if v <= 0.0031308 {
		return 12.92 * v
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

func linearize(v float64) float64 {
	v = math.Max(0, math.Min(1, v))
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func relativeLuminance(c rgb) float64 {
	return 0.2126*linearize(c.r) + 0.7152*linearize(c.g) + 0.0722*linearize(c.b)
}

func contrastRatio(a, b rgb) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}
