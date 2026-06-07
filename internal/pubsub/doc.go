// Package pubsub 实现事件总线，解耦 Agent 和 TUI。
//
// Agent 通过事件总线推送事件（文本片段、工具调用、状态变化），
// TUI 订阅事件并渲染。这样两个模块可以独立开发和测试。
package pubsub
