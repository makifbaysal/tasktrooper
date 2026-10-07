package workspace

import "strings"

// stripExtendedPrefix turns the \\?\ form GetFinalPathNameByHandle returns
// back into the spelling filepath.Abs produces, so the two can be compared.
func stripExtendedPrefix(p string) string {
	if rest, ok := strings.CutPrefix(p, `\\?\UNC\`); ok {
		return `\\` + rest
	}
	return strings.TrimPrefix(p, `\\?\`)
}
