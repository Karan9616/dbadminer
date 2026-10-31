package dbadminer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUISourcesAvoidHTMLInjection(t *testing.T) {
	banned := []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "{@html"}
	err := filepath.WalkDir("web/src", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".svelte", ".ts", ".js":
		default:
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(body)
		for _, needle := range banned {
			if strings.Contains(src, needle) {
				t.Errorf("%s contains %s", path, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUIBuildIsExternal(t *testing.T) {
	html, err := os.ReadFile("ui/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(html)
	if strings.Contains(page, "<script>") || strings.Contains(page, "<style") {
		t.Fatal("ui/index.html contains an inline script or style")
	}
	if !strings.Contains(page, "<script ") || !strings.Contains(page, " src=") {
		t.Fatal("ui/index.html does not load an external script")
	}
	if !strings.Contains(page, `rel="stylesheet"`) || !strings.Contains(page, " href=") {
		t.Fatal("ui/index.html does not load an external stylesheet")
	}
	if _, err := os.Stat("ui/favicon.svg"); err != nil {
		t.Fatal(err)
	}
}
