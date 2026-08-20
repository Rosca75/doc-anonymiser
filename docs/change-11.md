# CHANGE-11 — Reaching the reference: what the framework-agreement pair proves is missing

You are executing a change order against the existing **doc-anonymiser**
repository (pattern P0, pure Go + Wails v2, no CGo, no npm). It holds **five
self-contained implementation batches (B1 to B5)**, each sized for ONE Opus 5
session, followed by the **decisions taken**, the **conflict analysis**, the
**recommended execution sequence** and the **acceptance criteria**.

This order is different from CHANGE-10 in one important way: **it is not a
feature proposal, it is a measurement result.** Every finding below was produced
by running this repository's own engine over a real document pair and diffing the
output. Nothing here is speculative, and each batch closes a gap that was
observed rather than imagined.

The pair:

| File | Role |
|---|---|
| `framework_agreement.docx` | an 8-page consultancy framework agreement, two Luxembourg parties, 31 zip entries, no pictures |
| `framework_agreement_anon.docx` | the SAME document as a human anonymiser produced it: the target output |

Both live outside the repository (`doc-anonymiser-app/test dataset/`). B1 brings a
reduced, licence-safe derivative INTO `backend/testdata/` so the result becomes a
permanent regression guard rather than a one-off measurement.

Ground rules for this change order (unchanged from `CLAUDE.md`):

- `CLAUDE.md` and the two subtree charters remain authoritative. Nothing here
  weakens the local-only guarantee, the one-file Ollama boundary, the zero-CGo
  rule, or "originals are immutable".
- **No new Go module.** Every change in this order is `regexp`, `strings`,
  `encoding/xml` and arithmetic over text the engine already holds.
- **Anonymise still runs no discovery method and reaches no model.** Every
  recall improvement in this order is a change to what *Identify* produces, or to
  what pass 1 matches. `TestAnonymiseNeverCallsOllama` stays green untouched.
- **A change is not finished until its tests move with it** (`CLAUDE.md` §6;
  `docs/TESTING.md` owns the tiers and the scoping procedure). Both suites gate:
  `go test ./...` and `node --test "frontend/**/*.test.js"`.
- User-visible copy never contains em dashes (`copy_guard_test.go`,
  `frontend/copy.test.js`). The prose of THIS document is not copy; the strings
  it quotes are.
- The parity guards are load-bearing. This order touches
  `category_parity_test.go`, `detection_parity_test.go` and
  `value_shape_test.go`.
- Comments explain intent in the present tense. Do not write "used to be",
  "B2 changed this" or "CHANGE-11 added this" into the code.
- **This order changes authoritative rules in `CLAUDE.md` itself**: §5's category
  table gains rows, §5's signal-source paragraph gains a source, §5's session
  paragraph gains a new `SessionVersion`, and the allowlist rule gains a
  qualification. Those edits are part of the batches and are not optional.

---

## 0. Cold-start context for the implementing session

Read this section and YOUR batch. You do not need to read the other batches,
except the "what B\<n\> hands you" paragraph at the top of yours.

### Where the work stands

| Fact | Value |
|---|---|
| Repository | `Rosca75/doc-anonymiser`, module path `doc-anonymiser` |
| Branch to develop and push on | a fresh branch off `main`, e.g. `claude/change-11-b1-<suffix>`, one per batch |
| Suites | `go test ./...` and `node --test "frontend/**/*.test.js"`, both must be green |
| `SessionVersion` before this order | 9 |
| `SessionVersion` after this order | **10** (B2 bumps it; the reason is recorded in §"Decisions" below) |

### The deviation rule (this is why the batches are in one file)

If, while implementing your batch, you find that a step below is wrong, says
something the code contradicts, or cannot be done as written: **stop, say so, and
propose the alternative before writing it.** Do not silently implement something
different, and do not implement it and then note the deviation at the end. The
measurements in §1 are reproducible; if yours disagree with them, that disagreement
is the finding and it outranks the plan.

