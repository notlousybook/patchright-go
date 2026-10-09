package testmods

import (
	"regexp"
	"strings"
)

// This file ports the AST-driven portions of modify_tests.ts without ts-morph:
// a scanner finds `.evaluate(...)` / `.evaluateHandle(...)` / `.evaluateAll(...)`
// call sites and applies the same insertion/normalization rules, plus fixme()
// insertion into test blocks.

// fixtureServerRe mirrors the page-clock.spec.ts fixture regex:
// async ({...}) => { gains `server` when it mentions page but not server.
var fixtureServerRe = regexp.MustCompile(`async \(\{\s*([^}]*)\}\) => \{`)

// callSite is one parsed method call.
type callSite struct {
	start int // index of ".method"
	open  int // index of "("
	end   int // index of matching ")"
	args  []string
}

// findCallSites scans text for `.method(` call sites, skipping strings,
// templates, and comments. It returns sites in source order.
func findCallSites(text, method string) []callSite {
	var sites []callSite
	needle := "." + method + "("
	i := 0
	for i < len(text) {
		// Skip strings/templates/comments.
		c := text[i]
		if c == '\'' || c == '"' {
			j := skipQuoted(text, i)
			if j <= i {
				i++
				continue
			}
			i = j
			continue
		}
		if c == '`' {
			j := skipTemplateGo(text, i)
			if j <= i {
				i++
				continue
			}
			i = j
			continue
		}
		if c == '/' && i+1 < len(text) && (text[i+1] == '/' || text[i+1] == '*') {
			if text[i+1] == '/' {
				for i < len(text) && text[i] != '\n' {
					i++
				}
			} else {
				i += 2
				for i+1 < len(text) && !(text[i] == '*' && text[i+1] == '/') {
					i++
				}
				i += 2
			}
			continue
		}
		if strings.HasPrefix(text[i:], needle) {
			// Ensure the char before '.' is not part of an identifier.
			open := i + len(needle) - 1
			end, err := parenEndGo(text, open)
			if err != nil {
				i++
				continue
			}
			sites = append(sites, callSite{
				start: i,
				open:  open,
				end:   end,
				args:  splitArgs(text[open+1 : end]),
			})
			i = end + 1
			continue
		}
		i++
	}
	return sites
}

func skipQuoted(text string, start int) int {
	quote := text[start]
	i := start + 1
	for i < len(text) {
		if text[i] == '\\' {
			i += 2
			continue
		}
		if text[i] == quote {
			return i + 1
		}
		if text[i] == '\n' {
			return i
		}
		i++
	}
	return i
}

func skipTemplateGo(text string, start int) int {
	i := start + 1
	for i < len(text) {
		c := text[i]
		if c == '\\' {
			i += 2
			continue
		}
		if c == '`' {
			return i + 1
		}
		if c == '$' && i+1 < len(text) && text[i+1] == '{' {
			end, err := braceEndGo(text, i+1)
			if err != nil {
				return i + 2
			}
			i = end
			continue
		}
		i++
	}
	return i
}

func braceEndGo(text string, open int) (int, error) {
	depth := 0
	i := open
	for i < len(text) {
		c := text[i]
		if c == '\'' || c == '"' {
			j := skipQuoted(text, i)
			if j <= i {
				i++
				continue
			}
			i = j
			continue
		}
		if c == '`' {
			j := skipTemplateGo(text, i)
			if j <= i {
				i++
				continue
			}
			i = j
			continue
		}
		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return i + 1, nil
			}
		}
		i++
	}
	return 0, errUnbalanced
}

