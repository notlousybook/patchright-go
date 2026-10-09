package impact

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// This file ports the compare/report half of check_patch_impact.ts: unified
// diff parsing, per-symbol change classification, LCS line diffs, markdown
// rendering, and the GitHub compare/raw fetch client.

// ChangeType mirrors ChangeType.
type ChangeType string

const (
	ChangeSignatureChanged ChangeType = "signature_changed"
	ChangeBodyChanged      ChangeType = "body_changed"
	ChangeSymbolRemoved    ChangeType = "symbol_removed"
	ChangeSymbolAdded      ChangeType = "symbol_added"
	ChangeUnchanged        ChangeType = "unchanged"
)

// Row mirrors SymbolImpactRow.
type Row struct {
	Record
	ChangeType  ChangeType `json:"changeType"`
	DiffLine    *int       `json:"diffLine"`
	DiffSide    *string    `json:"diffSide"`
	DiffSnippet string     `json:"diffSnippet"`
}

// Report mirrors the report.json shape.
type Report struct {
	OldVersion string `json:"old_version"`
	NewVersion string `json:"new_version"`
	Summary    struct {
		Total      int `json:"total"`
		Affected   int `json:"affected"`
		Unaffected int `json:"unaffected"`
	} `json:"summary"`
	Affected   []Row `json:"affected"`
	Unaffected []Row `json:"unaffected"`
}

// DiffLine mirrors DiffParsedLine.
type DiffLine struct {
	Raw     string
	Side    string // "L" | "R" | "C"
	OldLine *int
	NewLine *int
}

// Hunk mirrors DiffHunk.
type Hunk struct {
	Lines []DiffLine
}

// ParsedPatch mirrors ParsedPatch.
type ParsedPatch struct {
	Hunks     []Hunk
	Additions []LineRef
	Deletions []LineRef
}

// LineRef is one changed line reference.
type LineRef struct {
	Line int
	Text string
}

var hunkRe = regexp.MustCompile(`^@@\s+-(\d+)(?:,\d+)?\s+\+(\d+)(?:,\d+)?\s+@@`)

func intPtr(i int) *int { return &i }

func strPtr(s string) *string { return &s }

// ParsePatch mirrors parsePatch.
func ParsePatch(patchText string) ParsedPatch {
	var out ParsedPatch
	var current *Hunk
	oldLine, newLine := 0, 0
	for _, raw := range strings.Split(patchText, "\n") {
		if m := hunkRe.FindStringSubmatch(raw); m != nil {
			out.Hunks = append(out.Hunks, Hunk{})
			current = &out.Hunks[len(out.Hunks)-1]
			fmt.Sscanf(m[1], "%d", &oldLine)
			fmt.Sscanf(m[2], "%d", &newLine)
			continue
		}
		if current == nil {
			continue
		}
		switch {
		case strings.HasPrefix(raw, "+"):
			current.Lines = append(current.Lines, DiffLine{Raw: raw, Side: "R", NewLine: intPtr(newLine)})
			out.Additions = append(out.Additions, LineRef{Line: newLine, Text: raw[1:]})
			newLine++
		case strings.HasPrefix(raw, "-"):
			current.Lines = append(current.Lines, DiffLine{Raw: raw, Side: "L", OldLine: intPtr(oldLine)})
			out.Deletions = append(out.Deletions, LineRef{Line: oldLine, Text: raw[1:]})
			oldLine++
		case strings.HasPrefix(raw, " "):
			current.Lines = append(current.Lines, DiffLine{Raw: raw, Side: "C", OldLine: intPtr(oldLine), NewLine: intPtr(newLine)})
			oldLine++
			newLine++
		case strings.HasPrefix(raw, "\\"):
			current.Lines = append(current.Lines, DiffLine{Raw: raw, Side: "C"})
		}
	}
	return out
}

