// Package agent 实现 Agent 核心循环（ReAct 引擎）。
//
// ReAct = Reasoning（推理）+ Acting（行动），Agent 的工作方式是：
//  1. 把用户问题 + 上下文发给 LLM
//  2. LLM 返回文本回答 或 工具调用指令
//  3. 如果是工具调用 → 校验参数 → 执行工具 → 把结果喂回 LLM → 回到第1步
//  4. 如果是文本回答 → 展示给用户 → 本轮结束
//
// 循环上限 15 步，防止 LLM 陷入死循环。
package agent
