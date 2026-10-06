package prompt_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
)

func TestLocaleDisplayName(t *testing.T) {
	cases := map[string]string{
		"":      "English",
		"en":    "English",
		"tr":    "Turkish",
		"es":    "Spanish",
		"de":    "German",
		"fr":    "French",
		"pt":    "Brazilian Portuguese",
		"pt-BR": "Brazilian Portuguese",
		"zh":    "Simplified Chinese",
		"zh_CN": "Simplified Chinese",
		"ja":    "ja",
	}
	for code, want := range cases {
		if got := prompt.LocaleDisplayName(code); got != want {
			t.Errorf("LocaleDisplayName(%q) = %q, want %q", code, got, want)
		}
	}
}
