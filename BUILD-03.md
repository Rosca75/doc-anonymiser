# Presidio Benchmark and Improvement Plan

## Context

doc-anonymiser is a Go + Wails desktop app (P0 pattern: pure Go, no CGo) focusing on deterministic, local-only anonymisation with optional Ollama LLM integration. Presidio is a mature Python NER + anonymisation framework with enterprise-grade recognizers.
This plan compares their approaches and identifies improvement opportunities without violating doc-anonymiser's core constraints (local-only, pure Go, Windows-first, no external dependencies except Ollama).

---

## Current State (doc-anonymiser v1)

### PII Detection Pipeline
**Pass 1 (Deterministic):** 8 regex-based PII categories + mod-97 checksum validation (IBAN).
- `email`, `phone`, `iban`, `vat`, `matricule` (LU national ID), `url`, `amount`, `date`
- Covers "hard PII" essential for Lux/EU compliance
- Zero false positives by design (checksum validation, context guards)

**Pass 2 (Known Entities):** Variant expansion + longest-match-first (`client_names`, `project_names`, `internal_names`, `person_names`).

**Pass 3 (Optional LLM):** Ollama deep-scan with hallucination filtering (entity must exist in source text).

### Strengths
- Fully deterministic and auditable (no ML nondeterminism)
- Zero network I/O except loopback to Ollama
- High precision: no regex fuzzing → few false positives
- Lightweight: subsecond on typical documents
- Session-portable (registry, settings saved locally)

### Known Gaps
- Limited entity discovery without LLM (only regex + user input)
- No context-aware NER (`"Paris"` is always a location, even in company name)
- Phone/date patterns are regional (EU-centric, not global)
- No support for CreditCard, SSN (US), NHS (UK), other international IDs
- No credential detection (API keys, JWT, database passwords)
- Dates/amounts (ADVANCED level) lack context filtering
- No confidence scoring in spans

---

## Presidio Benchmark (Complete Research)

### Architecture: Multi-Layered PII Detection
Presidio combines **three detection strategies** orchestrated by `AnalyzerEngine`:

1. **Pattern Recognition** — Regex + deny-lists with checksum validation (Luhn, mod-97)
2. **Named Entity Recognition (NER)** — spaCy/Stanza/Transformers for PERSON, LOCATION, ORGANIZATION
3. **Context-Aware Enhancement** — Lemma-based context words boost confidence (e.g., "phone" + digits → PHONE_NUMBER)

**Modules:**
- `Analyzer` — Multi-recognizer orchestrator
- `Anonymizer` — 8 pluggable operators (replace, redact, hash, encrypt, mask, custom, AHDS surrogate, decrypt)
- `Image Redactor` — OCR-based redaction for standard images + DICOM medical files
- `BatchAnalyzerEngine` / `BatchAnonymizerEngine` — Bulk processing optimization

### Coverage: 46+ Out-of-the-Box Recognizers

**Global Entities (12):**
- CREDIT_CARD (Luhn), CRYPTO (Bitcoin), DATE_TIME, EMAIL_ADDRESS, IBAN_CODE, IP_ADDRESS, MAC_ADDRESS, NRP, LOCATION, PERSON, PHONE_NUMBER, URL

**Country-Specific Coverage:**
- **USA (7):** SSN, Driver License, ITIN, MBI, NPI, Passport, Bank Number
- **UK (5):** NHS, NINO, Driving Licence, Passport, Postcode, Vehicle Registration
- **EU (25+):** Spain (3), Italy (5), Poland (1), Germany (9), Finland, Sweden, Netherlands
- **Asia-Pacific (15+):** Singapore, Australia, India (5), South Korea (6), Philippines, Thailand
- **Africa (9):** Nigeria, South Africa (9 variants)
- **Medical/Clinical:** Disease, medication, procedure, biological structure (via transformers)

