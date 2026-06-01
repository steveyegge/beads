package fix

// TestFixDependencyTargetExprMatchesCanonical removed: both
// `fixDependencyTargetExpr` and `issueops.DepTargetExpr` are legacy COALESCE
// expressions superseded by the split-dependency schema. The canonical
// `issueops.DepTargetExpr` was deleted; the local copy will follow when
// validation.go is migrated to per-table typed-column SQL (task 20 / cleanup).
