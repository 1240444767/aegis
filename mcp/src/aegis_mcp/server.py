"""组装：根服务 + 中间件 + 自动发现 modules/ 下的领域模块并按命名空间挂载。

每个领域模块是一个独立的 FastMCP 子服务（modules/<名>.py 里的 `server` 与 `NAMESPACE`），
挂载后工具名为 `<命名空间>_<工具名>`，如 users_search、vip_grant。`NAMESPACE = ""` 的模块不加前缀
（通用层的 whoami / call_api 就是这样）。新增领域只需新建一个模块文件，不必改这里。
"""

from __future__ import annotations

import importlib
import logging
import pkgutil
from collections.abc import Iterable
from types import ModuleType

from fastmcp import FastMCP
from fastmcp.server.middleware.error_handling import ErrorHandlingMiddleware
from fastmcp.server.middleware.logging import LoggingMiddleware
from fastmcp.server.middleware.response_limiting import ResponseLimitingMiddleware
from fastmcp.server.middleware.timing import TimingMiddleware

from . import __version__, modules
from .core.config import Settings
from .core.runtime import init_runtime

INSTRUCTIONS = """\
你正在通过 MCP 密钥操作 Aegis 管理端（多租户用户系统：应用、用户、流水、会员、抽奖、积分商城、卡密、工单、内容、审计……）。

工作方式：
1. 先调用 whoami 看清这把密钥代表谁、能访问哪些应用、权限范围、是否只读。
2. 工具按领域分组，名字前缀即领域：apps_ / users_ / audit_ / points_ / wallet_ / payment_ / store_ / cardkey_ /
   vip_ / lottery_ / content_ / tickets_ ……。优先用这些带类型参数的工具。
3. 没有现成工具时：list_endpoints 按关键词找接口 → describe_endpoint 看参数 → call_api 调用（覆盖全部约 970 条管理端接口）。
4. 应用参数 app 可填 appKey 或数字应用 ID；user_id 指管理端用户 ID（数据库主键）。
5. 写操作真实生效并记入审计日志（会话列 mcp:<密钥前缀>）。删除、封禁、扣减、撤销会员、退款、批量推送等
   不可逆或影响面大的操作前，先向用户复述将要做什么并取得确认；支持预览的（如 vip_grant_preview）先预览。
6. 遇到 40164 / 40165 / 40166 等权限错误不要换接口绕过，直接告诉用户需要在控制台调整密钥范围。
"""


def discover_modules() -> list[ModuleType]:
    found: list[ModuleType] = []
    for info in sorted(pkgutil.iter_modules(modules.__path__), key=lambda item: item.name):
        if info.name.startswith("_"):
            continue
        module = importlib.import_module(f"{modules.__name__}.{info.name}")
        if isinstance(getattr(module, "server", None), FastMCP) and hasattr(module, "NAMESPACE"):
            found.append(module)
    return found


def build_server(
    settings: Settings,
    *,
    read_only: bool = False,
    include_modules: Iterable[str] | None = None,
    exclude_modules: Iterable[str] | None = None,
) -> FastMCP:
    init_runtime(settings)
    root = FastMCP(
        "aegis",
        instructions=INSTRUCTIONS,
        version=__version__,
        middleware=[
            ErrorHandlingMiddleware(logger=logging.getLogger("aegis_mcp.errors"), include_traceback=False, transform_errors=False),
            TimingMiddleware(logger=logging.getLogger("aegis_mcp.timing")),
            LoggingMiddleware(logger=logging.getLogger("aegis_mcp.calls"), methods=["tools/call"]),
            ResponseLimitingMiddleware(
                max_size=settings.max_result_chars,
                truncation_suffix="\n\n…（结果过长已截断：请缩小 limit、加过滤条件或分页查看）",
            ),
        ],
    )
    include = {name.strip() for name in include_modules or [] if name.strip()}
    exclude = {name.strip() for name in exclude_modules or [] if name.strip()}
    for module in discover_modules():
        name = module.__name__.rsplit(".", 1)[-1]
        # 通用层（core）恒挂载：没有它，模块之外的接口就无路可走
        if name != "core" and ((include and name not in include) or name in exclude):
            continue
        root.mount(module.server, namespace=module.NAMESPACE or None)
    if read_only:
        root.disable(tags={"write"})
    return root
