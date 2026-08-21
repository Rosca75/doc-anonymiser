// app.go — the Wails bound struct. This is the ONLY seam between the
// frontend and Go: every method here must be a thin adapter that delegates
// straight to engine/* or ollama/* (CLAUDE.md §3 — no business logic in
// this file). The frontend calls these methods exclusively through
// frontend/api.js.
//
// app.go is also the only place allowed to touch the filesystem paths the
// user picks (dialogs, drag-drop): it reads the bytes ONCE and hands them
// to the engine — the engine itself never sees a path (CLAUDE.md §4), and
// nothing is ever written back to a source file ("originals are
// immutable").
package backend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/engine/imaging"
	"doc-anonymiser/backend/ollama"
)

// Settings are the user-tweakable options (the Identify rail): which preset
// each chip row is on, the granular category switches, Ollama port (host locked
// to loopback) and the model name (never hardcoded outside this default —
// CLAUDE.md §7).
//
// Wails bridge payload shape (matched by state.js settings):
//
//	{ "presets": {"patterns.depth": "standard", "names.depth": "standard"},
//	  "categories": {"email": true, ...},
//	  "ollamaPort": 11434, "model": "qwen3.5:0.8b" }
type Settings struct {
	// Presets records which chip each preset row is on, keyed
	// "<scope>.<family>" (engine.PresetKey). A row with NO key reads as Custom,
	// which is how a selection matching no preset is representable.
	//
	// It is a RECORD of the rail, never an instruction: Categories below is what
	// the pipeline obeys, and both sides derive the presets from it
	// (engine.MatchingPresets, state.js activePreset), so the two cannot
	// disagree about what the next run will do. Storing it at all is what lets a
	// saved session say which preset the user was on rather than only which
	// checkboxes were ticked.
	Presets map[string]string `json:"presets"`
	// Categories is the granular switch set the pipeline obeys
	// nil/empty means "use engine.DefaultSelection(Country)".
	Categories engine.CategorySelection `json:"categories"`
	OllamaPort int                      `json:"ollamaPort"` // loopback port only
	Model      string                   `json:"model"`      // Ollama model name
	// ContextSize is the Ollama num_ctx option.
	// Default 8192; 0 keeps the model default. Higher values let the model
	// read longer documents at once but use more memory.
	ContextSize int `json:"contextSize"`
	// Country scopes the country-specific built-in pattern categories. It is an
	// engine-owned concept (engine/country.go), mirrored in the rail.
	Country string `json:"country"`
	// UseLocalLLM is the Local LLM discovery ROUTE switch: OFF by default, and
	// gated on the live Ollama availability as well, so a stale true can never
	// start a model that is not there.
	//
	// It is a setting rather than a per-call argument so it survives in the
	// session file and so Go, not the frontend, decides whether a route runs.
	UseLocalLLM bool `json:"useLocalLLM"`
	// LLMStrictFormat asks the local model's discovery call for a schema-constrained
	// reply instead of loose JSON mode: it makes the model answer for every
	// category rather than only the ones it thought of.
	//
	// It is the user's choice because the measurement has no single winner: the
	// schema finds a little more on a short dense page and on small documents, and
	// on a slide-heavy document it costs about twice the time for no more values,
	// while on a small model it finds nothing at all. Default OFF, which is the
	// fast end.
	//
	// A POINTER for the reason UseBuiltInPatterns is one: "absent" and "the user
	// switched it off" must stay distinguishable across a session file. Here nil
	// reads as off, which is the default, so nothing is lost by silence.
	LLMStrictFormat *bool `json:"llmStrictFormat"`
	// LLMDetailLevel is how much text one local model request carries
	// (engine.DetailThorough or engine.DetailFaster).
	//
	// It is a setting rather than a constant because the measurement has no
	// single winner: small slices find the most and send the most requests, and
	// large ones can miss a document's names entirely on a small model. Where
	// that line falls depends on the model, which is the user's choice. The
	// engine owns the sizes (engine.ScanTargetBytes); this field is only which of
	// them a run asks for.
	//
	// An empty string is read as thorough, so a payload that says nothing lands
	// on the safe end: a level nobody chose must not be the one that finds less.
	LLMDetailLevel string `json:"llmDetailLevel"`
	// UseBuiltInPatterns and UseHeuristicDiscovery are two of the offline
	// three methods, controlled independently.
	//
	// UseBuiltInPatterns is the MASTER over the structured signal categories
	// (email, VAT, IBAN, amount, date, ...). OFF means pass 1 is skipped and no
	// signal category is replaced at anonymisation time, whatever the
	// per-category checkboxes say; the checkboxes keep their selection so turning
	// it back on restores exactly what was chosen. ON by default: the
	// deterministic pass is what most documents need.
	//
	// UseHeuristicDiscovery is heuristic discovery: spelling, context, frequency
	// and deterministic gazetteers. ON by default; it needs nothing installed.
	//
	// There is deliberately no fourth boolean summarising the routes.
	// The section is on when any of its methods is on, so the section switch is a
	// UI master that changes these settings in one action rather than a state of
	// its own that could disagree with them.
	UseBuiltInPatterns    bool `json:"useBuiltInPatterns"`
	UseHeuristicDiscovery bool `json:"useHeuristicDiscovery"`
	// SignalSuggestionSources drives signal-based discovery: which readings of
	// which built-in signals may DERIVE Suggestions (engine/signals.go). It is
	// keyed by source and then by derivation, because one signal supports several
	// readings through several mechanisms and each is switched on its own.
	//
	// It does NOT govern whether those signals are matched and replaced. Clearing
	// any reading here stops the Suggestions that reading produces and leaves email
	// anonymisation exactly as it was, which is the whole reason the setting is
	// separate from the category switch.
	SignalSuggestionSources engine.SignalSourceSelection `json:"signalSuggestionSources"`
	// RequireChecksum is the "Only replace when the checksum matches" switch,
	// inside Built-in patterns. Off by default, which is what the application has
	// always done: some identifiers carry a check digit, and when the digits do
	// not add up the match is kept, because a mistyped or partly redacted bank
	// identifier is still one. On, engine.RejectFailedChecksums drops those
	// matches, and nothing else.
	//
	// It needs no range validation, which is the point of a boolean: the floor it
	// replaces had two positions nobody could name and one of them dropped Values
	// the user had already accepted.
	RequireChecksum bool `json:"requireChecksum"`
	// HeuristicDiscovery is the heuristic tuning. A SETTING rather than a
	// per-run argument so it survives in the session file.
	HeuristicDiscovery engine.HeuristicDiscoveryOptions `json:"heuristicDiscovery"`
}

