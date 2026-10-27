package auditcatalog

// MCP 密钥与 MCP 服务自述（docs/mcp.md）。
func init() {
	Register("admin",
		View("GET /api/admin/profile/mcp-keys", "查看 MCP 密钥", ""),
		View("GET /api/admin/profile/mcp-keys/catalog", "查看 MCP 密钥权限目录", ""),
		View("GET /api/admin/profile/mcp-keys/:keyId", "查看 MCP 密钥详情", "MCP 密钥"),
		Write("POST /api/admin/profile/mcp-keys", "签发 MCP 密钥", "MCP 密钥", SeverityHigh),
		Write("PUT /api/admin/profile/mcp-keys/:keyId", "修改 MCP 密钥范围", "MCP 密钥", SeverityHigh),
		Write("POST /api/admin/profile/mcp-keys/:keyId/rotate", "轮换 MCP 密钥", "MCP 密钥", SeverityHigh),
		Write("POST /api/admin/profile/mcp-keys/:keyId/enable", "启用 MCP 密钥", "MCP 密钥", SeverityMedium),
		Write("POST /api/admin/profile/mcp-keys/:keyId/disable", "停用 MCP 密钥", "MCP 密钥", SeverityMedium),
		Write("DELETE /api/admin/profile/mcp-keys/:keyId", "撤销 MCP 密钥", "MCP 密钥", SeverityHigh),
		View("GET /api/admin/mcp/whoami", "MCP 查看凭证身份", ""),
		View("GET /api/admin/mcp/endpoints", "MCP 查看接口目录", ""),
	)
}
