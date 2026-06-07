// Package mcp 实现 MCP（Model Context Protocol）客户端集成。
//
// MCP 是 Anthropic 主导的开放协议，让 codebuddy 能连接外部工具和数据源。
// 比如 GitHub MCP 服务器可以让 Agent 直接操作 PR/Issue，Jira 服务器可以管理任务。
//
// 核心组件：
//   - Host：管理所有 MCP 服务器连接的生命周期
//   - Client：与单个 MCP 服务器通信
//   - ToolAdapter：把 MCP 工具适配为内置 Tool 接口，Agent 循环不区分工具来源
//
// 传输方式：stdio（本地子进程）和 Streamable HTTP（远程服务器）。
// 断路器保护：MCP 服务器挂掉不影响核心功能。
package mcp