// DocumentInfo is the frontend-facing summary of one loaded Document.
// The raw bytes stay in Go; the frontend only ever needs metadata and the
// markdown working form for the preview pane.
type DocumentInfo struct {
	Name      string   `json:"name"`
	Format    string   `json:"format"`
	SizeBytes int      `json:"sizeBytes"`
	Warnings  []string `json:"warnings"`
	Markdown  string   `json:"markdown"`
	// Experimental marks formats with an EXPERIMENTAL badge in the UI
	// (currently PDF only, CLAUDE.md §5).
	Experimental bool `json:"experimental"`
	// IsGrid is true for CSV-like documents (CSV, flat xlsx sheet) whose
	// preview renders as a table and whose export offers CSV round-trip.
	IsGrid bool `json:"isGrid"`
	// PreviewTruncated is true when Markdown holds only the first
	// engine.MaxPreviewLines lines of a very large document — the FULL
	// content is still processed; only this preview copy is cut.
	PreviewTruncated bool `json:"previewTruncated"`
	// UnitCount and Unit are the document's size in its OWN terms, for the
	// import list: "6 pages", "12 slides", "48 rows",
	// "412 lines". Unit is singular ("page"); the frontend pluralises,
	// because only the side printing the number knows which form it needs.
	//
	// A byte size does not tell a user whether they picked the right file. A
	// page count does. See engine.Document for why "line" is the common
	// fallback rather than an error case.
	UnitCount int    `json:"unitCount"`
	Unit      string `json:"unit"`
	// PageCount is how many addressable sub-units (pages/slides/rows/lines)
	// the local model can be scoped to (CLAUDE.md §5). It can differ from
	// UnitCount: a DOCX reports its cached page count for the import list but
	// can only be sliced where Word left break markers, so a document with no
	// finer boundary than itself reports 1 here. The frontend uses it to size
	// the page-range control in the Local LLM discovery section.
	PageCount int `json:"pageCount"`
}

// ImportResult reports one import action: what loaded and what failed.
// Per-file failures never abort the rest of the batch.
type ImportResult struct {
	Documents []DocumentInfo `json:"documents"` // full post-import list
	Errors    []string       `json:"errors"`    // one message per failed file
}

