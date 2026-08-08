#!/usr/bin/env python3
"""Sabotage cases for multichat's rune-safe last-message preview.

Two mechanisms live in one file here: the general walk-back helper, and the one
call site that uses it — the conversation preview served as `last_message`.
Both get cases. Scoring the helper alone would not be enough: its own tests can
be perfect while the call site byte-cuts, and that is exactly the state this
repo was in before the fix.

Run with --diffs at least once and read each applied edit against its label. A
row prints the name it was given, not the edit it made.
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from sabotage import Case, REPO, score  # noqa: E402

PACKAGES = ["./internal/matrix/"]

TARGET = REPO / "internal" / "matrix" / "truncate.go"

CASES = [
    # --- the helper ---
    # Written as drifted comparisons rather than deletions so no identifier is
    # orphaned and the case scores instead of reporting a compile error.
    Case("the walk-back never runs (comparison drifted)",
         [("for cut > 0 && !utf8.RuneStart(s[cut])",
           "for cut > len(s) && !utf8.RuneStart(s[cut])")]),
    Case("the walk-back runs the wrong way",
         [("\t\tcut--", "\t\tcut++")]),
    Case("the helper trims to nothing",
         [("\treturn s[:cut]", "\treturn \"\"")]),
    Case("the helper ignores its budget until the string is twice it",
         [("\tif len(s) <= maxBytes {", "\tif len(s) <= maxBytes*2 {")]),
    Case("the non-positive budget guard stops firing",
         [("\tif maxBytes <= 0 {", "\tif maxBytes <= -2 {")]),

    # --- the call site ---
    # This one is the defect itself. If the suite stays green here, the preview
    # is unpinned no matter how well the helper scores.
    Case("the preview byte-cuts the body again",
         [("truncateAtRuneBoundary(body, lastMessagePreviewBytes)",
           "body[:lastMessagePreviewBytes]")]),
    Case("the preview drops its ellipsis",
         [("lastMessagePreviewBytes) + \"…\"", "lastMessagePreviewBytes) + \"\"")]),
    Case("the preview truncates bodies that are exactly at the budget",
         [("\tif len(body) <= lastMessagePreviewBytes {",
           "\tif len(body) < lastMessagePreviewBytes {")]),
    Case("the budget itself drifts from 100 to 50",
         [("const lastMessagePreviewBytes = 100",
           "const lastMessagePreviewBytes = 50")]),

    Case("CONTROL known-positive: the preview returns a fixed string",
         [("\treturn truncateAtRuneBoundary(body, lastMessagePreviewBytes) + \"…\"",
           "\treturn \"SABOTAGE\"")]),
    Case("CONTROL known-negative: <= 0 rewritten as < 1, identical for ints",
         [("\tif maxBytes <= 0 {", "\tif maxBytes < 1 {")],
         expected_unnoticed="a behavioural no-op; it must NOT be caught"),
]

if __name__ == "__main__":
    score(TARGET, PACKAGES, CASES)
