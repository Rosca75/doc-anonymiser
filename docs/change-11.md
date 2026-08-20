# CHANGE-11 — Reaching the reference: what the framework-agreement pair proves is missing

You are executing a change order against the existing **doc-anonymiser**
repository (pattern P0, pure Go + Wails v2, no CGo, no npm). It is **ONE batch**,
sized for one session, followed by the **decisions taken**, the **execution order
inside the batch** and the **acceptance criteria**.

This order is not a feature proposal, it is a **measurement result**. Every
finding below was produced by running this repository's own engine and its own
frontend test harness, and diffing the output against a reference. Nothing here
is speculative.

The fixture pair, already committed:

| File | Role |
|---|---|
| `backend/testdata/framework_agreement.docx` | an 8-page consultancy framework agreement, two Luxembourg parties, 31 zip entries, no pictures |
| `backend/testdata/framework_agreement_anon.docx` | the SAME document as a human anonymiser produced it: the target output |

Ground rules (unchanged from `CLAUDE.md`):

- `CLAUDE.md` and the two subtree charters remain authoritative. Nothing here
  weakens the local-only guarantee, the one-file Ollama boundary, the zero-CGo
  rule, or "originals are immutable".
- **No new Go module and no npm dependency.** Everything here is `regexp`,
  `strings`, `encoding/xml` and arithmetic over text the engine already holds.
- **Anonymise still runs no discovery method and reaches no model.** Every recall
  improvement is a change to what *Identify* produces or to what pass 1 matches.
  `TestAnonymiseNeverCallsOllama` stays green untouched.
- **A change is not finished until its tests move with it** (`CLAUDE.md` §6;
  `docs/TESTING.md` owns the tiers and the scoping procedure). Both suites gate:
  `go test ./...` and `node --test "frontend/**/*.test.js"`.
- **Render tests over substring matches; wiring tests when the question is what a
  control DOES** (`docs/TESTING.md`). The UI findings in §2 were all found this
  way and their regression tests must be written the same way.
- User-visible copy never contains em dashes (`copy_guard_test.go`,
  `frontend/copy.test.js`). The prose of THIS document is not copy; the strings it
  quotes are.
- The parity guards are load-bearing. This order touches
  `category_parity_test.go`, `detection_parity_test.go` and `value_shape_test.go`.
- Comments explain intent in the present tense. Do not write "used to be", "this
  order changed it", or a change-request number, into the code.
- **This order changes authoritative rules in `CLAUDE.md` itself**: §5's category
  table gains rows, §5's signal-source paragraph gains a source, §5's session
  paragraph gains a new `SessionVersion`, and §5's allowlist rule gains a
  qualification. Those edits are part of the batch and are not optional.

### The deviation rule

If a step below is wrong, contradicted by the code, or cannot be done as
written: **stop, say so, and propose the alternative before writing it.** The
measurements are reproducible; if yours disagree, that disagreement is the
finding and it outranks the plan.

---

## 0. How the numbers were produced

Two harnesses, both throwaway, both replaced by real tests in this batch.

**Engine side**, a `main` package under a scratch directory, four steps:

1. `engine.LoadAll` over each .docx, then diff the two markdown conversions.
   That diff IS the ground truth: it shows exactly which strings the human
   replaced and with what.
2. `engine.DetectPIISelected(md, PresetSelection(LevelAdvanced), "LU")` for what
   pass 1 catches.
3. `engine.HeuristicDiscoverContext(...)` plus `engine.FoldValueFamilies` for the
   suggestion list a user actually reviews.
4. The full `engine.Run` with the Values the target implies **declared by hand**,
   diffed against the target's markdown with every `[LABEL_N]` normalised to
   `[#]`. This step is the important one: it separates a **discovery** gap (the
   engine could not find it) from a **pipeline** gap (the engine could not
   replace it even when told).

**Frontend side**, a temporary `frontend/probe.test.js` driven through
`testdom.js`, rendering the real Identify workspace and reading the card's
`textContent` after each gesture. `node --test` on the five existing
spelling/value suites passes **96/96**, and the full suite passes **847/847**, so
§2's findings are a **coverage gap, not a failing test**: every one of them is
invisible to the suite as it stands.