func parenEndGo(text string, open int) (int, error) {
	depth := 0
	i := open
	for i < len(text) {
		c := text[i]
		if c == '\'' || c == '"' {
			j := skipQuoted(text, i)
			if j <= i {
				i++
				continue
			}
			i = j
			continue
		}
		if c == '`' {
			j := skipTemplateGo(text, i)
			if j <= i {
				i++
				continue
			}
			i = j
			continue
		}
		if c == '/' && i+1 < len(text) && (text[i+1] == '/' || text[i+1] == '*') {
			if text[i+1] == '/' {
				for i < len(text) && text[i] != '\n' {
					i++
				}
			} else {
				i += 2
				for i+1 < len(text) && !(text[i] == '*' && text[i+1] == '/') {
					i++
				}
				i += 2
			}
			continue
		}
		if c == '(' {
			depth++
		} else if c == ')' {
			depth--
			if depth == 0 {
				return i, nil
			}
		}
		i++
	}
	return 0, errUnbalanced
}

// splitArgs splits a call argument list on top-level commas.
func splitArgs(s string) []string {
	var args []string
	depth := 0
	start := 0
	inStr := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr != 0 {
			if c == '\\' {
				i++
			} else if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			inStr = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	last := strings.TrimSpace(s[start:])
	if last != "" || len(args) > 0 {
		args = append(args, last)
	}
	return args
}

type unbalancedError struct{}

func (unbalancedError) Error() string { return "unbalanced" }

var errUnbalanced = unbalancedError{}

// isFunctionLike mirrors isFunctionLikeEvaluateExpressionArg: arrow functions,
// function expressions, async variants.
func isFunctionLike(arg string) bool {
	t := strings.TrimSpace(arg)
	if strings.HasPrefix(t, "async ") {
		t = strings.TrimSpace(strings.TrimPrefix(t, "async "))
	}
	if strings.HasPrefix(t, "function") {
		return true
	}
	// Arrow: contains => at depth 0 before any top-level delimiter.
	depth := 0
	for i := 0; i+1 < len(t); i++ {
		switch t[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '=', '>':
			_ = depth
		}
		if depth == 0 && t[i] == '=' && t[i+1] == '>' {
			return true
		}
	}
	return false
}

// isStringLike mirrors isStringLikeEvaluateExpressionArg.
func isStringLike(arg string) bool {
	t := strings.TrimSpace(arg)
	if len(t) < 2 {
		return false
	}
	if (t[0] == '\'' || t[0] == '"') && t[len(t)-1] == t[0] {
		return true
	}
	if t[0] == '`' && t[len(t)-1] == '`' && !strings.Contains(t, "${") {
		return true
	}
	if t[0] == '`' {
		return true
	}
	return false
}

// rewriteIsolatedContext applies insertIsolatedContextArgument /
// normalizeIsolatedContextArgument to one call site. hasOptions mirrors
// evaluateHasOptions (evaluateAll never takes options; Worker receivers and
// `worker.` receivers are excluded). It returns the replacement call text,
// insertion/normalization counts, and whether the call was unsafe-skipped.
func rewriteCall(text, method string, site callSite) (string, int, int, bool) {
	_ = text
	n := len(site.args)
	hasOptions := method != "evaluateAll"
	// Worker exclusion is receiver-based; approximate: check the receiver text
	// before ".method" on the same line for `worker` or Worker-typed names.
	// The TS version uses type info; without it we only skip `worker.` receivers.
	lineStart := strings.LastIndex(text[:site.start], "\n") + 1
	receiver := strings.TrimSpace(text[lineStart:site.start])
	_ = receiver
	if method != "evaluateAll" {
		// Heuristic: `worker.evaluate(` / `.worker.evaluate(` receivers are skipped.
		recv := receiver
		if idx := strings.LastIndex(recv, "."); idx >= 0 {
			recv = recv[:idx]
		}
		base := recv
		if idx := strings.LastIndexAny(base, " \t(=,!?:;{"); idx >= 0 {
			base = strings.TrimSpace(base[idx+1:])
		}
		if base == "worker" {
			return "", 0, 0, true
		}
	}
	_ = hasOptions
	need := 3
	if hasOptions {
		need = 4
	}
	// Normalization first (mirrors TS order): 3-arg object form ->
	// isolatedContext handling.
	if n >= 3 {
		third := strings.TrimSpace(site.args[2])
		if !hasOptions && strings.HasPrefix(third, "{") {
			propRe := regexp.MustCompile(`isolatedContext\s*:\s*false\b`)
			if propRe.MatchString(third) {
				newArgs := append([]string{site.args[0], site.args[1], "false"}, site.args[3:]...)
				return rebuildCall(site, newArgs, method), 0, 1, false
			}
			return "", 0, 0, false
		}
		if hasOptions {
			if third == "false" && n == 3 {
				newArgs := []string{site.args[0], site.args[1], "undefined", "false"}
				return rebuildCall(site, newArgs, method), 0, 1, false
			}
			if strings.HasPrefix(third, "{") {
				propRe := regexp.MustCompile(`(^|[,{]\s*)isolatedContext\s*:\s*false\b`)
				if propRe.MatchString(third) {
					// Single-prop object -> undefined; else drop the prop.
					inner := strings.TrimSpace(third[1 : len(third)-1])
					var opts string
					if regexp.MustCompile(`^isolatedContext\s*:\s*false$`).MatchString(inner) {
						opts = "undefined"
					} else {
						opts = propRe.ReplaceAllString(third, "$1")
						opts = strings.ReplaceAll(opts, ",,", ",")
						opts = strings.TrimSpace(opts)
					}
					newArgs := append([]string{site.args[0], site.args[1], opts, "false"}, site.args[3:]...)
					return rebuildCall(site, newArgs, method), 0, 1, false
				}
			}
		}
	}
	if n == 0 || n >= need {
		return "", 0, 0, true
	}
	for _, a := range site.args {
		if strings.Contains(strings.TrimSpace(a), "...") && strings.HasPrefix(strings.TrimSpace(a), "...") {
			return "", 0, 0, true
		}
	}
	first := ""
	if n > 0 {
		first = site.args[0]
	}
	if !isFunctionLike(first) && !isStringLike(first) {
		return "", 0, 0, true
	}
	// Insertion.
	var newArgs []string
	switch {
	case n == 1:
		newArgs = []string{site.args[0], "undefined"}
		if hasOptions {
			newArgs = append(newArgs, "undefined")
		}
		newArgs = append(newArgs, "false")
	case n == 2:
		newArgs = []string{site.args[0], site.args[1]}
		if hasOptions {
			newArgs = append(newArgs, "undefined")
		}
		newArgs = append(newArgs, "false")
	case n == 3 && hasOptions:
		newArgs = []string{site.args[0], site.args[1], site.args[2], "false"}
	default:
		return "", 0, 0, true
	}
	return rebuildCall(site, newArgs, method), 1, 0, false
}

func rebuildCall(site callSite, args []string, method string) string {
	_ = site
	_ = method
	// Callers splice: prefix (receiver + method + "(") + joined args.
	// The closing paren is supplied by the original text after site.end.
	return strings.Join(args, ", ")
}

// rewriteIsolatedContext scans the editor text for evaluate-family calls and
// rewrites them, returning insertions, normalizations, and unsafe-skip counts.
func rewriteIsolatedContext(e *editor) (ins, norm, skipped int) {
	// Collect sites for all methods first (positions shift as we edit, so
	// apply back-to-front).
	type siteRef struct {
		site   callSite
		method string
	}
	var refs []siteRef
	for _, m := range TargetMethods {
		for _, s := range findCallSites(e.text, m) {
			refs = append(refs, siteRef{s, m})
		}
	}
	// Sort back-to-front by open position.
	for i := 0; i < len(refs); i++ {
		for j := i + 1; j < len(refs); j++ {
			if refs[j].site.open > refs[i].site.open {
				refs[i], refs[j] = refs[j], refs[i]
			}
		}
	}
	for _, r := range refs {
		// Re-locate args in current text: positions may have shifted; rescan
		// the single call by re-finding from the (possibly stale) start.
		// Simpler: recompute end from open if still valid.
		if r.site.open >= len(e.text) {
			continue
		}
		end, err := parenEndGo(e.text, r.site.open)
		if err != nil {
			continue
		}
		site := callSite{start: r.site.start, open: r.site.open, end: end, args: splitArgs(e.text[r.site.open+1 : end])}
		prefix := e.text[site.start : site.open+1]
		newCall, i, nr, skip := rewriteCall(e.text, r.method, site)
		if skip {
			skipped++
			continue
		}
		if i == 0 && nr == 0 {
			continue
		}
		ins += i
		norm += nr
		e.text = e.text[:site.start] + prefix + newCall + e.text[site.end:]
	}
	return ins, norm, skipped
}

// insertFixmes adds `base.fixme(true, reason);` at the top of matching test
// blocks, mirroring insertConfiguredFixmes. Returns the insertion count.
func insertFixmes(e *editor, relativePath string) int {
	fileReason := FixmeTargetFiles[relativePath]
	reasons := FixmeTargets[relativePath]
	if fileReason == "" && len(reasons) == 0 {
		return 0
	}
	count := 0
	// Find test invocations: it/test/playwrightTest (optionally .only/.skip
	// chains are ignored — first-arg title match only).
	for _, base := range TestBaseNames {
		search := base + "("
		idx := 0
		for {
			i := strings.Index(e.text[idx:], search)
			if i < 0 {
				break
			}
			i += idx
			// Must be a call (not identifier suffix): preceding char check.
			if i > 0 && isIdentChar(e.text[i-1]) {
				idx = i + 1
				continue
			}
			open := i + len(search) - 1
			end, err := parenEndGo(e.text, open)
			if err != nil {
				idx = i + 1
				continue
			}
			args := splitArgs(e.text[open+1 : end])
			reason := fileReason
			if reason == "" {
				if len(args) == 0 {
					idx = i + 1
					continue
				}
				title := unquote(args[0])
				if title == "" {
					idx = i + 1
					continue
				}
				reason = reasons[title]
			}
			if reason == "" {
				idx = end
				continue
			}
			// Find the test function body: last function-like arg with a block.
			handled := false
			for k := len(args) - 1; k >= 0 && !handled; k-- {
				body := strings.TrimSpace(args[k])
				if strings.HasPrefix(body, "async ") {
					body = strings.TrimSpace(strings.TrimPrefix(body, "async "))
				}
				// Arrow with block: `... => {`.
				arrow := strings.Index(body, "=>")
				if arrow < 0 {
					continue
				}
				rest := strings.TrimSpace(body[arrow+2:])
				if !strings.HasPrefix(rest, "{") {
					continue
				}
				marker := base + ".fixme(true, " + quoteString(reason) + ");"
				if strings.Contains(body, marker) {
					handled = true
					continue
				}
				// Locate this arg's offset in the file to splice.
				argPos := strings.Index(e.text[open+1:end], args[k])
				if argPos < 0 {
					continue
				}
				braceRel := strings.Index(e.text[open+1+argPos:], "{")
				if braceRel < 0 {
					continue
				}
				braceAbs := open + 1 + argPos + braceRel
				e.text = e.text[:braceAbs+1] + "\n    " + marker + e.text[braceAbs+1:]
				count++
				// Continue after the inserted marker (positions shifted by its length).
				idx = braceAbs + len(marker) + 6
				handled = true
				break
			}
			if !handled {
				// No function body found: advance past this invocation.
				idx = end
			}
		}
	}
	return count
}

func isIdentChar(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func unquote(s string) string {
	t := strings.TrimSpace(s)
	if len(t) >= 2 && ((t[0] == '\'' && t[len(t)-1] == '\'') || (t[0] == '"' && t[len(t)-1] == '"')) {
		return t[1 : len(t)-1]
	}
	if len(t) >= 2 && t[0] == '`' && t[len(t)-1] == '`' && !strings.Contains(t, "${") {
		return t[1 : len(t)-1]
	}
	return ""
}

func quoteString(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString("\\\"")
		case '\\':
			sb.WriteString("\\\\")
		case '\n':
			sb.WriteString("\\n")
		default:
			sb.WriteRune(r)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}
