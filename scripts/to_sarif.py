#!/usr/bin/env python3
"""to_sarif.py - convert audit tool output to SARIF 2.1.0 for GitHub code scanning.

WHY PYTHON, AND WHY STDLIB ONLY
    The owner's laptop sits behind a TLS-inspecting proxy that breaks
    `pip install`, so this script imports nothing that is not in the standard
    library. Python rather than Go because it needs no build step and no
    toolchain warm-up: it runs from a clean checkout with `py scripts/to_sarif.py`.

WHICH TOOLS IT CONVERTS
    Two, and only the two that cannot emit SARIF themselves:

        deadcode      golang.org/x/tools/cmd/deadcode -json
        deadexports   scripts/deadexports (this repo's frontend scanner)

    golangci-lint and govulncheck both write SARIF natively
    (--output.sarif.path and -format sarif). Do not add converters for them;
    hand-rolling a format the tool already produces correctly is how the two
    drift apart.

WHAT SARIF IS, IN THE FIVE PARTS THAT MATTER HERE
    SARIF is a JSON schema for static analysis results. A file is:

        { "version", "$schema", "runs": [ run, ... ] }

    and a run is one tool's output:

      1. run.tool.driver          who produced this: name, version, infoUri,
                                  and `rules`, the catalogue of every rule the
                                  tool can report. GitHub shows a rule's
                                  description on the alert page, so the
                                  catalogue is what makes an alert readable
                                  rather than a bare string.
      2. run.results[]            the findings. Each names a ruleId from the
                                  catalogue, a level, a message and a location.
      3. result.locations[]       physicalLocation.artifactLocation.uri plus a
                                  region. The uri MUST be relative to the
                                  repository root and use forward slashes, or
                                  GitHub cannot attach the alert to a line and
                                  silently drops it.
      4. result.partialFingerprints
                                  how GitHub decides that an alert in this
                                  upload is THE SAME alert as one in the last
                                  upload. Get this wrong and every push closes
                                  the old alerts and opens identical new ones,
                                  discarding whatever triage state they carried.
                                  See the fingerprint note below.
      5. rule.properties["security-severity"]
                                  a numeric string, e.g. "2.0". GitHub reads it
                                  as a CVSS-style score and buckets it:
                                  >= 9.0 critical, >= 7.0 high, >= 4.0 medium,
                                  < 4.0 low. See the severity note below.

FINGERPRINTS
    The fingerprint is derived from rule id + file path + symbol name, and
    NEVER from a line number. A dead function that moves down its file because
    something was inserted above it is the same finding; if the line number
    were in the hash, that edit would close the alert and open a new one, and
    a dismissal the owner recorded last month would evaporate. The trade is
    that two findings of the same rule for the same symbol in the same file
    collapse into one, which cannot happen: a file cannot export one name twice.

SEVERITY
    Every rule here scores between 1.0 and 3.9, which GitHub renders as "low"
    and never as a security finding. This is deliberate and non-negotiable:
    dead code and lint are maintenance facts, not vulnerabilities. CodeQL and
    Dependabot own the security surface of this repository. An audit alert that
    presents itself as a security alert devalues the ones that are, and the
    first time someone chases a "high severity" alert that turns out to be an
    unused helper is the last time they trust the tab.

USAGE
    python3 scripts/to_sarif.py --tool deadcode \\
        --input .audit/deadcode.json --output .audit/deadcode.sarif

    Exit status is 0 whenever a valid SARIF file was written, INCLUDING when
    there were no findings and when the input was empty or missing. This script
    is a converter in the middle of `task audit`; failing here would stop the
    remaining tools from running over a condition that is not an error.
    Exit 2 means the conversion itself failed: unreadable input, malformed JSON,
    or an unwritable output path.
"""

import argparse
import hashlib
import json
import os
import sys

# SARIF version and schema. Both are required by GitHub's upload endpoint; an
# upload with a $schema it does not recognise is rejected wholesale rather than
# partially accepted.
SARIF_VERSION = "2.1.0"
SARIF_SCHEMA = (
    "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/"
    "Schemata/sarif-schema-2.1.0.json"
)