### How the numbers below were produced

A throwaway harness (not committed; B1 replaces it with real tests) did four things:

1. Ran `engine.LoadAll` over each .docx and diffed the two markdown conversions.
   That diff IS the ground truth: it shows exactly which strings the human
   anonymiser replaced and with what.
2. Ran `engine.DetectPIISelected(md, PresetSelection(LevelAdvanced), "LU")` to see
   what pass 1 catches.
3. Ran `engine.HeuristicDiscoverContext(...)` plus `engine.FoldValueFamilies` to
   see the suggestion list a user would actually review.
4. Ran the full `engine.Run` with the Values the target implies **declared by
   hand**, then diffed the produced markdown against the target's markdown with
   every `[LABEL_N]` normalised to `[#]`. That last step is the important one: it
   separates a **discovery** gap (the engine could not find it) from a
   **pipeline** gap (the engine could not replace it even when told).

### The ground truth: what the human anonymiser did

Twenty-five distinct originals, in fourteen placeholder families. The occurrence
counts are the target file's own.

| Original | Target placeholder | Occurrences | This build can express it? |
|---|---|---|---|
| `Contoso` | `[ENTITY_1]` | 113 | yes, `entity_names` |
| `Northstar` / `NStar` | `[ENTITY_2]` | 22 | yes, but only as ONE Value with a curated spelling |
| `Banque de la Place` | `[ENTITY_3]` | 1 | yes, `entity_names` |
| `Luxembourg` | `[COUNTRY_1]` | 12 | **no category, and it is allowlisted by default** |
| `Française` | `[NATIONALITY_1]` | 4 | **no category** |
| `Transport` | `[BUSINESS_SECTOR_1]` | 4 | **no category** |
| `01 January 2001` and `01.01.2001` | `[DATE_1]` | 2 | pattern exists; the second form is not matched |
| `1, Avenue de l'Innovation` | `[ADDRESS_1]` | 1 | **no category** |
| `12, rue des Tilleuls` | `[ADDRESS_2]` | 2 | **no category** |
| `L-1855`, `L-2550` | `[POSTAL_CODE_1..2]` | 3 | **no category** |
| `B 9999` (RCS number) | `[OTHER_1]` | 1 | `other_names`, manual only |
| `1012226518` (authorisation) | `[OTHER_2]` | 1 | `other_names`, manual only |
| `PIERRE DUPONT` | `[PERSON_1]` | 2 | yes |
| `MARTIN DESCHAMPS` | `[PERSON_2]` | 2 | yes |
| `Pierre Laventure` / `PIERRE LAVENTURE` | `[PERSON_3]` | 2 | yes, one Value two spellings |
| `THOMAS LAVANDOU` | `[PERSON_4]` | 1 | yes |
| `www.statistiques.public.lu` | `[WEB_SITE_1]` | 1 | **pattern requires a scheme** |
| `www.nstar.lu` | `[WEB_SITE_2]` | 1 | **pattern requires a scheme** |
| `www.nstar.lu/privacy` | `[WEB_SITE_3]` | 1 | **pattern requires a scheme** |
| `LU88 0055 6600 4321 6501` | `[IBAN_1]` | 1 | pattern matches, **checksum vetoes it** |
| `BABAAXIL` | `[BIC_1]` | 1 | **no pattern** |
| `LU22224633` | `[VAT_1]` | 1 | **yes, correct today** |
| `+352 29 19 19 5` | `[PHONE_1]` | 1 | pattern matches, **validator vetoes it** |
| `+352 29 19 19 2100` | `[PHONE_2]` | 1 | pattern matches, **validator vetoes it** |

Two things the reference did that this order deliberately does **not** copy; see
§"What is deliberately out of scope" for why.

---

## 1. The measurement results

### 1.1 The pipeline is nearly there; discovery is not