// App is bound to the frontend by main.go. Its exported methods become
// window.go.main.App.<Method> in JavaScript.
type App struct {
	// ctx is the Wails runtime context, stored at startup so methods can
	// open native dialogs and emit events.
	ctx context.Context
	// llm is the Ollama client, used ONLY by the Identify-time Local LLM
	// discovery route. Anonymise never touches it: a run that could reach the
	// model could mint a value the user never reviewed. engine/* never sees the
	// concrete client (CLAUDE.md §4, one-file external boundary).
	llm *ollama.Client

	// mu guards the mutable session state below (the pipeline runs in a
	// goroutine while the UI keeps calling methods).
	mu       sync.Mutex
	docs     []engine.Document // loaded documents, in import order
	settings Settings
	// registry is the session placeholder registry — ONE per session so
	// the same entity maps to the same placeholder across runs
	// (CLAUDE.md §5); created lazily on the first run.
	registry *engine.Registry
	// results of the latest pipeline run (feeds the results view and the
	// exports).
	results *engine.Results
	// running / cancelRun manage the in-flight pipeline goroutine.
	running   bool
	cancelRun context.CancelFunc
	// cancelDetection is the ONE cancellation slot every detection route shares,
	// so a single Cancel reaches whichever one is running.
	cancelDetection context.CancelFunc
	// lastReq remembers the latest pipeline inputs so the same-format
	// export reproduces identical replacements.
	lastReq *RunRequest
	// removed holds the values the user deleted from the session
	// It lives on the App rather than travelling in RunRequest for
	// two reasons: the prune and the exclusion are two halves of one action
	// that must not be able to happen separately, and the same-format export
	// builds its own allowlist from a.lastReq, so a removal carried only in the
	// request would be honoured by the pipeline and forgotten by the export.
	// Settings.UseLocalLLM is the precedent:  moved that decision into Go for
	// exactly this reason.
	//
	// It is deliberately NOT the allowlist, in state or in the session file: a
	// removed value must not appear as a term on the Allow tab, and "undo the
	// removal" must not be the same gesture as "delete an allowlist term". What
	// it shares with the allowlist is the ENFORCEMENT (allowlistFor folds it in),
	// because Allowlist.Contains is the single veto every span producer already
	// consults and a second veto is a seventh caller somebody forgets.
	removed []engine.RemovedValue
	// definedTerms is the vocabulary the IMPORTED DOCUMENTS declare about
	// themselves: the phrases a contract introduces as `"Work Order" means ...`
	// or `(the "Dedicated Advisors")`. A defined term is the strongest "do not
	// anonymise" signal a document can offer, because the document says the
	// phrase is part of its own machinery.
	//
	// It lives here, beside a.removed, for the reason a.removed lives here: it is
	// ENFORCED through allowlistFor, so detection, the run and every export
	// cannot disagree about it. And it is a SEPARATE list from the user's
	// never-anonymise terms, so "stop suppressing this term" is not the same
	// gesture as "delete a term I typed".
	//
	// It is filled by detection and dropped wherever the documents behind it can
	// change, because a term read out of a file the user has replaced would go on
	// suppressing a value that file no longer defines.
	definedTerms []engine.DefinedTerm
	// imageScans caches one image inventory per imported document
	// (app_images.go). The Anonymise step's IMAGE half asks on every repaint,
	// and re-walking a sixty-slide archive per repaint is a cost the user feels.
	//
	// It is keyed by document NAME because that is what the frontend holds, and
	// it is dropped by every path that can change the bytes behind it: a new
	// import, a removal, and both resets. A cached scan that outlived its
	// document would list the pictures of a file the user has replaced.
	imageScans map[string]imaging.Inventory
	// imageDecisions is what the user decided about each picture: document name,
	// then asset ID. A KEEP is stored as an ABSENT key, so the map holds only
	// what the user changed and "nothing was decided" is one empty map rather
	// than one entry per picture.
	//
	// It lives on the App for the reason a.removed does: the same-format export
	// builds its own config from a.lastReq, so a decision carried only in a
	// request would be honoured by one export path and forgotten by the other.
	imageDecisions map[string]map[string]imaging.Decision
}

// allowlistFor builds the allowlist every pass and every export must obey: the
// user's never-anonymise terms, plus the main text and spellings of every
// removed value (engine.ApplyRemovals), plus the terms the documents define
// about themselves (engine.ApplyDefinedTerms).
//
// It exists so a removal cannot be honoured by the run and forgotten by the
// export. Every caller that used to build its own `NewEmptyAllowlist()` and add
// the terms goes through here instead; a new one that does not is the bug this
// helper is shaped to make obvious.
//
// @param terms the never-anonymise terms from the request
// @return an allowlist ready to hand to the engine, never nil
func (a *App) allowlistFor(terms []string) *engine.Allowlist {
	allow := engine.NewEmptyAllowlist()
	for _, t := range terms {
		allow.Add(t)
	}
	a.mu.Lock()
	removed := make([]engine.RemovedValue, len(a.removed))
	copy(removed, a.removed)
	defined := make([]engine.DefinedTerm, len(a.definedTerms))
	copy(defined, a.definedTerms)
	a.mu.Unlock()
	engine.ApplyRemovals(allow, removed)
	engine.ApplyDefinedTerms(allow, defined)
	return allow
}

// definedTermsSnapshot returns a copy of the terms the documents define, for the
// callers that need the list itself rather than the veto it produces.
func (a *App) definedTermsSnapshot() []engine.DefinedTerm {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]engine.DefinedTerm, len(a.definedTerms))
	copy(out, a.definedTerms)
	return out
}