# ---------------------------------------------------------------------------
# Rule catalogues
# ---------------------------------------------------------------------------
#
# One entry per rule each tool can report. `help_text` is what a reviewer sees
# on the alert page, so it answers the question they actually have, which is
# never "what is dead code" but "why is THIS dead and what do I do about it".
#
# security_severity stays in the 1.0-3.9 band for every rule (see the module
# docstring). The relative ordering inside the band is the only judgement here:
# a whole unimported module (3.0) is a bigger fact than one unused export (2.5),
# and an export kept alive only by its own test (1.5) is a question rather than
# a defect.

DEADCODE_RULES = {
    "unreachable-function": {
        "name": "UnreachableFunction",
        "short": "Function is unreachable from any main package",
        "full": (
            "Rapid Type Analysis found no call path from any main function to "
            "this function, including calls through interfaces, function values "
            "and reflection."
        ),
        "help_text": (
            "**What this means**\n\n"
            "`deadcode` builds a call graph from every `main` package in the "
            "module and reports functions nothing reaches. Unlike a linter's "
            "unused check, this is whole-program: an exported function that "
            "another package could call in principle, but nothing actually "
            "does, is reported here and nowhere else.\n\n"
            "**Before deleting it, check the three things RTA cannot see**\n\n"
            "1. **Wails bindings.** Exported methods on the `App` struct in "
            "`backend/` are called from JavaScript through the Wails bridge, "
            "never from Go. `deadcode` cannot see those calls. Check "
            "`frontend/BRIDGE.md` and `frontend/api.js` before removing any "
            "`App` method; if the method is genuinely gone from the bridge "
            "contract, remove it from BRIDGE.md in the same change.\n"
            "2. **`//go:linkname`.** The tool does not follow linkname "
            "aliasing and will report the target as dead.\n"
            "3. **Test-only helpers.** The audit runs without `-test`, so a "
            "function used only by tests is reported. That is usually a real "
            "finding worth acting on, but it is a design question, not a "
            "deletion.\n\n"
            "**If it is genuinely dead**, delete it and delete its tests in the "
            "same change (CLAUDE.md §6: a change is not finished until its "
            "tests move with it)."
        ),
        "security_severity": "2.5",
        "level": "note",
    }
}

DEADEXPORTS_RULES = {
    "unused-export": {
        "name": "UnusedExport",
        "short": "Exported binding is never imported",
        "full": (
            "This ES module exports a binding that no other file in frontend/ "
            "imports, and no HTML entry point loads."
        ),
        "help_text": (
            "**What this means**\n\n"
            "The scanner (`scripts/deadexports`) built the module graph of "
            "`frontend/` from every `import` and `export` statement, including "
            "the inline module script in `index.html`, and found nothing that "
            "imports this name.\n\n"
            "**What to check first**\n\n"
            "The scanner reads statements, not semantics. It marks an export "
            "used if the module is pulled in wholesale by "
            "`import * as ns`, by a dynamic `await import(...)`, or by a "
            "side-effect import, so a finding here means no such import exists "
            "either. What it cannot see is a name reached through a string: if "
            "the export is referenced from an HTML template attribute or looked "
            "up by name at runtime, this is a false positive - dismiss it as "
            "\"used in tests\" or \"won't fix\" with a note.\n\n"
            "**If it is genuinely unused**, delete it along with any test that "
            "exists only to exercise it."
        ),
        "security_severity": "2.5",
        "level": "note",
    },
    "test-only-export": {
        "name": "TestOnlyExport",
        "short": "Export is imported only by test files",
        "full": (
            "Every importer of this binding is a *.test.js file. The export "
            "exists so its own test can reach it, and for no other reason."
        ),
        "help_text": (
            "**What this means**\n\n"
            "Nothing in the application imports this name; only test files do. "
            "The production code path that used to use it is gone, or the "
            "function was exported purely to make it testable.\n\n"
            "**This is a design question, not a defect.** Three honest "
            "outcomes:\n\n"
            "1. The behaviour is genuinely dead: delete the export AND its "
            "test, in the same change. A test asserting a retired contract is "
            "worse than no test (CLAUDE.md §6).\n"
            "2. The export exists only for test reach, and the behaviour is "
            "already covered through the module's public surface: unexport it "
            "and let the existing test cover it indirectly.\n"
            "3. The export is the right seam for the test and the indirect "
            "route would test nothing useful: dismiss the alert as "
            "\"used in tests\".\n\n"
            "Volume here is expected on a codebase with a thorough frontend "
            "suite; treat it as a list to read once, not a backlog."
        ),
        "security_severity": "1.5",
        "level": "note",
    },
    "unused-module": {
        "name": "UnusedModule",
        "short": "Module is never imported by anything",
        "full": (
            "No file in frontend/ imports this module, and no HTML entry point "
            "loads it. It is not reachable from the application at all."
        ),
        "help_text": (
            "**What this means**\n\n"
            "The whole file is unreachable, not just one of its exports. It is "
            "reported once for the module rather than once per export, so that "
            "a leftover file produces one alert instead of a wall of them.\n\n"
            "**What to check first**\n\n"
            "Is it loaded some way the scanner cannot see - a `<script>` tag "
            "the scanner did not parse, or a path built at runtime? If so this "
            "is a false positive and the fix is to make the load explicit, not "
            "to dismiss it.\n\n"
            "**If it is genuinely orphaned**, delete the file and its test."
        ),
        "security_severity": "3.0",
        "level": "warning",
    },
}