With the twenty-five Values declared by hand, `engine.Run` reproduces the target
to **within ten diff hunks out of 326 lines**, and the occurrence counts match the
target exactly (`Contoso` x113, `Northstar` x22, `Luxembourg` x12). That is the
headline: **the replacement engine is sound.** The registry, the family folding,
the curated spellings and the post-pass all do their job.

Of the ten residual hunks, four are real defects, four are converter fidelity, and
two are artefacts of the reference itself.

### 1.2 Finding 1 (BUG, leak) — a checksum-invalid IBAN is not merely missed, it is mislabelled

The document contains `IBAN LU88 0055 6600 4321 6501`. That string is
**checksum-invalid** (mod-97 remainder 74, not 1) because it is a synthetic test
IBAN, which is what any test or template document contains.

`validIBAN` therefore vetoes it, correctly by its own rule. What happens next is
the defect: the **credit-card** recognizer matches the 16-digit interior
`0055 6600 4321 6501`, that slice **passes Luhn**, and the anonymised output reads

```
[ENTITY_3]: IBAN LU88 [CREDIT_CARD_1] - BIC/SWIFT: [BIC_1]
```

Three separate harms in one line. The IBAN's country and check digits **survive in
clear text**. The mapping CSV asserts the document contained a **credit card that
does not exist**. And the failure is silent: the run reports a successful
replacement.

The design conclusion is the important part. **A checksum veto is the wrong
default for an anonymiser.** A document holding a mistyped, partly-redacted or
synthetic bank identifier is precisely the document that must be anonymised, and
"the checksum failed" is not evidence that the string is not a bank account, it is
evidence that it might be a bad one. This repository already has the right shape
for "probably, please review": a **Suggestion**.

Fix (B2): a failed checksum **lowers confidence**, it does not veto. The span is
still produced, at a reduced `Confidence`, so `MinConfidence` remains the user's
lever. Once the IBAN span exists, `resolveOverlaps` fixes the credit-card
false positive for free: both spans are `built_in_pattern`, neither carries an
explicit confidence, so length decides, and the IBAN (24 chars) beats the card
(19 chars). B2 additionally guards the credit-card recognizer against a candidate
immediately preceded by an IBAN country-and-check prefix, because that collision
class is independent of the checksum change and a 16-digit BBAN passes Luhn about
one time in ten.

### 1.3 Finding 2 (BUG) — `validLUPhone` rejects real Luxembourg numbers

Neither `+352 29 19 19 5` nor `+352 29 19 19 2100` is replaced. The regex matches
both; `validLUPhone` rejects both:

```go
func validLUPhone(s string) bool {
	digits := digitsOnly(s)
	if strings.HasPrefix(digits, "352") {
		digits = digits[3:]
	}
	return len(digits) == 9        // <-- exactly nine
}
```

Luxembourg national numbers are **not fixed-length**. The two in this document are
a six-digit base with a one-digit and a four-digit PBX extension: seven and ten
digits. The exactly-nine rule is wrong for the owner's own primary market, and it
fails **closed** (the phone number stays in the document), which is the direction
that leaks.

Fix (B2): accept the real allocated range for +352 (4 to 11 national significant
digits), and keep a length check rather than dropping validation, since the
validator's job is still to reject a bare year or an article number.

### 1.4 Finding 3 (BUG, high leverage) — per-run emphasis wrapping shreds tokens no regex can then match

`backend/engine/convert/docx.go` calls `wrapEmphasis` **once per `<w:r>`**. Word
splits a paragraph into runs for reasons that have nothing to do with formatting:
proofing state, language tagging, revision ids, and simply which editing session
typed which characters. The result, verbatim from the converted markdown:

```
... Framework Agreement for Consultancy Services dated **0****1****.01.20****01** (the "**Agreement**") ...
```

The date `01.01.2001` is intact in the document and **unmatchable** in the working
form. This is not a date-specific problem: it breaks every multi-token pattern
(dates, phones, IBANs, VAT numbers) and every adjacency heuristic, whenever an
author happened to edit mid-token.

