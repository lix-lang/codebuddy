// Package agent 实现 AI 编程助手的核心 Agent。
//
// 本文件（command.go）实现斜杠命令系统。
//
// # 什么是斜杠命令？
//
// 用户在对话中输入以 / 开头的指令，Agent 不把它当普通消息发给 LLM，
// 而是直接执行对应的操作（类似 Claude Code 的 /compact、/clear 等）。
//
// # 支持的命令（共 19 个，按需求文档 5.4 节定义）
//
//	/help              显示所有可用命令
//	/model <name>      切换 LLM 模型
//	/context           查看当前上下文（加载的文件、token 使用量、上下文健康度）
//	/diff              查看当前所有未提交的改动（可视化 side-by-side）
//	/undo              撤销上一次文件修改
//	/clear             清空对话历史
//	/save              保存当前对话
//	/cost              查看本次会话的 token 消耗和费用明细
//	/config            查看/修改配置
//	/verify            手动触发结果验证
//	/search <query>    手动触发 Web 搜索
//	/mcp               查看已连接的 MCP 服务器状态和可用工具
//	/compact           手动压缩上下文（自动压缩在 90% 时触发）
//	/add <file>        手动添加文件到上下文
//	/remove <file>     从上下文中移除文件
//	/export [file]     导出当前完整上下文到文件（默认 context_export.md）
//	/import <file>     导入之前导出的上下文文件
//	/theme <name>      切换终端主题
//	/lang <language>   切换主语言（启用该语言的专属优化）
//
// # 命令处理流程
//
// 用户输入 → ParseCommand() 判断是否是斜杠命令
//
//	├─ 是命令 → Agent 直接执行对应操作，返回结果文本
//	└─ 不是命令 → 当普通消息，交给 LLM 处理
package agent

import (
	"fmt"
	"strings"
)

// CommandType 斜杠命令类型
type CommandType string

const (
	// CmdHelp 显示所有可用命令
	CmdHelp CommandType = "/help"

	// CmdModel 切换 LLM 模型
	// 用法：/model gpt-4o、/model glm-4-flash、/model deepseek-chat
	CmdModel CommandType = "/model"

	// CmdContext 查看当前上下文
	// 显示加载了哪些文件、token 使用量、上下文健康度
	CmdContext CommandType = "/context"

	// CmdDiff 查看当前所有未提交的改动
	// 可视化 side-by-side diff 展示
	CmdDiff CommandType = "/diff"

	// CmdUndo 撤销上一次文件修改
	// 从 .codebuddy/backup/ 恢复最近的备份
	CmdUndo CommandType = "/undo"

	// CmdClear 清空对话历史
	// 开始全新对话，保留项目上下文和手动添加的文件
	CmdClear CommandType = "/clear"

	// CmdSave 保存当前对话
	// 把对话历史持久化到本地存储
	CmdSave CommandType = "/save"

	// CmdCost 查看 token 消耗和费用明细
	// 显示本次会话的 token 用量和对应费用
	CmdCost CommandType = "/cost"

	// CmdConfig 查看/修改配置
	// 不带参数显示当前配置，带参数修改指定配置项
	CmdConfig CommandType = "/config"

	// CmdVerify 手动触发结果验证
	// 对上一次操作的结果进行独立验证
	CmdVerify CommandType = "/verify"

	// CmdSearch 手动触发 Web 搜索
	// 用法：/search Go 1.22 新特性
	CmdSearch CommandType = "/search"

	// CmdMCP 查看已连接的 MCP 服务器状态
	// 显示各 MCP 服务器的连接状态和可用工具
	CmdMCP CommandType = "/mcp"

	// CmdCompact 手动压缩上下文
	// 把早期对话合并成摘要，释放 token 空间
	// 自动压缩在上下文使用达 90% 时触发
	CmdCompact CommandType = "/compact"

	// CmdAdd 手动添加文件到上下文
	// 用法：/add internal/config/config.go
	// 添加后每轮对话都会包含这个文件的内容
	CmdAdd CommandType = "/add"

	// CmdRemove 从上下文中移除文件
	// 用法：/remove internal/config/config.go
	CmdRemove CommandType = "/remove"

	// CmdExport 导出当前完整上下文到文件
	// 用法：/export、/export my_context.md
	// 默认文件名 context_export.md，导出 system prompt + 项目概览 + 文件 + 对话历史 + token 统计
	CmdExport CommandType = "/export"

	// CmdImport 导入之前导出的上下文文件
	// 用法：/import context_export.md
	// 恢复之前导出的上下文状态
	CmdImport CommandType = "/import"

	// CmdTheme 切换终端主题
	// 用法：/theme dark、/theme light
	CmdTheme CommandType = "/theme"

	// CmdLang 切换主语言
	// 启用该语言的专属优化（AST 分析、代码风格学习等）
	// 用法：/lang go、/lang python、/lang typescript
	CmdLang CommandType = "/lang"

	// CmdScan 手动扫描项目
	// 扫描项目目录结构、文件信息，用于构建上下文
	CmdScan CommandType = "/scan"

	// CmdUnknown 未知命令（不匹配任何已知命令时的默认值）
	CmdUnknown CommandType = ""
)

