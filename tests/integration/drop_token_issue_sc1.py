"""drop_token_issue: 발급한 토큰과 curl 견본으로 실물 drop 에 올리고 내려받는다 (e2e).

검증 시나리오: test-drop-token-issue.md#시나리오 1
실행 대상: primary
"""

from __future__ import annotations

import asyncio
import json
import os
import re
import tempfile
from datetime import datetime, timedelta, timezone

from mcp import ClientSession

from _drop import drop_url, issue, run_template
from _helpers import base_url, open_session, wait_for_healthz

VALID_MINUTES = 5


async def test_drop_token_issue_sc1_curl_round_trip(session: ClientSession) -> None:
    before = datetime.now(timezone.utc)
    payload = await issue(session, {"valid_minutes": VALID_MINUTES, "label": "e2e 왕복"})
    after = datetime.now(timezone.utc)

    assert payload["token"].startswith("drk_"), payload["token"][:4]
    assert payload["created_by"] == "sa:homelab-k3s-mcp/homelab-k3s-mcp", payload["created_by"]
    # Go 는 소수 초를 9자리까지 내고, 3.12 의 fromisoformat 은 6자리를 넘으면 거부할 수 있다.
    stamp = re.sub(r"(\.\d{6})\d+", r"\1", payload["expires_at"].replace("Z", "+00:00"))
    expires = datetime.fromisoformat(stamp)
    window = timedelta(minutes=VALID_MINUTES)
    assert before + window - timedelta(seconds=30) <= expires <= after + window + timedelta(seconds=30), (
        f"expires_at {expires} is not now + {VALID_MINUTES} min ({before}..{after})"
    )
    usage = payload["usage"]
    token = payload["token"]
    assert usage["setup"] == f"export DROP_TOKEN='{token}'", usage["setup"]

    content = os.urandom(4096) + b"drop-e2e-round-trip"
    with drop_url() as local, tempfile.TemporaryDirectory() as work:
        source = os.path.join(work, "source.bin")
        with open(source, "wb") as fh:
            fh.write(content)

        uploaded = json.loads(
            run_template(usage["upload"], local, token, work, **{"./path/to/file": source})
        )
        drop_id, password = uploaded["id"], uploaded["password"]
        assert [f["size"] for f in uploaded["files"]] == [len(content)], uploaded

        listing = json.loads(
            run_template(usage["download_list"], local, token, work, **{"<id>": drop_id, "<password>": password})
        )
        assert listing["id"] == drop_id and listing["files"][0]["name"] == "source.bin", listing

        stdout = run_template(
            usage["download_file"], local, token, work,
            **{"<id>": drop_id, "<password>": password, "<name>": "downloaded.bin", "<n>": "0"},
        )
        assert stdout == "", f"download printed to stdout instead of saving with -o: {stdout[:80]!r}"
        with open(os.path.join(work, "downloaded.bin"), "rb") as fh:
            assert fh.read() == content, "downloaded bytes differ from the uploaded file"

        mine = json.loads(run_template(usage["list"], local, token, work))
        assert drop_id in [d["id"] for d in mine], mine
        run_template(usage["delete"], local, token, work, **{"<id>": drop_id})
        mine = json.loads(run_template(usage["list"], local, token, work))
        assert drop_id not in [d["id"] for d in mine], mine


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url, timeout=60.0) as session:
        print("--- curl round trip (AC: drop-token-issue/AC1, AC2) ---")
        await test_drop_token_issue_sc1_curl_round_trip(session)
        print("ok: drop-token-issue 시나리오 1")


if __name__ == "__main__":
    asyncio.run(run())
