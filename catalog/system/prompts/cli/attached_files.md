---
key: cli.attached_files
version: 1
inputs: [Paths]
---
The user attached {{len .Paths}} file(s) to this message. They are saved as files — open each one with your file-reading tool to see its content before answering:
{{range .Paths}}- {{.}}
{{end}}
