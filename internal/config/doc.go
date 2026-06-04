// Package config 管理配置文件的读写和校验。
//
// 配置文件路径：~/.codebuddy/config.yaml
//
// 包含：
//   - LLM 配置（provider、api_key、model、base_url）
//   - Agent 配置（max_steps、require_confirm、max_cost_per_session）
//   - 工具配置（命令黑名单、超时时间）
//   - 安全配置（备份、最大文件修改数）
//   - 日志配置（级别、trace 开关）
//
// 支持 ${ENV_VAR} 环境变量插值，API Key 不用明文写在配置文件里。
package config
