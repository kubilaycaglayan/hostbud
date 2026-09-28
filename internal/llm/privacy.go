package llm

import (
	"regexp"
	"strings"
)

var (
	ansiRE             = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))`)
	secretAssignmentRE = regexp.MustCompile(`(?i)\b[A-Za-z0-9_]*(?:KEY|TOKEN|SECRET|PASS|AUTH)[A-Za-z0-9_]*\s*[:=]\s*[^\s,;]+`)
	bearerRE           = regexp.MustCompile(`(?i)\bBearer\s+[^\s]+`)
	knownSecretRE      = regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{8,}|ghp_[A-Za-z0-9]{8,}|github_pat_[A-Za-z0-9_]{8,}|xox[A-Za-z0-9-]{8,}|AKIA[A-Z0-9]{12,})\b`)
	jwtRE              = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)
	longTokenRE        = regexp.MustCompile(`\b(?:[A-Fa-f0-9]{32,}|[A-Za-z0-9_+/-]{32,}={0,2})\b`)
	userinfoRE         = regexp.MustCompile(`(?i)(https?://)[^/@\s]+:[^/@\s]+@`)
	pemRE              = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]+-----.*?-----END [A-Z0-9 ]+-----`)
)

// PreparePane removes terminal controls, scrubs likely credentials, and caps
// the provider input to its last 8 KiB. The token argument is always removed.
func PreparePane(pane string, scrub bool, runToken string) string {
	pane = ansiRE.ReplaceAllString(pane, "")
	pane = strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' || r == 127 {
			return -1
		}
		return r
	}, pane)
	if runToken != "" {
		pane = strings.ReplaceAll(pane, runToken, "[redacted]")
	}
	if scrub {
		for _, re := range []*regexp.Regexp{secretAssignmentRE, bearerRE, knownSecretRE, jwtRE, userinfoRE, pemRE, longTokenRE} {
			pane = re.ReplaceAllString(pane, "[redacted]")
		}
	}
	if len(pane) > 8<<10 {
		pane = pane[len(pane)-(8<<10):]
	}
	return pane
}
