package config

import (
	"fmt"
	"strings"
)

// Validate 校验配置是否合法
// 在 Load 之后调用，确保必填字段有值、格式正确
// 返回 nil 表示合法，返回 error 表示哪个字段有问题
func Validate(cfg *Config) error {
	// 1. LLM 必填字段检查
	// provider 和 model 是必须填的，不然不知道用哪个 LLM
	if cfg.LLM.Provider == "" {
		return fmt.Errorf("llm.provider 不能为空")
	}
	if cfg.LLM.Model == "" {
		return fmt.Errorf("llm.model 不能为空")
	}

	// 2. API Key 校验
	// 不能为空，也不能还是未替换的 ${...} 占位符（说明环境变量没设置）
	if cfg.LLM.APIKey == "" {
		return fmt.Errorf("llm.api_key 不能为空")
	}
	// strings.HasPrefix 检查字符串是否以指定前缀开头
	// 如果替换后还以 "${" 开头，说明环境变量不存在，os.Getenv 返回了空字符串
	// 但这里检查的是原始值没被替换的情况（expandEnvString 里 getenv 返回空串）
	if strings.HasPrefix(cfg.LLM.APIKey, "${") {
		return fmt.Errorf("llm.api_key 环境变量 %s 未设置", cfg.LLM.APIKey)
	}

	// 3. BaseURL 格式校验（选填，但填了必须合法）
	// 必须以 http:// 或 https:// 开头
	if cfg.LLM.BaseURL != "" {
		if !strings.HasPrefix(cfg.LLM.BaseURL, "http://") && !strings.HasPrefix(cfg.LLM.BaseURL, "https://") {
			return fmt.Errorf("llm.base_url 必须以 http:// 或 https:// 开头")
		}
	}

	return nil
}
