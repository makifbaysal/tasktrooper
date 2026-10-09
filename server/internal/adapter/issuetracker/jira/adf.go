package jira

import (
	"encoding/json"
	"strconv"
	"strings"
)

type adfNode struct {
	Type    string     `json:"type"`
	Text    string     `json:"text"`
	Content []*adfNode `json:"content"`
	Marks   []adfMark  `json:"marks"`
	Attrs   adfAttrs   `json:"attrs"`
}

type adfMark struct {
	Type  string `json:"type"`
	Attrs struct {
		Href string `json:"href"`
	} `json:"attrs"`
}

type adfAttrs struct {
	Text      string `json:"text"`
	ID        string `json:"id"`
	ShortName string `json:"shortName"`
	URL       string `json:"url"`
	Language  string `json:"language"`
	Level     int    `json:"level"`
}

// ADFToMarkdown converts an Atlassian Document Format document to Markdown.
// Anything it cannot parse yields ""; a node it does not know is rendered
// through its children so an unexpected document still reads.
func ADFToMarkdown(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var doc adfNode
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	return strings.TrimSpace(doc.renderBlock(""))
}

func (n *adfNode) renderBlock(indent string) string {
	switch n.Type {
	case "doc", "panel", "expand", "mediaSingle", "mediaGroup":
		return n.renderBlocks(indent)
	case "paragraph", "text":
		return indentText(n.renderInline(), indent)
	case "heading":
		level := n.Attrs.Level
		if level < 1 {
			level = 1
		}
		if level > 6 {
			level = 6
		}
		return indentText(strings.Repeat("#", level)+" "+n.renderInline(), indent)
	case "codeBlock":
		language := strings.TrimSpace(n.Attrs.Language)
		return indentText("```"+language+"\n"+n.plainText()+"\n```", indent)
	case "bulletList":
		return n.renderList(indent, func(int) string { return "- " })
	case "orderedList":
		return n.renderList(indent, func(i int) string { return strconv.Itoa(i+1) + ". " })
	case "listItem":
		return renderListItem(indent, "- ", n)
	case "blockquote":
		inner := n.renderBlocks(indent)
		if inner == "" {
			return ""
		}
		return prefixQuote(inner)
	case "rule":
		return indent + "---"
	case "table":
		return n.renderTable(indent)
	case "inlineCard", "blockCard", "mention", "emoji":
		return indentText(n.renderInline(), indent)
	default:
		if len(n.Content) > 0 {
			return n.renderBlocks(indent)
		}
		if n.Text != "" {
			return indentText(n.Text, indent)
		}
		return ""
	}
}

func (n *adfNode) renderBlocks(indent string) string {
	return renderBlocks(n.Content, indent, "\n\n")
}

func (n *adfNode) renderList(indent string, marker func(int) string) string {
	lines := make([]string, 0, len(n.Content))
	index := 0
	for _, item := range n.Content {
		if item.Type != "listItem" {
			if block := item.renderBlock(indent); block != "" {
				lines = append(lines, block)
			}
			continue
		}
		lines = append(lines, renderListItem(indent, marker(index), item))
		index++
	}
	return strings.Join(lines, "\n")
}

func renderListItem(indent, marker string, item *adfNode) string {
	inner := renderBlocks(item.Content, indent+"  ", "\n")
	if inner == "" {
		return indent + strings.TrimRight(marker, " ")
	}
	lines := strings.Split(inner, "\n")
	lines[0] = indent + marker + strings.TrimPrefix(lines[0], indent+"  ")
	return strings.Join(lines, "\n")
}

func renderBlocks(nodes []*adfNode, indent, sep string) string {
	parts := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if block := node.renderBlock(indent); block != "" {
			parts = append(parts, block)
		}
	}
	return strings.Join(parts, sep)
}

func (n *adfNode) renderTable(indent string) string {
	lines := make([]string, 0, len(n.Content))
	headerDone := false
	for _, row := range n.Content {
		if row.Type != "tableRow" {
			if block := row.renderBlock(indent); block != "" {
				lines = append(lines, block)
			}
			continue
		}
		cells := make([]string, 0, len(row.Content))
		for _, cell := range row.Content {
			text := cell.renderInline()
			cells = append(cells, strings.ReplaceAll(text, "|", "\\|"))
		}
		lines = append(lines, indent+"| "+strings.Join(cells, " | ")+" |")
		if !headerDone {
			lines = append(lines, indent+"|"+strings.Repeat(" --- |", len(cells)))
			headerDone = true
		}
	}
	return strings.Join(lines, "\n")
}

func (n *adfNode) renderInline() string {
	switch n.Type {
	case "text":
		return applyMarks(n.Text, n.Marks)
	case "hardBreak":
		return "\n"
	case "mention":
		if n.Attrs.Text != "" {
			return n.Attrs.Text
		}
		return "@" + n.Attrs.ID
	case "emoji":
		if n.Attrs.Text != "" {
			return n.Attrs.Text
		}
		return n.Attrs.ShortName
	case "inlineCard", "blockCard":
		return n.Attrs.URL
	case "rule":
		return "---"
	}
	inline := renderInlineNodes(n.Content)
	if inline == "" && n.Text != "" {
		return applyMarks(n.Text, n.Marks)
	}
	return inline
}

func renderInlineNodes(nodes []*adfNode) string {
	var out strings.Builder
	for _, node := range nodes {
		out.WriteString(node.renderInline())
	}
	return out.String()
}

func applyMarks(text string, marks []adfMark) string {
	if text == "" {
		return text
	}
	for _, kind := range []string{"code", "strike", "em", "strong", "link"} {
		for _, mark := range marks {
			if mark.Type != kind {
				continue
			}
			text = wrapMark(mark, text)
		}
	}
	return text
}

func wrapMark(mark adfMark, text string) string {
	switch mark.Type {
	case "strong":
		return "**" + text + "**"
	case "em":
		return "*" + text + "*"
	case "code":
		return "`" + text + "`"
	case "strike":
		return "~~" + text + "~~"
	case "link":
		if mark.Attrs.Href == "" {
			return text
		}
		return "[" + text + "](" + mark.Attrs.Href + ")"
	}
	return text
}

func (n *adfNode) plainText() string {
	var out strings.Builder
	if n.Text != "" {
		out.WriteString(n.Text)
	}
	for _, child := range n.Content {
		out.WriteString(child.plainText())
	}
	return out.String()
}

func indentText(text, indent string) string {
	if text == "" || indent == "" {
		return text
	}
	return prefixLines(text, indent)
}

func prefixLines(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[i] = ""
			continue
		}
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func prefixQuote(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[i] = ">"
			continue
		}
		lines[i] = "> " + line
	}
	return strings.Join(lines, "\n")
}
