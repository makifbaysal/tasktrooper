package indexer

import (
	"bytes"
	"path"
	"regexp"
	"strings"
)

const generatedHeadBytes = 1024

var (
	generatedDirs = map[string]struct{}{
		"mock": {}, "mocks": {}, "__mocks__": {}, "generated": {}, "__generated__": {},
	}
	generatedPrefixes = []string{"mock_", "mock-", "zz_generated"}
	generatedSuffixes = []string{".pb.go", "_pb2.py", "_pb2_grpc.py", ".pb.ts", "_gen.go", ".gen.go", ".gen.ts", ".min.js"}

	mockSuffixPattern = regexp.MustCompile(`[._-]mocks?\.[a-z0-9]+$`)
	mockPrefixPattern = regexp.MustCompile(`^Mock[A-Z]`)
	goGeneratedHeader = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.$`)
	doNotEditMarker   = "do not edit"
	generatedWordMark = "generated"
	atGeneratedMarker = "@generated"
)

func IsGeneratedOrMockPath(rel string) bool {
	rel = strings.ReplaceAll(rel, "\\", "/")
	dir, base := path.Split(rel)
	for _, seg := range strings.Split(strings.Trim(dir, "/"), "/") {
		if _, ok := generatedDirs[strings.ToLower(seg)]; ok {
			return true
		}
	}
	if base == "" {
		return false
	}
	if mockPrefixPattern.MatchString(base) {
		return true
	}
	lower := strings.ToLower(base)
	for _, p := range generatedPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	for _, s := range generatedSuffixes {
		if strings.HasSuffix(lower, s) {
			return true
		}
	}
	return mockSuffixPattern.MatchString(lower) || strings.Contains(lower, ".generated.")
}

func HasGeneratedMarker(head []byte) bool {
	if len(head) > generatedHeadBytes {
		head = head[:generatedHeadBytes]
	}
	if goGeneratedHeader.Match(head) {
		return true
	}
	for _, line := range bytes.Split(head, []byte("\n")) {
		trimmed := strings.TrimSpace(string(line))
		if !isCommentLine(trimmed) {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, atGeneratedMarker) {
			return true
		}
		if strings.Contains(lower, generatedWordMark) && strings.Contains(lower, doNotEditMarker) {
			return true
		}
	}
	return false
}

func isCommentLine(line string) bool {
	return strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") ||
		strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "*")
}
