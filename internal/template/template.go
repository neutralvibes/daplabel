// Package template implements label template management and variable
// expansion (SPECIFICATION.md §8.3, §2.7). Informed by
// src/commands/template.sh, but designed fresh in Go (DECISIONS.md
// Decision 34).
//
// A template is a plain text file of KEY=VALUE lines, read the same way
// internal/labelfile reads an external label file (§2.7: "a reusable
// label file that may include substitution variables"), stored in the
// configured template directory (DAPLABEL_TEMPLATE_DIR, §5.4). What
// distinguishes a template from an ordinary label file is only that its
// values may contain the three substitution variables Expand recognises
// — nothing at the file-format level differs.
package template

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Label is an ordered key/value pair as read from a template line
// (`KEY=VALUE`), before variable expansion.
type Label struct {
	Key   string
	Value string
}

// Load reads the KEY=VALUE lines of the template named name inside dir
// (DAPLABEL_TEMPLATE_DIR). Values are returned exactly as written, with
// no variable expansion performed yet — call Expand on each value
// separately once the target service/application's Vars are known,
// since the same loaded template may be applied to several services in
// one `template apply` run, each needing different substitutions.
func Load(dir, name string) ([]Label, error) {
	path := filepath.Join(dir, name)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("template %q not found in %s", name, dir)
		}
		return nil, fmt.Errorf("opening template %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var out []Label
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out = append(out, Label{Key: key, Value: value})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading template %s: %w", path, err)
	}
	return out, nil
}

// Vars holds the substitution values for SPECIFICATION.md §8.3's three
// supported template variables.
type Vars struct {
	ServiceName string // $SERVICE_NAME
	AppDir      string // $APP_DIR
	AppName     string // $APP_NAME
}

// names is checked in Expand against $-prefixed text at the current
// scan position; iteration order doesn't matter since none of these
// three names is a prefix of another.
func (v Vars) names() map[string]string {
	return map[string]string{
		"$SERVICE_NAME": v.ServiceName,
		"$APP_DIR":      v.AppDir,
		"$APP_NAME":     v.AppName,
	}
}

// Expand performs SPECIFICATION.md §8.3 / DECISIONS.md Decision 17's
// variable substitution on value:
//
//   - an exact, whole-token match of $SERVICE_NAME, $APP_DIR, or
//     $APP_NAME is replaced with the corresponding Vars field. "Whole
//     token" means the character immediately following the matched name
//     must not itself be a valid identifier character — so
//     $SERVICE_NAME_EXTRA is left completely unchanged, not partially
//     substituted (§8.3: "partial or prefix matches shall not be
//     substituted").
//   - a literal `$$` produces a single literal `$`, escaping what would
//     otherwise be a substitution (Decision 17) — this is checked before
//     the variable-name check, so `$$SERVICE_NAME` yields the literal
//     text `$SERVICE_NAME`, not a substituted value.
//   - any other `$` (unrecognised variable, or one not immediately
//     followed by a recognised name) is left exactly as written (§8.3:
//     "unrecognised variables shall remain unchanged").
func Expand(value string, vars Vars) string {
	names := vars.names()

	var b strings.Builder
	for i := 0; i < len(value); {
		if value[i] != '$' {
			b.WriteByte(value[i])
			i++
			continue
		}
		if i+1 < len(value) && value[i+1] == '$' {
			b.WriteByte('$')
			i += 2
			continue
		}

		matched := false
		for name, sub := range names {
			if !strings.HasPrefix(value[i:], name) {
				continue
			}
			end := i + len(name)
			if end < len(value) && isIdentByte(value[end]) {
				continue // e.g. $SERVICE_NAME_EXTRA — not a whole-token match
			}
			b.WriteString(sub)
			i = end
			matched = true
			break
		}
		if matched {
			continue
		}
		b.WriteByte('$')
		i++
	}
	return b.String()
}

func isIdentByte(c byte) bool {
	return c == '_' ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}