# ---------------------------------------------------------------------------
# SARIF construction
# ---------------------------------------------------------------------------


def build_rule(rule_id, spec, info_uri):
    """Build one reportingDescriptor (a rule) for the driver catalogue."""
    return {
        "id": rule_id,
        "name": spec["name"],
        "shortDescription": {"text": spec["short"]},
        "fullDescription": {"text": spec["full"]},
        "help": {
            # GitHub prefers markdown and falls back to text. Both are supplied
            # because the fallback is what appears in the SARIF viewer plugins.
            "text": spec["help_text"],
            "markdown": spec["help_text"],
        },
        "defaultConfiguration": {"level": spec["level"]},
        "properties": {
            # Numeric STRING, not a number. GitHub's parser rejects a bare
            # numeric literal here, which is a silent per-rule drop rather than
            # an upload error, so it is easy to ship broken.
            "security-severity": spec["security_severity"],
            # Tags appear as filter chips in the Security tab. "maintainability"
            # rather than "security" for the same reason the score is low.
            "tags": ["maintainability", "dead-code"],
        },
        "helpUri": info_uri,
    }


def fingerprint(rule_id, file_path, symbol):
    """Return the stable identity of a finding, as GitHub's primary fingerprint.

    Deliberately excludes the line number: see the module docstring. The hash is
    truncated to 16 hex characters, which is 64 bits - far more than enough to
    keep a few hundred findings distinct, and short enough to read in a diff.
    """
    payload = "|".join([rule_id, file_path, symbol or ""])
    return hashlib.sha256(payload.encode("utf-8")).hexdigest()[:16]


def build_result(rule_id, spec, file_path, line, symbol, message):
    """Build one SARIF result (a single finding)."""
    # GitHub requires a positive line number and rejects the whole run if a
    # region has startLine 0. Missing position data means the finding is about
    # the file as a whole, which SARIF expresses as line 1.
    if not isinstance(line, int) or line < 1:
        line = 1

    return {
        "ruleId": rule_id,
        "level": spec["level"],
        "message": {"text": message},
        "locations": [
            {
                "physicalLocation": {
                    # Forward slashes and repo-relative, always. A Windows path
                    # with backslashes uploads without error and then matches no
                    # file, producing alerts that cannot be opened.
                    "artifactLocation": {"uri": file_path.replace("\\", "/")},
                    "region": {"startLine": line},
                }
            }
        ],
        "partialFingerprints": {
            # "primaryLocationLineHash" is the name GitHub looks for first. The
            # name is historical - the value need not involve a line, and here
            # deliberately does not.
            "primaryLocationLineHash": fingerprint(rule_id, file_path, symbol)
        },
    }


def build_sarif(tool_name, info_uri, rules_used, results, version="unknown"):
    """Assemble the whole SARIF document.

    Only rules that produced at least one result are listed in the catalogue.
    A catalogue entry with no results is legal SARIF but shows up in GitHub as
    a rule with zero alerts, which reads like a rule that was switched off.
    """
    return {
        "version": SARIF_VERSION,
        "$schema": SARIF_SCHEMA,
        "runs": [
            {
                "tool": {
                    "driver": {
                        "name": tool_name,
                        "version": version,
                        "informationUri": info_uri,
                        "rules": rules_used,
                    }
                },
                "results": results,
            }
        ],
    }


