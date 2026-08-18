// icon_parity_test.go — every icon the frontend asks for exists, and every icon
// it holds is drawn
//
// The bug this exists to prevent already happened once, and it made EVERY help
// tooltip in the application invisible.
//
// `ui.js helpTooltip()` renders `icon("info")`, `frontend/icons.js ICONS` had no
// "info" key, and `ui.js icon(name)` returns the EMPTY STRING for a name it does
// not know (deliberately: a missing glyph must never break a button render). So
// every help trigger was a 1.15rem hit area with nothing in it. The tooltip
// machinery worked exactly as designed and there was nothing on screen to hover.
// The frontend suite passed throughout, because it asserts the WRAPPER element
// (`<button class="help-icon"`), and an empty button matches that.
//
// The same silence hid the Compare search's previous/next chevrons.
//
// So this guard holds the two lists to each other in both directions:
//
//	a name used but not in ICONS   a control that renders with no glyph
//	an ICONS entry used nowhere    dead vendored markup, shipped in the binary
//
// The second direction matters because the audit layer cannot see it: the
// dead-export scanner reads `import`/`export` statements and an icon is a KEY
// INSIDE an object literal, not an export.
//
// It also holds `frontend/assets/icons/<name>.svg` to the map, which is what
// icons.js's own header promises: the folder is the provenance record for the
// vendored Apache-2.0 markup, and a map entry with no source file makes it a
// record of only some of what shipped.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const iconsPath = "frontend/icons.js"

// iconsKeyRe matches one entry of the ICONS object literal. The keys are quoted
// and one per line, which is the declaration style this guard depends on.
var iconsKeyRe = regexp.MustCompile(`(?m)^\s{2}"([a-z0-9_]+)":\s*` + "`")

// iconCallRe matches `icon("name")`, the direct call form.
var iconCallRe = regexp.MustCompile(`\bicon\(\s*"([a-zA-Z0-9_]+)"\s*\)`)

// iconOptionRe matches the `icon:` option of ui.js button() and friends, and
// captures everything up to the end of that option's value. The value is not
// always a plain literal: at least one call site chooses between two glyphs with
// a ternary, so the names are pulled out of the captured slice rather than
// assumed to be the whole of it.
var iconOptionRe = regexp.MustCompile(`\bicon:\s*([^,}\n]+)`)

// quotedNameRe pulls the string literals out of one such option value.
var quotedNameRe = regexp.MustCompile(`"([a-zA-Z0-9_]+)"`)

// iconsInMap returns every key of ICONS, in the order the file declares them.
func iconsInMap(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(iconsPath)
	if err != nil {
		t.Fatalf("could not read %s: %v", iconsPath, err)
	}
	var keys []string
	for _, m := range iconsKeyRe.FindAllStringSubmatch(string(raw), -1) {
		keys = append(keys, m[1])
	}
	if len(keys) == 0 {
		t.Fatalf("found no `\"name\": `<svg...` entries in %s. This guard cannot work: "+
			"fix the parser or the declaration style.", iconsPath)
	}
	return keys
}

// iconUse is one place a name is asked for, kept with its location so a failure
// points somewhere openable.
type iconUse struct {
	name string
	file string
	line int
}

// iconNamesUsed collects every name the shipped frontend passes to icon() or to
// an `icon:` option. Test files are excluded: ui.test.js deliberately asks for a
// name that does not exist, to pin that icon() fails soft rather than throwing.
func iconNamesUsed(t *testing.T) []iconUse {
	t.Helper()
	var uses []iconUse
	for _, path := range frontendSourceFiles(t) {
		if path == iconsPath {
			continue // the map itself declares names, it does not use them
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("could not read %s: %v", path, err)
		}
		source := string(raw)

		for _, m := range iconCallRe.FindAllStringSubmatchIndex(source, -1) {
			uses = append(uses, iconUse{source[m[2]:m[3]], path, lineOf(source, m[0])})
		}
		for _, m := range iconOptionRe.FindAllStringSubmatchIndex(source, -1) {
			value := source[m[2]:m[3]]
			for _, q := range quotedNameRe.FindAllStringSubmatch(value, -1) {
				uses = append(uses, iconUse{q[1], path, lineOf(source, m[0])})
			}
		}
	}
	if len(uses) == 0 {
		t.Fatal("found no icon() calls or `icon:` options in the frontend. This guard cannot " +
			"work: fix the patterns at the top of icon_parity_test.go.")
	}
	return uses
}

