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

# The suite's fixture guards, by message. These fire when a mutation stops the
# test input reaching the code under test — the test falling over, not detecting
# anything — and `go test` exits non-zero for that exactly as it does for a real
# assertion. Naming them here is what lets the engine tell the two apart; see
# sabotage.classify_caught().
#
# This suite is where the 42nd pass found the defect: "the budget itself drifts
# from 100 to 50" resizes the fixture out of range of the guard on line 98, so
# the row went red with nothing having checked the budget. It reads CAUGHT today
# because 6d4e23e added TestLastMessagePreviewBudgetIsAHundredBytes, which pins
# the budget against a literal — and with these markers declared, the run proves
# that rather than inheriting the same red the guard used to supply.
GUARD_MARKERS = (
    "input never reaches the cut",
    "case is not a short body",
)

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
    # Kept under its original name so the 42nd pass's record stays comparable,
    # but read it as what it is: 100 -> 50 halves the budget, which pins it to a
    # BAND rather than to 100. A suite can catch it while agreeing with 99 and
    # 101. The value tests are the +-1 pair in the section below.
    Case("the budget itself drifts from 100 to 50",
         [("const lastMessagePreviewBytes = 100",
           "const lastMessagePreviewBytes = 50")]),

    # --- boundary VALUES: every numeric literal this file compares against or
    # slices by, moved by ONE unit ---
    #
    # The cases above move mechanisms: a comparison is drifted, a branch is
    # disabled, a call is swapped out. None of them moves a number by one, and a
    # suite can pin every mechanism here while agreeing with a budget of 99 or a
    # cut that starts a byte early. An UNNOTICED row below is a literal nothing
    # in the suite has an opinion about.

    # The non-positive guard's threshold, upward. `<= 0` and `<= 1` differ only
    # for a budget of exactly 1, so this is caught only by a case that asks for
    # one byte.
    Case("the non-positive guard swallows a budget of exactly 1 (0 -> 1)",
         [("\tif maxBytes <= 0 {", "\tif maxBytes <= 1 {")]),

    # The within-budget test, both directions. Downward makes a string of exactly
    # maxBytes take the cut path; upward serves a string one byte over the budget
    # whole.
    Case("a string of exactly the budget takes the cut path (<= -> <)",
         [("\tif len(s) <= maxBytes {", "\tif len(s) < maxBytes {")]),
    Case("a string one byte over the budget is served whole (maxBytes -> maxBytes+1)",
         [("\tif len(s) <= maxBytes {", "\tif len(s) <= maxBytes+1 {")]),

    # Where the cut starts. Both directions, because a cut one byte early returns
    # a valid-but-short prefix and a cut one byte late overruns the budget, and
    # neither is visible to an assertion that only checks the result is valid.
    Case("the cut starts one byte early (maxBytes -> maxBytes-1)",
         [("\tcut := maxBytes\n", "\tcut := maxBytes - 1\n")]),
    Case("the cut starts one byte late (maxBytes -> maxBytes+1)",
         [("\tcut := maxBytes\n", "\tcut := maxBytes + 1\n")]),

    # The walk-back's floor. `cut > 1` stops one step early, so a budget smaller
    # than the leading rune returns that rune's first byte instead of nothing.
    Case("the walk-back floor moves up by one (cut > 0 -> cut > 1)",
         [("for cut > 0 && ", "for cut > 1 && ")]),

    # Which byte the walk-back inspects. Off by one here reads the last byte of
    # the kept prefix rather than the first byte past it.
    Case("the walk-back inspects the byte before the cut (s[cut] -> s[cut-1])",
         [("!utf8.RuneStart(s[cut])", "!utf8.RuneStart(s[cut-1])")]),

    # The step size hidden inside `cut--`. Stepping by two skips a boundary, so
    # the walk-back can land one rune further back than it needed to.
    Case("the walk-back steps by two instead of one",
         [("\t\tcut--", "\t\tcut -= 2")]),

    # The floor's other direction. `cut > -1` lets the loop evaluate s[0], which
    # for any valid UTF-8 string is a rune start, so the loop exits there anyway.
    Case("the walk-back floor moves down by one (cut > 0 -> cut > -1)",
         [("for cut > 0 && ", "for cut > -1 && ")],
         expected_unnoticed="a no-op for valid UTF-8: s[0] is always a rune start, so the "
                            "loop exits at cut=0 either way. FALSIFY IT by adding a case "
                            "whose input begins with a continuation byte — this then walks "
                            "to cut=-1 and panics."),

    # The returned prefix's endpoint, both directions.
    Case("the returned prefix is one byte short (s[:cut] -> s[:cut-1])",
         [("\treturn s[:cut]", "\treturn s[:cut-1]")]),
    Case("the returned prefix is one byte long (s[:cut] -> s[:cut+1])",
         [("\treturn s[:cut]", "\treturn s[:cut+1]")]),

    # The budget literal itself, moved by one in each direction. This is the pair
    # the 100 -> 50 case above is NOT.
    Case("the budget drifts by one, upward (100 -> 101)",
         [("const lastMessagePreviewBytes = 100",
           "const lastMessagePreviewBytes = 101")]),
    Case("the budget drifts by one, downward (100 -> 99)",
         [("const lastMessagePreviewBytes = 100",
           "const lastMessagePreviewBytes = 99")]),

    # The preview's own short-body test, upward. A body of exactly 101 bytes is
    # then served whole and with no ellipsis. The downward move is already
    # covered by "the preview truncates bodies that are exactly at the budget".
    Case("a body one byte over the budget is previewed whole (budget -> budget+1)",
         [("\tif len(body) <= lastMessagePreviewBytes {",
           "\tif len(body) <= lastMessagePreviewBytes+1 {")]),

    # The budget the call site asks for, as distinct from the constant. Moving
    # this leaves the constant at 100 and still serves 99 bytes.
    Case("the preview asks the helper for one byte less than the budget",
         [("truncateAtRuneBoundary(body, lastMessagePreviewBytes)",
           "truncateAtRuneBoundary(body, lastMessagePreviewBytes-1)")]),
    Case("the preview asks the helper for one byte more than the budget",
         [("truncateAtRuneBoundary(body, lastMessagePreviewBytes)",
           "truncateAtRuneBoundary(body, lastMessagePreviewBytes+1)")]),

    Case("CONTROL known-positive: the preview returns a fixed string",
         [("\treturn truncateAtRuneBoundary(body, lastMessagePreviewBytes) + \"…\"",
           "\treturn \"SABOTAGE\"")]),
    Case("CONTROL known-negative: <= 0 rewritten as < 1, identical for ints",
         [("\tif maxBytes <= 0 {", "\tif maxBytes < 1 {")],
         expected_unnoticed="a behavioural no-op; it must NOT be caught"),
]

if __name__ == "__main__":
    sys.exit(score(TARGET, PACKAGES, CASES, GUARD_MARKERS))
