// Package pubsub 实现事件总线，解耦 Agent 和 TUI。
//
// Agent 通过事件总线推送事件（文本片段、工具调用、状态变化），
// TUI 订阅事件并渲染。这样两个模块可以独立开发和测试。
//
// 事件类型：
//   - EventContent：LLM 流式文本内容
//   - EventToolCall：工具调用开始
//   - EventToolResult：工具调用结果
//   - EventStateChange：Agent 状态变更
//   - EventError：错误
//   - EventUsageUpdate：Token 使用量更新
//
// 确认交互通过 ConfirmRequest 实现：
//
//	Agent 发送确认请求 → TUI 显示 Y/N → 用户按键回写 response channel
package pubsub