// TestEveryIconNameUsedExistsInTheMap is the direction that failed silently:
// icon() returns "" for an unknown name, so the control renders and shows nothing.
func TestEveryIconNameUsedExistsInTheMap(t *testing.T) {
	have := map[string]bool{}
	for _, k := range iconsInMap(t) {
		have[k] = true
	}

	// One message per missing NAME, not per call site: a glyph added once fixes
	// every use of it, and a wall of near-identical failures gets skimmed.
	missing := map[string][]string{}
	for _, u := range iconNamesUsed(t) {
		if have[u.name] {
			continue
		}
		missing[u.name] = append(missing[u.name], fmt.Sprintf("%s:%d", u.file, u.line))
	}

	names := make([]string, 0, len(missing))
	for name := range missing {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		sort.Strings(missing[name])
		t.Errorf("icon %q is used at %s but %s has no entry for it.\n"+
			"ui.js icon() returns the EMPTY STRING for an unknown name, so that control renders "+
			"with no glyph and nothing fails. Vendor frontend/assets/icons/%s.svg from Material "+
			"Symbols Outlined (Apache-2.0, the licence already covers the folder) and add the "+
			"entry to ICONS in alphabetical position, fill=\"currentColor\", "+
			"viewBox=\"0 -960 960 960\".",
			name, strings.Join(missing[name], ", "), iconsPath, name)
	}
}

// TestEveryIconInTheMapIsDrawn is the other direction: markup vendored into the
// binary that no screen renders. The audit layer's dead-export scanner cannot see
// this, because an icon is a key inside an object literal rather than an export.
func TestEveryIconInTheMapIsDrawn(t *testing.T) {
	used := map[string]bool{}
	for _, u := range iconNamesUsed(t) {
		used[u.name] = true
	}

	for _, name := range iconsInMap(t) {
		if used[name] {
			continue
		}
		t.Errorf("%s holds icon %q and nothing renders it.\n"+
			"That SVG is compiled into the shipped binary for nothing. Either draw it, or delete "+
			"the entry and frontend/assets/icons/%s.svg with it. An icon kept \"in case\" is "+
			"markup nobody can account for.",
			iconsPath, name, name)
	}
}

// TestEveryIconHasItsVendoredSourceFile keeps frontend/assets/icons/ the record
// icons.js's header says it is: one <name>.svg per map entry, and nothing else.
//
// The folder is the provenance of the vendored Apache-2.0 markup. A map entry
// with no file there makes it a record of only part of what ships, and a file
// with no map entry is a leftover from a deletion that stopped halfway.
func TestEveryIconHasItsVendoredSourceFile(t *testing.T) {
	const dir = "frontend/assets/icons"

	onDisk := map[string]bool{}
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".svg") {
			return nil
		}
		onDisk[strings.TrimSuffix(d.Name(), ".svg")] = true
		return nil
	})
	if err != nil {
		t.Fatalf("could not walk %s: %v", dir, err)
	}

	inMap := map[string]bool{}
	for _, name := range iconsInMap(t) {
		inMap[name] = true
		if !onDisk[name] {
			t.Errorf("%s holds icon %q but %s/%s.svg does not exist.\n"+
				"That folder is the provenance record for the vendored Apache-2.0 markup "+
				"(see the header of %s). Write the same SVG there under that name.",
				iconsPath, name, dir, name, iconsPath)
		}
	}
	for _, name := range sortedKeys(onDisk) {
		if !inMap[name] {
			t.Errorf("%s/%s.svg exists but %s has no entry for it.\n"+
				"Nothing reads the .svg files at runtime, so this one ships with the binary and "+
				"draws nothing. Add the entry, or delete the file.", dir, name, iconsPath)
		}
	}
}
