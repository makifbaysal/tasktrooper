---
key: cli.attached_images
version: 1
inputs: [Paths]
---
The user attached {{len .Paths}} image(s) to this message. They are saved as files — open each one with your file-reading tool and look at it before answering; what they show is part of the message:
{{range .Paths}}- {{.}}
{{end}}
