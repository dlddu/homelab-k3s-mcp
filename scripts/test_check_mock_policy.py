"""Regression checks for blocker plans in the E2E policy gate."""

import contextlib
import io
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import check_mock_policy as checker


HEADER = "| ID | 출처 | 등록일 | 해소 방향 | 소관 | 선행 | 재검토 |\n| --- | --- | --- | --- | --- | --- | --- |"
VALID = [
    "approval-gate-ac5-http-failures",
    "tbm_homelab-k3s-mcp-scenario-e2e/rct_20260914-0011",
    "2026-09-15",
    "real-environment",
    "tbm_homelab-k3s-mcp-e2e-mock-policy",
    "tbm_homelab-k3s-mcp-scenario-e2e: tests/k8s/kind/gatekeeper-fixture.yaml",
    "2026-10-15",
]


def policy(*rows):
    table = HEADER + "".join("\n| " + " | ".join(row) + " |" for row in rows)
    return checker.BLOCKERS_OPEN + "\n" + table + "\n" + checker.BLOCKERS_CLOSE


class BlockerPolicyTests(unittest.TestCase):
    def setUp(self):
        checker.failures.clear()

    def test_waiting_plan_and_empty_ledger_are_valid(self):
        self.assertEqual(len(checker.parse_blockers(policy(VALID))), 1)
        self.assertEqual(checker.parse_blockers(policy()), [])

    def test_ready_plan_and_exception_direction_are_valid(self):
        row = VALID.copy()
        row[3] = "exception-registration"
        row[5] = "없음"
        self.assertEqual(checker.parse_blockers(policy(row))[0]["선행"], "없음")

    def test_missing_duplicate_reversed_and_empty_markers_fail_closed(self):
        for text in [
            "", HEADER, checker.BLOCKERS_OPEN + checker.BLOCKERS_CLOSE,
            policy(VALID) + checker.BLOCKERS_OPEN,
            policy(VALID) + checker.BLOCKERS_CLOSE,
            checker.BLOCKERS_CLOSE + HEADER + checker.BLOCKERS_OPEN,
        ]:
            with self.subTest(text=text):
                with self.assertRaises(ValueError):
                    checker.parse_blockers(text)

    def test_malformed_tables_cannot_silently_drop_rows(self):
        for text in [
            policy(VALID).replace("| ID |", "| Name |"),
            policy(VALID).replace("| --- |", "| x |", 1),
            policy(VALID).replace("\n| approval", "\napproval"),
            policy(VALID).replace(" | 2026-10-15 |", " | 2026-10-15"),
            policy(VALID[:-1]),
            policy(VALID + ["extra"]),
            policy(VALID).replace(HEADER, HEADER + "\nnot a table row"),
        ]:
            with self.subTest(text=text):
                with self.assertRaises(ValueError):
                    checker.parse_blockers(text)

    def test_every_cell_is_required(self):
        for index in range(len(VALID)):
            for missing in ["", "—", "TBD", "미정", "`   `", "` TBD `"]:
                row = VALID.copy()
                row[index] = missing
                with self.subTest(index=index, value=missing):
                    with self.assertRaises(ValueError):
                        checker.parse_blockers(policy(row))

    def test_duplicate_or_invalid_id_is_rejected(self):
        with self.assertRaises(ValueError):
            checker.parse_blockers(policy(VALID, VALID))
        row = VALID.copy()
        row[0] = "not an ID"
        with self.assertRaises(ValueError):
            checker.parse_blockers(policy(row))

    def test_source_owner_and_direction_are_explicit(self):
        for index, value in [
            (1, "PR #92"), (1, "tbm_sender"), (3, "use a stub"),
            (4, "이 모델 소관이 아님"), (4, "TODO"), (4, "tbm_"),
        ]:
            row = VALID.copy()
            row[index] = value
            with self.subTest(index=index, value=value):
                with self.assertRaises(ValueError):
                    checker.parse_blockers(policy(row))
        row = VALID.copy()
        row[1] = "policy:#approval-gate-follow-up"
        self.assertEqual(len(checker.parse_blockers(policy(row))), 1)

    def test_review_date_boundary_and_calendar_are_checked(self):
        for date in ["2026-09-15", "2026-12-14"]:
            row = VALID.copy()
            row[6] = date
            with self.subTest(date=date):
                self.assertEqual(len(checker.parse_blockers(policy(row))), 1)
        for date in ["2026-09-14", "2026-12-15", "2026-02-30", "2026-9-20", "20261015"]:
            row = VALID.copy()
            row[6] = date
            with self.subTest(date=date):
                with self.assertRaises(ValueError):
                    checker.parse_blockers(policy(row))

    def test_registration_date_is_canonical(self):
        for date in ["20260915", "2026-02-30", "2026-09-15T00:00:00Z"]:
            row = VALID.copy()
            row[2] = date
            with self.subTest(date=date):
                with self.assertRaises(ValueError):
                    checker.parse_blockers(policy(row))

    def test_observable_review_events_are_valid(self):
        for event in [
            "file:tests/k8s/kind/gatekeeper-fixture.yaml",
            "task:tbm_homelab-k3s-mcp-scenario-e2e/rct_20260914-0011",
            "pr:dlddu/homelab-k3s-mcp#92",
        ]:
            row = VALID.copy()
            row[6] = event
            with self.subTest(event=event):
                self.assertEqual(len(checker.parse_blockers(policy(row))), 1)

    def test_vague_events_and_unsafe_paths_are_rejected(self):
        for event in [
            "later", "when ready", "task:rct_20260914-0011", "pr:#92",
            "pr:dlddu/homelab-k3s-mcp#0", "file:", "file:/tmp/test",
            "file:../test", "file:tests/../test", "file:tests//test",
            "file:tests/./test", "file:tests/a b", "file:C:\\test", "file:tests/a#b",
        ]:
            row = VALID.copy()
            row[6] = event
            with self.subTest(event=event):
                with self.assertRaises(ValueError):
                    checker.parse_blockers(policy(row))

    def test_cli_gate_rejects_missing_ledger_without_weakening_exception_checks(self):
        original = checker.POLICY.read_text()
        start = original.index(checker.BLOCKERS_OPEN)
        end = original.index(checker.BLOCKERS_CLOSE) + len(checker.BLOCKERS_CLOSE)
        with tempfile.TemporaryDirectory() as directory:
            document = Path(directory) / "policy.md"
            document.write_text(original[:start] + original[end:])
            stdout, stderr = io.StringIO(), io.StringIO()
            with patch.object(checker, "POLICY", document), contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
                result = checker.main()
            self.assertEqual(result, 1)
            self.assertIn("[B2]", stderr.getvalue())
            self.assertNotIn("[R", stderr.getvalue())

    def test_real_policy_keeps_exception_cap_independent_from_blocker_debt(self):
        text = checker.POLICY.read_text()
        start = text.index(checker.BLOCKERS_OPEN)
        end = text.index(checker.BLOCKERS_CLOSE) + len(checker.BLOCKERS_CLOSE)
        second = VALID.copy()
        second[0] = "another-failure-path"
        for rows in [(), (VALID,), (VALID, second)]:
            changed = text[:start] + policy(*rows) + text[end:]
            with self.subTest(debt_count=len(rows)):
                self.assertEqual(len(checker.parse_blockers(changed)), len(rows))
                self.assertEqual(checker.parse_cap(changed), checker.parse_cap(text))
                self.assertEqual(checker.parse_ledger(changed), checker.parse_ledger(text))


if __name__ == "__main__":
    unittest.main()