Measured effect of coalescing adjacent runs that share a formatting state (a close
marker touching an open marker, across any whitespace that sat between the runs):

| | pass 1 distinct matches | `entity_names` suggestions |
|---|---:|---|
| today, one wrap per `<w:r>` | 5 | `Banque de la Place` |
| coalesced, one wrap per format run | **6** (`01.01.2001` recovered) | `Banque de la Place`, **`Société Française de Transport S.A.`** (conf 0.95) |

It also removes exactly the cosmetic differences that account for four of the ten
residual diff hunks (`Entity(******ies******)` becomes `Entity(ies)`, `(****i****)`
becomes `(i)`, `Société** **coopérative` becomes `Société coopérative`) — that is,
**the reference document itself validates the fix**, because a human tool produced
the coalesced form.

Fix (B1): accumulate text per formatting state across consecutive runs in the
paragraph walker and wrap once. `wrapEmphasis` itself does not change.

One consequence to handle in the same batch, because coalescing exposes it:
removing the `**` that used to separate a name from a following job title lets the
title join the name, turning `MARTIN DESCHAMPS` into
`MARTIN DESCHAMPS Chief Information Officer`. B1 therefore lands the job-title
terminator from finding 6 alongside the coalescing, not after it.

### 1.5 Finding 4 (BUG) — a comma between a name and its legal form defeats entity discovery entirely

This is why `Contoso` and `Northstar`, the two subjects of the whole document, are
**never suggested** — not before coalescing and not after. Isolated:

| Input | Suggestion produced |
|---|---|
| `Acme S.A. signed the deal` | `Acme S.A.` (conf 0.95) |
| `Acme, S.A. signed the deal` | **nothing at all** |
| `Contoso Société Française de Transport S.A., ...` | `Contoso Société Française de Transport S.A.` (0.95) |
| `Contoso, Société Française de Transport S.A., ...` | `Société Française de Transport S.A.` — **the name is gone** |
| `Northstar Société coopérative, a Luxembourg group` | `Northstar Société coopérative` (0.85) |
| `Northstar, Société coopérative, a Luxembourg group` | **nothing at all** |

`Name, Société anonyme` / `Name, S.A.` / `Name, Sàrl` is the **standard**
continental legal-name form, and the dominant one in French and Luxembourg
drafting: the owner's market. The detector's run stops at the comma, and what
survives is either the legal form without the name (worthless: the name is the
only part worth replacing) or nothing at all.

Fix (B3): allow a legal-suffix run to reach back across a **single** comma when
what follows the comma is a legal-form phrase. Bound it tightly: one comma, no
newline, and the tail must be a recognised legal form, so the rule cannot walk a
list of ordinary nouns.

### 1.6 Finding 5 (INCONSISTENCY, dominant) — the review list is 8% signal

The list a user actually reviews on this document, after folding:

```
folded = 52    true positives = 4    precision = 8%
```

Four accepted Values out of fifty-two rows. The noise is not random; it falls into
two structural classes that between them account for **29 of the 48 false
positives (60%)**:

| Noise class | Count | Examples |
|---|---:|---|
| terms the document itself DEFINES | 19 | `Work Order` (x57 occurrences), `Confidential Information` (x14), `Dedicated Advisors` (x11), `Disclosing Party` (x10), `Work Product` (x9), `Contract Term`, `Effective Date`, `Force Majeure Event`, `Fee Basis` |
| ALL-CAPS heading text | 10 | `LAW AND COMPETENT JURISDICTION`, `ROLES AND COMMITMENTS`, `PARTIES ENTER INTO THIS AGREEMENT`, `WITNESS WHEREOF`, `AND BETWEEN`, `AND EXPENSES`, `AND INDEMNITY`, `AND TERMINATION` |

