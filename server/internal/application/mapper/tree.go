package mapper

import (
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type treeNode struct {
	name     string
	isFile   bool
	children map[string]*treeNode
}

func BuildTree(paths []string, maxDepth, maxFiles int) string {
	if len(paths) == 0 {
		return ".\n"
	}
	root := buildTreeStructure(paths)
	return renderTree(root, maxDepth, maxFiles, "")
}

func ExpandTree(prefix string, paths []string, maxDepth int) string {
	prefix = treePrefix(prefix)
	filtered := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.Trim(p, "/")
		if prefix == "" || p == prefix || strings.HasPrefix(p, prefix+"/") {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		return prefix + "\n"
	}
	root := buildTreeStructure(filtered)
	label := prefix
	if label == "" {
		label = "."
	}
	return renderTree(root, maxDepth, 0, label)
}

// treePrefix puts a model-supplied prefix in the form Walk's paths use —
// slash-separated, no "./", no trailing separator — so `src\app` on Windows
// and "./src/" anywhere still select src/app.
func treePrefix(prefix string) string {
	cleaned := path.Clean(filepath.ToSlash(strings.TrimSpace(prefix)))
	if cleaned == "." {
		return ""
	}
	return strings.Trim(cleaned, "/")
}

func buildTreeStructure(paths []string) *treeNode {
	root := &treeNode{name: ".", children: map[string]*treeNode{}}
	for _, rel := range paths {
		rel = strings.Trim(rel, "/")
		if rel == "" {
			continue
		}
		parts := strings.Split(rel, "/")
		current := root
		for i, part := range parts {
			isFile := i == len(parts)-1
			child, ok := current.children[part]
			if !ok {
				child = &treeNode{name: part, isFile: isFile, children: map[string]*treeNode{}}
				current.children[part] = child
			}
			if isFile {
				child.isFile = true
			}
			current = child
		}
	}
	return root
}

func renderTree(root *treeNode, maxDepth, maxFiles int, label string) string {
	if label == "" {
		label = root.name
	}
	var b strings.Builder
	b.WriteString(label)
	b.WriteByte('\n')
	remaining := maxFiles
	if maxFiles <= 0 {
		remaining = pathsInTree(root)
	}
	lines, omitted := renderChildren(root, "", maxDepth, 1, &remaining, maxFiles > 0)
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if omitted > 0 {
		b.WriteString("... ")
		b.WriteString(strconv.Itoa(omitted))
		b.WriteString(" more files\n")
	}
	return b.String()
}

func pathsInTree(node *treeNode) int {
	if node.isFile && len(node.children) == 0 {
		return 1
	}
	count := 0
	for _, child := range node.children {
		count += pathsInTree(child)
	}
	return count
}

func renderChildren(node *treeNode, prefix string, maxDepth, depth int, remaining *int, limitFiles bool) ([]string, int) {
	names := sortedChildNames(node)
	var lines []string
	omitted := 0
	for i, name := range names {
		child := node.children[name]
		isLast := i == len(names)-1
		branch := "├── "
		nextPrefix := prefix + "│   "
		if isLast {
			branch = "└── "
			nextPrefix = prefix + "    "
		}

		if child.isFile && len(child.children) == 0 {
			if limitFiles && *remaining <= 0 {
				omitted++
				continue
			}
			lines = append(lines, prefix+branch+name)
			if limitFiles {
				*remaining--
			}
			continue
		}

		if maxDepth > 0 && depth >= maxDepth {
			lines = append(lines, prefix+branch+name+"/")
			continue
		}

		lines = append(lines, prefix+branch+name+"/")
		subLines, subOmitted := renderChildren(child, nextPrefix, maxDepth, depth+1, remaining, limitFiles)
		lines = append(lines, subLines...)
		omitted += subOmitted
	}
	return lines, omitted
}

func sortedChildNames(node *treeNode) []string {
	names := make([]string, 0, len(node.children))
	for name := range node.children {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
