package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultConfigDir 默认配置目录名
const DefaultConfigDir = ".codebuddy"

// DefaultConfigFile 默认配置文件名
const DefaultConfigFile = "config.json"

// Load 加载配置，合并全局和项目两级配置文件
//
// 加载顺序：
//  1. 全局配置：~/.codebuddy/config.json（所有项目共享）
//  2. 项目配置：<projectDir>/.codebuddy/config.json（项目专属，覆盖全局）
//  3. 环境变量插值：把 "${OPENAI_API_KEY}" 这样的占位符替换成真实环境变量
//
// 如果全局配置文件不存在，不会报错（返回零值 Config）
// 如果项目配置文件不存在，也不会报错（只用全局配置）
//
// 参数 projectDir 是当前项目的根目录路径，用来定位项目级配置文件
func Load(projectDir string) (*Config, error) {
	// 1. 获取用户主目录，拼接全局配置路径 ~/.codebuddy/config.json
	// os.UserHomeDir() 返回当前用户的主目录路径，如 "/Users/lixiaoyang"
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("获取用户目录失败: %w", err)
	}
	// filepath.Join 把多个路径片段拼成一个完整路径，自动处理路径分隔符
	// 结果如："/Users/lixiaoyang/.codebuddy/config.json"
	globalPath := filepath.Join(homeDir, DefaultConfigDir, DefaultConfigFile)

	// 2. 尝试加载全局配置
	// os.Stat(path) 获取文件信息，err == nil 表示文件存在，err != nil 表示不存在
	cfg := &Config{}
	if _, err := os.Stat(globalPath); err == nil {
		cfg, err = loadFromFile(globalPath)
		if err != nil {
			// %w 是 "wrap" 的意思，把原始错误包进新的错误信息，上层可以用 errors.Unwrap 取出
			return nil, fmt.Errorf("加载全局配置失败: %w", err)
		}
	}

	// 3. 尝试加载项目级配置，存在的话覆盖全局配置的同名字段
	projectPath := filepath.Join(projectDir, DefaultConfigDir, DefaultConfigFile)
	if _, err := os.Stat(projectPath); err == nil {
		projectCfg, err := loadFromFile(projectPath)
		if err != nil {
			return nil, fmt.Errorf("加载项目配置失败: %w", err)
		}
		mergeConfig(cfg, projectCfg)
	}

	// 4. 环境变量插值：把 "${OPENAI_API_KEY}" 替换成 os.Getenv("OPENAI_API_KEY") 的值
	expandEnv(cfg)

	return cfg, nil
}

// loadFromFile 读取并解析单个 JSON 配置文件
// 内部函数，只被 Load 调用
func loadFromFile(path string) (*Config, error) {
	// os.ReadFile(path) 读取整个文件，返回 []byte（文件的原始字节内容）
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	// json.Unmarshal 把 JSON 字节流自动映射到 Go 结构体
	// 映射规则靠结构体的 json tag，比如 `json:"provider"` 对应 JSON 里的 "provider" 字段
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败: %w", err)
	}

	return &cfg, nil
}

// mergeConfig 用项目配置覆盖全局配置
// 只覆盖非零值的字段（零值 = 该字段在项目配置里没写，保留全局的值）
// 比如：全局设了 model=gpt-4o，项目没设 model → 保留 gpt-4o
//
//	全局设了 model=gpt-4o，项目设了 model=glm-4 → 用 glm-4
func mergeConfig(base, override *Config) {
	// LLM 配置
	if override.LLM.Provider != "" {
		base.LLM.Provider = override.LLM.Provider
	}
	if override.LLM.Model != "" {
		base.LLM.Model = override.LLM.Model
	}
	if override.LLM.APIKey != "" {
		base.LLM.APIKey = override.LLM.APIKey
	}
	if override.LLM.BaseURL != "" {
		base.LLM.BaseURL = override.LLM.BaseURL
	}
	if override.LLM.MaxTokens != 0 {
		base.LLM.MaxTokens = override.LLM.MaxTokens
	}
	if override.LLM.Temperature != 0 {
		base.LLM.Temperature = override.LLM.Temperature
	}
	// Agent 配置
	if override.Agent.MaxSteps != 0 {
		base.Agent.MaxSteps = override.Agent.MaxSteps
	}
	if override.Agent.MaxRetries != 0 {
		base.Agent.MaxRetries = override.Agent.MaxRetries
	}
	// Safety 配置
	if override.Safety.MaxFileChanges != 0 {
		base.Safety.MaxFileChanges = override.Safety.MaxFileChanges
	}
	// Log 配置
	if override.Log.Level != "" {
		base.Log.Level = override.Log.Level
	}
	if override.Log.File != "" {
		base.Log.File = override.Log.File
	}
}

// expandEnv 替换配置中可能包含环境变量占位符的字段
// 只替换完全匹配 "${VAR}" 格式的字符串，不影响普通字符串
// 比如 APIKey 写的是 "${OPENAI_API_KEY}"，会被替换成真实的环境变量值
func expandEnv(cfg *Config) {
	cfg.LLM.APIKey = expandEnvString(cfg.LLM.APIKey)
	cfg.LLM.BaseURL = expandEnvString(cfg.LLM.BaseURL)
	cfg.Log.File = expandEnvString(cfg.Log.File)
}

// expandEnvString 替换单个字符串中的环境变量
// 只处理 "${VAR}" 格式，其他字符串原样返回
func expandEnvString(s string) string {
	// strings.HasPrefix 检查字符串是否以指定前缀开头
	// strings.HasSuffix 检查字符串是否以指定后缀结尾
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		// s[2:len(s)-1] 是字符串切片，去掉前两个字符 "${" 和最后一个字符 "}"
		// 比如 "${OPENAI_API_KEY}" → "OPENAI_API_KEY"
		envName := s[2 : len(s)-1]
		// os.Getenv 读取环境变量的值，如果变量不存在返回空字符串
		return os.Getenv(envName)
	}
	return s
}
