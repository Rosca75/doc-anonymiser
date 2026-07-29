// embed_test.go — the local-only guarantee, checked against the bytes
// that actually ship (BUILD-04 Phase 7).
//
// The documentation window (CR6) is a SECOND window loading the
// application's own asset server. That is only safe because everything it
// asks for is embedded: an asset that failed to make it into the binary
// would leave a broken window at best, and at worst a page reaching for
// something that is not there.
//
// So this walks the embedded filesystem itself rather than the source
// tree, and checks two things: the documentation is present, and nothing
// under frontend/ references a remote origin (CLAUDE.md section 4).
package main

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"

	"doc-anonymiser/backend"
)

// requiredAssets must all be present in the embedded filesystem for the
// application to work offline. The paths carry the frontend/ prefix because
// //go:embed all:frontend bakes the folder name into every embedded path.
var requiredAssets = []string{
	"frontend/index.html",
	"frontend/brand.css",
	"frontend/style.css",
	"frontend/main.js",
	// BUILD-04 CR6: the documentation window's page and its stylesheet.
	"frontend/docs/index.html",
	"frontend/docs/docs.css",
}

func TestRequiredAssetsAreEmbedded(t *testing.T) {
	for _, name := range requiredAssets {
		if _, err := fs.Stat(assets, name); err != nil {
			t.Errorf("%s is not in the embedded filesystem: %v\n"+
				"The go:embed directive in main.go must cover it, or the application "+
				"cannot serve it offline.", name, err)
		}
	}
}

func TestDocumentationAssetMatchesTheBoundPath(t *testing.T) {
	// app.go tells the frontend where the documentation lives. If that
	// path and the embedded file ever drift, the Documentation menu entry
	// opens an empty window.
	full := path.Join("frontend", backend.DocumentationAsset)
	if _, err := fs.Stat(assets, full); err != nil {
		t.Errorf("DocumentationURL() returns %q, but %q is not embedded: %v",
			backend.DocumentationAsset, full, err)
	}
}

// remoteOriginRe matches an absolute http(s) URL in a position that
// actually makes a browser FETCH it: a src or href attribute, a CSS
// url(), or an @import. That distinction matters, because user-visible
// copy legitimately mentions addresses in prose ("For example
// https://example.com/report" is the label for the web-address
// recogniser), and flagging those would train people to ignore this test.
var remoteOriginRe = regexp.MustCompile(
	`(?:src|href)\s*=\s*["']?(https?://[^\s"'<>)]+)` +
		`|url\(\s*["']?(https?://[^\s"'<>)]+)` +
		`|@import\s+["'](https?://[^\s"'<>)]+)`)

func TestNoEmbeddedAssetReferencesARemoteOrigin(t *testing.T) {
	// Allowed absolute URLs: the loopback endpoint, and the SVG namespace,
	// which is an XML identifier rather than something a browser fetches.
	allowed := []string{
		"http://127.0.0.1",
		"https://127.0.0.1",
		"http://www.w3.org/2000/svg",
		"http://www.w3.org/1999/xlink",
	}
	isAllowed := func(url string) bool {
		for _, prefix := range allowed {
			if strings.HasPrefix(url, prefix) {
				return true
			}
		}
		return false
	}

	err := fs.WalkDir(assets, "frontend", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch path.Ext(name) {
		case ".html", ".css", ".js", ".svg":
		default:
			return nil // licences and the like are not fetched by the page
		}
		raw, err := fs.ReadFile(assets, name)
		if err != nil {
			return err
		}
		for _, m := range remoteOriginRe.FindAllStringSubmatch(string(raw), -1) {
			// Exactly one of the alternation's groups is non-empty.
			url := m[1] + m[2] + m[3]
			if url == "" || isAllowed(url) {
				continue
			}
			t.Errorf("%s loads %s from a remote origin\n"+
				"Every asset must be vendored and embedded: no CDN, no remote font, "+
				"no external stylesheet (CLAUDE.md section 4).", name, url)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the embedded assets failed: %v", err)
	}
}