Both classes are cheaply recognisable, and the first one is the interesting one.
**A defined term is not merely noise: it is the strongest "do not anonymise"
signal a contract can offer**, because the document declares it as its own
vocabulary. A phrase the document introduces as `"**Work Order**" means ...` or
`(the "**Dedicated Advisors**")` is definitionally not a client identity.

So it belongs where the repository already keeps negative rules: it becomes a
**discovery suppressor consulted through the allowlist**, exactly as the session
exclusions are (`CLAUDE.md` §5, "Allowlist wins"), and **visible** as such. It
must not become a new category, and it must not silently drop anything the user
cannot see.

Fix (B4), and note the two traps found while measuring:

- **Match the full term, not a prefix.** A prefix rule suppressed
  `Services NStar` because `Services` is a defined term, and `Services NStar`
  contains a real entity. The suppressor must compare whole terms.
- **Recognise both idioms.** `"X" means ...` alone caught 6 of the 19; adding the
  inline `(the "X")` / `(together the "X")` form caught all 19.

The all-caps rule has one sub-finding worth stating separately, because it is a
one-line fix with disproportionate value: four of the ten heading fragments start
with a conjunction (`AND BETWEEN`, `AND EXPENSES`, `AND INDEMNITY`,
`AND TERMINATION`). The multi-word run detector is **crossing a lowercase word via
"AND"** and harvesting the fragment after it. **A name run must never begin with a
conjunction.**

### 1.7 Finding 6 (BUG) — job titles join person names, and a first name is lost

Observed suggestions, against a source line reading
`_________________________ PIERRE LAVENTURE Partner | _________________________ THOMAS LAVANDOU Partner`:

| Produced | Should be |
|---|---|
| `LAVENTURE Partner` | `PIERRE LAVENTURE` (a spelling of `Pierre Laventure`) |
| `THOMAS LAVANDOU Partner` | `THOMAS LAVANDOU` |
| `Chief Information Officer` | nothing |

Two defects. A job title (`Partner`, `Officer`, `Chief ...`) **terminates** a
person run rather than joining it, and `PIERRE` was dropped from the front,
because the signature-line underscore run breaks the tokeniser's idea of where the
run starts. Note the second one is what makes `PIERRE LAVENTURE` fail to fold into
`Pierre Laventure`: `MergeSuggestions` dedupes case-insensitively within a
category, so the two forms WOULD have become one Value had the run been extracted
correctly.

Fix (B3): a closed list of role and title words terminates a person run; a run of
three or more underscores is a separator, not a word.

### 1.8 Finding 7 (GAP) — websites are unreachable, and the missing signal source is the one this document needs

`CatURL` is `https?://[^\s<>"')\]]+`, with a deliberate comment: no scheme means
no match, "too many false hits on ordinary word.word text". That reasoning is
sound for bare `word.word` and **wrong for `www.`**, which is unambiguous. The
cost here is three misses out of three websites, and the document contains **no
email address at all**, so the entire signal-based discovery route contributes
nothing.

That is the sharper finding. `AllSignalSources` is `{email}` only. Sitting in the
document is `www.nstar.lu`, which is deterministic evidence for the organisation
`NStar` — **the exact entity offline discovery otherwise misses**, and the one
whose `NStar` spelling no derivation rule can produce from `Northstar`. A website
signal source with an `organisation` derivation slots into the existing
`SignalDerivations` tree with no new concepts.

Fix (B2 for the pattern, B4 for the signal source): a `www.`-anchored URL
alternative, and `SignalSourceWebsite` with derivation `website.organisation`.
Longest-match-first already handles `www.nstar.lu/privacy` versus `www.nstar.lu`
correctly, which the reference confirms by giving them two placeholders.

### 1.9 Finding 8 (GAP) — six of the reference's fourteen placeholder families have no category