# ---------------------------------------------------------------------------
# Per-tool converters
# ---------------------------------------------------------------------------
#
# Each converter takes the tool's parsed JSON and returns (results, rules_used).
# Each must tolerate an empty or absent payload by returning ([], []), because
# "the tool found nothing" is the outcome we are hoping for and must not look
# like a crash.


def convert_deadcode(payload):
    """Convert `deadcode -json` output.

    The shape is a list of packages, each with a Funcs list:

        [ { "Name": "backend",
            "Path": "doc-anonymiser/backend",
            "Funcs": [ { "Name": "App.documentInfos",
                         "Position": {"File": "...", "Line": 408, "Col": 15},
                         "Generated": false } ] } ]

    `deadcode` already skips generated files by default, but the Generated flag
    is honoured here too: the audit must never ask anyone to edit generated code.
    """
    rule_id = "unreachable-function"
    spec = DEADCODE_RULES[rule_id]
    results = []

    if not payload:
        return [], []

    for package in payload:
        if not isinstance(package, dict):
            continue
        pkg_name = package.get("Name") or package.get("Path") or "?"
        for func in package.get("Funcs") or []:
            if not isinstance(func, dict) or func.get("Generated"):
                continue

            symbol = func.get("Name") or "?"
            position = func.get("Position") or {}
            file_path = position.get("File") or ""
            if not file_path:
                # A finding with no file cannot be attached to a line, and
                # GitHub drops results whose location it cannot resolve. Skip
                # it rather than upload something that vanishes silently.
                continue
            file_path = relative_path(file_path)

            results.append(
                build_result(
                    rule_id,
                    spec,
                    file_path,
                    position.get("Line"),
                    symbol,
                    "{} is never called from any main package "
                    "(package {}).".format(symbol, pkg_name),
                )
            )

    rules_used = [build_rule(rule_id, spec, DEADCODE_INFO_URI)] if results else []
    return results, rules_used


def convert_deadexports(payload):
    """Convert `scripts/deadexports` output.

    The shape is defined by the Go types in scripts/deadexports/main.go:

        { "tool": "deadexports",
          "findings": [ {"rule", "file", "line", "symbol", "message"} ],
          "stats": {...} }
    """
    results = []
    seen_rules = []

    findings = (payload or {}).get("findings") or []
    for f in findings:
        if not isinstance(f, dict):
            continue
        rule_id = f.get("rule")
        spec = DEADEXPORTS_RULES.get(rule_id)
        if spec is None:
            # An unknown rule id means the Go scanner grew a rule this
            # converter has not been taught. Say so loudly on stderr rather
            # than dropping findings quietly, but keep converting the rest.
            print(
                "to_sarif: warning: deadexports reported unknown rule {!r}; "
                "add it to DEADEXPORTS_RULES in scripts/to_sarif.py. "
                "This finding was skipped.".format(rule_id),
                file=sys.stderr,
            )
            continue

        results.append(
            build_result(
                rule_id,
                spec,
                relative_path(f.get("file") or ""),
                f.get("line"),
                f.get("symbol") or "",
                f.get("message") or spec["short"],
            )
        )
        if rule_id not in seen_rules:
            seen_rules.append(rule_id)

    rules_used = [
        build_rule(rid, DEADEXPORTS_RULES[rid], DEADEXPORTS_INFO_URI)
        for rid in sorted(seen_rules)
    ]
    return results, rules_used


DEADCODE_INFO_URI = "https://pkg.go.dev/golang.org/x/tools/cmd/deadcode"
DEADEXPORTS_INFO_URI = (
    "https://github.com/rosca75/doc-anonymiser/blob/main/scripts/deadexports/main.go"
)

CONVERTERS = {
    "deadcode": (convert_deadcode, "deadcode", DEADCODE_INFO_URI),
    "deadexports": (convert_deadexports, "deadexports", DEADEXPORTS_INFO_URI),
}


