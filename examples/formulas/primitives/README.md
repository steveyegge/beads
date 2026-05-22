# Formula primitive examples

One curated `.formula.toml` per *empirically wired* primitive in
`internal/formula/types.go`. Each fixture is minimal, self-documenting,
and exercises exactly one primitive.

## What's here

| Fixture                       | Primitive          | One-line behavior |
|-------------------------------|--------------------|-------------------|
| `loop-count.formula.toml`     | `LoopSpec.count`   | Body expanded N times into `.iterN.<id>` step IDs. |
| `loop-range.formula.toml`     | `LoopSpec.range`   | Body expanded over a numeric range; `loop.var` substitutes into title/description. |
| `children-epic.formula.toml`  | `Step.children`    | Nested steps form an epic with sibling `needs` honored. |
| `branch-fanin.formula.toml`   | `ComposeRules.branch` | Fork-join: parallel steps fan in to a single join step. |
| `condition.formula.toml`      | `Step.condition`   | Variable-driven step inclusion at cook time. |
| `gate-timer.formula.toml`     | `Step.gate`        | Sibling `gate`-typed issue with `await_type` blocks the target step. |
| `on-complete-fanout.formula.toml` | `OnCompleteSpec` | Runtime fanout: at close time the step iterates `metadata.output.<path>` and bonds the named formula once per item, substituting `{item}` / `{item.field}` / `{index}` into spec vars. |

For the full primitive index — every exported struct an agent can write
inside a `.formula.toml`/`.formula.json`, with field types and tags —
run:

```sh
bd formula schema                 # list every primitive
bd formula schema loop            # show LoopSpec fields
bd formula primitives on_complete # alias
bd formula schema --json          # machine-readable
```

## Smoke harness

`cmd/bd/formula_primitives_test.go` walks this directory, parses and
cooks every fixture, and asserts each primitive's observable effect on
the cooked subgraph. A new fixture added here without a registered
assertion is a deliberate test failure: the harness exists to prove
primitives are wired, not just that fixtures parse.

Run with `make test`, or in isolation:

```sh
go test -tags gms_pure_go -run TestFormulaPrimitiveExamples ./cmd/bd/
```

## What's NOT here

* Anything in `internal/formula/types.go` without a verified end-to-end
  parse → cook → pour audit. New primitives earn a fixture once they're
  empirically wired, not when they're declared.
