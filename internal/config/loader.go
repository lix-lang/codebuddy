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

// projectMarkers 项目根目录标记文件/目录
// 从当前目录往上逐级查找，遇到这些标记就认为是项目根
var projectMarkers = []string{
	".git",
	"go.mod",
	"package.json",
	"Cargo.toml",
	"pyproject.toml",
	"pom.xml",
	"Makefile",
}

// findProjectRoot 从 dir 开始往上找项目根目录
// 通过检测 projectMarkers 中的标记文件来判断
// 找不到返回空字符串
func findProjectRoot(dir string) string {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}

	homeDir, _ := os.UserHomeDir()

	for {
		// 到家目录就停，别往上跑到 / 去
		if absDir == homeDir || absDir == "/" {
			return ""
		}

		for _, marker := range projectMarkers {
			if _, err := os.Stat(filepath.Join(absDir, marker)); err == nil {
				return absDir
			}
		}

		parent := filepath.Dir(absDir)
		if parent == absDir {
			return ""
		}
		absDir = parent
	}
}

// Load 加载配置，合并全局和项目两级配置文件
//
// 加载顺序：
//  1. 全局配置：~/.codebuddy/config.json（所有项目共享）
//  2. 项目配置：自动检测项目根目录，加载 <projectRoot>/.codebuddy/config.json
//  3. 环境变量插值：把 "${OPENAI_API_KEY}" 这样的占位符替换成真实环境变量
//
// 全局配置始终从家目录加载，项目配置需要检测到项目标记（.git 等）才会查找
func Load() (*Config, error) {
	// 1. 获取用户主目录，拼接全局配置路径 ~/.codebuddy/config.json
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("获取用户目录失败: %w", err)
	}
	globalPath := filepath.Join(homeDir, DefaultConfigDir, DefaultConfigFile)

	// 2. 尝试加载全局配置
	cfg := &Config{}
	if _, err := os.Stat(globalPath); err == nil {
		cfg, err = loadFromFile(globalPath)
		if err != nil {
			return nil, fmt.Errorf("加载全局配置失败: %w", err)
		}
	}

	// 3. 检测项目根，找到才加载项目级配置
	if projectRoot := findProjectRoot("."); projectRoot != "" {
		projectPath := filepath.Join(projectRoot, DefaultConfigDir, DefaultConfigFile)
		if _, err := os.Stat(projectPath); err == nil {
			projectCfg, err := loadFromFile(projectPath)
			if err != nil {
				return nil, fmt.Errorf("加载项目配置失败: %w", err)
			}
			mergeConfig(cfg, projectCfg)
		}
	}

	// 4. 环境变量插值
	expandEnv(cfg)

	return cfg, nil
}

// loadFromFile 读取并解析单个 JSON 配置文件
// 内部函数，只被 Load 调用
// path: JSON 配置文件的完整路径
func loadFromFile(path string) (*Config, error) {
	// os.ReadFile(path) 读取整个文件，返回 []byte（文件的原始字节内容）
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	// json.Unmarshal 把 JSON 字节流自动映射到 Go 结构体
	// 映射规则靠结构体的 json tag，比如 `json:"provider"` 对应 JSON 里的 "provider" 字段
	// cfg: 解析结果会写入这个局部变量
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
	// base: 全局配置，作为被覆盖的底座
	// override: 项目级配置，非零值字段会覆盖 base 中的同名字段
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
// cfg: 指向需要做环境变量插值的配置对象，会直接修改其字段值
func expandEnv(cfg *Config) {
	cfg.LLM.APIKey = expandEnvString(cfg.LLM.APIKey)
	cfg.LLM.BaseURL = expandEnvString(cfg.LLM.BaseURL)
	cfg.Log.File = expandEnvString(cfg.Log.File)
}

// expandEnvString 替换单个字符串中的环境变量
// 只处理 "${VAR}" 格式，其他字符串原样返回
// s: 待检查的字符串，可能是 "${VAR_NAME}" 格式的环境变量占位符
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