### The ground truth: what the human anonymiser did

Twenty-five distinct originals in fourteen placeholder families. Occurrence
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

---

## 1. Engine findings

### 1.0 The pipeline is nearly there; discovery is not

With the twenty-five Values declared by hand, `engine.Run` reproduces the target
to **within ten diff hunks out of 326 lines**, and the occurrence counts match
exactly (`Contoso` x113, `Northstar` x22, `Luxembourg` x12). That is the
headline: **the replacement engine is sound.** The registry, the family folding,
the curated spellings and the post-pass all do their job.

Of the ten residual hunks: four are real defects, four are converter fidelity,
two are artefacts of the reference itself (§3).

### 1.1 (BUG, leak) A checksum-invalid IBAN is not merely missed, it is mislabelled

The document contains `IBAN LU88 0055 6600 4321 6501`. That string is
**checksum-invalid** (mod-97 remainder 74, not 1), because it is a synthetic test
IBAN, which is what any test or template document contains.

`validIBAN` vetoes it, correctly by its own rule. What happens next is the
defect: the **credit-card** recognizer matches the 16-digit interior
`0055 6600 4321 6501`, that slice **passes Luhn**, and the output reads

```
[ENTITY_3]: IBAN LU88 [CREDIT_CARD_1] - BIC/SWIFT: [BIC_1]
```

Three harms in one line. The IBAN's country and check digits **survive in clear
text**. The mapping CSV asserts the document contained a **credit card that does
not exist**. And the run reports success.

**A checksum veto is the wrong default for an anonymiser.** A document holding a
mistyped, partly-redacted or synthetic bank identifier is precisely the document
that must be anonymised, and "the checksum failed" is not evidence that the
string is not a bank account, only that it might be a bad one. This repository
already has the right shape for "probably, please review": a **Suggestion**.

Fix: a failed checksum **lowers confidence**, it does not veto. The span is still
produced, at a reduced `Confidence`, so `MinConfidence` stays the user's lever.
Once the IBAN span exists, `resolveOverlaps` fixes the credit-card false positive
for free: both spans are `built_in_pattern`, neither carries an explicit
confidence, so length decides and the IBAN (24 chars) beats the card (19). Also
guard the credit-card recognizer against a candidate immediately preceded by an
IBAN country-and-check prefix: that collision class is independent of the
checksum change, and a 16-digit BBAN passes Luhn about one time in ten.

### 1.2 (BUG) `validLUPhone` rejects real Luxembourg numbers

Neither `+352 29 19 19 5` nor `+352 29 19 19 2100` is replaced. The regex matches
both; the validator rejects both:

```go
func validLUPhone(s string) bool {
	digits := digitsOnly(s)
	if strings.HasPrefix(digits, "352") {
		digits = digits[3:]
	}
	return len(digits) == 9        // <-- exactly nine
}
```

Luxembourg national numbers are **not fixed-length**. The two here are a
six-digit base with a one-digit and a four-digit PBX extension: seven and ten
digits. The exactly-nine rule is wrong for the owner's primary market, and it
fails **closed** (the number stays in the document), which is the direction that
leaks.

Fix: accept the allocated range for +352 (4 to 11 national significant digits).
Keep a length check rather than dropping validation: the validator still has to
reject a bare year or an article number.

### 1.3 (BUG, high leverage) Per-run emphasis wrapping shreds tokens no regex can match

`convert/docx.go` calls `wrapEmphasis` **once per `<w:r>`**. Word splits a
paragraph into runs for reasons unrelated to formatting: proofing state, language
tagging, revision ids, and simply which editing session typed which characters.
Verbatim from the converted markdown:

```
... Framework Agreement for Consultancy Services dated **0****1****.01.20****01** (the "**Agreement**") ...
```

The date `01.01.2001` is intact in the document and **unmatchable** in the working
form. This is not date-specific: it breaks every multi-token pattern (dates,
phones, IBANs, VAT numbers) and every adjacency heuristic, whenever an author
edited mid-token.

Measured effect of coalescing adjacent runs that share a formatting state:

| | pass 1 distinct matches | `entity_names` suggestions |
|---|---:|---|
| today, one wrap per `<w:r>` | 5 | `Banque de la Place` |
| coalesced, one wrap per format run | **6** (`01.01.2001` recovered) | `Banque de la Place`, **`Société Française de Transport S.A.`** (conf 0.95) |

It also removes exactly the cosmetic differences behind four of the ten residual
hunks (`Entity(******ies******)` becomes `Entity(ies)`, `(****i****)` becomes
`(i)`, `Société** **coopérative` becomes `Société coopérative`). That is, **the
reference document itself validates the fix**, because a human tool produced the
coalesced form.

Fix: accumulate text per formatting state across consecutive runs in the
paragraph walker and wrap once. `wrapEmphasis` itself does not change.

Land the job-title terminator (§1.5) **in the same batch and before measuring**,
because coalescing exposes it: removing the `**` that separated a name from a
following job title lets the title join the name, turning `MARTIN DESCHAMPS` into
`MARTIN DESCHAMPS Chief Information Officer`.

### 1.4 (BUG) A comma between a name and its legal form defeats entity discovery entirely

This is why `Contoso` and `Northstar`, the two subjects of the document, are
**never suggested**: not before coalescing and not after. Isolated:

| Input | Suggestion produced |
|---|---|
| `Acme S.A. signed the deal` | `Acme S.A.` (conf 0.95) |
| `Acme, S.A. signed the deal` | **nothing at all** |
| `Contoso Société Française de Transport S.A., ...` | `Contoso Société Française de Transport S.A.` (0.95) |
| `Contoso, Société Française de Transport S.A., ...` | `Société Française de Transport S.A.` — **the name is gone** |
| `Northstar Société coopérative, a Luxembourg group` | `Northstar Société coopérative` (0.85) |
| `Northstar, Société coopérative, a Luxembourg group` | **nothing at all** |

`Name, Société anonyme` / `Name, S.A.` / `Name, Sàrl` is the **standard**
continental legal-name form and the dominant one in French and Luxembourg
drafting: the owner's market. The run stops at the comma, and what survives is
either the legal form without the name (worthless: the name is the only part
worth replacing) or nothing at all.

Fix: let a legal-suffix run reach back across a **single** comma when what
follows is a legal-form phrase. Bound it tightly: one comma, no newline, and the
tail must be a recognised legal form, so the rule cannot walk a list of ordinary
nouns.

### 1.5 (BUG) Job titles join person names, and a first name is lost

Against a source line reading
`_________________________ PIERRE LAVENTURE Partner | _________________________ THOMAS LAVANDOU Partner`:

| Produced | Should be |
|---|---|
| `LAVENTURE Partner` | `PIERRE LAVENTURE` (a spelling of `Pierre Laventure`) |
| `THOMAS LAVANDOU Partner` | `THOMAS LAVANDOU` |
| `Chief Information Officer` | nothing |

Two defects: a job title (`Partner`, `Officer`, `Chief ...`) **terminates** a
person run rather than joining it, and `PIERRE` is dropped from the front because
the signature-line underscore run breaks the tokeniser's idea of where the run
starts. The second is what makes `PIERRE LAVENTURE` fail to fold into
`Pierre Laventure`: `MergeSuggestions` dedupes case-insensitively within a
category, so the two forms WOULD have become one Value had the run been extracted
correctly.

Fix: a closed list of role and title words terminates a person run; a run of
three or more underscores is a separator, not a word.

### 1.6 (INCONSISTENCY, dominant) The review list is 8% signal

```
folded = 52    true positives = 4    precision = 8%
```

Four accepted Values out of fifty-two rows. The noise is not random; two
structural classes account for **29 of the 48 false positives (60%)**:

| Noise class | Count | Examples |
|---|---:|---|
| terms the document itself DEFINES | 19 | `Work Order` (x57 occurrences), `Confidential Information` (x14), `Dedicated Advisors` (x11), `Disclosing Party` (x10), `Work Product` (x9), `Contract Term`, `Effective Date`, `Force Majeure Event`, `Fee Basis` |
| ALL-CAPS heading text | 10 | `LAW AND COMPETENT JURISDICTION`, `ROLES AND COMMITMENTS`, `PARTIES ENTER INTO THIS AGREEMENT`, `WITNESS WHEREOF`, `AND BETWEEN`, `AND EXPENSES`, `AND INDEMNITY`, `AND TERMINATION` |