// removedValues returns a copy of the session's removed values, for the callers
// that need the list itself rather than the veto it produces.
func (a *App) removedValues() []engine.RemovedValue {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]engine.RemovedValue, len(a.removed))
	copy(out, a.removed)
	return out
}

// defaultSettings is the settings a fresh session starts from. It lives in its
// own function, rather than inline in NewApp, because ResetSession must rebuild
// EXACTLY these defaults: two copies of the literal would be a silent way for a
// "reset to defaults" to reset to something other than the defaults a new app
// gets.
func defaultSettings() Settings {
	return Settings{
		// The depth Standard presets, in both scopes: the documented default.
		Presets: map[string]string{
			engine.PresetKey(engine.ScopePatterns, engine.FamilyDepth): engine.PresetStandard,
			engine.PresetKey(engine.ScopeNames, engine.FamilyDepth):    engine.PresetStandard,
		},
		OllamaPort:  11434,
		Model:       ollama.DefaultModel,
		ContextSize: ollama.DefaultContextSize,
		Country:     engine.CountryLU,
		// Thorough is the default because it is the end that FINDS things. The
		// faster level trades recall for time, and a trade nobody asked for must
		// not be the one a fresh session makes on their behalf.
		LLMDetailLevel: engine.DetailThorough,
		// The stricter defaults, matching the frontend store: heuristic discovery
		// over-detecting is the failure mode that matters.
		HeuristicDiscovery: engine.DefaultHeuristicDiscoveryOptions(),
		// The offline routes are on by default: neither needs anything
		// installed. Local LLM discovery is off, because handing the
		// document to a model is the user's decision to make.
		UseBuiltInPatterns:      true,
		UseHeuristicDiscovery:   true,
		SignalSuggestionSources: engine.DefaultSignalSources(),
	}
}

// NewApp constructs the bound struct. Kept trivial on purpose: anything
// interesting belongs in engine/* so it stays headless-testable.
func NewApp() *App {
	return &App{
		llm:      ollama.New(""), // "" = default loopback base URL
		settings: defaultSettings(),
	}
}

// ResetRun discards everything a pipeline RUN produced, so the next run starts
// from a clean slate: the placeholder registry (numbering restarts from 1), the
// latest results, the remembered request the same-format export replays, and the
// session's removed-value list. The imported documents and the user's settings
// are deliberately KEPT, because a run reset is what a backward move out of the
// Anonymise step needs and that move keeps the documents and the configuration.
//
// This is the Go half of the leak fix: the frontend's resetStep("anonymise")
// cleared its OWN mirror of the run, but the registry and the removed list live
// here, and a re-run that reused old placeholder numbers or kept silently hiding
// a previously removed value was the "values not cleaned" the user reported.
//
// A run in progress owns this state, so the reset is skipped while one is live
// (a backward move is gated off during a run anyway); the caller still gets a
// clean state the moment the run finishes and it is called again.
func (a *App) ResetRun() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return
	}
	a.registry = nil
	a.results = nil
	a.lastReq = nil
	a.removed = nil
	// The scans survive a run reset: they describe the IMPORTED bytes, which a
	// run does not touch. Only a change to the documents themselves invalidates
	// them.
}

// ResetSession returns the whole session to the state a freshly launched app is
// in: no documents, no registry, no results, no removed values, and the default
// settings (which rebuild the Ollama client on the default loopback port). It is
// the "start from a clean sheet" action offered on the Import step, for a user
// beginning a completely separate anonymisation on new files who must not
// inherit anything from the previous one.
//
// It refuses while a run or a detection is in flight rather than pulling the
// state out from under a running goroutine: the error tells the user to let the
// current work finish first, which is recoverable, where a mid-run wipe is not.
func (a *App) ResetSession() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return fmt.Errorf("a run is still in progress, wait for it to finish before starting over")
	}
	if a.cancelDetection != nil {
		return fmt.Errorf("a detection is still in progress, cancel it or wait for it to finish before starting over")
	}
	a.docs = nil
	a.registry = nil
	a.results = nil
	a.lastReq = nil
	a.removed = nil
	a.definedTerms = nil
	a.forgetImageScansLocked()
	// A clean sheet drops the picture decisions too. They deliberately survive a
	// re-import, because an asset ID is stable across one, and just as
	// deliberately do NOT survive this: "start from a clean sheet" is the action
	// of a user beginning a separate engagement, who must inherit nothing.
	a.imageDecisions = nil
	a.settings = defaultSettings()
	// The client is rebuilt from the default port and model so a session that
	// changed the port does not leave the next one probing the wrong one.
	a.llm = ollama.New("")
	return nil
}

