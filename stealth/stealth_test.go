package stealth

import (
	"strings"
	"testing"

	"github.com/notlousybook/patchright-go/patcher"
)

func TestFixCSPScriptSrc(t *testing.T) {
	out := FixCSP("script-src 'self'; object-src 'none'", nil)
	if !strings.Contains(out, "'unsafe-eval'") || !strings.Contains(out, "'unsafe-inline'") {
		t.Errorf("script-src not relaxed: %q", out)
	}
	if !strings.Contains(out, "object-src 'none'") {
		t.Errorf("unrelated directive changed: %q", out)
	}
	// No wildcard when 'self' already present (mirrors _fixCSP); added otherwise.
	out2 := FixCSP("script-src blob:; object-src 'none'", nil)
	if !strings.Contains(out2, " *") {
		t.Errorf("wildcard not added: %q", out2)
	}
}

func TestFixCSPNonce(t *testing.T) {
	nonce := "abc123"
	out := FixCSP("script-src 'self' 'nonce-abc123'", &nonce)
	if !strings.Contains(out, "'nonce-abc123'") {
		t.Errorf("nonce lost: %q", out)
	}
	if strings.Contains(out, "'unsafe-inline'") {
		t.Errorf("unsafe-inline added despite nonce: %q", out)
	}
}

func TestFixCSPFrameAncestors(t *testing.T) {
	out := FixCSP("frame-ancestors 'none'", nil)
	if !strings.Contains(out, "frame-ancestors 'self'") {
		t.Errorf("frame-ancestors not relaxed: %q", out)
	}
}

func TestFixCSPConnectImg(t *testing.T) {
	out := FixCSP("img-src 'self'; connect-src 'self'", nil)
	if !strings.Contains(out, "data:") {
		t.Errorf("img-src missing data:: %q", out)
	}
	if !strings.Contains(out, "ws:") || !strings.Contains(out, "wss:") {
		t.Errorf("connect-src missing ws:: %q", out)
	}
}

func TestFixCSPEmpty(t *testing.T) {
	if FixCSP("", nil) != "" {
		t.Error("empty CSP should stay empty")
	}
}

func TestExtractNonce(t *testing.T) {
	if got := ExtractNonce("script-src 'self' 'nonce-r4nd0m'"); got != "r4nd0m" {
		t.Errorf("got %q", got)
	}
	if got := ExtractNonce("script-src 'self'"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestInjectIntoHead(t *testing.T) {
	body := "<html><head><meta charset=\"utf-8\"><script src=\"a.js\"></script></head><body></body></html>"
	out := InjectIntoHead(body, "<script>INJECT</script>")
	scriptIdx := strings.Index(out, "<script src")
	injectIdx := strings.Index(out, "INJECT")
	if injectIdx < 0 || injectIdx > scriptIdx {
		t.Errorf("injection not before first script:\n%s", out)
	}
	// Comment-skipping: commented script must not attract the injection.
	body2 := "<html><head><!-- <script src=\"x\"> --><script src=\"a.js\"></script></head></html>"
	out2 := InjectIntoHead(body2, "<script>INJECT</script>")
	if strings.Index(out2, "INJECT") > strings.Index(out2, "<script src=\"a.js\"") {
		t.Errorf("commented script confused placement:\n%s", out2)
	}
	// No head: inject after doctype.
	out3 := InjectIntoHead("<!doctype html><html><body></body></html>", "<script>INJECT</script>")
	if !strings.HasPrefix(out3, "<!doctype html><script>INJECT</script>") {
		t.Errorf("doctype placement wrong: %q", out3)
	}
}

func TestBuildInjectionHTML(t *testing.T) {
	n := 0
	out := BuildInjectionHTML("tag123", []string{"a=1", "b=2"}, "nonce1", func() string {
		n++
		if n == 1 {
			return "id1"
		}
		return "id2"
	})
	if strings.Count(out, "<script") != 2 {
		t.Errorf("want 2 scripts: %q", out)
	}
	if !strings.Contains(out, `class="tag123"`) || !strings.Contains(out, `nonce="nonce1"`) {
		t.Errorf("attrs missing: %q", out)
	}
	if !strings.Contains(out, `document.getElementById("id1")?.remove();a=1`) {
		t.Errorf("self-removal missing: %q", out)
	}
}

func TestShouldInjectScript(t *testing.T) {
	if !ShouldInjectScript("document", "http://example.com/") {
		t.Error("http document should inject")
	}
	if !ShouldInjectScript("document", "https://example.com/") {
		t.Error("https document should inject")
	}
	if ShouldInjectScript("document", "about:blank") {
		t.Error("about:blank must not inject (known limitation)")
	}
	if ShouldInjectScript("script", "http://example.com/a.js") {
		t.Error("subresource must not inject")
	}
}

func TestIsHTMLResponse(t *testing.T) {
	if !IsHTMLResponse("text/html; charset=utf-8") {
		t.Error("html not detected")
	}
	if IsHTMLResponse("application/json") {
		t.Error("json misdetected")
	}
}

func TestCallbackBridgeNames(t *testing.T) {
	name, err := NewCallbackBridgeName()
	if err != nil {
		t.Fatal(err)
	}
	if !IsCallbackBridgeName(name) {
		t.Errorf("generated name fails gate: %q", name)
	}
	if IsCallbackBridgeName("__pw_abc") || IsCallbackBridgeName("fZZZ") {
		t.Error("leaky name passes gate")
	}
	if !IsCallbackBridgeDescription("function callbackBridge(command) {") {
		t.Error("bridge description rejected")
	}
	if IsCallbackBridgeDescription("function foo() {") {
		t.Error("non-bridge description accepted")
	}
}

func TestSwitchPolicyParity(t *testing.T) {
	// stealth.FilterChromiumSwitches must match patcher.SwitchesToDisable policy.
	in := []string{"--enable-automation"}
	for _, sw := range patcher.SwitchesToDisable {
		flag := sw
		if i := strings.Index(flag, "'--"); i >= 0 {
			flag = flag[i+1:]
			if j := strings.Index(flag, "'"); j >= 0 {
				flag = flag[:j]
			}
		}
		in = append(in, flag)
	}
	out := patcher.FilterChromiumSwitches(in)
	if len(out) != 1 || out[0] != "--disable-blink-features=AutomationControlled" {
		t.Errorf("parity failure: %q", out)
	}
}
