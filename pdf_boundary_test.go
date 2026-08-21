// pdf_boundary_test.go — the local-only boundary guard for the vendored
// PDF library (github.com/aspose-pdf-foss/aspose-pdf-foss-for-go).
//
// The library brings code into the module that can POST document text and
// rendered page images to a configured OpenAI-compatible endpoint (its ai
// subpackage), and one root-package path that can POST a signature digest to
// an RFC 3161 timestamp server. Neither may ever be reachable from this
// application: the local-only guarantee (CLAUDE.md §4) allows network I/O to
// 127.0.0.1:11434 only, constructed only by backend/ollama/client.go.
//
// A promise in a comment cannot hold that, so this guard is a test with three
// teeth, in vocabulary_guard_test.go's idiom (whole-token source scan, tiny
// named exemptions, failure messages that name the fix):
//
//  1. FORBIDDEN SYMBOLS: no source file under backend/, frontend/ or scripts/
//     may reference the library's network-capable surface. A symbol never
//     referenced is code the Go linker drops, so this also keeps the copilot
//     machinery out of the shipped binary.
//  2. THE VENDORED NETWORK INVENTORY: the set of vendored library files that
//     import a network package is compared against a committed list, so a
//     version bump that widens the network surface fails the build until the
//     new surface is deliberately re-reviewed.
//  3. THE ENGINE PATH BAN: the engine takes bytes and returns bytes, so the
//     library's file-path entry points (Open, OpenWithPassword, Save) are
//     forbidden under backend/; only OpenStream, OpenStreamWithPassword and
//     WriteTo may appear.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// pdfLibImportPath is the module this guard is about, and pdfLibAlias is the
// ONE local name an import of it may take. Fixing the alias is what makes the
// path ban below a whole-token scan instead of a type-check: every file that
// imports the library calls it the same thing, so the forbidden call shapes
// have exactly one spelling.
const (
	pdfLibImportPath = "github.com/aspose-pdf-foss/aspose-pdf-foss-for-go"
	pdfLibAlias      = "asposepdf"
)

// pdfLibForbiddenSymbols is the library's network-capable surface: every
// exported symbol declared in a library file that imports net/http, plus the
// exported lever that reaches the one unexported network call. The table was
// generated from the vendored source at v0.7.0 (the files named in
// pdfLibNetworkInventory below) and is re-checked against it by hand on every
// version bump, which inventory drift (check 2) forces to happen.
var pdfLibForbiddenSymbols = map[string]string{
	// ai/openai.go: the HTTP client. The ONLY place in the library that
	// constructs an HTTP request to a configurable endpoint.
	"NewOpenAIClient":     "the copilot HTTP client constructor; nothing here may build one",
	"OpenAIClient":        "the copilot HTTP client type",
	"OpenAIClientOptions": "carries the copilot endpoint URL and API key",
	// ai/client.go: the seam every copilot reaches the network through.
	"AIClient": "the interface the copilots send document text through",
	// ai/summary.go, ai/chat.go, ai/imagedesc.go, ai/ocr.go: the copilots.
	// Each one sends extracted document text and/or rendered page images to
	// the configured endpoint.
	"SummaryCopilot":             "sends the document's text off the machine",
	"NewSummaryCopilot":          "constructs the summary copilot",
	"ChatCopilot":                "sends the document's text off the machine",
	"NewChatCopilot":             "constructs the chat copilot",
	"ImageDescriptionCopilot":    "sends rendered page images off the machine",
	"NewImageDescriptionCopilot": "constructs the image-description copilot",
	"OcrCopilot":                 "OCR through the copilot endpoint; also returns no coordinates, which in-place replacement needs",
	"NewOcrCopilot":              "constructs the OCR copilot",
	"MakeSearchable":             "the OCR copilot's document-rewriting entry point",
	"LLMOCREngine":               "the OCR engine that renders pages and sends them to the endpoint",
	"NewLLMOCREngine":            "constructs the LLM OCR engine",
	// sign.go: SignOptions.TimestampURL is the exported lever over the root
	// package's one network call (pkcs7_timestamp.go requestTimestamp, which
	// POSTs to the named RFC 3161 server). The application never signs a PDF,
	// so the whole lever is forbidden rather than carved around.
	"TimestampURL": "reaches the library's RFC 3161 timestamp POST",
}

