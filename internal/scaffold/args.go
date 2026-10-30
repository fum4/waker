package scaffold

import (
	"errors"
	"strings"
)

// SplitArgs splits a command line the way a POSIX shell splits words:
// blanks separate, '…' is literal, "…" honours \" \\ \$ \`, and a bare
// backslash escapes the next character. There is no expansion: start is
// an argv run without a shell.
func SplitArgs(s string) ([]string, error) {
	var (
		args  []string
		cur   strings.Builder
		inArg bool
		quote rune
	)
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			switch {
			case r == '"':
				quote = 0
			case r == '\\' && i+1 < len(rs) && strings.ContainsRune("\"\\$`", rs[i+1]):
				i++
				cur.WriteRune(rs[i])
			default:
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inArg = r, true
		case r == '\\':
			if i+1 == len(rs) {
				return nil, errors.New("a trailing backslash")
			}
			i++
			cur.WriteRune(rs[i])
			inArg = true
		case r == ' ' || r == '\t' || r == '\n':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, errors.New("an unclosed " + string(quote) + " quote")
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}
