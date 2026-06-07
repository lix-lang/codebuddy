// Package skills 实现 Skill 技能系统（用户自定义任务模板）。
//
// Skill 把重复性任务封装为固定步骤的模板，减少 LLM 的推理开销。
// 用户在 .codebuddy/skills/ 目录下用 JSON 定义自己的 Skill。
//
// 每个步骤可以指定：
//   - Prompt 模板（支持变量插值）
//   - 可用工具列表（限制 LLM 的工具权限）
//   - 分析器（可选，指定语言分析器）
//   - 是否需要用户确认
//
// Skill 的执行走标准 Agent 循环，灵活性和手动对话一样高。
package skills