`COUNTRY`, `NATIONALITY`, `BUSINESS_SECTOR`, `ADDRESS`, `POSTAL_CODE` and `BIC`
do not exist in this build. Five are `placeholderLabels` rows plus category
identifiers; `BIC` is additionally a pattern (`[A-Z]{6}[A-Z0-9]{2}([A-Z0-9]{3})?`
with a country-code check on positions 5 and 6, which is what stops it matching
ordinary eight-letter words).

B5 adds them. Two constraints from the measurement:

- `ADDRESS` and `POSTAL_CODE` earn a **pattern**, not just a category: `L-1855`
  and `L-2550` are a fixed Luxembourg shape (`L-` plus four digits), and
  `1, Avenue de l'Innovation` / `12, rue des Tilleuls` follow the
  `<number>, <street-type> <name>` form that a country-scoped street-type
  gazetteer can anchor. Both belong under `CategoryCountries` scoping.
- `NATIONALITY` and `BUSINESS_SECTOR` earn a **category only**, reachable by
  manual declaration and Local AI. `Française` and `Transport` are ordinary
  French words that happen to sit inside a legal name here; a pattern or a
  gazetteer for them would fire on running prose constantly. This is the honest
  answer, and §5's category table already carries a "nothing: it is defined by
  exclusion" row for precisely this situation.

### 1.10 Finding 9 (INCONSISTENCY) — the default allowlist makes the target unreachable, and the refusal is a wall

Declaring `Luxembourg` as a Value **blocks the entire run**:

```
BLOCKED: collision / block — "Luxembourg" is listed both as a value to replace
and as a term never to anonymise, and the never-anonymise list always wins
Fix: Remove it from one of the two lists, so the run does what you expect.
```

`defaultAllowlist` seeds fourteen country names, with the reasoning "a city can
identify a client site; countries rarely do". The reference document anonymises
`Luxembourg` twelve times, so for this class of document the premise is simply
false: in a two-party contract between two Luxembourg entities, the jurisdiction
is part of the identity.

The seed is still a **reasonable default** and B5 does not remove it. What B5
fixes is that the user cannot get past it without knowing to go and delete a
term from a list on a different screen. The blocking conflict is correct; the
**absence of an in-place resolution** is the defect. `ValidateValues` already
returns a `Fix` string, so the conflict card gains the action that performs it.

### 1.11 Finding 10 (INCONSISTENCY, low priority) — legal-citation dates are over-replaced

At `advanced`, `23 July 2016` (the law) and `27 April 2016` (the GDPR regulation)
are replaced; the reference keeps both, correctly, since a public statute date
identifies nobody. Two of the ten residual hunks.

This one is **deliberately left to the user** rather than fixed with a rule. A
date-with-legal-cue suppressor is the kind of heuristic that will get the
*engagement* date wrong in some other document, and `date` is already
advanced-only and per-category switchable. B4 does one cheap thing instead: the
Compare pane's existing "declare a missed Value" affordance gains its inverse for
a span the user wants to keep, which is a general improvement and not a
date-specific rule. If the owner prefers a statute-cue rule after seeing more
documents, that is a later change with more evidence behind it.

---

## 2. What is deliberately out of scope

Two differences between the two files are artefacts of the human process and must
**not** be reproduced. Recording them here so that a later session does not "fix"
the code towards them:

1. **The reference drops `(Partner)` in one place and keeps `(CEO)` and `(CIO)`
   in another.** Line 222 becomes `for the attention of **[PERSON_3]**.` while
   lines 219 to 221 keep `**[PERSON_1]** **(CEO)**`. A job title is either
   identifying or it is not; it cannot be both in one document. This is a
   hand-edit, and the app should keep role titles (they are not personal data),
   which is what finding 6 makes it do consistently.
2. **The reference normalises `Tilleuls ,` to `Tilleuls,`.** The source has a
   stray space before the comma. Tidying the user's punctuation is not an
   anonymiser's job, and doing it would mean the exported document differs from
   the original in a way the user did not ask for.

