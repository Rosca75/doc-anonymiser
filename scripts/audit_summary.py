#!/usr/bin/env python3
"""audit_summary.py - print a per-tool finding count from a directory of SARIF files.

WHY IT READS SARIF AND NOT THE TOOLS' OWN OUTPUT
    The number printed here is the number that will appear in GitHub's Security
    tab, because it is counted from the same bytes that get uploaded. Counting
    the tools' native output instead would let the two drift: a SARIF file that
    silently lost its results would still show a healthy count locally, and the
    discrepancy would only surface days later as an empty category.

WHY PYTHON AND NOT A SHELL LOOP
    Taskfile.yml has to run identically under PowerShell and bash, which rules
    out a shell loop. Python is already required by the SARIF conversion step,
    so this adds no dependency. Standard library only: the owner's laptop sits
    behind a proxy that breaks `pip install`.

USAGE
    python3 scripts/audit_summary.py .audit

    Exits 0 whatever the counts. This is a reporter at the end of `task audit`;
    what blocks a merge is decided in .github/workflows/audit.yml, not here.
    Exits 2 only when the directory itself cannot be read.
"""

import collections
import json
import os
import sys

# Column width for the tool name. Wide enough for the longest name in use
# ("golangci-lint") plus room for one more before this needs revisiting.
NAME_WIDTH = 16


def count_run(path):
    """Return (tool_name, total_results, per_rule_counts) for one SARIF file.

    A file that cannot be parsed returns a count of None, which the caller
    renders as an explicit error line. Returning 0 instead would be a lie in
    the one direction that matters: a broken scan would read as a clean one.
    """
    try:
        with open(path, "r", encoding="utf-8") as handle:
            doc = json.load(handle)
    except (OSError, json.JSONDecodeError) as err:
        return os.path.basename(path), None, str(err)

    runs = doc.get("runs") or []
    if not runs:
        # Legal SARIF, and meaningful: an upload with no runs closes every
        # alert in its category. Report it as zero rather than as an error.
        return os.path.basename(path).replace(".sarif", ""), 0, {}

    run = runs[0]
    driver = (run.get("tool") or {}).get("driver") or {}
    name = driver.get("name") or os.path.basename(path).replace(".sarif", "")

    results = run.get("results") or []
    per_rule = collections.Counter(r.get("ruleId") or "?" for r in results)
    return name, len(results), per_rule


def stderr_for(sarif_path):
    """Return the stderr a tool captured beside its SARIF, or "" if there is none.

    Taskfile.yml redirects each tool's stderr to <tool>.err next to its output.
    Some tools write progress chatter there on a successful run, so this is a
    signal to look, not a verdict: it is only consulted when the finding count
    is zero, where a crash and a clean scan are otherwise indistinguishable.
    """
    base = sarif_path[: -len(".sarif")] if sarif_path.endswith(".sarif") else sarif_path
    err_path = base + ".err"
    try:
        with open(err_path, "r", encoding="utf-8", errors="replace") as handle:
            return handle.read().strip()
    except OSError:
        return ""


def main(argv):
    out_dir = argv[1] if len(argv) > 1 else ".audit"

    if not os.path.isdir(out_dir):
        print(
            "audit_summary: {} does not exist.\n"
            "Run `task audit` first, or pass the output directory as an "
            "argument.".format(out_dir),
            file=sys.stderr,
        )
        return 2

    sarifs = sorted(
        os.path.join(out_dir, n) for n in os.listdir(out_dir) if n.endswith(".sarif")
    )

    print()
    print("Audit summary ({})".format(out_dir))
    print("-" * 52)

    if not sarifs:
        print("No SARIF files. Did `task audit` run, or did every tool fail?")
        print()
        return 0

    total = 0
    suspect = []
    for path in sarifs:
        name, count, detail = count_run(path)

        if count is None:
            # detail holds the parse error in this branch.
            print("{:<{w}} UNREADABLE  {}".format(name, detail, w=NAME_WIDTH))
            continue

        total += count
        print("{:<{w}} {:>5}".format(name, count, w=NAME_WIDTH))

        # A tool that crashed writes nothing and converts to zero findings,
        # which prints identically to a clean scan. That already happened once:
        # `deadcode` was invoked with a flag it does not have, printed its
        # usage to stderr, and the audit reported it as finding nothing. So a
        # zero count is cross-checked against the tool's captured stderr, and
        # a zero with stderr behind it is called out rather than celebrated.
        if count == 0 and stderr_for(path):
            suspect.append(name)

        # Per-rule breakdown, sorted by count descending then rule name, so the
        # thing worth looking at first is on top and the order is stable.
        for rule, n in sorted(detail.items(), key=lambda kv: (-kv[1], kv[0])):
            print("{:<{w}} {:>5}  {}".format("", n, rule, w=NAME_WIDTH))

    print("-" * 52)
    print("{:<{w}} {:>5}".format("total", total, w=NAME_WIDTH))
    print()

    if suspect:
        print("WARNING: {} reported no findings but wrote to stderr.".format(
            ", ".join(suspect)))
        print("A tool that fails looks exactly like a tool that found nothing.")
        print("Check {}<tool>.err before trusting the zero above.".format(
            os.path.join(out_dir, "")))
        print()

    print("Findings are not failures. This layer reports; it does not block.")
    print("Read {} for the lint findings in full.".format(
        os.path.join(out_dir, "golangci-lint.txt")))
    print()
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