// pdfLibForbiddenImport is the copilot subpackage's import path. Forbidding
// the import as well as the symbols means the package cannot even be linked
// in under a fresh local name.
const pdfLibForbiddenImport = pdfLibImportPath + "/ai"

// pdfLibNetworkInventory is the committed inventory of vendored library files
// whose import block names a network package (net/http, net, net/url). The
// vendored tree holds only the packages the module graph imports, so the ai
// subpackage is absent from vendor/ by construction (nothing may import it,
// and check 1 plus pdfLibForbiddenImport keep it that way); this inventory
// therefore covers the ROOT package, and the ai package is checked by
// absence below.
//
//	pkcs7_timestamp.go: POSTs a signature digest to an RFC 3161 timestamp
//	  server. Unexported (requestTimestamp); reachable only through
//	  SignOptions.TimestampURL, which the symbol table above forbids.
var pdfLibNetworkInventory = []string{
	"pkcs7_timestamp.go",
}

// pdfGuardScannedDirs are the trees the forbidden-symbol scan walks: all
// first-party source. vendor/ is excluded (the library may name its own
// symbols), docs/ is excluded (change orders quote what was rejected), and
// the repo root's guards are listed file by file via the exemption below.
var pdfGuardScannedDirs = []string{"backend", "frontend", "scripts"}

// pdfGuardScannedExts are the file kinds that can carry a reference the
// linker or the browser would follow.
var pdfGuardScannedExts = map[string]bool{
	".go": true, ".js": true, ".ps1": true, ".html": true,
}

// pdfGuardExemptFiles carry a forbidden token because their SUBJECT is the
// forbidden surface. The list is deliberately tiny: an exemption is a hole in
// the guard, so it has to be visible and argued.
var pdfGuardExemptFiles = map[string]string{
	// (none today; the guard itself lives at the repo root, outside the walk)
}

