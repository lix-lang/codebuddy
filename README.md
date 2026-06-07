# Codebuddy

基于 Go 语言的 AI 编程助手，运行在终端。

## 特性

- **Agent 自主执行** — 多步推理 + 工具调用，自动完成编码、测试、修复全流程
- **流式输出** — SSE 实时显示 LLM 回复，支持 Markdown 渲染 + 语法高亮
- **代码分析** — Go 项目用 `go/ast` 深度解析，其他语言按扩展名自动路由分析器
- **Web 搜索** — 多引擎搜索 + 智能摘要，搜索结果压缩后注入上下文，省 token
- **MCP 协议** — 连接外部 MCP 服务器（GitHub、Jira 等），扩展工具能力
- **幻觉防护** — 参数校验 → 结果验证 → AST 交叉验证 → 自纠错循环，四层防护
- **模型无关** — 支持 OpenAI / Claude / DeepSeek / GLM / Ollama
- **单二进制** — Go 编译，无运行时依赖

## 快速开始

### 安装

```bash
go install github.com/lix-lang/codebuddy@latest
```

### 初始化

```bash
codebuddy init
# 交互式配置：选择 LLM 提供商 → 输入 API Key → 选择默认模型
```

### 使用

```bash
# 默认模式：全自主执行
codebuddy

# 只读模式：不修改任何文件
codebuddy ask "这个项目的鉴权是怎么实现的？"

# 规划模式：只生成计划不执行
codebuddy plan "给 user 模块添加修改密码接口"
```

### 对话中

```text
> 帮我看看 main.go 有什么问题

⚠️ 发现 3 个问题：
1. 未处理的错误（第 42 行）
2. 硬编码端口（第 15 行）
3. 缺少优雅关闭

需要我自动修复吗？ [Y/n]
```

## 配置

配置文件：`~/.codebuddy/config.json`，项目级：`.codebuddy/config.json`

```json
{
  "llm": {
    "provider": "openai",
    "model": "gpt-4o",
    "api_key": "${OPENAI_API_KEY}",
    "base_url": "https://api.openai.com/v1"
  },
  "primary_language": "go",
  "agent": {
    "max_steps": 15,
    "require_confirm": true
  }
}
```

GLM 用户只需改 `base_url`：
```json
{
  "llm": {
    "provider": "openai",
    "model": "glm-4",
    "api_key": "${GLM_API_KEY}",
    "base_url": "https://open.bigmodel.cn/api/paas/v4"
  }
}
```

命令行管理配置：
```bash
codebuddy config set llm.model deepseek-chat
codebuddy config get llm.api_key
codebuddy config list
```

## 斜杠命令

| 命令 | 功能 |
|------|------|
| `/help` | 显示帮助 |
| `/model <name>` | 切换模型 |
| `/context` | 查看上下文状态（加载文件、token 用量） |
| `/diff` | 查看当前改动 |
| `/undo` | 撤销上一次修改 |
| `/clear` | 清空对话历史 |
| `/cost` | 查看 token 消耗和费用 |
| `/lang <language>` | 切换主语言 |
| `/search <query>` | 手动 Web 搜索 |
| `/mcp` | 查看 MCP 服务器状态 |
| `/compact` | 手动压缩上下文 |

## 内置工具

| 工具 | 功能 |
|------|------|
| `read_file` | 读取文件 |
| `write_file` | 创建/覆盖文件（需确认） |
| `edit_file` | 精确替换文件内容（需确认） |
| `list_dir` | 列出目录结构 |
| `search_code` | 搜索代码内容 |
| `run_command` | 执行 shell 命令（需确认） |
| `analyze` | 代码结构分析（AST 级别） |
| `go_test` | 运行 Go 测试 |
| `read_image` | 识别图片内容 |
| `web_search` | Web 搜索 |
| `web_fetch` | 抓取 URL 内容 |

## 项目结构

```text
codebuddy/
├── cmd/codebuddy/         # 入口
├── internal/
│   ├── agent/             # Agent 核心循环（ReAct）
│   ├── llm/               # LLM 客户端（OpenAI/Claude/Ollama）
│   ├── tool/              # 内置工具
│   ├── config/            # 配置管理（JSON）
│   ├── context/           # 上下文管理
│   ├── security/          # 安全（路径校验、备份、命令安全）
│   ├── hallucination/     # 幻觉防护
│   ├── tui/               # 终端 UI（bubbletea）
│   ├── mcp/               # MCP 协议集成
│   ├── search/            # Web 搜索引擎
│   ├── pubsub/            # 事件总线
│   └── skills/            # Skill 系统（用户自定义）
└── configs/               # 默认配置
```

## 开发计划

| 阶段 | 内容 |
|------|------|
| Phase 1 | CLI + 配置 + 安全 + 全部工具 + LLM 客户端 + Agent 循环 + TUI |
| Phase 2 | 搜索引擎 + Web 工具 + 可视化 Diff |
| Phase 3 | 项目扫描 + 上下文管理 + 对话持久化 |
| Phase 4 | Go AST 深度集成 + 幻觉防护完善 |
| Phase 5 | Git 集成 + 多模型 + 代码审查 |
| Phase 6 | MCP 集成 + 命令面板 + 多标签会话 |
| Phase 7 | Skills 技能系统（用户自定义） |

## 参考

从以下工具中汲取了设计灵感：

- [Claude Code](https://docs.anthropic.com/en/docs/claude-code) — Agent 循环、工具调用、MCP 协议
- [Codex CLI](https://github.com/openai/codex) — Plan Mode、OS 级沙箱、AGENTS.md 分层指令
- [Aider](https://aider.chat) — Repo Map、分层 Edit 匹配、Architect/Editor 双模型
- [OpenCode](https://github.com/opencode-ai/opencode) — Go 实现参考、泛型 Provider、PubSub
- [Devin](https://devin.ai) — 上下文压缩模型
- [Jules](https://jules.google) — 自验证门禁
- [Amazon Q](https://aws.amazon.com/q/developer) — Agent Profile
- [Cursor](https://cursor.sh) — 三模式 CLI（ask/plan/agent）
- [Goose](https://github.com/block/goose) — Recipe 系统

## License

MIT