The first class is the interesting one. **A defined term is not merely noise: it
is the strongest "do not anonymise" signal a contract can offer**, because the
document declares it as its own vocabulary. A phrase introduced as
`"**Work Order**" means ...` or `(the "**Dedicated Advisors**")` is definitionally
not a client identity.

So it belongs where the repository already keeps negative rules: a **discovery
suppressor consulted through the allowlist**, exactly as the session exclusions
are (`CLAUDE.md` §5, "Allowlist wins"), and **visible** as such. Not a new
category, and nothing dropped that the user cannot see.

Two traps found while measuring, both mandatory:

- **Match the full term, not a prefix.** A prefix rule suppressed
  `Services NStar`, because `Services` is a defined term and `Services NStar`
  contains a real entity.
- **Recognise both idioms.** `"X" means ...` alone caught 6 of the 19; adding the
  inline `(the "X")` / `(together the "X")` form caught all 19.

One sub-finding worth its own line, because it is a one-line fix with
disproportionate value: four of the ten heading fragments start with a
conjunction (`AND BETWEEN`, `AND EXPENSES`, `AND INDEMNITY`, `AND TERMINATION`).
The multi-word run detector is **crossing a lowercase word via "AND"** and
harvesting the fragment after it. **A name run must never begin with a
conjunction.**

### 1.7 (GAP) Websites are unreachable, and the missing signal source is the one this document needs

`CatURL` is `https?://[^\s<>"')\]]+`, with a deliberate comment: no scheme means
no match, "too many false hits on ordinary word.word text". Sound for bare
`word.word`, **wrong for `www.`**, which is unambiguous. Three misses out of
three websites.

The sharper finding: the document contains **no email address at all**, so
signal-based discovery contributes nothing, while `AllSignalSources` is `{email}`
only. Sitting in the document is `www.nstar.lu`, deterministic evidence for the
organisation `NStar` — **the exact entity offline discovery otherwise misses**,
and the one whose `NStar` spelling no derivation rule can produce from
`Northstar`.

Fix: a `www.`-anchored URL alternative, plus `SignalSourceWebsite` with
derivation `website.organisation`, which slots into the existing
`SignalDerivations` tree with no new concepts. Longest-match-first already
handles `www.nstar.lu/privacy` versus `www.nstar.lu`, which the reference confirms
by giving them two placeholders.

### 1.8 (GAP) Six of the reference's fourteen placeholder families have no category

`COUNTRY`, `NATIONALITY`, `BUSINESS_SECTOR`, `ADDRESS`, `POSTAL_CODE` and `BIC`
do not exist. Five are `placeholderLabels` rows plus category identifiers; `BIC`
is additionally a pattern (`[A-Z]{6}[A-Z0-9]{2}([A-Z0-9]{3})?` with a
country-code check on positions 5 and 6, which is what stops it matching ordinary
eight-letter words).

Two constraints from the measurement:

- `ADDRESS` and `POSTAL_CODE` earn a **pattern**, not just a category: `L-1855`
  and `L-2550` are a fixed Luxembourg shape (`L-` plus four digits), and
  `1, Avenue de l'Innovation` / `12, rue des Tilleuls` follow the
  `<number>, <street-type> <name>` form a country-scoped street-type gazetteer can
  anchor. Both belong under `CategoryCountries` scoping.
- `NATIONALITY` and `BUSINESS_SECTOR` earn a **category only**, reachable by
  manual declaration and Local AI. `Française` and `Transport` are ordinary
  French words that happen to sit inside a legal name here; a pattern or
  gazetteer for them would fire on running prose constantly. §5's category table
  already carries a "nothing: it is defined by exclusion" row for exactly this.

### 1.9 (INCONSISTENCY) The default allowlist makes the target unreachable, and the refusal is a wall

Declaring `Luxembourg` as a Value **blocks the entire run**:

```
BLOCKED: collision / block — "Luxembourg" is listed both as a value to replace
and as a term never to anonymise, and the never-anonymise list always wins
Fix: Remove it from one of the two lists, so the run does what you expect.
```

