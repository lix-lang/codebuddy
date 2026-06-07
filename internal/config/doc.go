// Package config 管理配置文件的读写和校验。
//
// 配置文件路径：~/.codebuddy/config.json（全局），.codebuddy/config.json（项目级覆盖）
//
// 包含：
//   - LLM 配置（provider、api_key、model、base_url）
//   - Agent 配置（max_steps、require_confirm、max_cost_per_session）
//   - 主语言配置（primary_language，启用该语言专属优化）
//   - MCP 服务器配置（mcp_servers）
//   - 搜索引擎配置（search）
//   - 工具配置（命令黑名单、超时时间）
//   - 安全配置（备份、最大文件修改数）
//   - 日志配置（级别、trace 开关）
//
// 支持 ${ENV_VAR} 环境变量插值，API Key 不用明文写在配置文件里。
// 支持命令行修改配置：codebuddy config set/get/list。
package config
