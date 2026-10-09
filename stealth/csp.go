// Package stealth is the sneaky runtime bits as plain testable go code: CSP
// relaxation so injected scripts actually run, <head> injection placement,
// the chromium switch blocklist, unguessable callback bridge names, and the
// "is this request worth injecting into" check.
package stealth

import (
	"regexp"
	"strings"
)

var directiveRe = regexp.MustCompile(`^([a-zA-Z-]+)(?:\s+(.*))?$`)
var nonceRe = regexp.MustCompile(`(?i)script-src[^;]*'nonce-([^'"\s;]+)'`)

// ExtractNonce mirrors the nonce extraction in RouteImpl.fulfill.
func ExtractNonce(csp string) string {
	m := nonceRe.FindStringSubmatch(csp)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

func addIfMissing(values []string, items ...string) []string {
	for _, item := range items {
		found := false
		for _, v := range values {
			if v == item {
				found = true
				break
			}
		}
		if !found {
			values = append(values, item)
		}
	}
	return values
}

func containsSub(values []string, sub string) bool {
	for _, v := range values {
		if strings.Contains(v, sub) {
			return true
		}
	}
	return false
}

// FixCSP mirrors RouteImpl._fixCSP: relaxes a Content-Security-Policy value so
// Patchright's injected init scripts execute while keeping the rest intact.
func FixCSP(csp string, scriptNonce *string) string {
	if csp == "" {
		return csp
	}
	var nonce string
	if scriptNonce != nil {
		nonce = *scriptNonce
	}
	directives := strings.Split(csp, ";")
	fixed := make([]string, 0, len(directives))
	hasScriptSrc := false
	hasDefaultSrc := false
	for _, directive := range directives {
		directive = strings.TrimSpace(directive)
		if directive == "" {
			continue
		}
		m := directiveRe.FindStringSubmatch(directive)
		if m == nil {
			fixed = append(fixed, directive)
			continue
		}
		name := strings.ToLower(m[1])
		var values []string
		if m[2] != "" {
			values = strings.Fields(m[2])
		}
		switch name {
		case "script-src":
			hasScriptSrc = true
			if nonce != "" && !containsSub(values, "nonce-"+nonce) {
				values = append(values, "'nonce-"+nonce+"'")
			}
			values = addIfMissing(values, "'unsafe-eval'")
			if nonce == "" {
				values = addIfMissing(values, "'unsafe-inline'")
			}
			hasWildcard := false
			for _, v := range values {
				if v == "*" || v == "'self'" || strings.Contains(v, "https:") {
					hasWildcard = true
					break
				}
			}
			if !hasWildcard {
				values = append(values, "*")
			}
			fixed = append(fixed, "script-src "+strings.Join(values, " "))
		case "style-src":
			values = addIfMissing(values, "'unsafe-inline'")
			fixed = append(fixed, "style-src "+strings.Join(values, " "))
		case "img-src", "font-src":
			star := false
			for _, v := range values {
				if v == "*" {
					star = true
					break
				}
			}
			if !star {
				values = addIfMissing(values, "data:")
			}
			fixed = append(fixed, name+" "+strings.Join(values, " "))
		case "connect-src":
			ws := false
			for _, v := range values {
				if strings.Contains(v, "ws:") || strings.Contains(v, "wss:") || v == "*" {
					ws = true
					break
				}
			}
			if !ws {
				values = addIfMissing(values, "ws:", "wss:")
			}
			fixed = append(fixed, "connect-src "+strings.Join(values, " "))
		case "frame-ancestors":
			joined := strings.Join(values, " ")
			if containsSub(values, "'none'") {
				joined = "'self'"
			}
			fixed = append(fixed, "frame-ancestors "+joined)
		case "default-src":
			hasDefaultSrc = true
			fixed = append(fixed, directive)
		default:
			fixed = append(fixed, directive)
		}
	}
	if !hasScriptSrc && hasDefaultSrc {
		if nonce != "" {
			fixed = append(fixed, "script-src 'self' 'unsafe-eval' 'nonce-"+nonce+"' *")
		} else {
			fixed = append(fixed, "script-src 'self' 'unsafe-eval' 'unsafe-inline' *")
		}
	}
	return strings.Join(fixed, "; ")
}

// InjectIntoHead mirrors RouteImpl._injectIntoHead: places injectionHTML before
// the first <script> in <head> (skipping HTML comments), else before </head>,
// else after <head>, <!DOCTYPE>, <html>, else prepended.
func InjectIntoHead(body, injectionHTML string) string {
	lower := strings.ToLower(body)
	headStart := strings.Index(lower, "<head")
	if headStart != -1 {
		rel := lower[headStart:]
		endRel := strings.Index(rel, ">")
		if endRel == -1 {
			return injectionHTML + body
		}
		headTagEnd := headStart + endRel + 1
		headEndRel := strings.Index(rel, "</head>")
		if headEndRel != -1 {
			headEnd := headStart + headEndRel
			headContent := lower[headTagEnd:headEnd]
			firstScript := -1
			searchPos := 0
			for searchPos < len(headContent) {
				commentStart := strings.Index(headContent[searchPos:], "<!--")
				scriptStart := strings.Index(headContent[searchPos:], "<script")
				cs := -1
				if commentStart != -1 {
					cs = searchPos + commentStart
				}
				ss := -1
				if scriptStart != -1 {
					ss = searchPos + scriptStart
				}
				if ss == -1 {
					break
				}
				if cs != -1 && cs < ss {
					commentEnd := strings.Index(headContent[cs:], "-->")
					if commentEnd == -1 {
						break
					}
					searchPos = cs + commentEnd + 3
				} else {
					firstScript = ss
					break
				}
			}
			insertAt := headEnd
			if firstScript != -1 {
				insertAt = headTagEnd + firstScript
			}
			return body[:insertAt] + injectionHTML + body[insertAt:]
		}
		return body[:headTagEnd] + injectionHTML + body[headTagEnd:]
	}
	if strings.HasPrefix(lower, "<!doctype") {
		end := strings.Index(body, ">")
		if end == -1 {
			return injectionHTML + body
		}
		return body[:end+1] + injectionHTML + body[end+1:]
	}
	if htmlIdx := lowerIndex(lower, "<html"); htmlIdx != -1 {
		endRel := strings.Index(lower[htmlIdx:], ">")
		if endRel == -1 {
			return injectionHTML + body
		}
		htmlEnd := htmlIdx + endRel + 1
		return body[:htmlEnd] + "<head>" + injectionHTML + "</head>" + body[htmlEnd:]
	}
	return injectionHTML + body
}

func lowerIndex(s, sub string) int {
	return strings.Index(s, sub)
}

// BuildInjectionHTML mirrors the injection builder in RouteImpl.fulfill: one
// self-removing <script> per init script, sharing the page's initScriptTag
// class and an optional CSP nonce.
func BuildInjectionHTML(initScriptTag string, sources []string, nonce string, newID func() string) string {
	var sb strings.Builder
	nonceAttr := ""
	if nonce != "" {
		nonceAttr = ` nonce="` + nonce + `"`
	}
	for _, source := range sources {
		id := newID()
		sb.WriteString(`<script class="` + initScriptTag + `" ` + nonceAttr + ` id="` + id + `" type="text/javascript">document.getElementById("` + id + `")?.remove();` + source + `</script>`)
	}
	return sb.String()
}

// IsHTMLResponse reports whether a content-type value denotes an HTML document.
func IsHTMLResponse(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/html")
}

// ShouldInjectScript mirrors the client installInjectRoute predicate: only
// top-level http(s) document requests carry init scripts.
func ShouldInjectScript(resourceType, url string) bool {
	if resourceType != "document" {
		return false
	}
	return strings.HasPrefix(url, "http")
}
