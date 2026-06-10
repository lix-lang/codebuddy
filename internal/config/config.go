package config

// LLMConfig LLM 提供商配置
type LLMConfig struct {
	Provider    string  `json:"provider"`    // "openai" / "claude" / "ollama"
	Model       string  `json:"model"`       // "gpt-4o" / "deepseek-chat" / "glm-4"
	APIKey      string  `json:"api_key"`     // API Key（支持 ${ENV_VAR} 环境变量插值）
	BaseURL     string  `json:"base_url"`    // API 地址
	MaxTokens   int     `json:"max_tokens"`  // 最大输出 token
	Temperature float64 `json:"temperature"` // 温度（0.0~2.0，越小越确定）
}

// FallbackModel 备用模型配置
type FallbackModel struct {
	Provider string `json:"provider"` // 提供商
	Model    string `json:"model"`    // 模型名
	BaseURL  string `json:"base_url"` // API 地址
	APIKey   string `json:"api_key"`  // API Key
}

// AgentConfig Agent 行为配置
type AgentConfig struct {
	MaxSteps          int     `json:"max_steps"`            // 最大执行步数（默认 15）
	MaxRetries        int     `json:"max_retries"`          // 最大重试次数
	RequireConfirm    bool    `json:"require_confirm"`      // 破坏性操作是否需要用户确认
	AutoTest          bool    `json:"auto_test"`            // 代码生成后是否自动测试
	MaxCostPerSession float64 `json:"max_cost_per_session"` // 单次会话最大费用（元）
}

// SafetyConfig 安全配置
type SafetyConfig struct {
	MaxFileChanges   int  `json:"max_file_changes"`   // 单次任务最大修改文件数
	BackupBeforeEdit bool `json:"backup_before_edit"` // 编辑前是否自动备份
	GitAutoCommit    bool `json:"git_auto_commit"`    // 是否自动 git commit
}

// MCPServerConfig MCP 服务器配置
type MCPServerConfig struct {
	Command string            `json:"command,omitempty"` // stdio 模式：启动命令
	URL     string            `json:"url,omitempty"`     // HTTP 模式：服务器地址
	Env     map[string]string `json:"env,omitempty"`     // 环境变量
	Headers map[string]string `json:"headers,omitempty"` // HTTP 请求头
}

// SearchEngineConfig 搜索引擎配置
type SearchEngineConfig struct {
	APIKey string `json:"api_key,omitempty"` // API Key（可选）
	CX     string `json:"cx,omitempty"`      // Google 自定义搜索 CX
	URL    string `json:"url,omitempty"`     // SearXNG 地址
}

// SearchConfig 搜索配置
type SearchConfig struct {
	DefaultEngine string                        `json:"default_engine"` // 默认搜索引擎
	Engines       map[string]SearchEngineConfig `json:"engines"`        // 各引擎配置
	CacheTTL      int                           `json:"cache_ttl"`      // 缓存过期时间（秒）
	MaxResults    int                           `json:"max_results"`    // 最大返回结果数
	Summarize     bool                          `json:"summarize"`      // 是否自动摘要
}

// HallucinationConfig 幻觉防护配置
type HallucinationConfig struct {
	Enabled             bool `json:"enabled"`                // 是否启用幻觉防护
	VerifyFileWrite     bool `json:"verify_file_write"`      // 验证文件写入结果
	VerifyTestResult    bool `json:"verify_test_result"`     // 独立验证测试结果
	ASTCrossCheck       bool `json:"ast_cross_check"`        // AST 交叉验证
	MaxSelfCorrection   int  `json:"max_self_correction"`    // 自纠错最大次数
	ForceReadBeforeEdit bool `json:"force_read_before_edit"` // 强制先读后改
}

// ContextConfig 上下文管理配置
type ContextConfig struct {
	MaxTokens        int  `json:"max_tokens"`        // 上下文窗口大小
	MaxFiles         int  `json:"max_files"`         // 最多包含的文件数
	SummaryThreshold int  `json:"summary_threshold"` // 触发摘要的对话轮数阈值
	AutoCompact      bool `json:"auto_compact"`      // 是否自动压缩
}

// ToolsConfig 工具配置
type ToolsConfig struct {
	Shell ShellToolConfig `json:"shell"` // shell 工具配置
	File  FileToolConfig  `json:"file"`  // 文件工具配置
}

