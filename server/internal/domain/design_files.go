package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// DesignSystemFile is one file a repository's effective design system is
// written to: DESIGN.md, the token tree, the CSS variables and the inventory.
type DesignSystemFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

const (
	DesignFileDesignMD  = "DESIGN.md"
	DesignFileTokens    = "design/tokens.json"
	DesignFileCSS       = "design/tokens.css"
	DesignFileInventory = "design/INVENTORY.md"
)

// DesignTokensCSS renders a token tree as CSS custom properties on :root,
// one per token, named by its path (color.primary → --color-primary); an
// alias becomes var() of the token it names.
func DesignTokensCSS(tokens json.RawMessage) (string, error) {
	tree, err := tokenTree(tokens)
	if err != nil {
		return "", err
	}
	flat := map[string]flatToken{}
	var empty []string
	flattenTokens(tree, "", "", flat, &empty)
	paths := make([]string, 0, len(flat))
	for p := range flat {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var sb strings.Builder
	sb.WriteString(":root {\n")
	for _, p := range paths {
		for _, decl := range cssDeclarations(cssVarName(p), flat[p].value) {
			sb.WriteString("  " + decl + "\n")
		}
	}
	sb.WriteString("}\n")
	return sb.String(), nil
}

func cssDeclarations(name string, value any) []string {
	switch v := value.(type) {
	case string:
		if m := aliasPattern.FindStringSubmatch(strings.TrimSpace(v)); m != nil {
			return []string{fmt.Sprintf("%s: var(%s);", name, cssVarName(m[1]))}
		}
		return []string{fmt.Sprintf("%s: %s;", name, v)}
	case float64:
		return []string{fmt.Sprintf("%s: %s;", name, strconv.FormatFloat(v, 'f', -1, 64))}
	case map[string]any:
		if css, ok := cssObjectValue(v); ok {
			return []string{fmt.Sprintf("%s: %s;", name, css)}
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var out []string
		for _, k := range keys {
			switch v[k].(type) {
			case string, float64, map[string]any:
				out = append(out, cssDeclarations(name+"-"+cssIdent(k), v[k])...)
			}
		}
		return out
	}
	return nil
}

// cssObjectValue reads the DTCG 2025.10 object forms that are one CSS value:
// a dimension or duration {value, unit} and a color {colorSpace, components}.
func cssObjectValue(v map[string]any) (string, bool) {
	if num, ok := v["value"].(float64); ok {
		if unit, ok := v["unit"].(string); ok {
			return strconv.FormatFloat(num, 'f', -1, 64) + unit, true
		}
	}
	if hex, ok := v["hex"].(string); ok {
		return hex, true
	}
	if space, ok := v["colorSpace"].(string); ok {
		comps, ok := v["components"].([]any)
		if !ok || len(comps) < 3 {
			return "", false
		}
		parts := make([]string, 0, 3)
		for _, c := range comps[:3] {
			f, ok := c.(float64)
			if !ok {
				return "", false
			}
			parts = append(parts, strconv.FormatFloat(f, 'f', -1, 64))
		}
		switch space {
		case "srgb":
			return "color(srgb " + strings.Join(parts, " ") + ")", true
		case "oklch", "oklab":
			return space + "(" + strings.Join(parts, " ") + ")", true
		}
		return "color(" + space + " " + strings.Join(parts, " ") + ")", true
	}
	return "", false
}

func cssVarName(path string) string {
	segments := strings.Split(path, ".")
	for i, s := range segments {
		segments[i] = cssIdent(s)
	}
	return "--" + strings.Join(segments, "-")
}

func cssIdent(s string) string {
	var sb strings.Builder
	for i, r := range s {
		switch {
		case unicode.IsUpper(r):
			if i > 0 {
				sb.WriteByte('-')
			}
			sb.WriteRune(unicode.ToLower(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			sb.WriteRune(r)
		default:
			sb.WriteByte('-')
		}
	}
	return sb.String()
}

// PrettyDesignTokens indents a token tree for a file a person reads.
func PrettyDesignTokens(tokens json.RawMessage) (string, error) {
	tree, err := tokenTree(tokens)
	if err != nil {
		return "", err
	}
	out, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}
