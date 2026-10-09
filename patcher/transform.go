package patcher

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// FileSet is an in-memory working copy of the file tree being patched,
// mirroring ts-morph's Project (addSourceFileAtPath / save).
type FileSet struct {
	Root  string
	files map[string]string
	dirty map[string]bool
}

// Open loads the checkout at root into memory.
func Open(root string) (*FileSet, error) {
	fs := &FileSet{Root: root, files: map[string]string{}, dirty: map[string]bool{}}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		switch filepath.Ext(path) {
		case ".ts", ".js", ".tsx", ".yml", ".yaml":
		default:
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fs.files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return fs, nil
}

// Get returns the current content of rel, erroring when the file is unknown
// (mirrors addSourceFileAtPath on a missing file).
func (fs *FileSet) Get(rel string) (string, error) {
	text, ok := fs.files[rel]
	if !ok {
		return "", fmt.Errorf("patcher: file not in checkout: %s", rel)
	}
	return text, nil
}

// MustGet returns content or panics; used by patch specs whose anchors are
// mandatory (mirrors getXOrThrow).
func (fs *FileSet) MustGet(rel string) string {
	text, err := fs.Get(rel)
	if err != nil {
		panic(err)
	}
	return text
}

// Set replaces the content of rel and marks it dirty.
func (fs *FileSet) Set(rel, text string) {
	fs.files[rel] = text
	fs.dirty[rel] = true
}

// Create overwrites or creates rel (mirrors project.createSourceFile overwrite).
func (fs *FileSet) Create(rel, text string) {
	fs.files[rel] = text
	fs.dirty[rel] = true
}

// Has reports whether rel is present in the working copy.
func (fs *FileSet) Has(rel string) bool {
	_, ok := fs.files[rel]
	return ok
}

// Files lists every file in the working copy in sorted order.
func (fs *FileSet) Files() []string {
	out := make([]string, 0, len(fs.files))
	for rel := range fs.files {
		out = append(out, rel)
	}
	sortStrings(out)
	return out
}

// Dirty lists modified/created files in sorted order.
func (fs *FileSet) Dirty() []string {
	out := make([]string, 0, len(fs.dirty))
	for rel := range fs.dirty {
		out = append(out, rel)
	}
	sortStrings(out)
	return out
}

// Save writes all dirty files back to the checkout root.
func (fs *FileSet) Save() error {
	for _, rel := range fs.Dirty() {
		path := filepath.Join(fs.Root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(fs.files[rel]), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// PatchFunc is one named patch unit (mirrors one exported patchX function).
type PatchFunc struct {
	// Name mirrors the TS export (e.g. patchChromiumSwitches).
	Name string
	// Target is the upstream file the patch operates on ("" for multi-file).
	Target string
	// Apply performs the transform, returning an error on missing anchors.
	Apply func(fs *FileSet) error
}

// Registry preserves patchright_driver_patch.ts application order.
var Registry []PatchFunc

func register(p PatchFunc) {
	Registry = append(Registry, p)
}

// ApplyAll runs every registered patch in order.
func ApplyAll(fs *FileSet) error {
	for _, p := range Registry {
		if err := p.Apply(fs); err != nil {
			return fmt.Errorf("%s: %w", p.Name, err)
		}
	}
	return nil
}

// ApplyNames runs a named subset in registry order.
func ApplyNames(fs *FileSet, names ...string) error {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	for _, p := range Registry {
		if want[p.Name] {
			if err := p.Apply(fs); err != nil {
				return fmt.Errorf("%s: %w", p.Name, err)
			}
		}
	}
	return nil
}

// --- text-transform helpers (Go equivalents of the ts-morph ops) ---

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// mustContain panics with a ts-morph-style OrThrow message when needle is absent.
func mustContain(text, needle, what string) {
	if !strings.Contains(text, needle) {
		panic(fmt.Sprintf("patcher: anchor %q not found for %s", needle, what))
	}
}

// replaceOnce replaces the first occurrence, erroring when absent.
func replaceOnce(text, old, new string) (string, error) {
	if !strings.Contains(text, old) {
		return text, fmt.Errorf("anchor not found: %q", truncate(old))
	}
	return strings.Replace(text, old, new, 1), nil
}

// ReplaceAll mirrors applyReplacements in cliAliasPatch.ts.
func ReplaceAll(text string, pairs [][2]string) string {
	for _, p := range pairs {
		text = strings.ReplaceAll(text, p[0], p[1])
	}
	return text
}

// removeLineContaining deletes the first line containing needle.
func removeLineContaining(text, needle string) (string, error) {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.Contains(line, needle) {
			return strings.Join(append(lines[:i], lines[i+1:]...), "\n"), nil
		}
	}
	return text, fmt.Errorf("line anchor not found: %q", needle)
}

// removeAllLinesContaining deletes every line containing needle, returning the count.
func removeAllLinesContaining(text, needle string) (string, int) {
	lines := strings.Split(text, "\n")
	kept := lines[:0]
	removed := 0
	for _, line := range lines {
		if strings.Contains(line, needle) {
			removed++
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n"), removed
}

// insertAfterAnchor inserts snippet (as new lines) after the first line containing anchor.
func insertAfterAnchor(text, anchor, snippet string) (string, error) {
	idx := strings.Index(text, anchor)
	if idx < 0 {
		return text, fmt.Errorf("anchor not found: %q", truncate(anchor))
	}
	eol := strings.Index(text[idx:], "\n")
	if eol < 0 {
		return text + "\n" + snippet, nil
	}
	pos := idx + eol + 1
	return text[:pos] + snippet + "\n" + text[pos:], nil
}

// insertBeforeAnchor inserts snippet before the first line containing anchor.
func insertBeforeAnchor(text, anchor, snippet string) (string, error) {
	idx := strings.Index(text, anchor)
	if idx < 0 {
		return text, fmt.Errorf("anchor not found: %q", truncate(anchor))
	}
	lineStart := strings.LastIndex(text[:idx], "\n") + 1
	return text[:lineStart] + snippet + "\n" + text[lineStart:], nil
}

// regexpReplace applies a regexp replacement, erroring when nothing matches.
//
// SAFETY: on failure it returns the ORIGINAL text (not "") so that
// `text, err = regexpReplace(...)` call sites which swallow err can never
// silently empty the file. Callers that need to distinguish must check err.
func regexpReplace(text, pattern, repl string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return text, err
	}
	if !re.MatchString(text) {
		return text, fmt.Errorf("pattern not found: %q", pattern)
	}
	return re.ReplaceAllString(text, repl), nil
}

// between extracts text between start and end markers (first match).
func between(text, start, end string) (string, error) {
	i := strings.Index(text, start)
	if i < 0 {
		return text, fmt.Errorf("start anchor not found: %q", truncate(start))
	}
	j := strings.Index(text[i+len(start):], end)
	if j < 0 {
		return text, fmt.Errorf("end anchor not found: %q", truncate(end))
	}
	return text[i+len(start) : i+len(start)+j], nil
}

// braceBlockEnd returns the index just past the matching closing brace for the
// opening brace at openIdx. The scan skips strings, template literals
// (including ${} interpolation), regex literals, and line/block comments.
func braceBlockEnd(text string, openIdx int) (int, error) {
	depth := 0
	i := openIdx
	n := len(text)
	for i < n {
		c := text[i]
		// Line comment.
		if c == '/' && i+1 < n && text[i+1] == '/' {
			for i < n && text[i] != '\n' {
				i++
			}
			continue
		}
		// Block comment.
		if c == '/' && i+1 < n && text[i+1] == '*' {
			i += 2
			for i+1 < n && !(text[i] == '*' && text[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}
		// String literal.
		if c == '\'' || c == '"' {
			quote := c
			i++
			for i < n {
				if text[i] == '\\' {
					i += 2
					continue
				}
				if text[i] == quote {
					i++
					break
				}
				if text[i] == '\n' {
					break
				}
				i++
			}
			continue
		}
		// Template literal with interpolation.
		if c == '`' {
			var err error
			i, err = skipTemplate(text, i)
			if err != nil {
				return 0, err
			}
			continue
		}
		// Heuristic regex literal.
		if c == '/' && isRegexStart(text, i) {
			j := i + 1
			inClass := false
			for j < n {
				cj := text[j]
				if cj == '\\' {
					j += 2
					continue
				}
				if cj == '[' {
					inClass = true
				} else if cj == ']' {
					inClass = false
				} else if cj == '/' && !inClass {
					break
				} else if cj == '\n' {
					break
				}
				j++
			}
			if j < n && text[j] == '/' {
				j++
				for j < n && (isWordChar(text[j])) {
					j++
				}
				i = j
				continue
			}
			i++
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1, nil
			}
		}
		i++
	}
	return 0, fmt.Errorf("unbalanced braces from offset %d", openIdx)
}

func isWordChar(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// skipTemplate skips a template literal starting at the backtick index,
// handling ${} interpolation nesting. Returns the index past the closing backtick.
func skipTemplate(text string, start int) (int, error) {
	n := len(text)
	i := start + 1
	for i < n {
		c := text[i]
		if c == '\\' {
			i += 2
			continue
		}
		if c == '`' {
			return i + 1, nil
		}
		if c == '$' && i+1 < n && text[i+1] == '{' {
			depth := 1
			i += 2
			for i < n && depth > 0 {
				cc := text[i]
				if cc == '\'' || cc == '"' {
					quote := cc
					i++
					for i < n {
						if text[i] == '\\' {
							i += 2
							continue
						}
						if text[i] == quote {
							i++
							break
						}
						if text[i] == '\n' {
							break
						}
						i++
					}
					continue
				}
				if cc == '`' {
					var err error
					i, err = skipTemplate(text, i)
					if err != nil {
						return 0, err
					}
					continue
				}
				if cc == '{' {
					depth++
				} else if cc == '}' {
					depth--
				}
				i++
			}
			continue
		}
		i++
	}
	return 0, fmt.Errorf("unterminated template literal at offset %d", start)
}

// isRegexStart heuristically reports whether '/' begins a regex literal.
func isRegexStart(text string, i int) bool {
	j := i - 1
	for j >= 0 && (text[j] == ' ' || text[j] == '\t' || text[j] == '\n' || text[j] == '\r') {
		j--
	}
	if j < 0 {
		return true
	}
	if strings.ContainsRune("(=:[!&|?{};,+-*%<>^~", rune(text[j])) {
		return true
	}
	k := j
	for k >= 0 && (isWordChar(text[k])) {
		k--
	}
	word := text[k+1 : j+1]
	switch word {
	case "return", "typeof", "instanceof", "in", "of", "new", "delete", "void", "throw", "case", "do", "else":
		return true
	}
	return false
}

// replaceMethodBody replaces the braced body of the method declaration whose
// signature line contains sigAnchor, keeping the signature intact and splicing
// in newBody (without surrounding braces). Anchors ending in "(" are matched
// generically (tolerating `<R, Arg>` type parameters).
func replaceMethodBody(text, sigAnchor, newBody string) (string, error) {
	idx := -1
	if strings.HasSuffix(sigAnchor, "(") {
		idx = findMethodDecl(text, strings.TrimSuffix(sigAnchor, "("))
	} else {
		idx = strings.Index(text, sigAnchor)
	}
	if idx < 0 {
		return text, fmt.Errorf("method anchor not found: %q", truncate(sigAnchor))
	}
	// Skip the parameter list first: a `{` inside type annotations (e.g.
	// `options: { strict?: boolean }`) must not be mistaken for the body open.
	pOpen := strings.Index(text[idx:], "(")
	if pOpen >= 0 {
		if pEnd, err := parenEnd(text, pOpen+idx); err == nil {
			idx = pEnd
		}
	}
	open := strings.Index(text[idx:], "{")
	if open < 0 {
		return text, fmt.Errorf("method body not found for: %q", truncate(sigAnchor))
	}
	open += idx
	end, err := braceBlockEnd(text, open)
	if err != nil {
		return text, err
	}
	return text[:open+1] + newBody + text[end-1:], nil
}

// appendToClassEnd appends member text before the closing brace of the class
// whose declaration contains classAnchor. The anchor may be the bare class
// name ("Frame") or a declaration fragment ("class Frame"); matching prefers a
// real class declaration line so member references don't shadow it.
func appendToClassEnd(text, classAnchor, member string) (string, error) {
	idx := findClassDecl(text, classAnchor)
	if idx < 0 {
		return text, fmt.Errorf("class anchor not found: %q", truncate(classAnchor))
	}
	open := strings.Index(text[idx:], "{")
	if open < 0 {
		return text, fmt.Errorf("class body not found for: %q", truncate(classAnchor))
	}
	open += idx
	end, err := braceBlockEnd(text, open)
	if err != nil {
		return text, err
	}
	return text[:end-1] + "\n" + member + "\n" + text[end-1:], nil
}

// findClassDecl locates the declaration of a class by name or fragment,
// skipping member/property references. Accepts "Frame", "class Frame", or
// "export class Frame ...".
func findClassDecl(text, anchor string) int {
	name := anchor
	name = strings.TrimPrefix(name, "export class ")
	name = strings.TrimPrefix(name, "class ")
	if i := strings.IndexAny(name, " <{(:"); i >= 0 {
		name = name[:i]
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return strings.Index(text, anchor)
	}
	// Prefer a declaration line: optional export/declare/abstract prefix,
	// then `class Name` at a line start (modulo whitespace).
	declRe := regexp.MustCompile(`(?m)^[ \t]*(?:export\s+)?(?:declare\s+|abstract\s+)?class\s+` + regexp.QuoteMeta(name) + `\b`)
	if loc := declRe.FindStringIndex(text); loc != nil {
		return loc[0]
	}
	return strings.Index(text, anchor)
}

// findMethodDecl locates a method declaration by name, tolerating generic type
// parameters (e.g. `async evaluate<R, Arg>(`). Returns the index of the method
// name occurrence that begins a declaration, or -1.
func findMethodDecl(text, method string) int {
	re := regexp.MustCompile(`(?m)^[ \t]*(?:async\s+|public\s+|private\s+|protected\s+|static\s+|override\s+|\*\s*)*` + regexp.QuoteMeta(method) + `(?:\s*<[^;{}]*>)?\s*\(`)
	if loc := re.FindStringIndex(text); loc != nil {
		// Return the index of the method name within the match.
		m := re.FindString(text[loc[0]:loc[1]])
		off := strings.Index(m, method)
		if off >= 0 {
			return loc[0] + off
		}
		return loc[0]
	}
	return strings.Index(text, method+"(")
}

func truncate(s string) string {
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}

func mustCompile(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pattern)
}

// replaceBlock replaces the braced block starting at the first "{" at or after
// startAnchor, preserving everything before the brace and after the matching
// close. It returns the updated text. Unlike regexp lookaheads (unsupported by
// Go's RE2), it resolves nesting via braceBlockEnd.
func replaceBlock(text, startAnchor, beforeBrace, newBody string) (string, error) {
	idx := strings.Index(text, startAnchor)
	if idx < 0 {
		return text, fmt.Errorf("block anchor not found: %q", truncate(startAnchor))
	}
	open := strings.Index(text[idx:], "{")
	if open < 0 {
		return text, fmt.Errorf("block open not found for: %q", truncate(startAnchor))
	}
	open += idx
	end, err := braceBlockEnd(text, open)
	if err != nil {
		return text, err
	}
	return text[:open+1] + newBody + text[end-1:], nil
}

// parenEnd returns the index of the closing paren matching the opening paren
// at openIdx, skipping strings, templates, regexes, and comments.
func parenEnd(text string, openIdx int) (int, error) {
	depth := 0
	i := openIdx
	n := len(text)
	for i < n {
		c := text[i]
		if c == '/' && i+1 < n && (text[i+1] == '/' || text[i+1] == '*') {
			if text[i+1] == '/' {
				for i < n && text[i] != '\n' {
					i++
				}
			} else {
				i += 2
				for i+1 < n && !(text[i] == '*' && text[i+1] == '/') {
					i++
				}
				i += 2
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote := c
			i++
			for i < n {
				if text[i] == '\\' {
					i += 2
					continue
				}
				if text[i] == quote {
					i++
					break
				}
				if text[i] == '\n' {
					break
				}
				i++
			}
			continue
		}
		if c == '`' {
			var err error
			i, err = skipTemplate(text, i)
			if err != nil {
				return 0, err
			}
			continue
		}
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
		i++
	}
	return 0, fmt.Errorf("unbalanced parens from offset %d", openIdx)
}

// appendParam appends a parameter to the parenthesized list opening at openIdx.
func appendParam(text string, openIdx int, param string) (string, error) {
	end, err := parenEnd(text, openIdx)
	if err != nil {
		return text, err
	}
	return text[:end] + ", " + param + text[end:], nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