// Startup is called by Wails once the runtime is ready (see main.go).
// It also registers the drag-and-drop handler: dropped files go through
// the exact same validation as dialog-picked ones (CLAUDE.md §5 requires
// rejection on drop too, because drop bypasses the dialog filter).
//
// It is exported because main.go (package main) wires it as the Wails
// OnStartup hook and can only reach exported methods across the package
// boundary introduced by the backend/ split.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	runtime.OnFileDrop(ctx, func(x, y int, paths []string) {
		result := a.importPaths(paths)
		// Push the outcome to the frontend — drops are not request/reply
		// like dialog imports, so an event carries the result instead.
		runtime.EventsEmit(a.ctx, "documents:changed", result)
	})
	a.warmLocalLLM(ctx)
}

// warmLocalLLM loads the local model into Ollama's memory ahead of the first
// detection run, so the model load is not sitting inside a wait the user is
// watching.
//
// Three properties matter, and each is deliberate.
//
// It runs in a GOROUTINE, so nobody waits: a slow or absent Ollama must not
// delay the window appearing, and a settings write must return as promptly as
// it did before. ctx is only ever a cancellation signal here, never something
// this function blocks on. Startup passes the Wails context, which is the right
// one to obey: the only consumer of a warmed model is the running window, so a
// warm-up still in flight when the window closes has nothing left to serve.
// ApplySettings has no context of its own and passes a background one.
//
// It only runs when Local LLM discovery is switched on. A user who never uses it
// never pays RAM for a model they did not ask for.
//
// Its failure is SWALLOWED. ProbeOllama already owns telling the user that
// Ollama is missing; a second, louder error path for a performance optimisation
// would report a problem the user cannot act on, about a feature they may not
// have asked for. A warm-up that did not happen costs latency, never
// correctness.
func (a *App) warmLocalLLM(ctx context.Context) {
	a.mu.Lock()
	on := a.settings.UseLocalLLM
	llm := a.llm
	a.mu.Unlock()
	if !on || llm == nil {
		return
	}
	go func() {
		if err := llm.Warm(ctx); err != nil {
			// Nothing to do and nobody to tell: see the note above.
			return
		}
	}()
}

// Ping proves the JavaScript ↔ Go bridge end to end: the shell calls it on
// load. If "pong" does not appear, the bridge (not business logic) broke.
func (a *App) Ping() string {
	return "pong"
}

// OllamaState is what the app answers about the local model server: the
// client's own probe result, plus the model this session will actually post to.
//
// The resolved model travels BESIDE ollama.OllamaStatus rather than inside it
// because resolving it reads the stored settings, and ollama.Client must never
// start doing that: it is the one external boundary (CLAUDE.md §4), and the
// preference order below is an application decision.
type OllamaState struct {
	Status ollama.OllamaStatus `json:"status"`
	// Model is the name a run will post to. Empty only when there is nothing to
	// run: no reachable server, or a server with no models installed.
	Model string `json:"model"`
}

// resolveModel settles which model this session posts to, from what the probe
// just saw, and stores that answer in the settings and on the client.
//
// The rule is that the effective model is ALWAYS one the probe listed. A name
// that is not installed fails at the very end of a run the user has already
// waited for, and the failure arrives as a per-file detection problem rather
// than as the configuration mistake it is.
//
// The preference order is the user's stored choice, then ollama.DefaultModel,
// then the first model installed. The stored choice comes first because it is a
// decision someone made; the pinned default comes next because it is a
// documented preference and not an installed fact; the first installed model is
// the last resort, and it is a resort rather than a rule because it means the
// effective model is decided by the server's tag ordering, which is how a
// session ends up on a model nobody picked.
//
// A probe that FAILED changes nothing. An unreachable Ollama says nothing about
// which models exist, so rewriting the setting from it would throw away the
// user's choice the moment they stopped the server.
func (a *App) resolveModel(status ollama.OllamaStatus) string {
	a.mu.Lock()
	defer a.mu.Unlock()

	stored := a.settings.Model
	if !status.Available || len(status.Models) == 0 {
		return stored
	}
	installed := func(name string) bool {
		if name == "" {
			return false
		}
		for _, m := range status.Models {
			if m == name {
				return true
			}
		}
		return false
	}
	resolved := status.Models[0]
	switch {
	case installed(stored):
		resolved = stored
	case installed(ollama.DefaultModel):
		resolved = ollama.DefaultModel
	}

	a.settings.Model = resolved
	if a.llm != nil {
		a.llm.Model = resolved
	}
	return resolved
}

// ProbeOllama reports whether a local Ollama server is reachable, which models
// it offers, and which of them this session will use. It never fails: "not
// available" is a normal state carried inside the returned status (graceful
// degradation, CLAUDE.md §4).
func (a *App) ProbeOllama() OllamaState {
	status := a.llm.Probe()
	return OllamaState{Status: status, Model: a.resolveModel(status)}
}

