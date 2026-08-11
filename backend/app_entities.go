// app_entities.go — bound methods for the Entities screen (Phase 7):
// LLM discovery over selected files, variant expansion for the review
// table, and custom-pattern validation/testing. Thin adapters only
// (CLAUDE.md §3): all logic lives in engine/* and ollama/*.
package backend

import (
	"context"
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/ollama"
)

// runtimeEventsEmit is an indirection over the Wails event runtime so unit
// tests (which have no Wails context) never touch it — emit() already
// guards on a nil ctx, and this var could be swapped in tests if needed.
var runtimeEventsEmit = func(a *App, name string, payload interface{}) {
	runtime.EventsEmit(a.ctx, name, payload)
}

// DiscoveryResult is what RunDiscovery hands back to the UI: merged
// proposals plus a human-readable status (BUILD-02 Phase 7d). A cancelled
// run is NOT an error: the partial proposals are kept and Status explains
// how far the scan got.
type DiscoveryResult struct {
	Proposals []engine.ProposedEntity `json:"proposals"`
	Status    string                  `json:"status"`
	Cancelled bool                    `json:"cancelled"`
}

// DiscoveryEstimate is the pre-run size check for one file (BUILD-02
// Phase 7e, wrapping Phase 5d EstimateChunks). TooLarge files should be
// excluded by the UI before the run starts, never fail mid-run.
type DiscoveryEstimate struct {
	Name     string `json:"name"`
	Chunks   int    `json:"chunks"`
	TooLarge bool   `json:"tooLarge"`
	Message  string `json:"message,omitempty"`
}

// EstimateDiscovery reports the chunk count per named file so the UI can
// warn about (and exclude) oversized documents before a discovery run.
func (a *App) EstimateDiscovery(fileNames []string) []DiscoveryEstimate {
	a.mu.Lock()
	llm := a.llm
	a.mu.Unlock()

	docs := a.docsByName(fileNames)
	out := make([]DiscoveryEstimate, 0, len(docs))
	for _, doc := range docs {
		est := DiscoveryEstimate{Name: doc.Name, Chunks: llm.EstimateChunks(doc.Markdown)}
		if est.Chunks > ollama.MaxChunksPerDocument {
			est.TooLarge = true
			est.Message = fmt.Sprintf(
				"%q is very large (%d chunks); it is excluded from AI discovery. Split it into smaller files or run Smart detection instead.",
				doc.Name, est.Chunks)
		}
		out = append(out, est)
	}
	return out
}

// RunDiscovery executes the Phase-A discovery prompt on the named imported
// files (the user picks representative ones) and returns merged,
// deduplicated proposals for the review table. allowTerms is the current
// session allowlist — allowlisted proposals are vetoed inside the client.
//
// Progress is emitted per file on the "discovery:progress" event so the UI
// can show which file is being scanned. The run is cancellable via
// CancelDiscovery: cancellation between files AND between chunks (the
// client honours ctx) returns the partial proposals with a
// "cancelled after N of M files" status instead of an error.
func (a *App) RunDiscovery(fileNames []string, allowTerms []string) (*DiscoveryResult, error) {
	docs := a.docsByName(fileNames)
	if len(docs) == 0 {
		return nil, fmt.Errorf("no matching imported files to scan, import documents first, then pick at least one for discovery")
	}

	// Wire the allowlist veto exactly once per call: the same allowlist
	// the pipeline will use later (allowlist wins in EVERY pass).
	allow := engine.NewEmptyAllowlist()
	for _, t := range allowTerms {
		allow.Add(t)
	}

	// Hold a cancellable context on the App, exactly like the run
	// pipeline does (BUILD-02 Phase 7d).
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	if a.cancelDiscovery != nil {
		a.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("a discovery run is already in progress, cancel it or wait for it to finish")
	}
	a.cancelDiscovery = cancel
	llm := a.llm
	a.mu.Unlock()
	defer func() {
		cancel()
		a.mu.Lock()
		a.cancelDiscovery = nil
		a.mu.Unlock()
	}()
	llm.Allow = allow.Contains

	var batches [][]engine.ProposedEntity
	completed := 0
	for i, doc := range docs {
		if ctx.Err() != nil {
			break // cancelled between files
		}
		a.emit("discovery:progress", map[string]interface{}{
			"docIndex": i, "docCount": len(docs), "docName": doc.Name,
		})
		proposals, err := llm.Discover(ctx, doc.Markdown)
		// Partial chunk proposals survive a mid-file cancellation.
		if len(proposals) > 0 {
			batches = append(batches, proposals)
		}
		if err != nil {
			if ctx.Err() != nil {
				break // cancelled mid-file; keep what we have
			}
			return nil, fmt.Errorf("discovery failed on %q: %w", doc.Name, err)
		}
		completed++
	}

	res := &DiscoveryResult{Proposals: ollama.MergeProposals(batches...)}
	if ctx.Err() != nil && completed < len(docs) {
		res.Cancelled = true
		res.Status = fmt.Sprintf("cancelled after %d of %d files", completed, len(docs))
	} else {
		res.Status = fmt.Sprintf("scanned %d file(s)", completed)
	}
	return res, nil
}

