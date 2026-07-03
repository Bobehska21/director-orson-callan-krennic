// Package redact keeps secrets out of both the shadow snapshot and the AI
// event. It matches paths against deny globs (applied BEFORE staging and
// BEFORE diffing) and can mask secret-looking tokens on surviving lines.
package redact

import (
	"regexp"
	"strings"
)

// Redactor decides which paths are excluded and masks secret tokens.
type Redactor struct {
	deny      []string
	scanRegex bool
	secretRE  []*regexp.Regexp
}

// New builds a Redactor from deny globs and a scan toggle.
func New(deny []string, scanRegex bool) *Redactor {
	return &Redactor{
		deny:      deny,
		scanRegex: scanRegex,
		secretRE:  defaultSecretPatterns(),
	}
}

// DenyPathspecs returns git pathspec exclusions (":!pattern") for the deny list,
// so redacted files are never staged into the temp index nor diffed.
func (r *Redactor) DenyPathspecs() []string {
	out := make([]string, 0, len(r.deny))
	for _, p := range r.deny {
		out = append(out, ":(glob)!"+p)
	}
	return out
}

// IsDenied reports whether a repo-relative path matches any deny glob.
func (r *Redactor) IsDenied(relPath string) bool {
	rel := filepathSlash(relPath)
	base := rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		base = rel[i+1:]
	}
	for _, pat := range r.deny {
		if strings.Contains(pat, "/") {
			if globMatch(pat, rel) {
				return true
			}
		} else if globMatch(pat, base) {
			return true
		}
	}
	return false
}

// MaskLine masks any secret-looking token in a single line if scanning is on.
// Returns the (possibly) masked line and whether anything was masked.
func (r *Redactor) MaskLine(line string) (string, bool) {
	if !r.scanRegex {
		return line, false
	}
	masked := false
	for _, re := range r.secretRE {
		if re.MatchString(line) {
			line = re.ReplaceAllString(line, "[REDACTED]")
			masked = true
		}
	}
	return line, masked
}

func defaultSecretPatterns() []*regexp.Regexp {
	pats := []string{
		`AKIA[0-9A-Z]{16}`,                          // AWS access key id
		`(?i)aws_secret_access_key\s*[:=]\s*\S+`,    // aws secret
		`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`, // JWT
		`-----BEGIN [A-Z ]*PRIVATE KEY-----`,        // PEM private key header
		`(?i)(api[_-]?key|secret|token|password)\s*[:=]\s*['"][^'"]{6,}['"]`, // key=value
		`ghp_[A-Za-z0-9]{36}`,                       // GitHub PAT
		`xox[baprs]-[A-Za-z0-9-]{10,}`,              // Slack token
	}
	out := make([]*regexp.Regexp, 0, len(pats))
	for _, p := range pats {
		out = append(out, regexp.MustCompile(p))
	}
	return out
}

func filepathSlash(p string) string { return strings.ReplaceAll(p, "\\", "/") }

// globMatch supports `*`, `?`, `**` (across path separators) and literals.
func globMatch(pattern, name string) bool {
	re := "^" + globToRegexp(pattern) + "$"
	m, err := regexp.MatchString(re, name)
	return err == nil && m
}

func globToRegexp(glob string) string {
	var b strings.Builder
	runes := []rune(glob)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch c {
		case '*':
			if i+1 < len(runes) && runes[i+1] == '*' {
				b.WriteString(".*") // ** matches across separators
				i++
				// swallow a following slash so a/**/b matches a/b
				if i+1 < len(runes) && runes[i+1] == '/' {
					b.WriteString("/?")
					i++
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteRune(c)
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}