// DiffSnippetForAnchor mirrors diffSnippetForAnchorWithContext: it finds the
// hunk containing the changed line (or the first changed hunk) and returns it
// with contextLines of surrounding context.
func DiffSnippetForAnchor(parsed ParsedPatch, side *string, line *int, contextLines int) string {
	changed := func() bool {
		for _, h := range parsed.Hunks {
			for _, l := range h.Lines {
				if l.Side != "C" {
					return true
				}
			}
		}
		return false
	}
	if !changed() {
		return " No diff hunk available."
	}
	s := ""
	if side != nil {
		s = *side
	}
	ln := -1
	if line != nil {
		ln = *line
	}
	var target *Hunk
	targetIndex := -1
	for hi := range parsed.Hunks {
		h := &parsed.Hunks[hi]
		for i, l := range h.Lines {
			if s == "R" && ln >= 0 && l.Side == "R" && l.NewLine != nil && *l.NewLine == ln {
				target, targetIndex = h, i
				break
			}
			if s == "L" && ln >= 0 && l.Side == "L" && l.OldLine != nil && *l.OldLine == ln {
				target, targetIndex = h, i
				break
			}
		}
		if target != nil {
			break
		}
	}
	if target == nil {
		for hi := range parsed.Hunks {
			h := &parsed.Hunks[hi]
			for i, l := range h.Lines {
				if l.Side != "C" {
					target, targetIndex = h, i
					break
				}
			}
			if target != nil {
				break
			}
		}
	}
	if target == nil || targetIndex < 0 {
		return " No diff hunk available."
	}
	left := targetIndex
	for left > 0 && target.Lines[left-1].Side != "C" {
		left--
	}
	right := targetIndex
	for right < len(target.Lines)-1 && target.Lines[right+1].Side != "C" {
		right++
	}
	start := left - contextLines
	if start < 0 {
		start = 0
	}
	end := right + contextLines
	if end > len(target.Lines)-1 {
		end = len(target.Lines) - 1
	}
	var raws []string
	for _, l := range target.Lines[start : end+1] {
		raws = append(raws, l.Raw)
	}
	return strings.Join(raws, "\n")
}

// LineDiff mirrors lineDiff: LCS-based line diff of two texts.
func LineDiff(oldLines, newLines []string) []string {
	m, n := len(oldLines), len(newLines)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < m && j < n {
		if oldLines[i] == newLines[j] {
			out = append(out, " "+oldLines[i])
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			out = append(out, "-"+oldLines[i])
			i++
		} else {
			out = append(out, "+"+newLines[j])
			j++
		}
	}
	for ; i < m; i++ {
		out = append(out, "-"+oldLines[i])
	}
	for ; j < n; j++ {
		out = append(out, "+"+newLines[j])
	}
	return out
}