Also out of scope: `GFS LU999` is left in clear text by the reference (only the
`TVA` number beside it is replaced). Nothing in this order tries to catch it. It
is a good example of a value only a human or the Local AI will find, and the
manual-declaration path already covers it.

---

## 3. Decisions taken

1. **A checksum failure lowers confidence; it never vetoes a span.** Applies to
   IBAN first (B2) and is the stated policy for every checksum-validated
   recognizer. The reason is in §1.2: failing closed on a bank identifier leaks it.
2. **Run coalescing happens in the converter, not in a pre-detection pass.** The
   working form should be the faithful markdown of the document's *formatting*,
   not of Word's *run bookkeeping*. Fixing it once at the source means every
   consumer (pass 1, discovery, the preview, the Local AI slices, export) benefits
   and none of them needs to know.
3. **Defined-term suppression is enforced through the allowlist, and it is
   visible.** No new negative-rule mechanism. It appears in the never-anonymise
   list with its own provenance, and the user can delete any entry, exactly as
   the session exclusions work.
4. **`SessionVersion` goes to 10.** Required, not cosmetic: `Registry.Assign`
   **panics** on a category with no `placeholderLabels` row. A version-9 session
   written by the new build containing a `country_names` Value would be accepted
   by an older version-9 binary and then crash it on the next run. The bump turns
   a crash into the existing, clear "this session file was written by a different
   version" refusal.
5. **New categories that no offline method can find are still added.**
   `nationality`, `business_sector` and the rest are reachable by manual
   declaration and Local AI only. §5's table already models this honestly with
   its "Also found offline by: nothing" rows, and a missing category is worse
   than an empty one: without it the user has to file a nationality under
   `other_names` and the mapping CSV loses the distinction.
6. **The country seed stays in the default allowlist.** Findings 9's fix is the
   in-place resolution of the conflict, not a change of default. A user
   anonymising internal documents about many countries is served by the current
   default; a user anonymising a two-party national contract needs one click.

---

## 4. The five batches at a glance

| Batch | Subject | Findings closed | Risk |
|---|---|---|---|
| **B1** | Converter fidelity: coalesce adjacent same-format runs; the fixture pair enters `backend/testdata/` | 3, and 4 of the 10 residual hunks | medium: touches the converter every format test depends on |
| **B2** | Pass 1 corrections: IBAN confidence not veto, credit-card guard, LU phone range, `www.` URLs, BIC pattern | 1, 2, 7 (pattern half) | low: each is one pattern or one validator, each with a fixture |
| **B3** | Discovery precision, structural half: the legal-form comma, job-title terminators, the conjunction rule, underscore separators | 4, 6, and the conjunction half of 5 | low: additive rules, each measurable against the fixture |
| **B4** | Discovery precision, suppression half: defined-term suppressor, all-caps headings, the website signal source | 5, 7 (signal half), 10 | medium: the suppressor must not hide a real entity (see the `Services NStar` trap) |
| **B5** | The six missing categories, their patterns where one is honest, and the in-place allowlist-conflict resolution | 8, 9 | medium: parity guards, `SessionVersion` bump, frontend mirrors, copy |

### Recommended execution sequence

**B1 first, and alone.** It changes the working form every other measurement is
taken against, so running it first means B2 to B5 are measured on the text the
app will actually produce. Re-run the §1 measurements after B1 lands and record
the new baseline in the batch's own notes.

**B2 and B3 in either order, and they do not conflict**: B2 touches
`pii.go`/validators, B3 touches `discover.go`. **B4 after B3**, because both edit
`discover.go` and B4's suppressor is easiest to size once B3 has removed the
structural noise. **B5 last**: it is the only batch that bumps `SessionVersion`
and touches the parity guards and the frontend, so landing it last keeps one
session-format change in the order rather than two.

### Conflict analysis