// --- Documentation window -------------------------------------

// DocumentationAsset is the path, relative to the asset server root, of
// the bundled documentation page. It lives under frontend/ and is therefore
// already inside the //go:embed all:frontend directive in main.go: the
// documentation window loads embedded bytes and nothing else, so the
// local-only guarantee (CLAUDE.md section 4) is untouched.
//
// It is exported so the root package's embed_test.go (which owns the
// embedded filesystem) can assert this exact path is present in the binary.
const DocumentationAsset = "docs/index.html"

// DocumentationURL returns where the bundled documentation lives, so the
// frontend can open it in a separate window without hardcoding the path.
//
// Why this is a path and not an "open a window" call: Wails v2 (the pinned
// version, CLAUDE.md section 7) drives exactly ONE window per process.
// Its runtime package exposes WindowShow, WindowHide, WindowSetSize and so
// on, all of which act on that single window, and there is no API to
// create a second one. Multi-window arrived in Wails v3, whose idioms this
// project must not use. So Go owns WHERE the documentation is (it is Go
// that embeds it) and the frontend opens it with window.open, which the
// WebView creates as a real separate window served by this same embedded
// asset server. See frontend/api.js openDocumentation.
func (a *App) DocumentationURL() string {
	return DocumentationAsset
}

// --- Import ---------------------------------------------------------------

// dialogFilters is the file-dialog filter for the seven supported formats
// (CLAUDE.md §5). The same extension check happens again in engine.LoadAll,
// so drag-drop cannot smuggle unsupported files in.
var dialogFilters = []runtime.FileFilter{
	{
		DisplayName: "Documents (*.txt, *.csv, *.md, *.docx, *.pptx, *.xlsx, *.pdf)",
		Pattern:     "*.txt;*.csv;*.md;*.docx;*.pptx;*.xlsx;*.pdf",
	},
}

// ImportFiles opens the native multi-file dialog and loads the selection.
// A cancelled dialog returns the current list unchanged with no errors.
func (a *App) ImportFiles() (ImportResult, error) {
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Import documents",
		Filters: dialogFilters,
	})
	if err != nil {
		return ImportResult{}, fmt.Errorf(
			"the file dialog could not be opened (%v), try again; if it keeps failing, restart the application", err)
	}
	return a.importPaths(paths), nil
}

// importPaths reads each file ONCE and hands the bytes to engine.LoadAll.
// Shared by the dialog and drag-drop paths so validation cannot diverge.
func (a *App) importPaths(paths []string) ImportResult {
	var errs []string
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Sprintf(
				"could not read %q: %v, check that the file still exists and you have permission to read it", path, err))
			continue
		}
		docs, err := engine.LoadAll(filepath.Base(path), raw)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		for _, doc := range docs {
			a.upsertDocLocked(doc)
		}
	}
	return ImportResult{Documents: a.documentInfosLocked(), Errors: errs}
}

// upsertDocLocked adds a document, replacing any previous import with the
// same name (re-importing a corrected file should not duplicate it).
// Caller holds a.mu.
func (a *App) upsertDocLocked(doc engine.Document) {
	a.forgetImageScansLocked()
	// The defined terms were read out of the documents. A new or replaced
	// document changes what is defined, so the list is rebuilt by the next
	// detection rather than carried across: a term read out of a file the user
	// has replaced would go on suppressing a value that file no longer defines.
	a.definedTerms = nil
	for i, existing := range a.docs {
		if existing.Name == doc.Name {
			a.docs[i] = doc
			return
		}
	}
	a.docs = append(a.docs, doc)
}

// RemoveDocument drops one document from the session by name and returns
// the updated list. Removing an unknown name is a no-op.
func (a *App) RemoveDocument(name string) ImportResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	kept := a.docs[:0]
	for _, d := range a.docs {
		if d.Name != name {
			kept = append(kept, d)
		}
	}
	a.docs = kept
	a.forgetImageScansLocked()
	a.definedTerms = nil
	return ImportResult{Documents: a.documentInfosLocked()}
}

// DocumentSource is the source text of ONE imported document, for the
// panes that must show what the user imported rather than what the pipeline
// produced (the Import preview and the Anonymise screen's ORIGINAL pane).
//
// It exists so those panes have a single producer to read from. Before
//
//	the ORIGINAL pane read a copy of the source that travelled inside
//
// the pipeline result, which meant two paths to "the original text" and no
// test that they agreed.
type DocumentSource struct {
	// Found is false when the name is not (or no longer) imported. That is a
	// normal state, not a failure: the user can remove a document while a
	// result for it is still on screen.
	Found bool `json:"found"`
	// Markdown is the working form, cut to engine.MaxPreviewLines exactly
	// like the import preview, so both panes hold the same bytes.
	Markdown string `json:"markdown"`
	// Truncated tells the UI to show the truncation notice.
	Truncated bool `json:"truncated"`
	// IsGrid mirrors DocumentInfo.IsGrid so the pane can render a table.
	IsGrid bool `json:"isGrid"`
}

