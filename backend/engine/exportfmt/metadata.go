// engine/exportfmt/metadata.go — OOXML document-properties extraction and
// rewrite (BUILD-02 Phase 12): docProps/core.xml (title, subject,
// creator, lastModifiedBy, keywords, description), docProps/app.xml
// (Company, Manager) and docProps/custom.xml STRING properties.
//
// Nothing is rewritten silently: extraction feeds the review panel,
// proposals run through the exact same pipeline path as body text
// (Config.AnonymiseText: deterministic passes + registry + allowlist),
// and only the values the user reviewed come back through
// RewriteMetadata. Non-string custom properties are never touched.
package exportfmt

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

// MetaField is one document property: Part is the archive entry, Name the
// field identifier (element local name, or the property name for
// custom.xml), Value its current text.
type MetaField struct {
	Part  string `json:"part"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// MetaProposal pairs a field with its pipeline-proposed replacement.
// Changed is false when the pipeline left the value alone (the review
// panel shows those rows as "unchanged").
type MetaProposal struct {
	MetaField
	Proposed string `json:"proposed"`
	Changed  bool   `json:"changed"`
}

// coreFields / appFields are the harvested element local names per part.
var coreFields = map[string]bool{
	"title": true, "subject": true, "creator": true,
	"lastModifiedBy": true, "keywords": true, "description": true,
}
var appFields = map[string]bool{"Company": true, "Manager": true}

// metaParts lists the property parts, in stable review order.
var metaParts = []string{"docProps/core.xml", "docProps/app.xml", "docProps/custom.xml"}

// ExtractMetadata harvests the reviewable properties from an OOXML file
// (docx/pptx/xlsx alike; they share the docProps layout). Missing parts
// simply yield no fields.
func ExtractMetadata(raw []byte) ([]MetaField, error) {
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("the original file could not be opened as a zip archive (%v); re-import the document and try again", err)
	}
	var out []MetaField
	for _, part := range metaParts {
		for _, entry := range reader.File {
			if entry.Name != part {
				continue
			}
			src, err := entry.Open()
			if err != nil {
				return nil, fmt.Errorf("could not read %q: %w", part, err)
			}
			data, err := io.ReadAll(src)
			src.Close()
			if err != nil {
				return nil, fmt.Errorf("could not read %q: %w", part, err)
			}
			fields, err := extractPartFields(part, data)
			if err != nil {
				return nil, err
			}
			out = append(out, fields...)
		}
	}
	return out, nil
}

// ProposeMetadata runs every field through the body pipeline's text path
// (registry, allowlist and all) and marks which values would change.
func ProposeMetadata(fields []MetaField, cfg Config) []MetaProposal {
	out := make([]MetaProposal, 0, len(fields))
	for _, f := range fields {
		proposed, n := cfg.AnonymiseText(f.Value)
		out = append(out, MetaProposal{MetaField: f, Proposed: proposed, Changed: n > 0})
	}
	return out
}

// RewriteMetadata returns a new archive with the reviewed field values
// applied. Only fields present in reviewed are touched; everything else
// (including non-string custom properties) passes through byte-for-byte.
func RewriteMetadata(raw []byte, reviewed []MetaField) ([]byte, error) {
	byPart := map[string]map[string]string{}
	for _, f := range reviewed {
		if byPart[f.Part] == nil {
			byPart[f.Part] = map[string]string{}
		}
		byPart[f.Part][f.Name] = f.Value
	}
	if len(byPart) == 0 {
		return raw, nil
	}
	return rewriteZip(raw, func(name string) RewriteFunc {
		values, ok := byPart[name]
		if !ok {
			return nil
		}
		partName := name
		return func(data []byte) ([]byte, error) {
			return rewritePartFields(partName, data, values)
		}
	})
}

// isMetaElement reports whether an element in the given part is a
// harvestable field, returning its field name.
func metaFieldName(part string, elem xml.Name, propertyName string) (string, bool) {
	switch part {
	case "docProps/core.xml":
		if coreFields[elem.Local] {
			return elem.Local, true
		}
	case "docProps/app.xml":
		if appFields[elem.Local] {
			return elem.Local, true
		}
	case "docProps/custom.xml":
		// Only the STRING payload of a named property qualifies.
		if elem.Local == "lpwstr" || elem.Local == "lpstr" {
			return propertyName, propertyName != ""
		}
	}
	return "", false
}

// extractPartFields token-scans one properties part.
func extractPartFields(part string, data []byte) ([]MetaField, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var out []MetaField
	propertyName := "" // current custom.xml property name
	fieldName := ""
	var textBuf bytes.Buffer
	collecting := false

	for {
		tok, err := decoder.RawToken()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%s is not well-formed XML (%v)", part, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "property" {
				propertyName = ""
				for _, a := range t.Attr {
					if a.Name.Local == "name" {
						propertyName = a.Value
					}
				}
			}
			if name, ok := metaFieldName(part, t.Name, propertyName); ok {
				collecting = true
				fieldName = name
				textBuf.Reset()
			}
		case xml.CharData:
			if collecting {
				textBuf.Write(t)
			}
		case xml.EndElement:
			if collecting && (metaLocalMatches(part, t.Name.Local)) {
				out = append(out, MetaField{Part: part, Name: fieldName, Value: textBuf.String()})
				collecting = false
			}
		}
	}
	return out, nil
}

// metaLocalMatches reports whether an end element closes a harvested field.
func metaLocalMatches(part, local string) bool {
	switch part {
	case "docProps/core.xml":
		return coreFields[local]
	case "docProps/app.xml":
		return appFields[local]
	case "docProps/custom.xml":
		return local == "lpwstr" || local == "lpstr"
	}
	return false
}

// rewritePartFields splices new values into the harvested elements of one
// part, leaving everything else byte-identical (same offset-splicing
// technique as the body rewriter).
func rewritePartFields(part string, data []byte, values map[string]string) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var splices []splice
	propertyName := ""
	prevOffset := int64(0)
	collecting := false
	fieldName := ""
	contentStart := 0

	for {
		tok, err := decoder.RawToken()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%s is not well-formed XML (%v)", part, err)
		}
		offset := decoder.InputOffset()

		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "property" {
				propertyName = ""
				for _, a := range t.Attr {
					if a.Name.Local == "name" {
						propertyName = a.Value
					}
				}
			}
			if name, ok := metaFieldName(part, t.Name, propertyName); ok {
				if _, wanted := values[name]; wanted {
					collecting = true
					fieldName = name
					contentStart = int(offset)
				}
			}
		case xml.EndElement:
			if collecting && metaLocalMatches(part, t.Name.Local) {
				contentEnd := int(prevOffset)
				if contentEnd < contentStart {
					contentEnd = contentStart // self-closing element
				}
				var escaped bytes.Buffer
				if err := xml.EscapeText(&escaped, []byte(values[fieldName])); err == nil {
					splices = append(splices, splice{rawStart: contentStart, rawEnd: contentEnd, content: escaped.Bytes()})
				}
				collecting = false
			}
		}
		prevOffset = offset
	}

	out := append([]byte{}, data...)
	for i := len(splices) - 1; i >= 0; i-- {
		s := splices[i]
		out = append(out[:s.rawStart], append(append([]byte{}, s.content...), out[s.rawEnd:]...)...)
	}
	return out, nil
}
