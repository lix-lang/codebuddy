// Package tui 实现终端用户界面（Terminal User Interface）。
//
// 基于 bubbletea 框架的 Elm Architecture，采用子模型组合：
//   - ChatModel：聊天区（viewport + 100ms 节流流式输出）
//   - InputModel：输入框（textinput，Shift+Enter 多行模式）
//   - StatusBarModel：状态栏（模型名 + 费用 + 工具步数）
//
// 事件通过 PubSub EventBus 解耦：
//   - Agent 推送事件（流式内容、工具调用、状态变化）→ EventBus channel
//   - TUI waitForEvent 从 channel 读取 → eventMsg → Update 分发到各子模型
//
// 确认交互：
//   - 破坏性工具执行前 → ConfirmRequest → TUI 显示 Y/N → 回写 response channel
//
// 界面布局：
//
//	┌─────────────────────────────────────┐
//	│                                     │
//	│  聊天区（ChatModel）                 │  ← viewport 滚动 + 流式输出
//	│                                     │
//	├─────────────────────────────────────┤
//	│  ─── ● 模型名 · step n/max ───────  │  ← StatusBarModel
//	├─────────────────────────────────────┤
//	│  > 输入消息                          │  ← InputModel
//	└─────────────────────────────────────┘
package tui
