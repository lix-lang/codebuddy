// Package hallucination 实现幻觉防护四层机制。
//
// LLM 幻觉是 Agent 面临的核心难题，本包通过四层防护最大限度降低影响：
//
//  1. Prompt 约束（预防）：System Prompt 里的七条防幻觉规则
//  2. 参数校验（拦截）：JSON Schema 校验 + 业务逻辑校验
//  3. 结果验证（检测）：写入文件后独立验证、Go 文件语法检查、AST 交叉验证
//  4. 自纠错循环（修复）：验证失败时把错误信息喂回 LLM，让它自己修正
package hallucination
