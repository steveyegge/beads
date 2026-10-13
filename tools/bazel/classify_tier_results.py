#!/usr/bin/env python3
"""Classify a union `bazel test` run's result by tier, from its BEP.

Usage: classify_tier_results.py --bep <build_event_json_file> --rc <bazel exit code>
                                 --tier NAME=LABEL[,LABEL...] [--tier NAME=LABEL...]
                                 --gate-tier NAME --github-output <path>

item 5 A2's bazel-dolt-int job runs two tiers (server-storage, cmd-dolt) in
one `bazel test --config=dolt-int` invocation. The job carries no job-level
continue-on-error: on push, nightly and dispatch, nothing reads
BAZEL_SERVER_STORAGE off a `workflow_call` output, so a job-level
continue-on-error (reading job.status as success regardless of the `bazel
test` step's own outcome) would let a server-storage failure end the run
green there, where it must stay red exactly as it would on main today. The
`bazel test` step instead carries continue-on-error, and this script -- an
always() step of its own -- computes each tier's result independently from
the BEP and decides whether the job itself must fail.

Bazel's own exit code governs first. 0 (ran, all passed) and 3 (ran, some
test failed) are the only codes this script trusts the BEP for: a build
failure, an infra error or a crash (any other code) cannot be safely
attributed to one tier's tests, so both tiers are marked "failure" and the
script always exits 1 for it (no silent cmd-dolt-only downgrade of a
not-actually-a-test-failure).

Otherwise each --tier is "failure" iff a BEP testResult for one of its
labels has a non-PASSED status (FAILED, TIMEOUT, REMOTE_FAILURE, INCOMPLETE,
FLAKY all count: these lanes run flaky=False, so a flaky-looking status is
still a failure), or if the BEP has no result at all for one of its labels
(an attempt that never ran cannot be presumed to have passed; a genuine
coverage gap already fails the job through check_testcases.py's own step).

Exit status is 1 iff --gate-tier's tier is "failure" (or the exit-code branch
above applied); a failure confined to another tier leaves this script's own
exit 0, so the job step -- and so job.status -- stays green, mirroring that
tier's own advisory, continue-on-error nature outside mode remote. Every
tier's result, win or lose, is appended to --github-output as
"<name>_result=success|failure" (NAME lowercased, "-" turned into "_"), so
the job can expose one output per tier.
"""

import argparse
import json
import sys

PASS_STATUS = "PASSED"


def read_test_statuses(bep_path):
    """Return {label: set(status seen across every testResult event)}."""
    statuses = {}
    with open(bep_path, encoding="utf-8") as f:
        for line in f:
            if not line.strip():
                continue
            try:
                ev = json.loads(line)
            except ValueError:
                continue
            tr_id = ev.get("id", {}).get("testResult")
            tr = ev.get("testResult")
            if not tr_id or not tr:
                continue
            label = tr_id["label"]
            statuses.setdefault(label, set()).add(tr.get("status", "NO_STATUS"))
    return statuses


def tier_result(statuses, labels):
    """Return "success" or "failure" for one tier's labels."""
    for label in labels:
        seen = statuses.get(label)
        if not seen or seen - {PASS_STATUS}:
            return "failure"
    return "success"


def output_key(name):
    return name.lower().replace("-", "_") + "_result"


def classify(bep_path, rc, tiers, gate_tier):
    """Return ({tier: "success"|"failure"}, exit_status)."""
    if rc not in (0, 3):
        return {name: "failure" for name in tiers}, 1
    statuses = read_test_statuses(bep_path)
    results = {name: tier_result(statuses, labels) for name, labels in tiers.items()}
    return results, (1 if results.get(gate_tier) == "failure" else 0)


def parse_tier_arg(spec):
    name, _, labels = spec.partition("=")
    if not name or not labels:
        raise argparse.ArgumentTypeError(f"--tier {spec!r}: want NAME=LABEL[,LABEL...]")
    return name, [l for l in labels.split(",") if l]


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--bep", required=True)
    p.add_argument("--rc", type=int, required=True)
    p.add_argument("--tier", action="append", type=parse_tier_arg, default=[], dest="tiers", metavar="NAME=LABEL[,LABEL...]")
    p.add_argument("--gate-tier", required=True)
    p.add_argument("--github-output", required=True)
    args = p.parse_args(argv)

    tiers = dict(args.tiers)
    if not tiers:
        p.error("at least one --tier is required")
    if args.gate_tier not in tiers:
        p.error(f"--gate-tier {args.gate_tier!r} is not one of --tier's names {sorted(tiers)}")

    results, status = classify(args.bep, args.rc, tiers, args.gate_tier)

    with open(args.github_output, "a", encoding="utf-8") as f:
        for name, result in results.items():
            line = f"{output_key(name)}={result}"
            print(line)
            f.write(line + "\n")

    if status:
        reason = (
            f"bazel exit {args.rc} (not 0 or 3): a build or infra failure, not attributable to one tier"
            if args.rc not in (0, 3)
            else f"{args.gate_tier} tier failed"
        )
        print(f"::error::classify_tier_results: {reason}", file=sys.stderr)
    return status


if __name__ == "__main__":
    sys.exit(main())