// TestPDFLibraryNetworkSurfaceIsUnreachable is check 1: no first-party source
// references the library's network-capable symbols or imports its ai package.
func TestPDFLibraryNetworkSurfaceIsUnreachable(t *testing.T) {
	var hits []string
	for _, dir := range pdfGuardScannedDirs {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if info.Name() == "node_modules" || info.Name() == "assets" {
					return filepath.SkipDir
				}
				return nil
			}
			if _, exempt := pdfGuardExemptFiles[info.Name()]; exempt {
				return nil
			}
			if !pdfGuardScannedExts[filepath.Ext(info.Name())] {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for lineNo, line := range strings.Split(string(raw), "\n") {
				if strings.Contains(line, pdfLibForbiddenImport) {
					hits = append(hits, fmt.Sprintf("%s:%d: imports %s (the copilot package; nothing may link it): %s",
						path, lineNo+1, pdfLibForbiddenImport, strings.TrimSpace(line)))
				}
				for symbol, why := range pdfLibForbiddenSymbols {
					if !containsToken(line, symbol) {
						continue
					}
					hits = append(hits, fmt.Sprintf("%s:%d: %s (%s): %s",
						path, lineNo+1, symbol, why, strings.TrimSpace(line)))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
	sort.Strings(hits)
	if len(hits) > 0 {
		t.Errorf("the PDF library's network-capable surface is referenced by first-party code.\n"+
			"The local-only guarantee (CLAUDE.md §4) allows network I/O to 127.0.0.1:11434 only,\n"+
			"constructed only by backend/ollama/client.go. Remove every reference below; if a\n"+
			"symbol is a false positive, rename the first-party identifier rather than widening\n"+
			"this guard's exemptions.\n%s", strings.Join(hits, "\n"))
	}
}

// TestPDFLibraryVendoredNetworkInventoryIsUnchanged is check 2: the set of
// vendored library files importing a network package matches the committed
// inventory, and the ai subpackage is not vendored at all.
func TestPDFLibraryVendoredNetworkInventoryIsUnchanged(t *testing.T) {
	vendorRoot := filepath.Join("vendor", filepath.FromSlash(pdfLibImportPath))
	if _, err := os.Stat(vendorRoot); err != nil {
		t.Fatalf("the PDF library is not vendored at %s (%v). The module must be vendored\n"+
			"(go mod vendor) so its exact source is auditable in-tree and this inventory has\n"+
			"something stable to hold to; see CLAUDE.md §7's pin row.", vendorRoot, err)
	}

	// The ai subpackage must be ABSENT: vendor/ holds only what the module
	// graph imports, so its presence means some first-party file imports it.
	if _, err := os.Stat(filepath.Join(vendorRoot, "ai")); err == nil {
		t.Errorf("the library's ai subpackage is vendored at %s/ai, which means something\n"+
			"imports it. Nothing may: it is the copilot package that sends document text to a\n"+
			"configured endpoint. Find and remove the import.", vendorRoot)
	}

	var found []string
	err := filepath.Walk(vendorRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Import-block scan: a quoted network package path on its own import
		// line. Substring on the quoted form is exact enough: "net/http",
		// "net" and "net/url" as full quoted strings.
		for _, netPkg := range []string{`"net/http"`, `"net"`, `"net/url"`} {
			if strings.Contains(string(raw), netPkg) {
				rel, _ := filepath.Rel(vendorRoot, path)
				found = append(found, filepath.ToSlash(rel))
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", vendorRoot, err)
	}
	sort.Strings(found)

	want := append([]string(nil), pdfLibNetworkInventory...)
	sort.Strings(want)
	if strings.Join(found, "\n") != strings.Join(want, "\n") {
		t.Errorf("the vendored PDF library's network surface changed.\n"+
			"Files importing a network package now: [%s]\n"+
			"Committed inventory:                   [%s]\n"+
			"This fires on a version bump that adds or moves network code. Re-review each new\n"+
			"file deliberately, confirm nothing first-party can reach it (extend the forbidden\n"+
			"symbol table if it gained exported levers), update pdfLibNetworkInventory, and\n"+
			"record the review in docs/change-13.md §7 (the findings log).",
			strings.Join(found, ", "), strings.Join(want, ", "))
	}
}

// TestPDFLibraryEnginePathsAreBytesOnly is check 3: under backend/, every
// import of the library uses the fixed alias, and the file-path entry points
// never appear. The engine takes bytes and returns bytes (CLAUDE.md §4), so
// only OpenStream, OpenStreamWithPassword and WriteTo are permitted.
func TestPDFLibraryEnginePathsAreBytesOnly(t *testing.T) {
	var hits []string
	err := filepath.Walk("backend", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(raw)
		if !strings.Contains(src, `"`+pdfLibImportPath+`"`) {
			return nil
		}
		for lineNo, line := range strings.Split(src, "\n") {
			// The import must carry the fixed alias, so the call shapes below
			// have one spelling everywhere.
			if strings.Contains(line, `"`+pdfLibImportPath+`"`) &&
				!strings.Contains(line, pdfLibAlias+` "`+pdfLibImportPath+`"`) {
				hits = append(hits, fmt.Sprintf("%s:%d: import the library as %s (one alias, so this guard's token scan holds): %s",
					path, lineNo+1, pdfLibAlias, strings.TrimSpace(line)))
			}
			// The package-level file-path constructors.
			for _, call := range []string{pdfLibAlias + ".Open(", pdfLibAlias + ".OpenWithPassword("} {
				if strings.Contains(line, call) {
					hits = append(hits, fmt.Sprintf("%s:%d: %s reads a user path inside the engine; use %s.OpenStream on the bytes the App already holds: %s",
						path, lineNo+1, call, pdfLibAlias, strings.TrimSpace(line)))
				}
			}
			// The method-level path writers (Document.Save, Image.Save). A
			// whole-token ".Save(" in a file that imports the library is one
			// of them: nothing else in such a file has a Save method.
			if idx := strings.Index(line, ".Save("); idx >= 0 {
				hits = append(hits, fmt.Sprintf("%s:%d: .Save( writes a file path; use WriteTo on a buffer the caller owns: %s",
					path, lineNo+1, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking backend: %v", err)
	}
	sort.Strings(hits)
	if len(hits) > 0 {
		t.Errorf("the engine's bytes-only boundary is broken:\n%s", strings.Join(hits, "\n"))
	}
}