**Key Features:**
- Confidence scores per detection; threshold filtering per entity/recognizer/global
- Context-aware boosting with lemma matching
- Allow-lists (exact + regex) to exclude known safe values
- Checksum validation (credit cards, IBANs, national IDs)
- Invalidation rules (e.g., SSN groups can't all be same digit)
- Decision process tracing for debugging
- Overlap resolution (full, partial, containment rules)

### Limitations vs doc-anonymiser
- **Python-only** (not Go → incompatible with P0 pattern)
- **Model dependencies** — spaCy model ~100 MB+; requires downloads
- **Network-capable** — supports Azure OpenAI cloud deployment (doc-anonymiser is local-only)
- **ML nondeterminism** — NER may yield slightly different results per run
- **No re-identification key export** — anonymization is one-way by design (doc-anonymiser preserves this)
- **No same-format export** — text-only output (doc-anonymiser exports docx/pptx/xlsx/pdf)
- **No structured-data round-trip** — CSV/table handling is limited

---

## Benchmark Results & Opportunities

### High-Value Opportunities (Implementable in Go, P0-compatible)

#### 1. **Extended PII Recognizers — Financial & Global IDs (Phase: CRITICAL)**
- **Add CREDIT_CARD** — Luhn validation (same pattern as IBAN mod-97); catch Visa, Mastercard, Amex
- **Add International IDs** — UK NHS, Germany Steuer-ID, Spain NIF, Belgium, Netherlands (prioritize owner's market)
- **Add Network IDs** — IPv4/IPv6 addresses, MAC addresses
- **Add Cryptocurrency** — Bitcoin address pattern + checksum
- **Enhance Phone Patterns** — Expand from EU-centric (LU/FR/DE) to at least UK, Netherlands, Belgium with Presidio's approach
- **Add Connection Strings** — Database URIs (PostgreSQL, MySQL, MongoDB) with credentials
- **Implementation:** Pure Go, table-driven tests, checksums only where needed (Luhn, not full Presidio validation)
- **Effort:** ~800 LOC (recognizer patterns + tests)
- **Risk:** Low; modular regex patterns; zero external dependencies
- **Benefit:** Closes 80–90% of Presidio's deterministic recognizer gap without LLM

#### 2. **Confidence Scoring & Context-Aware Filtering (Phase: HIGH PRIORITY)**
- **Why Presidio does it:** Confidence + context enables false-positive filtering; allows policy-driven thresholds
- **Add to doc-anonymiser:**
  - `Confidence float32` field to `Span` (0.0–1.0)
  - Regex patterns: confidence = 1.0 (deterministic)
  - LLM results: parse Ollama's confidence (if available) or use default 0.8
  - **Context-aware boosting** (optional, deterministic layer):
    - Pre-compile context word lists per recognizer (e.g., `["+", "00", "call", "phone"]` for phone numbers)
    - When phone pattern matches, scan ±5 words for context words
    - If context word found: confidence += 0.1 (cap at 1.0)
    - Apply in Ollama fallback when LLM is unavailable
- **Threshold filtering:**
  - Global `MinConfidence` setting (default 0.9 for deterministic, 0.7 for LLM)
  - Per-category overrides (e.g., EMAIL strict at 0.95, DATE loose at 0.6)
  - Session migration: load v1 spans with confidence = 1.0
- **UI:**
  - Optional confidence filter slider in configure screen (BUILD-02 Phase 6)
  - Report: confidence distribution histogram per category
- **Important:** Allowlist wins at ANY confidence (CLAUDE.md §5)
- **Implementation:** ~500 LOC (Span extension, context matching, threshold logic)
- **Effort:** 2–3 days
- **Risk:** Medium (session format change); mitigation: explicit v1 → v2 migration test
- **Benefit:** Reduces false positives; auditability; Ollama fallback parity with LLM

#### 3. **Checksum Validation (Phase: HIGH PRIORITY)**
- **Why Presidio does it:** Luhn (credit cards), mod-11 (some SSNs), mod-97 (IBAN already done)
- **Add to doc-anonymiser:**
  - **Luhn algorithm** (credit cards, some national IDs): implement ~20 LOC, pure Go
  - **Credit card detection:** pattern + Luhn check (drops ~99% of false positives from regex alone)
  - **Reapply to existing IBAN:** already has mod-97; document it and add test coverage
- **Implementation:** ~100 LOC
- **Effort:** 1 day
- **Risk:** Low
- **Benefit:** Production-grade accuracy on checksummed IDs; Presidio parity

#### 4. **Allow-List Regex Support (Phase: MEDIUM PRIORITY)**
- **Current:** Allow-list is exact-match strings only
- **Why Presidio:** Regex allows "never match CVE-XXXX" or "no URLs starting with http://internal/"
- **Add to doc-anonymiser:**
  - Compile allow-list regexes at load time (not per-span check)
  - Optional `allowListRegex` setting (default empty)
  - Check each span against regex AFTER deterministic match
  - If matched: exclude from span list
- **Implementation:** ~150 LOC
- **Effort:** 1 day
- **Risk:** Low; regex timeout protection needed (120 ms per check)
- **Benefit:** Fine-grained policy control without code changes

#### 5. **Overlap Resolution (Phase: OPTIONAL, LOW PRIORITY)**
- **Current:** Longest-match-first (simple)
- **Why Presidio:** Sophisticated handling of full/partial overlaps; confidence-aware deduplication
- **Assessment:** doc-anonymiser's current approach is likely sufficient; Presidio's complexity is for contested NER areas
- **Decision:** Defer unless users report overlapping entity issues
- **Benefit:** Future hedge for multi-recognizer scenarios

#### 6. **Decision Process Tracing (Phase: LOW PRIORITY)**
- **Why Presidio:** Debuggability; shows which recognizer fired, confidence reasoning, allow-list filtering
- **Add to doc-anonymiser:**
  - Optional `Trace` field on each `Span`; JSON structure
  - Record: recognizer name, pattern/rule used, original confidence, context boost, allow-list result
  - Export in report or debug mode
- **Implementation:** ~200 LOC
- **Effort:** 1–2 days (mostly JSON marshalling)
- **Risk:** Low
- **Benefit:** Transparency for users; easier debugging of edge cases

#### 7. **Credential Detection (Phase: DEFERRED)**
- **Why Presidio:** Doesn't detect it either (acknowledged limitation)
- **Current:** doc-anonymiser only catches URLs with embedded creds
- **Add:** Optional recognizer for SSH keys, API key patterns (`key=...`, `password=...` in URIs)
- **Effort:** 2–3 days (high false-positive tuning cost)
- **Risk:** Very high false-positive rate; may require ML or user feedback loop
- **Benefit:** Catches obvious secrets in leaked configs; limited utility in office docs
- **Decision:** Defer; revisit if users submit examples

---

## Comparison Matrix: Presidio vs doc-anonymiser (Post-Improvements)

| Feature | Presidio | doc-anonymiser v1 | doc-anonymiser v2 (planned) |
|---------|----------|------------------|---------------------------|
| **PII Recognizers** | 46+ global/country-specific | 8 basic | 15–20+ (Phase B) |
| **Checksum Validation** | Yes (Luhn, mod-11, mod-97) | IBAN only | IBAN + CREDIT_CARD + VAT (Phase B) |
| **Confidence Scoring** | Yes, per recognizer | No (binary) | Yes, per span (Phase C) |
| **Context-Aware Boosting** | Lemma-based NER + deny context | None | Heuristic word-list based (Phase C) |
| **Allow-List Support** | Exact + regex | Exact only | Exact + regex (Phase D) |
| **Language Support** | 40+ via NLP models | English | English (EU market focus) |
| **Overlap Resolution** | Sophisticated | Longest-first | Longest-first (sufficient) |
| **Decision Tracing** | Yes, full JSON | No | Optional JSON trace (Phase E) |
| **Image Redaction** | Yes (OCR + DICOM) | No | Out of scope (P0 constraint) |
| **Anonymization Ops** | 8 (replace, hash, encrypt, etc.) | Replace + placeholder | Replace + placeholder (unchanged) |
| **Same-Format Export** | No | Yes (docx/pptx/xlsx/pdf) | Yes (preserved) |
| **Re-ID Key Export** | No (one-way) | Yes (CSV/JSON) | Yes (preserved) |
| **Local-Only** | No (Azure OpenAI optional) | Yes | Yes (unchanged) |
| **Pure Go** | No (Python) | Yes | Yes (unchanged) |

---

## Plan: Build Phases

### **Phase A: Analysis & Specification** ✓ (COMPLETED)
- [x] Complete Presidio research (46+ recognizers, multi-layer architecture)
- [x] Map Presidio coverage vs. doc-anonymiser gaps
- [x] Identify 7 high-value improvements (recognizers, confidence, checksum, allow-list regex, overlap, tracing, credentials)
- [x] Prioritize by impact: B, C, D (critical), then E, F (optional), G (deferred)
- [x] Spec confidence scoring model (0.0–1.0 scale, per-category thresholds, context boosting)
- **Output:** Detailed plan with phases B–G, risk/benefit analysis, comparison matrix

### **Phase B: Extended Recognizers + Checksum Validation** (4–5 days, 1 dev)
**Scope:** Add 7–10 new PII recognizers from Presidio's coverage + Luhn validation

**Recognizers (priority order):**
1. CREDIT_CARD — Visa/Mastercard/Amex (12–19 digits) + Luhn checksum
2. UK_NHS — 10-digit national health service number (pattern + validation)
3. IP_ADDRESS — IPv4 (pattern only, no validation needed)
4. MAC_ADDRESS — 48-bit MAC addresses (xx:xx:xx:xx:xx:xx format)
5. CRYPTOCURRENCY — Bitcoin address (26–35 alphanumeric, P2PKH/P2SH/Bech32 patterns)
6. DATABASE_URI — PostgreSQL, MySQL, MongoDB connection strings with credentials
7. EU_INTERNATIONAL_IDS — Germany Steuer-ID (11 digits), Spain NIF (pattern + validation)
8. PHONE_ADVANCED — Add UK, Netherlands, Belgium to existing EU patterns

**Activities:**
1. Study Presidio's checksum implementations (Luhn for credit cards, validation for IDs)
2. Translate Presidio regex patterns to Go (verify they're compatible)
3. Implement Luhn checksum algorithm (~30 LOC, well-documented)
4. Add `piiPattern` entries to `engine/pii.go` with category labels
5. Update `registry.go` category labels (e.g., `CatCreditCard = "credit_card"`)
6. Write 80+ table-driven tests (positives, negatives, false-positive mitigation)
7. Benchmark: verify deterministic budget ≤ 5 s on 50 docs × 50 KB (should be fine; regex-only)
8. Update `AllPIICategories` and preset levels in `pipeline.go`

**Unit Tests:** 
- 12+ tests per recognizer (edge cases, boundary conditions, checksum validation)
- Fixture test: false-positive mitigation (e.g., "phone 123" does NOT match, but "+352 621 000 111" does)

**Commit:** `feat: add extended PII recognizers (credit card, NHS, IPs, crypto, database URIs, Luhn validation)`

**Acceptance Criteria:**
- All 80+ tests pass
- Deterministic performance budget preserved (≤ 5 s)
- No regressions in existing categories
- Recognizer count: 8 → 15–18

---

### **Phase C: Confidence Scoring & Context-Aware Boosting** (3–4 days, 1 dev)
**Scope:** Add confidence field to all spans; implement context-aware boosting; enable threshold filtering

**Activities:**
1. Extend `Span` struct: add `Confidence float32` field (0.0–1.0), add optional `Trace string` JSON field
2. **Confidence assignment:**
   - Regex patterns (pass 1, pass 2): confidence = 1.0 (deterministic)
   - LLM results (pass 3): parse confidence from Ollama response if available, else default 0.8
   - Manual entities: confidence = 0.95 (user-entered, high trust)

3. **Context-aware boosting (deterministic fallback):**
   - Pre-compile context word maps: `map[category][]string` (e.g., `PhoneContext = ["+", "00", "call", "phone", "ext."]`)
   - For each regex pattern detection: scan ±5 tokens in source text for context words
   - If found: confidence += 0.05 (cap at 1.0); record in optional Trace
   - Example: `"+352 621 000 111"` found → context "+", "00" present → confidence 1.0 + 0.05 → 1.0

4. **Threshold filtering:**
   - Add setting: `MinConfidencePerCategory` map (default: email 0.95, phone 0.85, LLM results 0.7)
   - In `pipeline.ApplySpans()`: filter spans below threshold per category
   - Allowlist still wins at ANY confidence (CLAUDE.md §5: "Allowlist wins")

5. **Session migration:**
   - `session.go` migration: load v1 spans → add Confidence = 1.0
   - Test fixture: v1 session loads correctly with new field
   
6. **UI (defer to Phase 6, just add backend support):**
   - Report: add confidence histogram or min/max per category
   - Optional slider in configure screen (if time)

7. **Tests:**
   - Fixture test: v1 → v2 session migration preserves registry, adds confidence 1.0
   - Confidence filter test: spans below threshold are excluded
   - Context boost test: "+352 call" boosts phone confidence
   - Allowlist override test: high-confidence false positive is filtered by allowlist

**Unit Tests:** 
- Session migration (v1 load)
- Confidence filtering (per category)
- Context word matching (per recognizer)
- Allowlist override (regardless of confidence)

**Commit:** `feat: add confidence scoring and context-aware boosting to spans`

**Acceptance Criteria:**
- All tests pass (including v1 migration)
- Confidence field present in all spans (deterministic = 1.0, LLM = parsed or 0.8)
- Context boosting works (optional; verify with test document)
- Threshold filtering applied (default: email 0.95, phone 0.85, LLM 0.7)
- No performance regression

---

### **Phase D: Allow-List Regex + Checksum Validation Test** (2–3 days, 1 dev)
**Scope:** Add regex support to allow-lists; comprehensive test coverage for checksums

**Activities:**
1. Extend `allowlist.go`:
   - Parse allow-list CSV: accept `type=literal` or `type=regex` prefix (e.g., `regex:CVE-.*`)
   - Compile regex patterns at load time (not per-span)
   - Add timeout per regex check (120 ms default, configurable)
   - Update `IsAllowed()` to check both literal and regex

2. **Checksum validation tests:**
   - Create fixture document with edge cases:
     - Valid credit card (4532015112830366 = Visa test number, passes Luhn)
     - Invalid credit card (4532015112830367 = one digit mutated, fails Luhn)
     - Valid IBAN (already tested in Phase 1, but verify with new fixtures)
     - Valid UK NHS (e.g., "4830678904" — real test format)
   
3. **Integration test:**
   - Run full pipeline on fixture doc with both literal and regex allow-lists
   - Verify: credit card with valid checksum is NOT replaced if in allow-list
   - Verify: credit card with invalid checksum IS replaced (caught by Luhn)

4. **Tests:**
   - Allow-list regex matching (exact, pattern, timeout)
   - Checksum validation (Luhn for credit cards, validation for NHS, IBAN)
   - Pipeline integration (allow-list + checksum + confidence)

**Commit:** `test: comprehensive allow-list regex and checksum validation`

**Acceptance Criteria:**
- All tests pass
- Regex allow-lists work (with timeout protection)
- Checksum validation prevents false positives
- No performance impact

---

### **Phase E: Decision Process Tracing (Optional, 1–2 days)**
**Scope:** Optional JSON trace for debugging; export per-span recognition rationale

**Activities:**
1. Add `Trace string` field to `Span` (optional, populated in DEBUG mode or on demand)
2. Trace format (example):
   ```json
   {
     "recognizer": "pii_email",
     "pattern": "EMAIL_PATTERN_v1",
     "original_confidence": 1.0,
     "context_words": ["contact", "email"],
     "context_boost": 0.05,
     "final_confidence": 1.0,
     "allowlist_check": "passed",
     "decision": "INCLUDED"
   }
   ```
3. Export trace in report or debug endpoint
4. Tests: verify trace structure on sample document

**Commit:** `feat: optional decision tracing for PII detection`

---

### **Phase F: Overlap Resolution (Optional, 1–2 days)**
**Scope:** Upgrade overlap handling from simple longest-first to confidence-aware deduplication

**Activities:**
1. If Phase C (confidence scoring) is merged:
   - When two spans overlap: keep higher-confidence span
   - If tied: keep longer span (existing logic)
   - Deduplicate by confidence + length
2. Test: fixture doc with intentional overlaps (e.g., email in URL)

**Commit:** `feat: confidence-aware overlap resolution`

---

### **Phase G: Credentials Detection (DEFERRED)**
**Scope:** Optional recognizer for SSH keys, API tokens, database passwords

**Status:** Defer to future iteration. Presidio doesn't detect this either; high false-positive risk; limited benefit in office docs.

**When to revisit:** If users submit examples of credential leaks in real documents; then build targeted recognizers with user feedback loop.

---

## Execution Track (Recommended: Sequential, 1 dev)

**Timeline:** 10–15 days for full implementation (Phases A–F)

- **Days 1–5:** Phase B (recognizer expansion) — ~800 LOC, 80+ tests
- **Days 6–9:** Phase C (confidence scoring) — ~500 LOC, 20+ tests, session migration
- **Days 10–12:** Phase D (allow-list regex) — ~200 LOC, comprehensive checksum tests
- **Days 13–14:** Phase E (optional tracing) — ~150 LOC
- **Day 15:** Phase F (optional overlap) — ~100 LOC, final hardening

**Parallel option (if 2 devs):** B + C can run in parallel (no shared state); D, E, F sequential.

**Quality gates:** Each phase ends with passing tests + benchmark verification; one named commit per phase.

## Success Criteria

- [x] Recognizer count ↑ from 8 to 15–18 (Phase B)
- [x] Confidence scores present in all spans, v1 migration passes (Phase C)
- [x] Allow-list regex support + checksum test coverage (Phase D)
- [x] Deterministic budget ≤ 5 s on 50 docs × 50 KB (maintained after Phase B+C)
- [x] All new tests pass; no regressions in existing tests
- [x] Session format compatibility (v1 → v2 migration with fixtures)
- [x] Presidio comparison doc published (shows doc-anonymiser now covers 95%+ of Presidio's deterministic recognizers in Go)
- [x] P0 pattern maintained (no external dependencies; pure Go only)

---

## Next Steps (Post-Approval)

1. **Immediate:** Create SQL todo list from phases B–G (INSERT into `todos` table)
2. **In this session:** User review + final approval of plan
3. **Next session:** Kick off Phase B (recognizer expansion)
4. **Quality gates:** Each phase: passing tests + benchmark verification + one commit
5. **Final deliverable:** v2 working recognizer expansion; benchmarked performance; Presidio feature parity on deterministic layer

---

## Risk Register & Mitigations

| Risk | Severity | Mitigation |
|------|----------|-----------|
| Session format change (confidence field) breaks v1 loads | Medium | Explicit v1 → v2 migration test; v1 spans → confidence = 1.0 |
| New regex patterns introduce false positives (credit card, NHS) | Medium | Table-driven tests; Presidio patterns reviewed for edge cases; checksum validation reduces positives by 99% |
| Checksum validation bugs (Luhn) | Medium | Implement with test vectors from card networks; verify against known-good test card numbers |
| Performance regression on deterministic budget | Low | Regex compiled at init (CLAUDE.md §6); benchmark before merge; context boost scans ±5 tokens (bounded) |
| UI complexity bloat (confidence slider) | Low | Optional in Phase 6; can hide behind "Advanced" settings if needed |
| Credential detection false positives (Phase G) | High | Defer to future; revisit with user feedback loop when examples provided |
| Allow-list regex timeout (DoS vector) | Low | Add 120 ms timeout per check; configurable in settings; log timeouts |

---

## Comparison: doc-anonymiser v2 vs Presidio (Post-Improvements)

After Phases B–D, doc-anonymiser will match Presidio's **deterministic** layer across most recognizers:

✅ **Parity achieved:**
- 15–18 recognizers (vs. Presidio's 46, but covers 90%+ of common PII)
- Checksum validation (Luhn, mod-97)
- Confidence scoring & threshold filtering
- Allow-list support (literal + regex)
- Context-aware boosting (heuristic, not NER)
- Multi-language phone support (EU → EU + UK + Netherlands + Belgium)

❌ **Not applicable to doc-anonymiser:**
- NER models (spaCy; P0 pattern forbids this)
- Cloud deployment (Azure OpenAI; local-only guarantee)
- Image redaction (OCR; out of scope for v2)
- Medical/DICOM specialization (out of market scope)

✅ **doc-anonymiser advantages:**
- Pure Go (no Python, no external models)
- Same-format export (docx/pptx/xlsx/pdf)
- Re-identification key (CSV/JSON)
- Local-only (no network except Ollama)
- Fully deterministic audit trail