// CancelDiscovery aborts an in-flight discovery run (between files, or
// mid-chunk via HTTP context cancellation). A no-op when idle. Smart
// detection runs share the same cancellation slot.
func (a *App) CancelDiscovery() {
	a.mu.Lock()
	cancel := a.cancelDiscovery
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// SmartDetectionResult is what RunSmartDetection returns: candidates for
// the review UI (NEVER auto-committed entities, BUILD-02 Phase 9b) plus
// a status line mirroring DiscoveryResult.
type SmartDetectionResult struct {
	Candidates []engine.Candidate `json:"candidates"`
	Status     string             `json:"status"`
	Cancelled  bool               `json:"cancelled"`
}

// RunSmartDetection executes the offline Smart-detection tier over the
// named files (BUILD-02 Phase 8c), mirroring RunDiscovery: per-file
// progress events, cancellable, allowlist from the UI state. When
// classify is true AND the local AI is reachable, candidate categories
// are refined through ClassifyCandidates (span classification: only
// candidate texts and snippets travel to the model, never documents).
// Classification failures degrade to the heuristic categories with a
// status note; they never fail the run (the deterministic tier is the
// whole point of Smart detection).
//
// opts is the BUILD-04 CR13 tuning the Values screen sends. A zero value
// means no filtering, so a caller that has nothing to say still gets the
// pre-BUILD-04 behaviour rather than a surprise.
func (a *App) RunSmartDetection(fileNames []string, allowTerms []string, classify bool, opts engine.SmartDetectOptions) (*SmartDetectionResult, error) {
	docs := a.docsByName(fileNames)
	if len(docs) == 0 {
		return nil, fmt.Errorf("no matching imported files to scan, import documents first, then pick at least one for smart detection")
	}

	allow := engine.NewEmptyAllowlist()
	for _, t := range allowTerms {
		allow.Add(t)
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	if a.cancelDiscovery != nil {
		a.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("a discovery run is already in progress, cancel it or wait for it to finish")
	}
	a.cancelDiscovery = cancel
	llm := a.llm
	a.mu.Unlock()
	defer func() {
		cancel()
		a.mu.Lock()
		a.cancelDiscovery = nil
		a.mu.Unlock()
	}()

	// Per-file smart detection, merged case-sensitively by text: counts
	// add up, contexts cap at 3, and a suffix/title-derived category
	// (client/person) wins over the positional default.
	merged := map[string]*engine.Candidate{}
	var order []string
	completed := 0
	for i, doc := range docs {
		if ctx.Err() != nil {
			break
		}
		a.emit("discovery:progress", map[string]interface{}{
			"docIndex": i, "docCount": len(docs), "docName": doc.Name,
		})
		for _, cand := range engine.SmartDetectWithOptions(doc.Markdown, allow, opts) {
			m, ok := merged[cand.Text]
			if !ok {
				copyCand := cand
				merged[cand.Text] = &copyCand
				order = append(order, cand.Text)
				continue
			}
			m.Count += cand.Count
			// The strongest sighting across documents wins: a name that
			// appears once in one file and next to a legal form in another
			// is as good as the legal-form sighting (BUILD-04 CR13).
			if cand.Confidence > m.Confidence {
				m.Confidence = cand.Confidence
			}
			for _, ctxSnippet := range cand.Contexts {
				if len(m.Contexts) >= 3 {
					break
				}
				m.Contexts = append(m.Contexts, ctxSnippet)
			}
		}
		completed++
	}

	candidates := make([]engine.Candidate, 0, len(order))
	for _, key := range order {
		candidates = append(candidates, *merged[key])
	}

	res := &SmartDetectionResult{Candidates: candidates}
	if ctx.Err() != nil && completed < len(docs) {
		res.Cancelled = true
		res.Status = fmt.Sprintf("cancelled after %d of %d files", completed, len(docs))
		return res, nil
	}
	res.Status = fmt.Sprintf("scanned %d file(s)", completed)

	// Optional AI category refinement (BUILD-02 Phase 8b).
	if classify && len(candidates) > 0 {
		llm.Allow = allow.Contains
		proposals, err := llm.ClassifyCandidates(ctx, candidates)
		if err != nil {
			res.Status += "; AI classification unavailable, heuristic categories kept (" + err.Error() + ")"
		} else {
			refined := map[string]string{}
			for _, p := range proposals {
				refined[p.Text] = p.Category
			}
			for i := range res.Candidates {
				if cat, ok := refined[res.Candidates[i].Text]; ok {
					res.Candidates[i].Category = cat
				}
			}
			res.Status += "; categories refined by the local AI"
		}
	}
	return res, nil
}

// ExpandEntityVariants returns the automatic + manual variants of one
// entity for the expandable variant list in the review table.
func (a *App) ExpandEntityVariants(e engine.Entity) []string {
	return engine.ExpandVariants(e)
}

// SetEntityPlaceholder renames the placeholder one value gets replaced with
// (BUILD-05 Phase 3): the editable field on an entity card in the Identify
// workspace.
//
// It exists because "[CLIENT_1]" is sometimes less useful downstream than a
// name the reader recognises, and the user is the only one who knows which.
// The engine validates the shape and refuses a collision (engine.Registry
// SetPlaceholder); this method only finds the registry and reports back.
//
// The rename takes effect on the NEXT run or fast re-run, not retroactively:
// the anonymised text already on screen was produced with the old placeholder,
// and silently rewriting it here would leave the report and the mapping
// describing text that no longer exists.
//
// @param category the engine category identifier (never a visible label)
// @param canonical the real-world value whose placeholder is being renamed
// @param placeholder the new placeholder, in [NAME_N] form
// @return an actionable error the UI shows verbatim, or nil
func (a *App) SetEntityPlaceholder(category, canonical, placeholder string) error {
	a.mu.Lock()
	reg := a.registry
	a.mu.Unlock()

	if reg == nil {
		return fmt.Errorf(
			"there are no placeholders to rename yet: run the anonymisation once, " +
				"then edit the placeholder of any value it replaced")
	}
	return reg.SetPlaceholder(category, canonical, placeholder)
}

// EntityPlaceholder returns the placeholder currently assigned to one value,
// or "" when it has not been assigned one yet (BUILD-05 Phase 3).
//
// The entity cards render the field read-only-looking-but-empty in that case,
// which is honest: before a run there is nothing to rename.
//
// @param category the engine category identifier
// @param canonical the real-world value
// @return the placeholder, or "" when none is assigned
func (a *App) EntityPlaceholder(category, canonical string) string {
	a.mu.Lock()
	reg := a.registry
	a.mu.Unlock()
	if reg == nil {
		return ""
	}
	placeholder, _ := reg.Lookup(category, canonical)
	return placeholder
}

// --- Phase 5 methods: value management and placeholder editing on step 3 ---

// SetValuePlaceholder renames the placeholder assigned to a value after the
// anonymisation run (BUILD-06 Phase 5, step 3). The user can customize what
// each original value becomes (e.g., changing [ENTITY_1] to [CLIENT_ACME]).
//
// @param placeholder the existing placeholder (e.g., "[ENTITY_1]")
// @param newPlaceholder the new placeholder (e.g., "[CLIENT_ACME]")
// @return an actionable error, or nil on success
func (a *App) SetValuePlaceholder(placeholder, newPlaceholder string) error {
	a.mu.Lock()
	reg := a.registry
	a.mu.Unlock()

	if reg == nil {
		return fmt.Errorf(
			"there are no placeholders to edit yet: run the anonymisation once, " +
				"then edit a placeholder on step 3 (Anonymise)")
	}

	// Use the registry Rename method to validate and apply the change
	return reg.Rename(placeholder, newPlaceholder)
}

// ValuePlaceholders returns all current placeholder assignments:
// the map of placeholder → {original value, category} for display in step 3.
//
// @return map of placeholder to MappingInfo (e.g., {"[ENTITY_1]": {"original": "Acme Corp", "category": "entity_names"}})
func (a *App) ValuePlaceholders() map[string]MappingInfo {
	a.mu.Lock()
	reg := a.registry
	a.mu.Unlock()

	out := map[string]MappingInfo{}
	if reg == nil {
		return out
	}

	for _, e := range reg.Export() {
		out[e.Placeholder] = MappingInfo{Original: e.Original, Category: e.Category}
	}
	return out
}

// RemovedValueInfo is the frontend-facing summary of a removed value.
type RemovedValueInfo struct {
	Original string `json:"original"`
	Category string `json:"category"`
}

// ValidationError is a single validation issue (BUILD-06 Phase 3/4).
type ValidationError struct {
	Kind     string `json:"kind"`     // "duplicate", "collision", "conflict"
	Severity string `json:"severity"` // "block", "warn"
	Message  string `json:"message"`
}

// RemoveValue marks a value as removed so it won't be replaced in future runs
// (BUILD-06 Phase 4/5). The removal uses the registry.Forget mechanism to mark
// it as retired; the user can potentially restore it with RestoreValue.
//
// @param placeholder the placeholder of the value to remove
// @return actionable error, or nil on success
func (a *App) RemoveValue(placeholder string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	reg := a.registry
	if reg == nil {
		return fmt.Errorf("no values to remove yet: run the anonymisation first")
	}

	// Find the original value this placeholder maps to
	entry, ok := reg.PlaceholderOwner(placeholder)
	if !ok {
		return fmt.Errorf("placeholder %q not found in the registry", placeholder)
	}

	// Use registry.Forget to mark as removed (the entry moves to retired)
	// This keeps the placeholder reserved so it won't be reused.
	_, _ = reg.Forget(entry.Category, entry.Original)
	return nil
}

// RestoreValue un-marks a value as removed (BUILD-06 Phase 4/5).
// Currently this is a no-op since removals are not yet persisted in the App.
//
// @param placeholder the placeholder of the value to restore
// @return actionable error, or nil on success
func (a *App) RestoreValue(placeholder string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	reg := a.registry
	if reg == nil {
		return fmt.Errorf("no registry yet")
	}

	// Find the original value
	entry, ok := reg.PlaceholderOwner(placeholder)
	if !ok {
		return fmt.Errorf("placeholder %q not found", placeholder)
	}

	// TODO Phase 8: implement restoration from retired list
	// For now, this is a placeholder that will be implemented when session
	// persistence is added.
	_ = entry
	return fmt.Errorf("value restoration will be implemented in Phase 8")
}

// ListRemovedValues returns all currently removed values so the user can
// restore them if needed (BUILD-06 Phase 4/5).
//
// @return slice of {original, category} for each retired/removed value
func (a *App) ListRemovedValues() []RemovedValueInfo {
	a.mu.Lock()
	defer a.mu.Unlock()

	reg := a.registry
	if reg == nil {
		return []RemovedValueInfo{}
	}

	// Get the list of retired placeholders from the registry
	retired := reg.Retired()
	if len(retired) == 0 {
		return []RemovedValueInfo{}
	}

	out := make([]RemovedValueInfo, 0, len(retired))
	for _, placeholder := range retired {
		entry, ok := reg.PlaceholderOwner(placeholder)
		if ok {
			out = append(out, RemovedValueInfo{
				Original: entry.Original,
				Category: entry.Category,
			})
		}
	}
	return out
}

// NextRulePlaceholder returns the next available placeholder number for
// simple rules, avoiding collisions with auto-assigned placeholders
// (BUILD-06 Phase 5). Allows the user to mint new placeholders for
// find-and-replace rules without accidentally reusing a number.
//
// @param category the category name (e.g., "email")
// @return the next placeholder number (e.g., 7 if [EMAIL_1] through [EMAIL_6] are taken)
func (a *App) NextRulePlaceholder(category string) int {
	a.mu.Lock()
	reg := a.registry
	a.mu.Unlock()

	if reg == nil {
		return 1
	}

	// Get all reserved numbers for this category
	reserved := reg.Reserved()
	next := 1
	usedInCategory := make(map[int]bool)

	for _, r := range reserved {
		// Parse placeholders like "[EMAIL_5]" or "[ENTITY_3]"
		var cat string
		var num int
		if n, _ := fmt.Sscanf(r, "[%[^_]_%d]", &cat, &num); n == 2 && cat == category {
			usedInCategory[num] = true
		}
	}

	// Also check all current entries in the registry
	for _, entry := range reg.Entries() {
		// Extract number from placeholder like [EMAIL_5]
		var cat string
		var num int
		if n, _ := fmt.Sscanf(entry.Placeholder, "[%[^_]_%d]", &cat, &num); n == 2 && cat == category {
			usedInCategory[num] = true
		}
	}

	// Find the next available number
	for {
		if !usedInCategory[next] {
			return next
		}
		next++
	}
}

// ValidateValuesRequest is the input for ValidateValues (BUILD-06 Phase 5).
type ValidateValuesRequest struct {
	Entities   []engine.Entity        `json:"entities"`
	Patterns   []engine.CustomPattern `json:"patterns"`
	Rules      []engine.SimpleRule    `json:"rules"`
	AllowTerms []string               `json:"allowTerms"`
}

// ValidateValuesResult is the output from ValidateValues.
type ValidateValuesResult struct {
	Blocking []ValidationError `json:"blocking"`
	Warnings []ValidationError `json:"warnings"`
}

// ValidateValues checks the current entities, patterns and rules for conflicts
// before running the pipeline (BUILD-06 Phase 3/5). Returns blocking errors
// (which prevent the run) and warnings (informational only).
//
// @param req the validation request with entities, patterns, rules and allowlist
// @return blocking errors (must be resolved before running) and warnings
func (a *App) ValidateValues(req ValidateValuesRequest) (*ValidateValuesResult, error) {
	a.mu.Lock()
	reg := a.registry
	a.mu.Unlock()

	// Build the allowlist from the request
	allowlist := engine.NewEmptyAllowlist()
	for _, t := range req.AllowTerms {
		allowlist.Add(t)
	}

	// Run the engine's validation
	result := engine.ValidateValues(engine.ValidationInput{
		Entities:       req.Entities,
		Patterns:       req.Patterns,
		SimpleRules:    req.Rules,
		Allowlist:      allowlist,
		Categories:     nil,
		Registry:       reg,
		SkipValidation: false,
	})

	// Convert to frontend-friendly format
	blocking := make([]ValidationError, len(result.Blocking))
	for i, c := range result.Blocking {
		blocking[i] = ValidationError{
			Kind:     c.Kind,
			Severity: c.Severity,
			Message:  c.Message,
		}
	}

	warnings := make([]ValidationError, len(result.Warnings))
	for i, c := range result.Warnings {
		warnings[i] = ValidationError{
			Kind:     c.Kind,
			Severity: c.Severity,
			Message:  c.Message,
		}
	}

	return &ValidateValuesResult{
		Blocking: blocking,
		Warnings: warnings,
	}, nil
}

// TermMatchInfo is the live manual-entry preview payload (BUILD-02
// Phase 9c): how often a term occurs, and in how many documents.
type TermMatchInfo struct {
	Count     int `json:"count"`
	Documents int `json:"documents"`
}

// CountTermMatches counts case-insensitive word-boundary occurrences of
// term across every loaded document, for the "Found N times in M
// documents" preview under the manual add-entity inputs.
func (a *App) CountTermMatches(term string) TermMatchInfo {
	a.mu.Lock()
	docs := a.docs
	a.mu.Unlock()

	info := TermMatchInfo{}
	for _, doc := range docs {
		if n := engine.CountTermMatches(doc.Markdown, term); n > 0 {
			info.Count += n
			info.Documents++
		}
	}
	return info
}

// ValidatePattern compile-checks a user regex. It returns the error as a
// STRING ("" = valid) instead of a Go error, because a validation failure
// is expected feedback for the live checker, not a rejected promise.
func (a *App) ValidatePattern(expr string) string {
	if err := engine.ValidateCustomPattern(expr); err != nil {
		return err.Error()
	}
	return ""
}

// PatternMatches runs a (valid) custom pattern over every loaded document
// and returns up to 20 sample matches for the tester UI.
func (a *App) PatternMatches(expr string) ([]string, error) {
	if err := engine.ValidateCustomPattern(expr); err != nil {
		return nil, err
	}
	a.mu.Lock()
	docs := a.docs
	a.mu.Unlock()

	const maxSamples = 20
	var samples []string
	seen := map[string]bool{}
	for _, doc := range docs {
		spans := engine.DetectCustomPatterns(doc.Markdown, []engine.CustomPattern{{Expr: expr}}, nil)
		for _, s := range spans {
			if seen[s.Original] {
				continue // show each distinct match once
			}
			seen[s.Original] = true
			samples = append(samples, s.Original)
			if len(samples) >= maxSamples {
				return samples, nil
			}
		}
	}
	return samples, nil
}

// docsByName returns the loaded documents matching the given names, in
// request order. Unknown names are silently skipped (the UI list and the
// Go list can only diverge for a moment during removal).
func (a *App) docsByName(names []string) []engine.Document {
	a.mu.Lock()
	defer a.mu.Unlock()
	byName := map[string]engine.Document{}
	for _, d := range a.docs {
		byName[d.Name] = d
	}
	var out []engine.Document
	for _, n := range names {
		if d, ok := byName[n]; ok {
			out = append(out, d)
		}
	}
	return out
}

// emit fires a frontend event, tolerating the headless case (a.ctx is nil
// in unit tests — events are UI sugar, never load-bearing).
func (a *App) emit(name string, payload interface{}) {
	if a.ctx == nil {
		return
	}
	runtimeEventsEmit(a, name, payload)
}