// GetDocumentSource returns one imported document's source text. An unknown
// name resolves with Found=false rather than an error: asking about a
// document that has just been removed is an ordinary race in the UI, not a
// fault the user has to act on.
func (a *App) GetDocumentSource(name string) DocumentSource {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, d := range a.docs {
		if d.Name != name {
			continue
		}
		preview, truncated := engine.PreviewMarkdown(d.Markdown)
		return DocumentSource{
			Found:     true,
			Markdown:  preview,
			Truncated: truncated,
			IsGrid:    d.Grid != nil,
		}
	}
	return DocumentSource{}
}

// documentInfos is the current import list. Nothing bound calls it: the
// frontend receives the list as the RESULT of importing or removing, so a
// second read path would be a second answer to "what is imported".
func (a *App) documentInfos() []DocumentInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.documentInfosLocked()
}

// documentInfosLocked builds the frontend summaries. Caller holds a.mu.
func (a *App) documentInfosLocked() []DocumentInfo {
	infos := make([]DocumentInfo, 0, len(a.docs))
	for _, d := range a.docs {
		// Very large documents are previewed truncated (first 5 000
		// lines) so the WebView never chokes; the pipeline still sees
		// the full a.docs content.
		preview, truncated := engine.PreviewMarkdown(d.Markdown)
		infos = append(infos, DocumentInfo{
			Name:             d.Name,
			Format:           string(d.Format),
			SizeBytes:        len(d.Raw),
			Warnings:         d.Warnings,
			Markdown:         preview,
			Experimental:     d.Format == engine.FormatPDF,
			IsGrid:           d.Grid != nil,
			PreviewTruncated: truncated,
			UnitCount:        d.UnitCount,
			Unit:             d.Unit,
			PageCount:        d.PageCount(),
		})
	}
	return infos
}

// --- Settings ---------------------------------------------------------------

// GetSettings returns the current settings for the Configure screen.
func (a *App) GetSettings() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings
}