// MethodDiffSnippet mirrors makeMethodOrFunctionDiffSnippet.
func MethodDiffSnippet(oldBody, newBody, fallback string) string {
	if oldBody == "" && newBody == "" {
		return fallback
	}
	var oldLines, newLines []string
	if oldBody != "" {
		oldLines = strings.Split(strings.ReplaceAll(oldBody, "\r\n", "\n"), "\n")
	}
	if newBody != "" {
		newLines = strings.Split(strings.ReplaceAll(newBody, "\r\n", "\n"), "\n")
	}
	if max(len(oldLines), len(newLines)) > 80 {
		return fallback
	}
	diff := LineDiff(oldLines, newLines)
	if len(diff) == 0 {
		return fallback
	}
	return strings.Join(diff, "\n")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// YamlPathExists mirrors yamlPathExists.
func YamlPathExists(doc any, dotPath string) bool {
	current := doc
	for _, part := range strings.Split(dotPath, ".") {
		m, ok := current.(map[string]any)
		if !ok {
			return false
		}
		v, ok := m[part]
		if !ok {
			return false
		}
		current = v
	}
	return true
}

// ProtocolChangeType mirrors getProtocolChangeType.
func ProtocolChangeType(symbol string, oldDoc, newDoc any) ChangeType {
	inOld := YamlPathExists(oldDoc, symbol)
	inNew := YamlPathExists(newDoc, symbol)
	switch {
	case inOld && !inNew:
		return ChangeSymbolRemoved
	case !inOld && inNew:
		return ChangeSymbolAdded
	default:
		return ChangeUnchanged
	}
}

// SignatureText builds a normalized declaration signature for comparison,
// mirroring getDeclSignatureText (without a TS parser: name + params + return
// extracted textually).
func SignatureText(decl string) string {
	// Collapse whitespace; keep the text as-is otherwise.
	return strings.Join(strings.Fields(decl), " ")
}

// ClassifySymbol mirrors the per-symbol loop: given old/new declaration
// signatures and body texts plus diff ranges, it returns the change type.
func ClassifySymbol(existsOld, existsNew bool, fileStatus string, oldSig, newSig []string, patchTouches bool, kind Kind) ChangeType {
	if fileStatus == "removed" || (existsOld && !existsNew) {
		return ChangeSymbolRemoved
	}
	if fileStatus == "added" || (!existsOld && existsNew) {
		return ChangeSymbolAdded
	}
	if !patchTouches {
		return ChangeUnchanged
	}
	if kind == KindMethod || kind == KindFunction || kind == KindParameter {
		a := append([]string{}, oldSig...)
		b := append([]string{}, newSig...)
		sort.Strings(a)
		sort.Strings(b)
		if strings.Join(a, "\n") != strings.Join(b, "\n") {
			return ChangeSignatureChanged
		}
		return ChangeBodyChanged
	}
	return ChangeBodyChanged
}

func escapeUnderscore(s string) string {
	return strings.ReplaceAll(s, "_", "\\_")
}

func colorToken(value, color string) string {
	return "${\\color{" + color + "}\\text{" + escapeUnderscore(value) + "}}$"
}

func changeColor(c ChangeType) string {
	switch c {
	case ChangeSignatureChanged, ChangeSymbolRemoved:
		return "brown"
	default:
		return "orange"
	}
}

func kindColor(k Kind) string {
	if k == KindClass {
		return "green"
	}
	return "lime"
}

func playwrightURL(file, tag string, start, end *int) string {
	s := 1
	if start != nil {
		s = *start
	}
	e := s
	if end != nil {
		e = *end
	}
	return fmt.Sprintf("https://github.com/microsoft/playwright/blob/%s/%s#L%d-L%d", tag, file, s, e)
}

func patchURL(patchFile, sha string, start, end *int) string {
	p := patchFile
	if p != "patchright_driver_patch.ts" {
		p = "driver_patches/" + patchFile
	}
	s := 1
	if start != nil {
		s = *start
	}
	e := s
	if end != nil {
		e = *end
	}
	return fmt.Sprintf("https://github.com/Kaliiiiiiiiii-Vinyzu/patchright/blob/%s/%s#L%d-L%d", sha, p, s, e)
}

// FormatAffectedDetail mirrors formatAffectedDetail.
func FormatAffectedDetail(row Row, oldTag, newTag, sha string) string {
	kindToken := colorToken(string(row.Kind), kindColor(row.Kind))
	changeToken := colorToken(string(row.ChangeType), changeColor(row.ChangeType))
	snippet := row.DiffSnippet
	if snippet == "" {
		snippet = " No diff hunk available."
	}
	return strings.Join([]string{
		"<details>",
		fmt.Sprintf("<summary><code>%s</code> &nbsp;·&nbsp; (%s | %s)</summary>", row.Symbol, kindToken, changeToken),
		"",
		fmt.Sprintf("**Playwright File:** [%s](%s)", row.PlaywrightFile, playwrightURL(row.PlaywrightFile, newTag, row.PlaywrightFileLineStart, row.PlaywrightFileLineEnd)),
		"</br>",
		fmt.Sprintf("**Patch File:** [%s](%s)", row.PatchFile, patchURL(row.PatchFile, sha, row.PatchFileLineStart, row.PatchFileLineEnd)),
		"```diff",
		snippet,
		"```",
		"",
		"</details>",
	}, "\n")
}

// FormatUnaffectedTable mirrors formatUnaffectedTable.
func FormatUnaffectedTable(rows []Row, newTag, sha string) string {
	lines := []string{
		"| Symbol | Kind | Playwright File | Patch File |",
		"|--------|------|-----------------|------------|",
	}
	for _, row := range rows {
		lines = append(lines, fmt.Sprintf("| %s | %s | [%s](%s) | [%s](%s) |",
			row.Symbol, row.Kind, row.PlaywrightFile,
			playwrightURL(row.PlaywrightFile, newTag, row.PlaywrightFileLineStart, row.PlaywrightFileLineEnd),
			row.PatchFile, patchURL(row.PatchFile, sha, row.PatchFileLineStart, row.PatchFileLineEnd)))
	}
	return strings.Join(lines, "\n")
}

// RenderSummary mirrors the summaryMarkdown assembly.
func RenderSummary(oldTag, newTag string, affected, unaffected []Row, sha string) string {
	var blocks []string
	for _, row := range affected {
		blocks = append(blocks, FormatAffectedDetail(row, oldTag, newTag, sha))
	}
	parts := []string{
		fmt.Sprintf("## Playwright %s -> %s: Patch Impact Report", oldTag, newTag),
		"",
		fmt.Sprintf("Breaking: %d of %d patched symbols were affected", len(affected), len(affected)+len(unaffected)),
		"",
	}
	parts = append(parts, blocks...)
	parts = append(parts, "",
		fmt.Sprintf("<details><summary>Passing: Unaffected patched symbols (%d)</summary>", len(unaffected)),
		"", FormatUnaffectedTable(unaffected, newTag, sha), "", "</details>", "")
	return strings.Join(parts, "\n")
}

// ToPatchSection mirrors toPatchSection.
func ToPatchSection(playwrightFile, patchText string) string {
	var body []string
	for _, line := range strings.Split(patchText, "\n") {
		if strings.HasPrefix(line, "@@ ") {
			continue
		}
		body = append(body, line)
	}
	return strings.Join([]string{
		fmt.Sprintf("diff --git a/%s b/%s", playwrightFile, playwrightFile),
		fmt.Sprintf("--- a/%s", playwrightFile),
		fmt.Sprintf("+++ b/%s", playwrightFile),
		strings.Join(body, "\n"),
	}, "\n")
}

// Client is a GitHub compare/raw fetch client, mirroring fetchJson/fetchRawFile.
type Client struct {
	Token      string
	HTTP       *http.Client
	RawBaseURL string // override for tests
	APIBaseURL string // override for tests
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (c *Client) get(url string, userAgent string, v any) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("request failed (%d) for %s", resp.StatusCode, url)
	}
	if v == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// CompareFile mirrors CompareApiFile.
type CompareFile struct {
	Filename string `json:"filename"`
	Status   string `json:"status"`
	Patch    string `json:"patch"`
}

// Compare fetches the GitHub compare payload for oldTag...newTag.
func (c *Client) Compare(oldTag, newTag string) ([]CompareFile, error) {
	base := c.APIBaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	var out struct {
		Files []CompareFile `json:"files"`
	}
	if err := c.get(base+"/repos/microsoft/playwright/compare/"+oldTag+"..."+newTag, "patchright-check-patch-impact", &out); err != nil {
		return nil, err
	}
	return out.Files, nil
}

// RawFile fetches a file at a tag, returning ("", false, nil) on 404,
// mirroring fetchRawFile.
func (c *Client) RawFile(tag, filePath string) (string, bool, error) {
	base := c.RawBaseURL
	if base == "" {
		base = "https://raw.githubusercontent.com/microsoft/playwright/" + tag
		return fetchRaw(base+"/"+filePath, c.Token, "patchright-check-patch-impact")
	}
	return fetchRaw(base+"/"+filePath, c.Token, "patchright-check-patch-impact")
}

func fetchRaw(url, token, agent string) (string, bool, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", agent)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("failed to fetch %s: HTTP %d", url, resp.StatusCode)
	}
	var sb strings.Builder
	buf := make([]byte, 32768)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String(), true, nil
}

