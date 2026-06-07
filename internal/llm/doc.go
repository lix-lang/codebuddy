// Package llm 封装 LLM（大语言模型）客户端，统一不同 API 的调用方式。
//
// 支持三种提供商：
//   - OpenAI 格式（GPT-4o、DeepSeek、通义千问、GLM 等，都走同一个 API 格式）
//   - Claude 格式（Anthropic Messages API）
//   - Ollama 格式（本地模型）
//
// 核心能力：
//   - 流式输出（SSE）：LLM 一字一字地返回，用户不用等全部生成完
//   - 工具调用（Function Calling）：LLM 可以说"我要调用 read_file 工具"
//   - 多模态：发送图片给 LLM 识别（配合 read_image 工具）
//   - Token 计数：估算每次请求消耗的 token 数
package llm
