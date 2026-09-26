"""Regression checks for blocker plans in the E2E policy gate."""

import contextlib
import io
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import check_mock_policy as checker


HEADER = "| ID | 출처 | 등록일 | 해소 방향 | 선행 | 재검토 |\n| --- | --- | --- | --- | --- | --- |"
VALID = [
    "approval-gate-ac5-http-failures",
    "tbm_homelab-k3s-mcp-scenario-e2e/rct_20260914-0011",
    "2026-09-15",
    "real-environment",
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
        row[4] = "없음"
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

    def test_owner_column_cannot_come_back(self):
        """B4: 해소 주체는 항상 이 모델이라 원장에 소관 칸을 두지 않는다.

        칸이 없으면 남의 모델을 적을 자리도 없다 — 규약을 산문에만 두던 동안, 소관이
        `tbm_homelab-k3s-mcp-scenario-e2e` 인 행은 게이트를 rc=0 으로 통과했다.
        """
        owner_header = HEADER.replace(
            "| 해소 방향 |", "| 해소 방향 | 소관 |"
        ).replace("| --- | --- | --- | --- | --- | --- |", "| --- | --- | --- | --- | --- | --- | --- |")
        row = VALID.copy()
        row.insert(4, "tbm_homelab-k3s-mcp-scenario-e2e")
        text = checker.BLOCKERS_OPEN + "\n" + owner_header + "\n| " + " | ".join(row) + " |\n" + checker.BLOCKERS_CLOSE
        with self.assertRaises(ValueError):
            checker.parse_blockers(text)

    def test_duplicate_or_invalid_id_is_rejected(self):
        with self.assertRaises(ValueError):
            checker.parse_blockers(policy(VALID, VALID))
        row = VALID.copy()
        row[0] = "not an ID"
        with self.assertRaises(ValueError):
            checker.parse_blockers(policy(row))

    def test_source_and_direction_are_explicit(self):
        for index, value in [
            (1, "PR #92"), (1, "tbm_sender"), (3, "use a stub"),
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
            row[5] = date
            with self.subTest(date=date):
                self.assertEqual(len(checker.parse_blockers(policy(row))), 1)
        for date in ["2026-09-14", "2026-12-15", "2026-02-30", "2026-9-20", "20261015"]:
            row = VALID.copy()
            row[5] = date
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
            row[5] = event
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
            row[5] = event
            with self.subTest(event=event):
                with self.assertRaises(ValueError):
                    checker.parse_blockers(policy(row))

    def test_prose_may_not_state_registration_counts(self):
        """R7: 마커 밖 산문이 개수를 말하면 잡고, 개수가 아닌 수는 흘려보낸다."""
        text = checker.POLICY.read_text()
        self.assertEqual(checker.find_prose_counts(text), [])
        for claim in [
            "등재 행 수와 상한은 그대로 4다.",
            "허용목록은 4행 그대로다.",
            "등재 4(상한 4)",
            "등재 5 · 상한 5 불변",
        ]:
            with self.subTest(claim=claim):
                self.assertTrue(checker.find_prose_counts(text + "\n\n" + claim))
        for innocent in [
            "허용목록에 등재된 지점의 집합은 불변식 1 이 재는 것이다.",
            "등재가 선언한 세 지점이 `MCP_AUTH_DISABLED=1` 을 넣는다.",
            "등재 대상이 아니다(위 2번 판정).",
            "상한을 검사하는 것은 R6 이다.",
        ]:
            with self.subTest(innocent=innocent):
                self.assertEqual(checker.find_prose_counts(text + "\n\n" + innocent), [])

    def test_marker_blocks_are_masked_not_cut(self):
        """R7 의 마커 제거는 길이를 보존한다 — 줄 번호와 문장 경계가 어긋나면 오탐이 난다."""
        text = checker.POLICY.read_text()
        masked = checker.prose_outside_markers(text)
        self.assertEqual(len(masked), len(text))
        self.assertEqual(masked.count("\n"), text.count("\n"))
        self.assertNotIn(checker.CAP_OPEN, masked)
        self.assertNotIn(checker.LEDGER_OPEN, masked)
        self.assertNotIn(checker.BLOCKERS_OPEN, masked)

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
