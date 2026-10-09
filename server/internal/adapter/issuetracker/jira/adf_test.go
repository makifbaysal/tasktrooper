package jira

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestADFToMarkdown(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "doc",
		"version": 1,
		"content": [
			{"type": "heading", "attrs": {"level": 2}, "content": [{"type": "text", "text": "Plan"}]},
			{"type": "paragraph", "content": [
				{"type": "text", "text": "See "},
				{"type": "text", "text": "guide", "marks": [{"type": "strong"}]},
				{"type": "text", "text": " or "},
				{"type": "text", "text": "handbook", "marks": [{"type": "link", "attrs": {"href": "https://acme.atlassian.net/wiki/x"}}]},
				{"type": "text", "text": " now"}
			]},
			{"type": "bulletList", "content": [
				{"type": "listItem", "content": [
					{"type": "paragraph", "content": [{"type": "text", "text": "top"}]}
				]},
				{"type": "listItem", "content": [
					{"type": "paragraph", "content": [{"type": "text", "text": "nested parent"}]},
					{"type": "bulletList", "content": [
						{"type": "listItem", "content": [
							{"type": "paragraph", "content": [{"type": "text", "text": "deep"}]}
						]}
					]}
				]}
			]},
			{"type": "orderedList", "content": [
				{"type": "listItem", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "first"}]}]},
				{"type": "listItem", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "second"}]}]}
			]},
			{"type": "codeBlock", "attrs": {"language": "go"}, "content": [
				{"type": "text", "text": "fmt.Println(1)"}
			]},
			{"type": "paragraph", "content": [
				{"type": "mention", "attrs": {"id": "5f0"}},
				{"type": "text", "text": " owns it"}
			]}
		]
	}`)

	want := "## Plan\n\n" +
		"See **guide** or [handbook](https://acme.atlassian.net/wiki/x) now\n\n" +
		"- top\n- nested parent\n  - deep\n\n" +
		"1. first\n2. second\n\n" +
		"```go\nfmt.Println(1)\n```\n\n" +
		"@5f0 owns it"

	assert.Equal(t, want, ADFToMarkdown(raw))
}

func TestADFToMarkdownNodes(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{name: "empty", doc: ``, want: ""},
		{name: "null", doc: `null`, want: ""},
		{name: "invalid json", doc: `{"type":`, want: ""},
		{name: "wrong shape", doc: `["not","a","doc"]`, want: ""},
		{name: "empty doc", doc: `{"type":"doc","content":[]}`, want: ""},
		{
			name: "heading levels clamp",
			doc:  `{"type":"doc","content":[{"type":"heading","attrs":{"level":9},"content":[{"type":"text","text":"deep"}]},{"type":"heading","attrs":{"level":0},"content":[{"type":"text","text":"shallow"}]}]}`,
			want: "###### deep\n\n# shallow",
		},
		{
			name: "hard break keeps the paragraph together",
			doc:  `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"a"},{"type":"hardBreak"},{"type":"text","text":"b"}]}]}`,
			want: "a\nb",
		},
		{
			name: "code wins over the marks around it",
			doc:  `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"em"},{"type":"code"}]}]}]}`,
			want: "*`x`*",
		},
		{
			name: "every mark",
			doc:  `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"a","marks":[{"type":"strong"}]},{"type":"text","text":"b","marks":[{"type":"em"}]},{"type":"text","text":"c","marks":[{"type":"code"}]},{"type":"text","text":"d","marks":[{"type":"strike"}]},{"type":"text","text":"e","marks":[{"type":"link","attrs":{"href":"https://x.test"}}]},{"type":"text","text":"f","marks":[{"type":"underline"}]},{"type":"text","text":"g","marks":[{"type":"link"}]}]}]}`,
			want: "**a***b*`c`~~d~~[e](https://x.test)fg",
		},
		{
			name: "blockquote",
			doc:  `{"type":"doc","content":[{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"warned"}]},{"type":"paragraph","content":[{"type":"text","text":"twice"}]}]}]}`,
			want: "> warned\n>\n> twice",
		},
		{
			name: "rule",
			doc:  `{"type":"doc","content":[{"type":"rule"},{"type":"paragraph","content":[{"type":"text","text":"after"}]}]}`,
			want: "---\n\nafter",
		},
		{
			name: "code block without a language",
			doc:  `{"type":"doc","content":[{"type":"codeBlock","content":[{"type":"text","text":"plain\nlines"}]}]}`,
			want: "```\nplain\nlines\n```",
		},
		{
			name: "table",
			doc: `{"type":"doc","content":[{"type":"table","content":[
				{"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Key"}]}]},{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Note"}]}]}]},
				{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"PROJ-1"}]}]},{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"a|b"}]}]}]}
			]}]}`,
			want: "| Key | Note |\n| --- | --- |\n| PROJ-1 | a\\|b |",
		},
		{
			name: "mention falls back to the id",
			doc:  `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"7"}}]}]}`,
			want: "@7",
		},
		{
			name: "mention prefers its text",
			doc:  `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"7","text":"Dev"}}]}]}`,
			want: "Dev",
		},
		{
			name: "emoji prefers its text, then its short name",
			doc:  `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"emoji","attrs":{"shortName":":smile:","text":"😄"}},{"type":"emoji","attrs":{"shortName":":wave:"}}]}]}`,
			want: "😄:wave:",
		},
		{
			name: "inline and block cards render their url",
			doc:  `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"inlineCard","attrs":{"url":"https://x.test/a"}}]},{"type":"blockCard","attrs":{"url":"https://x.test/b"}}]}`,
			want: "https://x.test/a\n\nhttps://x.test/b",
		},
		{
			name: "panel and expand render their children",
			doc: `{"type":"doc","content":[
				{"type":"panel","attrs":{"panelType":"info"},"content":[{"type":"paragraph","content":[{"type":"text","text":"inside"}]}]},
				{"type":"expand","content":[{"type":"paragraph","content":[{"type":"text","text":"hidden"}]}]}
			]}`,
			want: "inside\n\nhidden",
		},
		{
			name: "mediaSingle renders its children, bare media nothing",
			doc:  `{"type":"doc","content":[{"type":"mediaSingle","content":[{"type":"paragraph","content":[{"type":"text","text":"caption"}]}]},{"type":"media","attrs":{"type":"file"}}]}`,
			want: "caption",
		},
		{
			name: "unknown node renders its children",
			doc:  `{"type":"doc","content":[{"type":"taskList","content":[{"type":"taskItem","content":[{"type":"text","text":"check"}]}]}]}`,
			want: "check",
		},
		{
			name: "unknown leaf renders its text",
			doc:  `{"type":"doc","content":[{"type":"somethingNew","text":"payload"}]}`,
			want: "payload",
		},
		{
			name: "unknown empty node renders nothing",
			doc:  `{"type":"doc","content":[{"type":"somethingNew","content":[{"type":"text","text":"kept"}]},{"type":"nothing"}]}`,
			want: "kept",
		},
		{
			name: "output is trimmed",
			doc:  `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"  padded  "}]},{"type":"paragraph","content":[{"type":"text","text":"tail  "}]}]}`,
			want: "padded  \n\ntail",
		},
		{
			name: "empty paragraphs vanish",
			doc:  `{"type":"doc","content":[{"type":"paragraph","content":[]},{"type":"paragraph","content":[{"type":"text","text":"only"}]}]}`,
			want: "only",
		},
		{
			name: "bare doc node",
			doc:  `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"root"}]}]}`,
			want: "root",
		},
		{
			name: "single paragraph document",
			doc:  `{"type":"paragraph","content":[{"type":"text","text":"no doc wrapper"}]}`,
			want: "no doc wrapper",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ADFToMarkdown(json.RawMessage(tt.doc)))
		})
	}
}
