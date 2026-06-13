# Go Sidecar Handover — 2026-06-13

## Goal
Achieve parity/beat Python sidecar accuracy.

## Active Context & Recent Discovery
We identified that Go's parser cleanup regex (`rxCleanup` in `internal/parse/filename.go`) is more aggressive than Python's `_RE_CLEANUP` (stripping terms like `480p`, `540p`, `KTR`, `p2p`, etc.). This causes:
1. Cache misses on the caching proxy.
2. Title parsing differences causing mismatch penalties (`score -= 500.0` or `-150.0`) during candidates evaluation.

An implementation plan has been proposed to align the filename cleanup regex with Python's reference pattern.

## Code Changes Active
- **Query Formatting**: Aligned `stashSearchQuery` and `stashFindQuery` strings in `internal/stashdb/client.go` with Python reference to match leading/trailing newlines.

## Baseline vs Current Stats (691 Adult Titles)
- **Run 1**: ONLY_PYTHON = 48
- **Latest Run (After Query Alignment)**: ONLY_PYTHON = 23 (34 mismatch overall)
- **Expected After Regex Alignment**: ONLY_PYTHON = 0

## Live Mismatches Test Results (447 Mismatch/Non-Enriched Adult Titles)
A live comparison test was executed against live TPDB and StashDB APIs using a pool of 2 workers (4 total connection channels).
- **Comparison Scripts:**
  - Cached comparison tool: [compare.go](file:///Users/deepanshukumar/Downloads/Octor/sidecar/test/compare.go)
  - Live comparison tool: [compare_live.go](file:///Users/deepanshukumar/Downloads/Octor/sidecar/test/compare_live.go)
- **Detailed Reports & Logs:**
  - Detailed report: [compare_live_results.md](file:///Users/deepanshukumar/Downloads/Octor/sidecar/test/compare_live_results.md)
  - Detailed raw log: [compare_live_output.log](file:///Users/deepanshukumar/Downloads/Octor/sidecar/test/compare_live_output.log)
- **Active Testsheets:**
  - Active Parity Delta Testsheet: [testsheet275-alpha1.json](file:///Users/deepanshukumar/Downloads/Octor/sidecar/test/testsheet275-alpha1.json) (275 titles)
  - Baseline Mismatches Testsheet: [testsheet447-mismatches.json](file:///Users/deepanshukumar/Downloads/Octor/sidecar/test/testsheet447-mismatches.json) (447 titles)
  - Baseline Adult Titles Testsheet: [testsheet691-adult.json](file:///Users/deepanshukumar/Downloads/Octor/sidecar/test/testsheet691-adult.json) (691 titles)

### Live Test Stats
- **Total Processed (Parity Evaluated):** 443
- **Perfect Matches:** 169
- **Neither Enriched:** 101
- **Mismatches (Total):** 173
  - **Only Python:** 7
  - **Only Go:** 48
  - **Field Mismatches (Same/Different Scenes):** 118
    - **Major Mismatches (Different Scenes Matched):** 58
    - **Minor Mismatches (Same Scene, Field Discrepancies):** 60
- **Excluded Network Timeouts (Errors):** 2

## Cleanup Done
- Removed all temporary API worker logs and prompt files (`worker_*`).
- Cleaned up obsolete log files in the test directory (only kept Run 1, Run 4, and last baseline run).
- Cleared cache hits `.json` files.
- Deleted all obsolete/irrelevant test utility scripts.
## Parity Refinement & Testing Rules
When refining the Go sidecar behavior:
1. **Incremental Testsheets**: After each refinement or set of refinements, generate a new testsheet JSON in the `sidecar/test/` directory containing only the remaining non-parities. These must follow the naming convention `testsheet+$totalnum+testname.json` (e.g. `testsheet275-alpha1.json`).
2. **Exclude Perfect Matches**: Running subsequent comparison tests should exclude previously verified perfect matches (the list of titles will decrease over time as parity is achieved).
3. **Exclude Non-Enriched**: Ignore titles that neither side could enrich (`NEITHER_ENRICHED`), as they are typically junk/poorly named files and not useful for parity tracing.