`defaultAllowlist` seeds fourteen country names, reasoning "a city can identify a
client site; countries rarely do". The reference anonymises `Luxembourg` twelve
times, so for this class of document the premise is false: in a two-party contract
between two Luxembourg entities, the jurisdiction is part of the identity.

The seed is still a **reasonable default** and stays. What is defective is that
the user cannot get past it without knowing to go and delete a term from a list on
another screen. The blocking conflict is correct; the **absence of an in-place
resolution** is the bug. `ValidateValues` already returns a `Fix` string, so the
conflict card gains the action that performs it.

### 1.10 (INCONSISTENCY, deliberately not fixed) Legal-citation dates are over-replaced

At `advanced`, `23 July 2016` (the law) and `27 April 2016` (the GDPR regulation)
are replaced; the reference keeps both, correctly, since a public statute date
identifies nobody. Two of the ten residual hunks.

**Left to the user rather than fixed with a rule.** A date-with-legal-cue
suppressor is the kind of heuristic that will get the *engagement* date wrong in
some other document, and `date` is already advanced-only and per-category
switchable. If the owner wants a statute-cue rule after seeing more documents,
that is a later change with more evidence behind it.

---

## 2. Frontend findings: the spelling and MainText gestures

These were found by probing, not by a failing test. `node --test` on
`valuemodel`, `spellingspopup`, `identifyvalues`, `identifyworkspace` and
`identifyactions` passes **96/96**, and the whole suite passes **847/847**. Each
finding below is therefore a **coverage gap as well as a bug**, and the regression
test is as much of the fix as the code change.

All three are reproduced through `testdom.js` against the real
`renderIdentifyWorkspace`, reading the card's `textContent`.

### 2.1 (BUG) Amending a CURATED value leaves the card permanently "working out the other spellings..."

Three writers set `derivedSpellings: null` — the PENDING sentinel — without
touching `spellingPolicy`:

- `state.js addSpelling`
- `state.js renameValue`
- `state.js changeValueCategory`