// ApplySettings validates and stores new settings. Changing the port
// rebuilds the Ollama client — still loopback-only, enforced by ollama.New
// (CLAUDE.md §8). Returns the freshly probed status so the UI can update
// its badge in the same round-trip.
func (a *App) ApplySettings(s Settings) (OllamaState, error) {
	// An unknown preset row or preset ID is REFUSED rather than stored, for the
	// reason an unknown signal source is: a key no reader resolves is a control
	// that appears to do something and does not, and here it would also make the
	// rail claim a preset the engine's table does not hold. The engine owns the
	// table, so it owns the message too, and the message names what IS valid.
	if err := engine.ValidatePresets(s.Presets); err != nil {
		return OllamaState{}, err
	}
	if s.OllamaPort < 1 || s.OllamaPort > 65535 {
		return OllamaState{}, fmt.Errorf(
			"invalid Ollama port %d, expected a number between 1 and 65535 (default 11434)", s.OllamaPort)
	}
	if s.ContextSize < 0 || s.ContextSize > 1<<20 {
		return OllamaState{}, fmt.Errorf(
			"invalid context size %d, expected 0 (model default) or a positive number of tokens such as 8192", s.ContextSize)
	}
	if !engine.KnownCountry(s.Country) {
		return OllamaState{}, fmt.Errorf(
			"unknown country %q, expected one of %v", s.Country, engine.SupportedCountries)
	}
	// A misspelt detail level is refused rather than stored. Stored, it would
	// silently read as thorough for the rest of the session while the rail showed
	// whatever the user picked: a control that appears to do something and does
	// not. An EMPTY level is accepted, because absence is not a mistake; it is
	// what a session file written before the setting existed carries, and it has
	// a documented meaning.
	if !engine.ValidDetailLevel(s.LLMDetailLevel) {
		return OllamaState{}, fmt.Errorf(
			"unknown local model detail level %q, expected %s or %s",
			s.LLMDetailLevel, engine.DetailThorough, engine.DetailFaster)
	}
	if s.HeuristicDiscovery.MinConfidence < 0 || s.HeuristicDiscovery.MinConfidence > 1 {
		return OllamaState{}, fmt.Errorf(
			"invalid heuristic discovery confidence %v, expected a number between 0 (show every suggestion) and 1 (show only the strongest)", s.HeuristicDiscovery.MinConfidence)
	}
	if s.HeuristicDiscovery.MinLength < 0 || s.HeuristicDiscovery.MinOccurrences < 0 {
		return OllamaState{}, fmt.Errorf(
			"invalid heuristic discovery limits (minimum length %d, minimum occurrences %d), both must be zero or a positive number",
			s.HeuristicDiscovery.MinLength, s.HeuristicDiscovery.MinOccurrences)
	}
	// An unknown signal source or derivation is REFUSED rather than stored. Storing
	// one would be a key nothing reads for the rest of the session: a control that
	// appears to do something and does not, which is exactly what the identifier
	// lists exist to prevent. Both levels are checked, because a valid derivation
	// filed under the wrong source is just as dead as an invented one.
	for source, derivations := range s.SignalSuggestionSources {
		if !engine.ValidSignalSource(source) {
			return OllamaState{}, fmt.Errorf(
				"unknown signal suggestion source %q, expected one of %v",
				source, engine.AllSignalSources)
		}
		for derivation := range derivations {
			if !engine.ValidSignalDerivation(source, derivation) {
				return OllamaState{}, fmt.Errorf(
					"unknown signal derivation %q for source %q, expected one of %v",
					derivation, source, engine.SignalDerivations[source])
			}
		}
	}

	a.mu.Lock()
	// Whether Local LLM discovery was already on decides whether this call is the
	// moment to pre-load the model: see below.
	wasOn := a.settings.UseLocalLLM
	// Filled out to the complete set of known sources and derivations, so a payload
	// that omitted one lands on its DEFAULT rather than on Go's zero value. Reading
	// an absent key as "off" would silently disable a reading the user never
	// touched.
	s.SignalSuggestionSources = engine.NormaliseSignalSources(s.SignalSuggestionSources)
	// An absent level is filled out to the default it already means, for the same
	// reason: what GetSettings answers is what the rail's dropdown marks as
	// selected, and an empty string marks nothing.
	if s.LLMDetailLevel == "" {
		s.LLMDetailLevel = engine.DetailThorough
	}
	a.settings = s
	a.llm = ollama.New(fmt.Sprintf("http://127.0.0.1:%d", s.OllamaPort))
	if s.Model != "" {
		a.llm.Model = s.Model
	}
	a.llm.ContextSize = s.ContextSize
	// nil reads as off, which is the shipped default: a payload that says nothing
	// about the reply format gets the fast one rather than the slow one.
	a.llm.StrictFormat = s.LLMStrictFormat != nil && *s.LLMStrictFormat
	a.mu.Unlock()

	// Switching the route ON is the moment to pre-load the model, and the only
	// one that reaches most sessions: Local LLM discovery is off in the defaults a fresh
	// app starts from, so the warm-up at startup fires only for a session that
	// arrived with the route already on. Warming on the TRANSITION rather than
	// on every settings write is what keeps an unrelated change (a country, a
	// confidence slider) from re-posting a load request each time.
	if !wasOn && s.UseLocalLLM {
		a.warmLocalLLM(context.Background())
	}
	// The probe is also where the effective model is settled: the port may have
	// changed, so the models this call can reach are not necessarily the ones the
	// last probe saw.
	status := a.llm.Probe()
	return OllamaState{Status: status, Model: a.resolveModel(status)}, nil
}

// ListOllamaModels populates the model dropdown from the live server
// (never hardcoded — CLAUDE.md §7). The error string is shown in the UI.
func (a *App) ListOllamaModels() ([]string, error) {
	return a.llm.ListModels()
}

// --- Allowlist -------------------------------------------

// DefaultAllowlist returns the seeded never-anonymise terms so the
// frontend can show them in state.allowlist at startup. The user can
// remove any of them; the UI list is the only runtime source.
func (a *App) DefaultAllowlist() []string {
	return engine.DefaultAllowlistTerms()
}

// ImportAllowlistCSV opens a native open dialog for a CSV of terms and
// returns the parsed list. The frontend merges the terms into
// state.allowlist with its usual dedupe semantics; a cancelled dialog
// returns nil, nil (a no-op, matching the save-dialog convention).
func (a *App) ImportAllowlistCSV() ([]string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Import allowlist CSV",
		Filters: []runtime.FileFilter{{DisplayName: "CSV files", Pattern: "*.csv"}},
	})
	if err != nil {
		return nil, fmt.Errorf("the file dialog could not be opened (%v), try again; if it keeps failing, restart the application", err)
	}
	if path == "" {
		return nil, nil // user cancelled
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read %q: %v, check that the file still exists and is readable", path, err)
	}
	return engine.ParseAllowlistCSV(raw)
}

// SaveAllowlistTemplate writes the downloadable allowlist template behind
// a native save dialog (cancel is a silent no-op).
func (a *App) SaveAllowlistTemplate() error {
	return a.saveWithDialog("allowlist_template.csv", "CSV", "*.csv", engine.AllowlistTemplateCSV())
}