// ParseProtocol parses a protocol YAML document into a generic map.
func ParseProtocol(text string) (map[string]any, error) {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

// Analyze runs the full impact analysis, mirroring main() in
// check_patch_impact.ts. fetch provides old/new file texts; compare provides
// the GitHub compare files. It returns the report, summary markdown,
// affected-diff text, and issue markdown.
func Analyze(oldTag, newTag, sha string, symbols []Record, files []CompareFile, fetch func(tag, path string) (string, bool)) (Report, string, string, string, error) {
	patchByFile := map[string]string{}
	statusByFile := map[string]string{}
	parsedByFile := map[string]ParsedPatch{}
	for _, f := range files {
		if !IsRelevantPath(f.Filename) {
			continue
		}
		patchByFile[f.Filename] = f.Patch
		status := f.Status
		if status == "" {
			status = "modified"
		}
		statusByFile[f.Filename] = status
		parsedByFile[f.Filename] = ParsePatch(f.Patch)
	}
	texts := map[string][2]string{}
	seen := map[string]bool{}
	for _, s := range symbols {
		if seen[s.PlaywrightFile] {
			continue
		}
		seen[s.PlaywrightFile] = true
		oldText, _ := fetch(oldTag, s.PlaywrightFile)
		newText, _ := fetch(newTag, s.PlaywrightFile)
		texts[s.PlaywrightFile] = [2]string{oldText, newText}
	}
	var oldProto, newProto map[string]any
	if t, ok := texts["packages/protocol/src/protocol.yml"]; ok {
		if t[0] != "" {
			oldProto, _ = ParseProtocol(t[0])
		}
		if t[1] != "" {
			newProto, _ = ParseProtocol(t[1])
		}
	}
	var allRows, affected []Row
	for _, s := range symbols {
		row := Row{Record: s, ChangeType: ChangeUnchanged}
		if s.Kind == KindProtocolParam || s.Kind == KindProtocolProperty {
			row.ChangeType = ProtocolChangeType(s.Symbol, oldProto, newProto)
		} else {
			filePatch := patchByFile[s.PlaywrightFile]
			status := statusByFile[s.PlaywrightFile]
			if status == "" {
				status = "modified"
			}
			pair := texts[s.PlaywrightFile]
			oldExists := symbolPresent(pair[0], s.Kind, s.Symbol)
			newExists := symbolPresent(pair[1], s.Kind, s.Symbol)
			var re strings.Builder
			re.WriteString(`\b` + regexp.QuoteMeta(s.Symbol) + `\b`)
			touches := regexp.MustCompile(re.String()).MatchString(filePatch)
			row.ChangeType = ClassifySymbol(oldExists, newExists, status,
				signaturesIn(pair[0], s.Kind, s.Symbol),
				signaturesIn(pair[1], s.Kind, s.Symbol),
				touches, s.Kind)
			if row.ChangeType != ChangeUnchanged {
				parsed := parsedByFile[s.PlaywrightFile]
				fallback := DiffSnippetForAnchor(parsed, strPtr("R"), firstChangeLine(parsed), 10)
				switch s.Kind {
				case KindMethod, KindFunction:
					oldBody := bodyIn(pair[0], s.Symbol)
					newBody := bodyIn(pair[1], s.Symbol)
					row.DiffSnippet = MethodDiffSnippet(oldBody, newBody, fallback)
				default:
					row.DiffSnippet = fallback
				}
				// Anchor line: first addition by default.
				if len(parsed.Additions) > 0 {
					row.DiffLine = intPtr(parsed.Additions[0].Line)
					row.DiffSide = strPtr("R")
				}
			}
		}
		allRows = append(allRows, row)
		if row.ChangeType != ChangeUnchanged {
			affected = append(affected, row)
		}
	}
	var unaffected []Row
	for _, r := range allRows {
		if r.ChangeType == ChangeUnchanged {
			unaffected = append(unaffected, r)
		}
	}
	sort.Slice(affected, func(i, j int) bool {
		ri, rj := 0, 1
		if affected[i].Kind == KindClass {
			ri = 0
		} else {
			ri = 1
		}
		if affected[j].Kind == KindClass {
			rj = 0
		} else {
			rj = 1
		}
		if ri != rj {
			return ri < rj
		}
		if affected[i].Kind != affected[j].Kind {
			return affected[i].Kind < affected[j].Kind
		}
		return affected[i].Symbol < affected[j].Symbol
	})
	report := Report{OldVersion: strings.TrimPrefix(oldTag, "v"), NewVersion: strings.TrimPrefix(newTag, "v")}
	report.Summary.Total = len(symbols)
	report.Summary.Affected = len(affected)
	report.Summary.Unaffected = len(unaffected)
	report.Affected = affected
	report.Unaffected = unaffected
	if report.Affected == nil {
		report.Affected = []Row{}
	}
	if report.Unaffected == nil {
		report.Unaffected = []Row{}
	}
	summary := RenderSummary(oldTag, newTag, affected, unaffected, sha)
	var sections []string
	added := map[string]bool{}
	for _, row := range affected {
		patch, ok := patchByFile[row.PlaywrightFile]
		if !ok || patch == "" {
			continue
		}
		section := ToPatchSection(row.PlaywrightFile, patch)
		if added[section] {
			continue
		}
		added[section] = true
		sections = append(sections, section)
	}
	diffText := strings.Join(sections, "\n\n") + "\n"
	var blocks []string
	for _, row := range affected {
		blocks = append(blocks, FormatAffectedDetail(row, oldTag, newTag, sha))
	}
	issue := strings.Join(append([]string{
		fmt.Sprintf("## Playwright %s -> %s: Patch Impact Report", oldTag, newTag),
		"",
		fmt.Sprintf("Detected %d affected patched symbol changes.", len(affected)),
		"",
	}, append(blocks, "")...), "\n")
	// Write GITHUB_OUTPUT when present, mirroring the TS script.
	if out := os.Getenv("GITHUB_OUTPUT"); out != "" {
		f, err := os.OpenFile(out, os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "breaking_count=%d\naffected_count=%d\ntotal_count=%d\nissue_title=[Patch Impact] Playwright %s -> %s: %d breaking changes detected\n",
				len(affected), len(affected), len(symbols), oldTag, newTag, len(affected))
			f.Close()
		}
	}
	return report, summary, diffText, issue, nil
}

