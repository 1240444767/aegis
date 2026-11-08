"""aegis-mcp 内核：配置、HTTP 客户端、运行时、接口声明与写工具的公共零件。"""

from .route import DELETE, GET, PATCH, POST, PUT, Route
from .toolkit import (
    DESTRUCTIVE,
    READ,
    WRITE,
    AppRef,
    IsoTime,
    Limit,
    Page,
    Reason,
    RequestID,
    UserID,
    app_id_of,
    body,
    q,
    require_one,
)

__all__ = [
    "GET", "POST", "PUT", "PATCH", "DELETE", "Route",
    "READ", "WRITE", "DESTRUCTIVE",
    "AppRef", "UserID", "Page", "Limit", "IsoTime", "Reason", "RequestID",
    "q", "body", "app_id_of", "require_one",
]
