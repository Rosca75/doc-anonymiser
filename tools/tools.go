// tools.go — package marker for the audit-tooling module.
//
// This file exists so `tools/` is a valid Go package. It is never compiled
// into the application: the `tools` module is SEPARATE from the doc-anonymiser
// module on purpose (see tools/go.mod for why), so nothing here can reach the
// shipped binary.
//
// The tool dependencies themselves live in the `tool` directive block in
// tools/go.mod, which is the Go 1.24+ replacement for the old
// `//go:build tools` + blank-import trick. Do NOT add blank imports here.
package tools