| File | B1 | B2 | B3 | B4 | B5 |
|---|---|---|---|---|---|
| `backend/engine/convert/docx.go` | edit | | | | |
| `backend/engine/pii.go` | | edit | | | edit (new patterns) |
| `backend/engine/discover.go` | edit (title terminator) | | edit | edit | |
| `backend/engine/signals.go` | | | | edit | |
| `backend/engine/allowlist.go` | | | | edit | |
| `backend/engine/registry.go` | | | | | edit (labels) |
| `backend/engine/pipeline.go` | | | | | edit (categories, presets) |
| `backend/engine/session.go` | | | | | edit (version 10) |
| `backend/engine/country.go` | | edit (BIC, postal scoping) | | | edit |
| `frontend/state.js`, `copy.js` | | edit (labels) | | edit | edit |
| `CLAUDE.md` §5 | | | | edit | edit |

The one real overlap is `discover.go` across B1, B3 and B4. B1's touch is a
single closed word list (the title terminator) and is deliberately small for that
reason.

---

## 5. Acceptance criteria for the whole order

Measured on the committed fixture, with `LevelAdvanced`, country `LU`, the
`Luxembourg` allowlist term removed, and the twenty-five Values of §0 accepted:

1. **Reproduction.** The structural diff (every `[LABEL_N]` normalised) between
   the produced markdown and the reference's markdown is **empty except** for the
   two out-of-scope artefacts of §2 and the two legal-citation dates of §1.11.
   Ten residual hunks today; four after this order, all four accounted for above.
2. **No mislabelled span.** The mapping contains no `credit_card` entry, and
   `LU88 0055 6600 4321 6501` maps to a single `[IBAN_1]`.
3. **No survivor in the numbers.** Neither phone number, neither website, nor the
   BIC appears in the anonymised text.
4. **Both dates found.** `01 January 2001` and `01.01.2001` both map to the same
   `[DATE_1]`, which is what the reference does and what the registry's
   `byOriginal` index makes possible only if both spans exist.
5. **Recall, offline and unaided.** `Contoso` and `Northstar` are both **suggested**
   by Smart detection with no Local AI and no manual typing. This is the criterion
   that matters most to a user, and it is the one nothing in the build satisfies
   today.
6. **Precision.** The folded suggestion list is **at most 25 rows** (52 today) with
   **at least 6 true positives** (4 today), so precision moves from 8% to at least
   24%. Both numbers are asserted by a test, so a later change that floods the
   list again fails the build.
7. **Both suites green**: `go test ./...` and
   `node --test "frontend/**/*.test.js"`. The parity guards
   (`category_parity_test.go`, `detection_parity_test.go`, `value_shape_test.go`,
   `copy_guard_test.go`) pass without being weakened.
8. **`TestAnonymiseNeverCallsOllama` untouched and green.** Nothing in this order
   moves a discovery method into the run.

---

## 6. The fixture (B1 delivers it, everything else depends on it)

The measurement above is worthless if it cannot be repeated. B1 adds to
`backend/testdata/`:

- `framework_agreement.docx` — a **reduced derivative** of the source: the
  signature blocks, the party recitals, the bank-details paragraph, the notices
  list and the Work Order annex, which between them carry all twenty-five
  originals. Reduced because the fixture rules in `docs/TESTING.md` want small
  files, and because the full 68 KB document is 90% boilerplate that tests nothing.
  Keep the run-fragmented `**0****1****.01.20****01**` intact: it is the finding.
- `framework_agreement_anon.docx` — the matching reduced target.
- `framework_agreement_expected.json` — the twenty-five-row ground-truth table of
  §0 as data, so a test asserts the mapping rather than a golden markdown blob.
  A golden blob would fail on every unrelated converter improvement and teach the
  next session to regenerate it without reading it.

The names are English/French mixed, which matches the existing fixture rules. All
personal names, company names, numbers and addresses in the source are already
fictional (`Contoso`, `Northstar`, `Banque de la Place`, a checksum-invalid IBAN),
so no scrubbing is needed beyond the reduction; confirm this while reducing rather
than assuming it.
