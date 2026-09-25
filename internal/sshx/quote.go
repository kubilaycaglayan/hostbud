package sshx

import "strings"

// Quote single-quotes s for a POSIX shell, so the remote shell passes it
// through verbatim (spaces, $, quotes, newlines and all).
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Command joins args into a remote command line, quoting every argument.
// It is the only way remote command lines are built.
func Command(args ...string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = Quote(a)
	}
	return strings.Join(q, " ")
}
