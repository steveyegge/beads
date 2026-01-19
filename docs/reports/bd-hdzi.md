# Fix Report: bd-hdzi - gt done creates merge requests without gt:merge-request label

## Summary
Fixed `gt done` to ensure merge request beads always have the `gt:merge-request` label by adding an explicit verification check.

## Problem
The bug report indicated that `gt done` was creating merge request beads with `type=merge-request` but without the `gt:merge-request` label, causing them to be invisible to `gt mq list`.

## Investigation
1. Found that automatic type-to-label conversion code already exists in `internal/beads/beads.go` (commit 96970071, merged Jan 9)
2. When creating a bead with `Type: "merge-request"`, it should automatically add `--labels=gt:merge-request`
3. Manual testing confirmed that beads created with the label flag do receive the label correctly
4. Existing MR beads (bd-vbeu, etc.) all have the label, suggesting the auto-conversion is working

## Solution
Added a belt-and-suspenders safety check in `/home/ubuntu/gastown/internal/cmd/done.go`:
- After creating an MR bead, verify the `gt:merge-request` label is present
- If missing, add it explicitly with a warning
- Non-fatal error handling to ensure `gt done` completes even if label addition fails

## Implementation
- Added `hasLabel()` helper function to check for specific labels
- Added verification code after MR bead creation in `runDone()`
- Built and installed updated `gt` binary

## Repository
Changes were made to the **gastown** repository (not beads) since `gt done` is part of gastown:
- Branch: `polecat/pearl/bd-hdzi@mklra1ie`
- Commit: c9a322cd
- Pushed to: https://github.com/groblegark/gastown

## Status
- [x] Code changes implemented
- [x] Binary built and installed
- [x] Branch pushed to origin
- [x] Bead bd-hdzi closed

The fix is ready for review and merge to main.
