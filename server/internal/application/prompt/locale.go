package prompt

import "strings"

// Keyed by the UI's language codes (desktop/ui/src/lib/languages.ts), plus the
// region tags a hand-edited setting may carry. The name is what the language
// instruction tells the model to answer in.
var localeDisplayNames = map[string]string{
	"":        "English",
	"en":      "English",
	"tr":      "Turkish",
	"es":      "Spanish",
	"de":      "German",
	"fr":      "French",
	"pt":      "Brazilian Portuguese",
	"pt-br":   "Brazilian Portuguese",
	"zh":      "Simplified Chinese",
	"zh-cn":   "Simplified Chinese",
	"zh-hans": "Simplified Chinese",
}

func LocaleDisplayName(lang string) string {
	key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(lang), "_", "-"))
	if name, ok := localeDisplayNames[key]; ok {
		return name
	}
	return lang
}