func firstChangeLine(parsed ParsedPatch) *int {
	if len(parsed.Additions) > 0 {
		return intPtr(parsed.Additions[0].Line)
	}
	if len(parsed.Deletions) > 0 {
		return intPtr(parsed.Deletions[0].Line)
	}
	return nil
}

// symbolPresent is a textual approximation of findNodesByKindAndSymbol: it
// reports whether a class/method/function/property/parameter declaration for
// the symbol exists in the file text.
func symbolPresent(text string, kind Kind, symbol string) bool {
	if text == "" {
		return false
	}
	q := regexp.QuoteMeta(symbol)
	patterns := map[Kind]string{
		KindClass:     `(?m)^\s*(?:export\s+)?(?:declare\s+|abstract\s+)?class\s+` + q + `\b`,
		KindMethod:    `(?m)^\s*(?:async\s+|public\s+|private\s+|protected\s+|static\s+|override\s+|\*\s*)*` + q + `\s*(?:<[^;{}]*>)?\s*\(`,
		KindFunction:  `(?m)^\s*(?:export\s+)?(?:async\s+)?function\s+` + q + `\b`,
		KindProperty:  `(?m)^\s*(?:public\s+|private\s+|protected\s+|readonly\s+|static\s+)*` + q + `\s*[?:=;]`,
		KindParameter: `\b` + q + `\s*[?:=,)]`,
	}
	pat, ok := patterns[kind]
	if !ok {
		return strings.Contains(text, symbol)
	}
	return regexp.MustCompile(pat).MatchString(text)
}

// signaturesIn extracts normalized declaration signature lines for comparison.
func signaturesIn(text string, kind Kind, symbol string) []string {
	if text == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if !strings.Contains(t, symbol) {
			continue
		}
		switch kind {
		case KindMethod:
			if strings.Contains(t, symbol+"(") || strings.Contains(t, symbol+"<") {
				out = append(out, SignatureText(t))
			}
		case KindFunction:
			if strings.Contains(t, "function "+symbol) {
				out = append(out, SignatureText(t))
			}
		case KindParameter:
			if strings.Contains(t, symbol) {
				out = append(out, SignatureText(t))
			}
		}
	}
	return out
}

// bodyIn extracts a method/function body textually for LCS diffing.
func bodyIn(text, symbol string) string {
	if text == "" {
		return ""
	}
	idx := strings.Index(text, symbol)
	if idx < 0 {
		return ""
	}
	open := strings.Index(text[idx:], "{")
	if open < 0 {
		return ""
	}
	open += idx
	depth := 0
	for i := open; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[open+1 : i]
			}
		}
	}
	return ""
}
