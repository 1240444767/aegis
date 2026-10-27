"""运行配置：全部来自环境变量，命令行参数可覆盖其中几项。"""

from __future__ import annotations

import os
from dataclasses import dataclass

KEY_PREFIX = "aegis_mcp_"


@dataclass(frozen=True)
class Settings:
    # Aegis 后端（或控制台，控制台会把 /api 与 /openapi.json 反代到后端）的根地址
    base_url: str
    # 默认使用的 MCP 密钥；HTTP 模式下客户端也可以在每次请求的 Authorization 头里自带
    api_key: str
    timeout: float = 30.0
    # 单次工具结果的最大字符数，超出截断并提示改用分页或过滤参数
    max_result_chars: int = 60_000
    verify_tls: bool = True

    @classmethod
    def from_env(cls) -> "Settings":
        base_url = os.environ.get("AEGIS_BASE_URL", "").strip().rstrip("/")
        return cls(
            base_url=base_url,
            api_key=os.environ.get("AEGIS_MCP_KEY", "").strip(),
            timeout=_float_env("AEGIS_TIMEOUT", 30.0),
            max_result_chars=int(_float_env("AEGIS_MAX_RESULT_CHARS", 60_000)),
            verify_tls=os.environ.get("AEGIS_VERIFY_TLS", "1").strip().lower() not in {"0", "false", "no"},
        )

    def validate(self, *, require_key: bool) -> None:
        if not self.base_url:
            raise SystemExit("缺少 AEGIS_BASE_URL：请设置为 Aegis 后端或控制台的根地址，例如 https://aegis.example.com")
        if not self.base_url.startswith(("http://", "https://")):
            raise SystemExit(f"AEGIS_BASE_URL 必须以 http:// 或 https:// 开头：{self.base_url}")
        if require_key and not self.api_key:
            raise SystemExit("缺少 AEGIS_MCP_KEY：请在 Aegis 控制台「个人资料 → MCP 密钥」签发后填入")
        if self.api_key and not self.api_key.startswith(KEY_PREFIX):
            raise SystemExit(f"AEGIS_MCP_KEY 格式不对：MCP 密钥以 {KEY_PREFIX} 开头")


def _float_env(name: str, default: float) -> float:
    raw = os.environ.get(name, "").strip()
    if not raw:
        return default
    try:
        return float(raw)
    except ValueError:
        raise SystemExit(f"{name} 必须是数字：{raw}") from None