def relative_path(file_path):
    """Make a path repo-relative with forward slashes.

    `deadcode` reports absolute paths. GitHub matches alerts to files by a path
    relative to the repository root, so an absolute one attaches to nothing.
    The repo root is taken as this script's parent directory's parent, which
    holds as long as this file stays at scripts/to_sarif.py.
    """
    if not file_path:
        return ""

    file_path = file_path.replace("\\", "/")
    repo_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    repo_root = repo_root.replace("\\", "/")

    if os.path.isabs(file_path):
        try:
            # os.path.relpath rather than a prefix strip, so that a path
            # reached through a symlinked or differently-cased checkout still
            # resolves. It can return a ../ path if the file is outside the
            # repo, which is handled below.
            rel = os.path.relpath(file_path, repo_root).replace("\\", "/")
        except ValueError:
            # Different drive letters on Windows: no relative path exists.
            return file_path
        if rel.startswith(".."):
            # Outside the repository, e.g. a dependency in the module cache.
            # Return it unchanged; the caller decides, and GitHub will simply
            # not attach it to a file.
            return file_path
        return rel

    return file_path.lstrip("./") if file_path.startswith("./") else file_path


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------


def load_input(path):
    """Read and parse the tool's JSON output.

    Returns None for "there is nothing to convert", which covers a missing file
    and an empty one. Both happen legitimately: `task audit` creates the output
    directory and runs tools that may write nothing, and a tool that found no
    problems may produce a zero-byte file rather than `[]`.
    """
    if not os.path.exists(path):
        print(
            "to_sarif: {} does not exist; emitting an empty run.".format(path),
            file=sys.stderr,
        )
        return None

    with open(path, "r", encoding="utf-8") as handle:
        raw = handle.read().strip()

    if not raw:
        print(
            "to_sarif: {} is empty; emitting an empty run.".format(path),
            file=sys.stderr,
        )
        return None

    try:
        return json.loads(raw)
    except json.JSONDecodeError as err:
        # This one IS an error: the tool ran, wrote something, and it is not
        # JSON. That usually means the tool crashed and its error text landed
        # in the output file, which is worth stopping for.
        raise SystemExit(
            "to_sarif: {} is not valid JSON: {}\n"
            "The tool that wrote it probably failed. Check the matching "
            ".err file in the same directory for its stderr.".format(path, err)
        )


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Convert audit tool JSON output to SARIF 2.1.0.",
        epilog="golangci-lint and govulncheck emit SARIF natively and are not "
        "handled here.",
    )
    parser.add_argument(
        "--tool",
        required=True,
        choices=sorted(CONVERTERS),
        help="which tool produced the input file",
    )
    parser.add_argument("--input", required=True, help="path to the tool's JSON output")
    parser.add_argument("--output", required=True, help="path to write SARIF to")
    parser.add_argument(
        "--tool-version",
        default="unknown",
        help="version string recorded in the SARIF driver (informational)",
    )
    args = parser.parse_args(argv)

    convert, tool_name, info_uri = CONVERTERS[args.tool]

    payload = load_input(args.input)
    if payload is None:
        # A valid run with no results, rather than no file at all. Uploading an
        # empty run is what tells GitHub to CLOSE the alerts from the previous
        # upload in this category. Skipping the upload would leave fixed
        # findings showing as open for ever.
        results, rules_used = [], []
    else:
        results, rules_used = convert(payload)

    document = build_sarif(
        tool_name, info_uri, rules_used, results, version=args.tool_version
    )

    out_dir = os.path.dirname(os.path.abspath(args.output))
    try:
        os.makedirs(out_dir, exist_ok=True)
        with open(args.output, "w", encoding="utf-8") as handle:
            # sort_keys so that two runs over unchanged findings produce
            # byte-identical files; a diff of the SARIF should show real change.
            json.dump(document, handle, indent=2, sort_keys=True)
            handle.write("\n")
    except OSError as err:
        raise SystemExit(
            "to_sarif: could not write {}: {}\n"
            "Check that {} exists and is writable.".format(args.output, err, out_dir)
        )

    print(
        "to_sarif: {} -> {} ({} result(s), {} rule(s))".format(
            args.input, args.output, len(results), len(rules_used)
        ),
        file=sys.stderr,
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