// ShellToolConfig shell 命令工具配置
type ShellToolConfig struct {
	Blocked []string `json:"blocked"` // 禁止的命令模式
	Timeout string   `json:"timeout"` // 默认超时时间
}

// FileToolConfig 文件工具配置
type FileToolConfig struct {
	MaxSize      string `json:"max_size"`      // 文件大小上限
	WatchChanges bool   `json:"watch_changes"` // 是否监控文件变化
}

// TUIConfig TUI 界面配置
type TUIConfig struct {
	Theme    string `json:"theme"`    // 主题名（dark/light）
	Currency string `json:"currency"` // 费用币种（CNY/USD）
}

// CostConfig 费用控制配置
type CostConfig struct {
	MaxPerSession float64 `json:"max_per_session"` // 单次会话费用上限
	WarnAt        float64 `json:"warn_at"`         // 费用预警阈值
}

// LogConfig 日志配置
type LogConfig struct {
	Level string `json:"level"` // "debug" / "info" / "warn" / "error"
	File  string `json:"file"`  // 日志文件路径
	Trace bool   `json:"trace"` // 是否记录完整 LLM 交互日志
}

// Config 顶层配置，对应 config.json 文件
type Config struct {
	LLM             LLMConfig                  `json:"llm"`              // LLM 提供商配置
	Fallback        []FallbackModel            `json:"fallback"`         // 备用模型列表
	Agent           AgentConfig                `json:"agent"`            // Agent 行为配置
	PrimaryLanguage string                     `json:"primary_language"` // 主语言（启用专属优化）
	MCPServers      map[string]MCPServerConfig `json:"mcp_servers"`      // MCP 服务器配置
	Search          SearchConfig               `json:"search"`           // 搜索引擎配置
	Hallucination   HallucinationConfig        `json:"hallucination"`    // 幻觉防护配置
	Context         ContextConfig              `json:"context"`          // 上下文管理配置
	Tools           ToolsConfig                `json:"tools"`            // 工具配置
	Safety          SafetyConfig               `json:"safety"`           // 安全配置
	TUI             TUIConfig                  `json:"tui"`              // TUI 界面配置
	Cost            CostConfig                 `json:"cost"`             // 费用控制配置
	Log             LogConfig                  `json:"log"`              // 日志配置
}

// DefaultConfig 返回默认配置
func DefaultConfig() Config {
	return Config{
		LLM: LLMConfig{
			Provider:    "openai",
			Model:       "glm-5",
			BaseURL:     "https://open.bigmodel.cn/api/paas/v4",
			MaxTokens:   4096,
			Temperature: 0.1,
		},
		Agent: AgentConfig{
			MaxSteps:          15,
			MaxRetries:        3,
			RequireConfirm:    true,
			AutoTest:          true,
			MaxCostPerSession: 30.0,
		},
		PrimaryLanguage: "go",
		Search: SearchConfig{
			DefaultEngine: "duckduckgo",
			Engines:       map[string]SearchEngineConfig{"duckduckgo": {}},
			CacheTTL:      3600,
			MaxResults:    5,
			Summarize:     true,
		},
		Hallucination: HallucinationConfig{
			Enabled:             true,
			VerifyFileWrite:     true,
			VerifyTestResult:    true,
			ASTCrossCheck:       true,
			MaxSelfCorrection:   3,
			ForceReadBeforeEdit: true,
		},
		Context: ContextConfig{
			MaxTokens:        128000,
			MaxFiles:         20,
			SummaryThreshold: 50,
			AutoCompact:      true,
		},
		Tools: ToolsConfig{
			Shell: ShellToolConfig{
				Blocked: []string{"rm -rf /", "sudo", "mkfs", "dd"},
				Timeout: "120s",
			},
			File: FileToolConfig{
				MaxSize:      "1MB",
				WatchChanges: true,
			},
		},
		Safety: SafetyConfig{
			MaxFileChanges:   10,
			BackupBeforeEdit: true,
		},
		TUI: TUIConfig{
			Theme:    "dark",
			Currency: "CNY",
		},
		Cost: CostConfig{
			MaxPerSession: 30.0,
			WarnAt:        20.0,
		},
		Log: LogConfig{
			Level: "info",
			File:  "~/.codebuddy/codebuddy.log",
		},
	}
}
