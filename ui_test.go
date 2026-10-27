package dbadminer

import (
	"os"
	"strings"
	"testing"
)

func TestUIUsesTextContent(t *testing.T) {
	body, err := os.ReadFile("ui/app.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)
	for _, banned := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write"} {
		if strings.Contains(src, banned) {
			t.Fatalf("ui/app.js contains %s", banned)
		}
	}
	if !strings.Contains(src, "textContent") {
		t.Fatal("ui/app.js does not render with textContent")
	}
	html, err := os.ReadFile("ui/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(html)
	if strings.Contains(page, "<script>") && !strings.Contains(page, `src="app.js"`) {
		t.Fatal("inline script would require a looser content security policy")
	}
	if !strings.Contains(page, `src="app.js"`) || !strings.Contains(page, `href="app.css"`) {
		t.Fatal("UI assets are not external")
	}
}
