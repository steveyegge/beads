#!/usr/bin/env python3
"""Unit tests for classify_tier_results.py.

Run directly: python3 tools/bazel/classify_tier_results_test.py
(or: python3 -m unittest tools.bazel.classify_tier_results_test, from the repo root)
"""

import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import classify_tier_results as ctr  # noqa: E402

SS = "//internal/storage/dolt:dolt_server_full_test"
SS2 = "//internal/storage/dolt:dolt_server_conformance_test"
CD = "//cmd/bd:bd_dolt_server_test"
CD2 = "//tests/regression:regression_test"

TIERS = {"server-storage": [SS, SS2], "cmd-dolt": [CD, CD2]}


def write_bep(events):
    f = tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False, encoding="utf-8")
    for ev in events:
        f.write(json.dumps(ev) + "\n")
    f.close()
    return f.name


def result_event(label, status):
    return {"id": {"testResult": {"label": label}}, "testResult": {"status": status}}


def all_passed_bep():
    return write_bep([result_event(l, "PASSED") for l in (SS, SS2, CD, CD2)])


class TierResultTest(unittest.TestCase):
    def test_all_passed_is_success(self):
        statuses = {SS: {"PASSED"}, SS2: {"PASSED"}}
        self.assertEqual(ctr.tier_result(statuses, [SS, SS2]), "success")

    def test_any_failed_label_is_failure(self):
        statuses = {SS: {"PASSED"}, SS2: {"FAILED"}}
        self.assertEqual(ctr.tier_result(statuses, [SS, SS2]), "failure")

    def test_missing_label_is_failure(self):
        statuses = {SS: {"PASSED"}}
        self.assertEqual(ctr.tier_result(statuses, [SS, SS2]), "failure")

    def test_flaky_status_is_failure(self):
        # These lanes run flaky=False: a FLAKY-looking status is still a
        # failure, never silently treated as a pass.
        statuses = {SS: {"PASSED", "FLAKY"}}
        self.assertEqual(ctr.tier_result(statuses, [SS]), "failure")


class ClassifyTest(unittest.TestCase):
    def test_rc_0_both_tiers_succeed(self):
        path = all_passed_bep()
        try:
            results, status = ctr.classify(path, 0, TIERS, "server-storage")
        finally:
            os.unlink(path)
        self.assertEqual(results, {"server-storage": "success", "cmd-dolt": "success"})
        self.assertEqual(status, 0)

    def test_server_storage_fail_only_fails_the_job(self):
        path = write_bep([
            result_event(SS, "FAILED"),
            result_event(SS2, "PASSED"),
            result_event(CD, "PASSED"),
            result_event(CD2, "PASSED"),
        ])
        try:
            results, status = ctr.classify(path, 3, TIERS, "server-storage")
        finally:
            os.unlink(path)
        self.assertEqual(results, {"server-storage": "failure", "cmd-dolt": "success"})
        self.assertEqual(status, 1)

    def test_cmd_dolt_fail_only_leaves_the_job_green(self):
        path = write_bep([
            result_event(SS, "PASSED"),
            result_event(SS2, "PASSED"),
            result_event(CD, "FAILED"),
            result_event(CD2, "PASSED"),
        ])
        try:
            results, status = ctr.classify(path, 3, TIERS, "server-storage")
        finally:
            os.unlink(path)
        self.assertEqual(results, {"server-storage": "success", "cmd-dolt": "failure"})
        # A cmd-dolt-only test failure must not fail the job: it stays
        # advisory, exactly as bazel-cmd-dolt's own continue-on-error outside
        # mode remote.
        self.assertEqual(status, 0)

    def test_both_tiers_fail(self):
        path = write_bep([
            result_event(SS, "FAILED"),
            result_event(SS2, "PASSED"),
            result_event(CD, "TIMEOUT"),
            result_event(CD2, "PASSED"),
        ])
        try:
            results, status = ctr.classify(path, 3, TIERS, "server-storage")
        finally:
            os.unlink(path)
        self.assertEqual(results, {"server-storage": "failure", "cmd-dolt": "failure"})
        self.assertEqual(status, 1)

    def test_build_failure_fails_both_tiers_and_the_job(self):
        # rc=1 (BUILD_FAILURE): not attributable to one tier, even though the
        # BEP happens to show every known label passing (e.g. an unrelated
        # target failed to build under --keep_going).
        path = all_passed_bep()
        try:
            results, status = ctr.classify(path, 1, TIERS, "server-storage")
        finally:
            os.unlink(path)
        self.assertEqual(results, {"server-storage": "failure", "cmd-dolt": "failure"})
        self.assertEqual(status, 1)

    def test_gate_tier_selects_which_tier_fails_the_job(self):
        # Flip which tier gates: a cmd-dolt failure now fails the job, a
        # server-storage failure does not. (Not bazel-dolt-int's own wiring,
        # which always gates on server-storage; this just proves the script
        # is not hardcoded to one tier name.)
        path = write_bep([
            result_event(SS, "FAILED"),
            result_event(SS2, "PASSED"),
            result_event(CD, "PASSED"),
            result_event(CD2, "PASSED"),
        ])
        try:
            results, status = ctr.classify(path, 3, TIERS, "cmd-dolt")
        finally:
            os.unlink(path)
        self.assertEqual(results, {"server-storage": "failure", "cmd-dolt": "success"})
        self.assertEqual(status, 0)


class MainTest(unittest.TestCase):
    def test_writes_github_output_and_exits_1_on_gate_tier_failure(self):
        bep = write_bep([
            result_event(SS, "FAILED"),
            result_event(SS2, "PASSED"),
            result_event(CD, "PASSED"),
            result_event(CD2, "PASSED"),
        ])
        out = tempfile.NamedTemporaryFile(mode="w", delete=False)
        out.close()
        try:
            status = ctr.main([
                "--bep", bep,
                "--rc", "3",
                "--tier", f"server-storage={SS},{SS2}",
                "--tier", f"cmd-dolt={CD},{CD2}",
                "--gate-tier", "server-storage",
                "--github-output", out.name,
            ])
            with open(out.name, encoding="utf-8") as f:
                lines = set(f.read().splitlines())
        finally:
            os.unlink(bep)
            os.unlink(out.name)
        self.assertEqual(status, 1)
        self.assertEqual(lines, {"server_storage_result=failure", "cmd_dolt_result=success"})

    def test_exits_0_when_only_the_non_gate_tier_fails(self):
        bep = write_bep([
            result_event(SS, "PASSED"),
            result_event(SS2, "PASSED"),
            result_event(CD, "FAILED"),
            result_event(CD2, "PASSED"),
        ])
        out = tempfile.NamedTemporaryFile(mode="w", delete=False)
        out.close()
        try:
            status = ctr.main([
                "--bep", bep,
                "--rc", "3",
                "--tier", f"server-storage={SS},{SS2}",
                "--tier", f"cmd-dolt={CD},{CD2}",
                "--gate-tier", "server-storage",
                "--github-output", out.name,
            ])
        finally:
            os.unlink(bep)
            os.unlink(out.name)
        self.assertEqual(status, 0)


if __name__ == "__main__":
    unittest.main()
