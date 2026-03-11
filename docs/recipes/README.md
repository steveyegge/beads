# Doc Recipes

Doc recipes define deterministic, branch-aware documentation regeneration rules.

Each `*.recipe.yaml` file should include:

1. Sources of truth (CLI commands, code paths, watched refs).
2. Generation strategy (preferred capabilities + fallback path).
3. Validation checks and assertions.
4. Branch policy for drift monitoring.

Current recipe set:

- `cli-reference.recipe.yaml`: CLI reference drift detection and generation contract.

Design reference: `docs/design/doc-drift-recipes.md`.
