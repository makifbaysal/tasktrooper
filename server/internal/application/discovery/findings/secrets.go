package findings

import (
	"regexp"
	"strings"
	"unicode"
)

// Evidence for a secret names what kind of secret sits on the line, never
// the value: findings are stored, shown and copied into tasks.

type secretPattern struct {
	hint  string
	re    *regexp.Regexp
	label string
}

var secretPatterns = []secretPattern{
	{"AKIA", re(`\bAKIA[0-9A-Z]{16}\b`), "AWS access key id"},
	{"PRIVATE KEY", re(`-----BEGIN (RSA |EC |DSA |OPENSSH |PGP |ENCRYPTED )?PRIVATE KEY`), "private key"},
	{"gh", re(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`), "GitHub token"},
	{"github_pat_", re(`\bgithub_pat_[A-Za-z0-9_]{40,}`), "GitHub token"},
	{"xox", re(`\bxox[abprs]-[A-Za-z0-9-]{10,}`), "Slack token"},
	{"_live_", re(`\b(sk|rk)_live_[0-9A-Za-z]{20,}`), "Stripe live key"},
	{"sk-ant-", re(`\bsk-ant-[A-Za-z0-9_-]{20,}`), "Anthropic API key"},
	{"sk-", re(`\bsk-(proj-)?[A-Za-z0-9_-]{20,}T3BlbkFJ|\bsk-proj-[A-Za-z0-9_-]{40,}`), "OpenAI API key"},
	{"SG.", re(`\bSG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}\b`), "SendGrid API key"},
	{"glpat-", re(`\bglpat-[A-Za-z0-9_-]{20,}`), "GitLab token"},
	{"npm_", re(`\bnpm_[A-Za-z0-9]{36}\b`), "npm token"},
	{"AccountKey=", re(`\bAccountKey=[A-Za-z0-9+/=]{40,}`), "Azure storage key"},
}

var (
	placeholderRe = re(`(?i)(example|sample|placeholder|dummy|changeme|change_me|change-me|your[_-]|<|>|\$\{|\{\{|%s|xxxx|\*\*\*|redacted|todo|fake|mock|test|lorem|secret_here|insert)`)
	credentialRe  = re(`(?i)\b([a-z0-9_]*(password|passwd|pwd|secret|api_?key|apikey|access_?key|access_?token|auth_?token|client_?secret|private_?key|signing_?key)[a-z0-9_]*)["']?\s*(:=|=|:)\s*["']([^"'\s]{10,})["']`)
	urlCredRe     = re(`\b[a-z][a-z0-9+.-]*://[^/\s:@'"]+:([^/\s:@'"$<{]{6,})@([^/\s'"?#:]+)`)
	urlCredHostOK = re(`(?i)^(localhost|127\.0\.0\.1|0\.0\.0\.0|host\.docker\.internal|example\.\w+|postgres|mysql|mariadb|db|database|redis|mongo|mongodb|rabbitmq|kafka|minio)$`)
	weakPasswords = map[string]bool{
		"password": true, "postgres": true, "secret": true, "changeme": true, "admin": true,
		"root": true, "guest": true, "example": true, "pass": true, "mysql": true,
	}
	// envPlaceholder is narrower than placeholderRe: an env file's sk_test_…
	// key is still a live credential for the test account.
	envPlaceholder = re(`(?i)(example|sample|placeholder|dummy|changeme|change_me|change-me|your[_-]|<|>|\$\{|\{\{|xxxx|\*\*\*|redacted|secret_here|insert)`)
	envSecretRe    = re(`^\s*(export\s+)?([A-Za-z0-9_]*(SECRET|TOKEN|PASSWORD|PASSWD|API_KEY|APIKEY|PRIVATE_KEY|ACCESS_KEY|CLIENT_SECRET|DATABASE_URL|DB_URL|DSN|CONNECTION_STRING|CREDENTIAL)[A-Za-z0-9_]*)\s*=\s*["']?([^"'\s#]{6,})`)
	envExample     = re(`(?i)\.(example|sample|template|dist|defaults?|schema|tpl|ci|test)$`)
	dottedKeyRe    = re(`^[a-zA-Z_]+(\.[\w-]+)+$`)
	envNameRe      = re(`^[A-Z0-9_]+$`)
)

var keyMaterial = re(`[A-Za-z0-9+/=]{40,}`)

// secretOnLine reports the kind of known-format secret on the line. A
// private key header only counts with key material after it, on the same
// line (an escaped PEM string) or the next: a placeholder that shows the
// header alone is not a key.
func secretOnLine(line, next string) (string, bool) {
	for _, p := range secretPatterns {
		if !strings.Contains(line, p.hint) {
			continue
		}
		loc := p.re.FindStringIndex(line)
		if loc == nil || strings.Contains(strings.ToUpper(line[loc[0]:loc[1]]), "EXAMPLE") {
			continue
		}
		if p.label == "private key" && !keyMaterial.MatchString(line[loc[1]:]) && !keyMaterial.MatchString(next) {
			continue
		}
		return p.label, true
	}
	return "", false
}

// credentialOnLine reports the key a literal credential is assigned to, or
// "credentials in URL" for user:password@host.
func credentialOnLine(line string) (string, bool) {
	lower := strings.ToLower(line)
	if !strings.Contains(lower, "://") && !strings.Contains(lower, "pass") && !strings.Contains(lower, "pwd") &&
		!strings.Contains(lower, "secret") && !strings.Contains(lower, "key") && !strings.Contains(lower, "token") {
		return "", false
	}
	if m := credentialRe.FindStringSubmatch(line); m != nil && looksSecret(m[4]) {
		return m[1], true
	}
	if m := urlCredRe.FindStringSubmatch(line); m != nil {
		pass, host := m[1], m[2]
		if !urlCredHostOK.MatchString(host) && !weakPasswords[strings.ToLower(pass)] && !placeholderRe.MatchString(pass) {
			return "credentials in URL", true
		}
	}
	return "", false
}

// looksSecret wants a value that reads like a key rather than a word, a
// translation key or another variable's name.
func looksSecret(v string) bool {
	if placeholderRe.MatchString(v) || dottedKeyRe.MatchString(v) || envNameRe.MatchString(v) {
		return false
	}
	var letters, digits int
	distinct := map[rune]bool{}
	for _, r := range v {
		distinct[r] = true
		switch {
		case unicode.IsLetter(r):
			letters++
		case unicode.IsDigit(r):
			digits++
		}
	}
	return letters > 0 && digits > 0 && len(distinct) >= 6
}

// envSecret reports the variable name of a real-looking secret in an env file.
func envSecret(line string) (string, bool) {
	m := envSecretRe.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	value := m[4]
	if envPlaceholder.MatchString(value) || strings.HasPrefix(value, "$") {
		return "", false
	}
	if strings.Contains(value, "://") {
		if u := urlCredRe.FindStringSubmatch(value); u == nil || urlCredHostOK.MatchString(u[2]) || weakPasswords[strings.ToLower(u[1])] {
			return "", false
		}
	}
	return m[2], true
}

func exampleEnvFile(base string) bool { return envExample.MatchString(base) }
