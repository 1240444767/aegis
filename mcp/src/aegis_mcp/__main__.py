"""命令行入口：`aegis-mcp`（默认 stdio，`--transport streamable-http` 起 HTTP 服务）。"""

from __future__ import annotations

import argparse
import logging
import sys

from . import __version__
from .config import Settings
from .server import build_server


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser(prog="aegis-mcp", description="Aegis 管理端 MCP 服务")
    parser.add_argument("--transport", choices=["stdio", "streamable-http"], default="stdio",
                        help="stdio：由 MCP 客户端拉起（默认）；streamable-http：作为独立 HTTP 服务运行")
    parser.add_argument("--host", default="127.0.0.1", help="HTTP 模式监听地址（默认 127.0.0.1）")
    parser.add_argument("--port", type=int, default=8765, help="HTTP 模式端口（默认 8765）")
    parser.add_argument("--path", default="/mcp", help="HTTP 模式的 MCP 路径（默认 /mcp）")
    parser.add_argument("--base-url", help="覆盖 AEGIS_BASE_URL")
    parser.add_argument("--version", action="version", version=f"aegis-mcp {__version__}")
    args = parser.parse_args(argv)

    settings = Settings.from_env()
    if args.base_url:
        settings = Settings(**{**settings.__dict__, "base_url": args.base_url.strip().rstrip("/")})
    # HTTP 模式下每个客户端可以在请求头里自带密钥，环境变量里的那把只是缺省值
    settings.validate(require_key=args.transport == "stdio")

    # httpx 默认每个请求打一行 INFO，stdio 模式下会刷满客户端的服务日志
    logging.getLogger("httpx").setLevel(logging.WARNING)
    server = build_server(settings)
    if args.transport == "stdio":
        server.run("stdio")
        return
    if args.host not in {"127.0.0.1", "localhost", "::1"} and not settings.api_key:
        print("提示：HTTP 模式未设置 AEGIS_MCP_KEY，每个客户端必须在 Authorization 头携带自己的 MCP 密钥。", file=sys.stderr)
    server.run("streamable-http", host=args.host, port=args.port, streamable_http_path=args.path)


if __name__ == "__main__":
    main()
