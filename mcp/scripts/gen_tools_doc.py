"""生成 TOOLS.md：按模块列出全部工具、读写类别与一句话说明。

    .venv/bin/python scripts/gen_tools_doc.py > TOOLS.md

tests/test_tools_doc.py 会检查 TOOLS.md 与代码一致，新增或改名工具后记得重新生成。
"""

from __future__ import annotations

import asyncio
import sys

from fastmcp import Client

from aegis_mcp.core.config import Settings
from aegis_mcp.server import build_server, discover_modules


async def render() -> str:
    server = build_server(Settings(base_url="http://localhost", api_key="aegis_mcp_" + "x" * 43))
    async with Client(server) as client:
        tools = await client.list_tools()
    by_name = {tool.name: tool for tool in tools}
    lines = ["# aegis-mcp 工具清单", "", "> 由 `scripts/gen_tools_doc.py` 生成，勿手改。", ""]
    lines.append(f"共 {len(tools)} 个工具。类别：读 = 只读；写 = 可逆或增量的写；危险 = 不可逆或影响面大的写（客户端会要求确认）。")
    lines.append("")
    for module in discover_modules():
        prefix = f"{module.NAMESPACE}_" if module.NAMESPACE else ""
        name = module.__name__.rsplit(".", 1)[-1]
        mine = sorted(
            (tool for tool in tools if (tool.name.startswith(prefix) if prefix else not any(
                tool.name.startswith(f"{m.NAMESPACE}_") for m in discover_modules() if m.NAMESPACE))),
            key=lambda tool: tool.name,
        )
        if not mine:
            continue
        summary = (module.__doc__ or "").strip().splitlines()[0]
        lines += [f"## {name}（{len(mine)}）", "", summary, "", "| 工具 | 类别 | 说明 |", "|---|---|---|"]
        for tool in mine:
            tags = set((tool.meta or {}).get("fastmcp", {}).get("tags") or [])
            kind = "危险" if "destructive" in tags else "写" if "write" in tags else "读"
            first = (tool.description or "").strip().splitlines()[0].replace("|", "\\|")
            lines.append(f"| `{tool.name}` | {kind} | {first} |")
            by_name.pop(tool.name, None)
        lines.append("")
    if by_name:
        lines += ["## 其他", ""] + [f"- `{name}`" for name in sorted(by_name)] + [""]
    return "\n".join(lines)


if __name__ == "__main__":
    sys.stdout.write(asyncio.run(render()))
