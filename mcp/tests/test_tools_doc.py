"""TOOLS.md 必须与代码一致。"""

from __future__ import annotations

import importlib.util
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


async def test_tools_doc_is_current():
    spec = importlib.util.spec_from_file_location("gen_tools_doc", ROOT / "scripts" / "gen_tools_doc.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    expected = await module.render()
    actual = (ROOT / "TOOLS.md").read_text(encoding="utf-8") if (ROOT / "TOOLS.md").exists() else ""
    assert actual == expected, "TOOLS.md 过期：运行 .venv/bin/python scripts/gen_tools_doc.py > TOOLS.md"
