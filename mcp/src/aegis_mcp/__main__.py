"""命令行入口：`aegis-mcp`（默认 stdio；`--transport http` 起 Streamable HTTP 服务）。"""

from __future__ import annotations

import argparse
import dataclasses
import logging
import sys

from . import __version__
from .core.config import Settings
from .server import build_server, discover_modules


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser(prog="aegis-mcp", description="Aegis 管理端 MCP 服务")
    parser.add_argument("--transport", choices=["stdio", "http", "streamable-http", "sse"], default="stdio",
                        help="stdio：由 MCP 客户端拉起（默认）；http：作为独立 HTTP 服务运行")
    parser.add_argument("--host", default="127.0.0.1", help="HTTP 模式监听地址（默认 127.0.0.1）")
    parser.add_argument("--port", type=int, default=8765, help="HTTP 模式端口（默认 8765）")
    parser.add_argument("--path", default="/mcp", help="HTTP 模式的 MCP 路径（默认 /mcp）")
    parser.add_argument("--base-url", help="覆盖 AEGIS_BASE_URL")
    parser.add_argument("--read-only", action="store_true", help="只暴露只读工具（与密钥本身是否只读无关，双保险）")
    parser.add_argument("--modules", default="", help="只挂载这些模块，逗号分隔（通用层恒挂载），如 users,vip,lottery")
    parser.add_argument("--exclude-modules", default="", help="不挂载这些模块，逗号分隔")
    parser.add_argument("--list-modules", action="store_true", help="列出可用模块后退出")
    parser.add_argument("--version", action="version", version=f"aegis-mcp {__version__}")
    args = parser.parse_args(argv)

    if args.list_modules:
        for module in discover_modules():
            name = module.__name__.rsplit(".", 1)[-1]
            print(f"{name:12} {(module.__doc__ or '').strip().splitlines()[0] if module.__doc__ else ''}")
        return

    settings = Settings.from_env()
    if args.base_url:
        settings = dataclasses.replace(settings, base_url=args.base_url.strip().rstrip("/"))
    stdio = args.transport == "stdio"
    # HTTP 模式下每个客户端可以在请求头里自带密钥，环境变量里的那把只是缺省值
    settings.validate(require_key=stdio)

    logging.basicConfig(level=logging.WARNING, stream=sys.stderr)
    logging.getLogger("httpx").setLevel(logging.WARNING)
    server = build_server(
        settings,
        read_only=args.read_only,
        include_modules=args.modules.split(","),
        exclude_modules=args.exclude_modules.split(","),
    )
    if stdio:
        server.run("stdio", show_banner=False)
        return
    if args.host not in {"127.0.0.1", "localhost", "::1"} and settings.api_key:
        print("警告：HTTP 模式对外监听且设置了 AEGIS_MCP_KEY，未带请求头的客户端都会以这把密钥的身份操作。", file=sys.stderr)
    transport = "sse" if args.transport == "sse" else "http"
    server.run(transport, host=args.host, port=args.port, path=args.path, show_banner=False)


if __name__ == "__main__":
    main()
