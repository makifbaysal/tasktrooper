package core

import (
	"runtime"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
)

type promptInFileInput struct {
	Path  string
	Chars int
}

var promptInFileKey = prompt.Define("cli.prompt_in_file", promptInFileInput{Path: `C:\Users\me\AppData\Local\Temp\tt-cli-prompt-1.md`, Chars: 48210})

// maxPromptArg is how long a prompt passed as one argv element may be.
// Windows caps the whole command line at 32,767 characters; Linux caps one
// argument at 128 KiB (MAX_ARG_STRLEN); macOS caps argv plus environment at
// 1 MiB. A flattened history crosses the Windows limit on a long task, and
// the run then fails before the CLI starts.
func maxPromptArg(goos string) int {
	switch goos {
	case "windows":
		return 30_000
	case "darwin":
		return 900 * 1024
	default:
		return 120 * 1024
	}
}

// ArgvSafePrompt returns text unchanged when it fits on this OS's command
// line; otherwise it writes text to a 0600 temp file and returns a short
// prompt pointing the CLI at it, plus the cleanup for that file.
func ArgvSafePrompt(text string) (string, func(), error) {
	return argvSafePrompt(runtime.GOOS, text)
}

func argvSafePrompt(goos, text string) (string, func(), error) {
	if len(text) <= maxPromptArg(goos) {
		return text, func() {}, nil
	}
	path, cleanup, err := WriteSystemPromptFile(text)
	if err != nil {
		return "", func() {}, err
	}
	return promptInFileKey.Render(promptInFileInput{Path: path, Chars: len([]rune(text))}), cleanup, nil
}