// Command 解析后的斜杠命令
type Command struct {
	Type CommandType // 命令类型，如 CmdCompact、CmdAdd
	Args string      // 命令参数（命令名后面的部分），如 "/add config.go" 的参数是 "config.go"
}

// commandHelp 命令帮助文本，用于 /help 命令显示
var commandHelp = map[CommandType]string{
	CmdHelp:    "显示所有可用命令",
	CmdModel:   "切换 LLM 模型，用法: /model <模型名>",
	CmdContext: "查看当前上下文（文件、token、健康度）",
	CmdDiff:    "查看当前所有未提交的改动",
	CmdUndo:    "撤销上一次文件修改",
	CmdClear:   "清空对话历史，开始新对话",
	CmdSave:    "保存当前对话",
	CmdCost:    "查看 token 消耗和费用明细",
	CmdConfig:  "查看/修改配置",
	CmdVerify:  "手动触发结果验证",
	CmdSearch:  "手动触发 Web 搜索，用法: /search <关键词>",
	CmdMCP:     "查看 MCP 服务器状态和可用工具",
	CmdCompact: "手动压缩上下文（自动压缩在 90% 时触发）",
	CmdAdd:     "添加文件到上下文，用法: /add <文件路径>",
	CmdRemove:  "从上下文移除文件，用法: /remove <文件路径>",
	CmdExport:  "导出当前完整上下文，用法: /export [文件名]",
	CmdImport:  "导入上下文文件，用法: /import <文件路径>",
	CmdTheme:   "切换终端主题，用法: /theme <主题名>",
	CmdLang:    "切换主语言，用法: /lang <语言>",
	CmdScan:    "扫描项目目录，构建上下文索引",
}

// commandOrder 命令显示顺序（按使用频率和逻辑分组排列）
var commandOrder = []CommandType{
	// 对话管理
	CmdClear, CmdCompact, CmdSave, CmdUndo,
	// 上下文操作
	CmdAdd, CmdRemove, CmdExport, CmdImport, CmdContext,
	// 模型与配置
	CmdModel, CmdConfig, CmdCost,
	// 工具与搜索
	CmdSearch, CmdMCP, CmdVerify, CmdDiff,
	// 外观
	CmdTheme, CmdLang,
	// 项目
	CmdScan,
	// 帮助
	CmdHelp,
}

// allCommands 所有已知命令的集合，用于 ParseCommand 快速查找
var allCommands = map[CommandType]bool{
	CmdHelp:    true,
	CmdModel:   true,
	CmdContext: true,
	CmdDiff:    true,
	CmdUndo:    true,
	CmdClear:   true,
	CmdSave:    true,
	CmdCost:    true,
	CmdConfig:  true,
	CmdVerify:  true,
	CmdSearch:  true,
	CmdMCP:     true,
	CmdCompact: true,
	CmdAdd:     true,
	CmdRemove:  true,
	CmdExport:  true,
	CmdImport:  true,
	CmdTheme:   true,
	CmdLang:    true,
	CmdScan:    true,
}

// ParseCommand 解析用户输入是否是斜杠命令
// input 是用户的原始输入，如 "/compact"、"/add config.go"
//
// 返回值：
//   - Command：解析后的命令（如果不是命令，Type 为 CmdUnknown）
//   - bool：是否是斜杠命令
//
// 用法：
//
//	cmd, ok := ParseCommand("/add config.go")
//	// cmd = Command{Type: CmdAdd, Args: "config.go"}
//	// ok = true
//
//	cmd, ok := ParseCommand("帮我看看 config.go")
//	// cmd = Command{Type: CmdUnknown, Args: ""}
//	// ok = false
func ParseCommand(input string) (Command, bool) {
	// strings.TrimSpace 去掉首尾空格
	input = strings.TrimSpace(input)

	// 斜杠命令必须以 / 开头
	if !strings.HasPrefix(input, "/") {
		return Command{Type: CmdUnknown}, false
	}

	// 按空格拆分命令名和参数
	// strings.SplitN(input, " ", 2) 最多拆成 2 部分
	// 比如 "/add config.go" → parts = ["/add", "config.go"]
	// 比如 "/clear" → parts = ["/clear"]
	parts := strings.SplitN(input, " ", 2)

	// 命令名转小写，不区分大小写（/Help 和 /help 等效）
	cmdType := CommandType(strings.ToLower(parts[0]))

	// 在已知命令集合中查找
	if !allCommands[cmdType] {
		return Command{Type: CmdUnknown}, false
	}

	// 提取参数（如果有）
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	return Command{Type: cmdType, Args: args}, true
}

// FormatHelp 格式化帮助文本，显示所有可用命令
// 返回人类可读的帮助信息，供 /help 命令使用
// 按 commandOrder 定义的顺序排列（按使用频率和逻辑分组）
func FormatHelp() string {
	var sb strings.Builder
	sb.WriteString("可用命令:\n\n")

	// 按 commandOrder 顺序列出所有命令
	for _, cmd := range commandOrder {
		sb.WriteString(fmt.Sprintf("  %-20s %s\n", string(cmd), commandHelp[cmd]))
	}

	return sb.String()
}
