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

// AgentConfig Agent 行为配置
type AgentConfig struct {
	MaxSteps       int  `json:"max_steps"`       // 最大执行步数（默认 15）
	MaxRetries     int  `json:"max_retries"`     // 最大重试次数
	RequireConfirm bool `json:"require_confirm"` // 破坏性操作是否需要用户确认
	AutoTest       bool `json:"auto_test"`       // 代码生成后是否自动测试
}

// SafetyConfig 安全配置
type SafetyConfig struct {
	MaxFileChanges   int  `json:"max_file_changes"`   // 单次任务最大修改文件数
	BackupBeforeEdit bool `json:"backup_before_edit"` // 编辑前是否自动备份
}

// LogConfig 日志配置
type LogConfig struct {
	Level string `json:"level"` // "debug" / "info" / "warn" / "error"
	File  string `json:"file"`  // 日志文件路径
	Trace bool   `json:"trace"` // 是否记录完整 LLM 交互日志
}

// Config 顶层配置，对应 config.json 文件
type Config struct {
	LLM    LLMConfig    `json:"llm"`    // LLM 提供商配置
	Agent  AgentConfig  `json:"agent"`  // Agent 行为配置
	Safety SafetyConfig `json:"safety"` // 安全配置
	Log    LogConfig    `json:"log"`    // 日志配置
}