`valuemodel.js pendingExpansions` **deliberately excludes curated rows** ("A
CURATED row is settled by definition"), so no expansion is ever requested and
nothing ever clears the sentinel. `identifyworkspace.js:957` renders
`derivedSpellings === null` as `WORKSPACE.spellingsPending`.

Measured, card `textContent` after each gesture on a curated row:

| Gesture | Card renders | Will an expansion arrive? |
|---|---|---|
| `addSpelling("Northstar", "NStar")` | `Spellings Northstar NStar working out the other spellings... add` | **no** |
| `renameValue("Pierre Laventure" → "P. Laventure")` | `P. Laventure ... Spellings Pierre Laventure PIERRE LAVENTURE working out the other spellings...` | **no** |
| `changeValueCategory("Transport" → entity_names)` | pending hint rendered | **no** |

The chips are correct and present; the row claims to still be working, forever.
This is the bug the owner has been seeing.

**The fix is not to drop the curated policy.** That would let a deleted spelling
come straight back, which is the whole point curation exists to prevent
(`CLAUDE.md` §5, "Deleting, renaming or moving a spelling CURATES the Value").
The correct sentinel for a curated row is `[]` (settled), which is exactly what
`curate()` already writes and documents. So: **one shared helper that re-pends a
row correctly — `[]` when the policy is curated, `null` when it is automatic —
used by all three writers.** A curated row has nothing to derive, so it is
settled the moment it is amended.

### 2.2 (BUG, data loss) Renaming the MainText onto the value's own spelling silently discards the old name

```
before: ["Northstar", "NStar"]     (mainText + spelling: both replaced)
rename mainText -> "NStar"
after:  ["NStar"]                  reason returned: ""   (the UI reports success)
LOST:   ["Northstar"]
```

`renameValue` checks `newMainText` against **other values' keys** and never
against the row's **own spellings**, and it clears `derivedSpellings`, which is
where `Northstar` lived. The Value that used to replace both forms now replaces
only `NStar`, so on this document **9 occurrences of `Northstar` silently stop
being anonymised**. It returns `""`, so nothing warns anybody.

This is a leak, not a cosmetic bug, and it sits directly on the flow this order
depends on (§0's `Northstar` / `NStar` row).

Fix: when the new MainText matches one of the row's own spellings
case-insensitively, that is a **promotion**, not a rename: the old MainText
becomes a spelling, the promoted spelling becomes the MainText, the row curates,
and nothing is lost. That is also the gesture a user renaming onto an existing
spelling is actually asking for, so it needs no new control.

### 2.3 (INCONSISTENCY) Three return conventions in one family of operations

| Function | Returns |
|---|---|
| `renameValue` | `""` on success, else a reason string |
| `renameSpelling` | `""` on success, else a reason string |
| `changeValueCategory` | `""` on success, else a reason string |
| `moveSpelling` | **a boolean** |
| `groupValues` | **a number** |

A caller writing `if (moveSpelling(...)) { showError() }` shows an error on
success, and the falsy-on-success convention of the other three makes exactly
that mistake natural. Fix: `moveSpelling` and `groupValues` return the same
`""`-or-reason shape, and their callers are updated with them. Where a count is
genuinely useful to a caller, return it separately rather than in the slot the
family uses for failure.

---

## 3. What is deliberately out of scope

Two differences between the two files are artefacts of the human process and must
**not** be reproduced. Recorded here so a later session does not "fix" the code
towards them:

1. **The reference drops `(Partner)` in one place and keeps `(CEO)` and `(CIO)`
   in another.** Line 222 becomes `for the attention of **[PERSON_3]**.` while
   lines 219 to 221 keep `**[PERSON_1]** **(CEO)**`. A job title is either
   identifying or it is not; it cannot be both in one document. This is a
   hand-edit, and the app should keep role titles consistently, which is what
   §1.5 makes it do.
2. **The reference normalises `Tilleuls ,` to `Tilleuls,`.** The source has a
   stray space before the comma. Tidying the user's punctuation is not an
   anonymiser's job, and doing it would make the export differ from the original
   in a way the user did not ask for.

Also out of scope: `GFS LU999` is left in clear text by the reference (only the
`TVA` number beside it is replaced). Nothing here tries to catch it. It is a good
example of a value only a human or the Local AI will find, and manual declaration
already covers it.

---

## 4. Decisions taken

1. **A checksum failure lowers confidence; it never vetoes a span.** IBAN first,
   and it is the stated policy for every checksum-validated recognizer. Failing
   closed on a bank identifier leaks it (§1.1).
2. **Run coalescing happens in the converter, not in a pre-detection pass.** The
   working form should be the faithful markdown of the document's *formatting*,
   not of Word's *run bookkeeping*. Fixing it at the source means every consumer
   (pass 1, discovery, preview, Local AI slices, export) benefits and none needs
   to know.
3. **Defined-term suppression is enforced through the allowlist, and it is
   visible.** No new negative-rule mechanism. It appears in the never-anonymise
   list with its own provenance, and the user can delete any entry, exactly as
   the session exclusions work.
4. **A curated row's amended sentinel is `[]`, never `null`.** Curated means
   settled; the pending sentinel belongs to automatic rows only (§2.1). This is
   an invariant, so it gets an assertion and not just a fix.
5. **Renaming a MainText onto one of the row's own spellings is a PROMOTION.**
   Refusing it would be defensible but unhelpful; silently losing the old name is
   not defensible at all (§2.2).
6. **`SessionVersion` goes to 10.** Required, not cosmetic: `Registry.Assign`
   **panics** on a category with no `placeholderLabels` row. A version-9 session
   written by the new build containing a `country_names` Value would be accepted
   by an older version-9 binary and crash it on the next run. The bump turns a
   crash into the existing, clear "written by a different version" refusal.
7. **New categories that no offline method can find are still added.**
   `nationality` and `business_sector` are reachable by manual declaration and
   Local AI only. §5's table already models this honestly with its "Also found
   offline by: nothing" rows, and a missing category is worse than an empty one:
   without it the user files a nationality under `other_names` and the mapping CSV
   loses the distinction.
8. **The country seed stays in the default allowlist.** §1.9's fix is the
   in-place resolution of the conflict, not a change of default.

---

## 5. Execution order inside the batch

One batch, but the order matters, because two steps change what the later ones
measure.

1. **Converter coalescing (§1.3) plus the job-title terminator (§1.5).** Do these
   first and together: coalescing changes the working form every later
   measurement is taken against, and it exposes the title bug. **Re-run the §0
   measurements here** and record the new baseline before continuing.
2. **Pass 1 corrections (§1.1, §1.2, §1.7 pattern half).** Independent of each
   other; each is one pattern or one validator with one fixture assertion.
3. **Discovery structure (§1.4, §1.5 remainder, §1.6 conjunction rule).**
   Additive rules in `discover.go`, each measurable against the fixture.
4. **Discovery suppression (§1.6 defined terms and all-caps, §1.7 signal
   source).** After step 3, so the suppressor is sized against the noise that
   actually remains. Mind both traps in §1.6.
5. **The six categories and their patterns (§1.8), the `SessionVersion` bump, the
   parity guards and the frontend mirrors.** Last on the engine side, because it
   is the only step touching the session format and the parity tests.
6. **The three frontend fixes (§2).** Independent of everything above and can be
   done at any point; doing them last keeps `state.js` out of the way while the
   engine work is in flight. §2.1's shared helper lands before §2.2 and §2.3,
   since both touch the same writers.
7. **The allowlist conflict action (§1.9).** Needs step 5's categories to be
   useful, and it is the one item that touches both sides of the bridge.

### Files this batch touches

| File | Why |
|---|---|
| `backend/engine/convert/docx.go` | run coalescing (§1.3) |
| `backend/engine/pii.go` | IBAN confidence, credit-card guard, LU phone range, `www.` URLs, BIC, postal code, address (§1.1, §1.2, §1.7, §1.8) |
| `backend/engine/discover.go` | legal-form comma, title terminators, underscore separator, conjunction rule, defined-term suppressor (§1.4, §1.5, §1.6) |
| `backend/engine/signals.go` | `SignalSourceWebsite` and its derivation (§1.7) |
| `backend/engine/allowlist.go` | the defined-term suppressor's home (§1.6) |
| `backend/engine/registry.go` | six `placeholderLabels` rows (§1.8) |
| `backend/engine/pipeline.go` | six category identifiers, preset membership (§1.8) |
| `backend/engine/country.go` | scoping for BIC, postal code, address (§1.8) |
| `backend/engine/session.go` | `SessionVersion` 10 (decision 6) |
| `backend/engine/conflicts.go` | the resolvable-conflict shape (§1.9) |
| `frontend/state.js` | the curated sentinel helper, the promotion path, the return conventions, the category mirrors (§2.1, §2.2, §2.3, §1.8) |
| `frontend/valuemodel.js` | the shared re-pend helper if it lives here rather than in `state.js` (§2.1) |
| `frontend/views/identifyworkspace.js` | the conflict card's resolve action (§1.9) |
| `frontend/copy.js` | labels for six categories, the new signal source, the resolve action (§1.8, §1.7, §1.9) |
| `frontend/countries.js` | the country mirror for the new scoped patterns (§1.8) |
| `CLAUDE.md` | §5 category table, signal sources, session version, allowlist qualification |
| `backend/CLAUDE.md`, `frontend/CLAUDE.md` | subtree detail |

---

## 6. Tests this batch adds

The engine harness and the frontend probe were both throwaway. They are replaced
by:

**Engine, unit tier, `backend/engine/`:**

- A reproduction test over the fixture pair: convert both, run the pipeline with
  the twenty-five Values, and assert the structural diff is empty except for the
  four accounted-for hunks (§3 and §1.10). Assert against
  `framework_agreement_expected.json` (see below), **not** against a golden
  markdown blob: a blob fails on every unrelated converter improvement and
  teaches the next session to regenerate it without reading it.
- One assertion per §1 finding, at the smallest scope that shows it: a
  checksum-invalid IBAN still produces a span and no `credit_card` entry; `+352`
  numbers of 7 and 10 national digits validate; `**0****1****.01.20****01**`
  converts to `**01.01.2001**`; `Acme, S.A.` yields `Acme, S.A.`; a person run
  stops at `Partner`; a run never starts with `AND`; a defined term is suppressed
  while `Services NStar` is NOT.
- **Precision and recall assertions** over the fixture, with numbers, so a later
  change that floods the review list again fails the build (§7 criteria 5 and 6).

**Frontend, `frontend/`:**

- `valuemodel.test.js`: the curated sentinel invariant, as a property of the
  helper (§2.1, decision 4).
- `identifyworkspace.test.js` or `spellingspopup.test.js`, driven through
  `testdom.js` and asserting the card's rendered text: after `addSpelling`,
  `renameValue` and `changeValueCategory` on a curated row, the pending hint is
  **absent** and the chips are present. These are the tests whose absence let
  §2.1 live (`docs/TESTING.md`: wiring tests when the question is what a control
  does).
- `state.test.js`: the promotion path keeps every form that was being replaced
  (§2.2), asserted as a set comparison before and after, and the uniform return
  convention across the five functions in §2.3's table.

**Fixture data:**

- `backend/testdata/framework_agreement_expected.json` — the twenty-five-row
  ground-truth table of §0 as data: original, category, expected placeholder
  family, occurrence count.

The two .docx files are already in `backend/testdata/` at full size (68 KB each).
Leave them there rather than reducing them: they are the only fixture in the
repository that exercises a real document's run fragmentation, its defined-term
vocabulary and its signature blocks together, and every one of those is
load-bearing for a finding above. `docs/TESTING.md` asks for small fixtures and
for the unit tier to stay under 10 seconds; if the reproduction test threatens
that budget, make it an **integration-tier** test rather than shrinking the
document, and keep the per-finding assertions (which use short inline strings) in
the unit tier.

All names, companies, numbers and addresses in the source are fictional
(`Contoso`, `Northstar`, `Banque de la Place`, a checksum-invalid IBAN). Confirm
that while writing the expected-JSON rather than assuming it.

---

## 7. Acceptance criteria

Measured on the committed fixture, with `LevelAdvanced`, country `LU`, the
`Luxembourg` allowlist term removed, and the twenty-five Values of §0 accepted:

1. **Reproduction.** The structural diff (every `[LABEL_N]` normalised) between
   the produced markdown and the reference's is **empty except** for the two
   out-of-scope artefacts of §3 and the two legal-citation dates of §1.10. Ten
   residual hunks today; four after this order, all four accounted for.
2. **No mislabelled span.** The mapping contains no `credit_card` entry, and
   `LU88 0055 6600 4321 6501` maps to a single `[IBAN_1]`.
3. **No survivor in the numbers.** Neither phone number, neither website, nor the
   BIC appears in the anonymised text.
4. **Both dates found.** `01 January 2001` and `01.01.2001` both map to the same
   `[DATE_1]`, which is what the reference does and what the registry's
   `byOriginal` index makes possible only if both spans exist.
5. **Recall, offline and unaided.** `Contoso` and `Northstar` are both
   **suggested** by Smart detection with no Local AI and no manual typing. This is
   the criterion that matters most to a user, and nothing in the build satisfies
   it today.
6. **Precision.** The folded suggestion list is **at most 25 rows** (52 today)
   with **at least 6 true positives** (4 today), so precision moves from 8% to at
   least 24%. Both numbers asserted by a test.
7. **No stuck card.** After `addSpelling`, `renameValue` or
   `changeValueCategory` on a curated Value, the rendered card shows its chips and
   **no** pending hint. Asserted through `testdom.js` on rendered text, not on
   store shape.
8. **No silent spelling loss.** Renaming a MainText onto one of the row's own
   spellings leaves the set of replaced forms **unchanged**, and no operation in
   §2.3's table reports success while losing a form.
9. **Uniform return convention.** All five functions in §2.3's table return
   `""`-or-reason, and every call site is updated.
10. **Both suites green**: `go test ./...` and
    `node --test "frontend/**/*.test.js"`. The parity guards
    (`category_parity_test.go`, `detection_parity_test.go`,
    `value_shape_test.go`, `copy_guard_test.go`, `dataset_parity_test.go`) pass
    without being weakened.
11. **`TestAnonymiseNeverCallsOllama` untouched and green.** Nothing here moves a
    discovery method into the run.
