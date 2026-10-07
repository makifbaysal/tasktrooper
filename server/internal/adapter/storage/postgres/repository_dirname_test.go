package postgres

import "testing"

// A row can be written by a host of either OS, and on Unix filepath.Base of a
// Windows path is the whole string.
func TestRepoDirName_SplitsOnBothSeparators(t *testing.T) {
	cases := map[string]string{
		`C:\Users\me\code\acme-web`:  "acme-web",
		`C:\Users\me\code\acme-web\`: "acme-web",
		`\\srv\share\acme-web`:       "acme-web",
		`C:\`:                        "",
	}
	for in, want := range cases {
		if got := repoDirName(in); got != want {
			t.Fatalf("repoDirName(%q) = %q, want %q", in, got, want)
		}
	}
}
