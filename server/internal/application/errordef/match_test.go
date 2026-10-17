package errordef

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func group(fingerprint, message, sample string) domain.OpsErrorGroup {
	return domain.OpsErrorGroup{
		Provider:          domain.CloudGCP,
		RuntimeErrorGroup: domain.RuntimeErrorGroup{Fingerprint: fingerprint, Message: message, Sample: sample, Source: "api-7f9"},
	}
}

func TestMatchesEachFieldAndTheirConjunction(t *testing.T) {
	timeout := group("fp-timeout", "POST /v1/charges 504 upstream timeout tenant=acme", "")
	npe := group("fp-npe", "java.lang.NullPointerException: user was null", "at com.acme.Users.get(Users.java:42)")
	py := group("fp-py", "worker crashed", "Traceback (most recent call last):\n  File \"x.py\"\nValueError: bad literal")
	tests := []struct {
		name  string
		match domain.ErrorMatch
		group domain.OpsErrorGroup
		want  bool
	}{
		{name: "empty match never matches", match: domain.ErrorMatch{}, group: timeout, want: false},
		{name: "fingerprint equal", match: domain.ErrorMatch{Fingerprint: "fp-timeout"}, group: timeout, want: true},
		{name: "fingerprint other", match: domain.ErrorMatch{Fingerprint: "fp-other"}, group: timeout, want: false},
		{name: "regex on message", match: domain.ErrorMatch{MessageRegex: `upstream timeout`}, group: timeout, want: true},
		{name: "regex on sample", match: domain.ErrorMatch{MessageRegex: `Users\.java`}, group: npe, want: true},
		{name: "regex miss", match: domain.ErrorMatch{MessageRegex: `^deadlock`}, group: timeout, want: false},
		{name: "regex that does not compile matches nothing", match: domain.ErrorMatch{MessageRegex: `(`}, group: timeout, want: false},
		{name: "exception qualified", match: domain.ErrorMatch{ExceptionType: "java.lang.NullPointerException"}, group: npe, want: true},
		{name: "exception bare name", match: domain.ErrorMatch{ExceptionType: "NullPointerException"}, group: npe, want: true},
		{name: "exception from a traceback's last line", match: domain.ErrorMatch{ExceptionType: "ValueError"}, group: py, want: true},
		{name: "exception partial name is not a match", match: domain.ErrorMatch{ExceptionType: "PointerException"}, group: npe, want: false},
		{name: "endpoint path", match: domain.ErrorMatch{Endpoint: "/v1/charges"}, group: timeout, want: true},
		{name: "endpoint with method", match: domain.ErrorMatch{Endpoint: "POST /v1/charges"}, group: timeout, want: true},
		{name: "endpoint wrong method", match: domain.ErrorMatch{Endpoint: "GET /v1/charges"}, group: timeout, want: false},
		{name: "endpoint prefix", match: domain.ErrorMatch{Endpoint: "/v1/*"}, group: timeout, want: true},
		{name: "endpoint segment wildcard", match: domain.ErrorMatch{Endpoint: "/:version/charges"}, group: timeout, want: true},
		{name: "endpoint other", match: domain.ErrorMatch{Endpoint: "/v1/refunds"}, group: timeout, want: false},
		{name: "status after the path", match: domain.ErrorMatch{Status: 504}, group: timeout, want: true},
		{name: "status other", match: domain.ErrorMatch{Status: 500}, group: timeout, want: false},
		{name: "attribute from the message", match: domain.ErrorMatch{Attributes: map[string]string{"tenant": "acme"}}, group: timeout, want: true},
		{name: "attribute source and provider", match: domain.ErrorMatch{Attributes: map[string]string{"source": "api-7f9", "provider": "gcp"}}, group: timeout, want: true},
		{name: "attribute missing", match: domain.ErrorMatch{Attributes: map[string]string{"region": "eu"}}, group: timeout, want: false},
		{name: "all set and all hold", match: domain.ErrorMatch{Fingerprint: "fp-timeout", MessageRegex: "timeout", Endpoint: "/v1/charges", Status: 504}, group: timeout, want: true},
		{name: "all set and one fails", match: domain.ErrorMatch{Fingerprint: "fp-timeout", MessageRegex: "timeout", Status: 500}, group: timeout, want: false},
	}
	cache := newRegexCache()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cache.matches(tt.match, factsOf(tt.group)))
		})
	}
}

func TestStatusIsReadFromCommonPrintings(t *testing.T) {
	tests := []struct {
		text string
		want int
	}{
		{text: "request failed with status code 502", want: 502},
		{text: "HTTP 429 too many requests", want: 429},
		{text: "status=404 path=/x", want: 404},
		{text: "GET /health 503 1ms", want: 503},
		{text: "retrying in 500ms", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			assert.Equal(t, tt.want, statusIn(tt.text))
		})
	}
}

func TestCompileBoundedRefusesLongAndHugePatterns(t *testing.T) {
	_, err := compileBounded(strings.Repeat("a", maxRegexLen+1))
	require.ErrorIs(t, err, errRegexTooLong)
	_, err = compileBounded(`(?:ab|cd|ef|gh){1000}`)
	require.ErrorIs(t, err, errRegexTooLarge)
	_, err = compileBounded(`(a{100}){100}`)
	require.Error(t, err)
	_, err = compileBounded(`timeout after \d+ms`)
	require.NoError(t, err)
	_, err = compileBounded(`x{300}y{300}z{300}w{300}`)
	require.ErrorIs(t, err, errRegexTooLarge, "a program past a thousand instructions is refused")
}

func TestMatchingReadsOnlyTheHeadOfAHugeSample(t *testing.T) {
	g := group("fp", "boom", strings.Repeat("x", maxMatchText)+"NEEDLE")
	assert.False(t, newRegexCache().matches(domain.ErrorMatch{MessageRegex: "NEEDLE"}, factsOf(g)))
}
