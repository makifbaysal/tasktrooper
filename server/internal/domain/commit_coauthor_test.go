package domain

import "testing"

func TestNormalizeCommitCoauthor(t *testing.T) {
	ok := map[string]string{
		"":                            "",
		"  ":                          "",
		"Jane Doe <jane@example.com>": "Jane Doe <jane@example.com>",
		"Co-authored-by: Jane <jane@example.com>": "Jane <jane@example.com>",
		"co-authored-by:Jane  <jane@example.com>": "Jane <jane@example.com>",
	}
	for in, want := range ok {
		got, err := NormalizeCommitCoauthor(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{
		"Jane",
		"<jane@example.com>",
		"Jane <jane>",
		"Jane <jane@example.com>\nSigned-off-by: x <x@y.zz>",
		"Jane <jane@example.com>\rX",
		"Jane  <jane@example.com>",
		"Jane <a@b.cc> <c@d.ee>",
		"Jane <jane@example.com> trailing",
		"Jane\x00 <jane@example.com>",
	}
	for _, in := range bad {
		if _, err := NormalizeCommitCoauthor(in); err == nil {
			t.Errorf("%q: expected error", in)
		}
	}
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := NormalizeCommitCoauthor(string(long) + " <a@b.cc>"); err == nil {
		t.Error("expected length error")
	}
}
