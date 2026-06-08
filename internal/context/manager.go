// Package context 实现 Token 计数和上下文管理。
//
// 本文件（manager.go）实现 ContextManager 上下文管理器。
//
// # ContextManager 是什么？
//
// LLM 的上下文窗口有限（比如 128K token），不可能把所有文件内容都塞进去。
// ContextManager 负责决定"发给 LLM 看什么"，核心任务：
//   - 选择跟当前问题最相关的文件（用 SelectFiles 三信号算法）
//   - 组装完整消息列表（system prompt + 项目概览 + 文件 + 对话历史 + 用户新消息）
//   - 控制 token 总量不超过预算（超了就压缩早期对话）
//
// # 三层 Prompt 架构
//
// 发给 LLM 的消息由三层组成，每层的更新频率不同：
//
//	┌─────────────────────────────────┐
//	│  稳定层（Stable）                │  ← SOUL.md 系统提示词，很少变
//	│  "你是一个编程助手..."            │
//	├─────────────────────────────────┤
//	│  上下文层（Context）              │  ← 项目概览 + 相关文件，按需更新
//	│  "项目: codebuddy (Go 1.22)"    │
//	│  "// internal/llm/types.go..."  │
//	├─────────────────────────────────┤
//	│  易变层（Volatile）              │  ← 对话历史 + 当前用户消息，每轮都变
//	│  用户: "帮我看看 config.go"       │
//	│  助手: "config.go 里定义了..."   │
//	└─────────────────────────────────┘
//
// # BuildMessages 流程
//
// Agent 每轮调用 BuildMessages(userMsg) 时：
//  1. 加载 SOUL.md → 组装 system prompt（稳定层）
//  2. 拼接项目概览（上下文层）
//  3. 用 SelectFiles 选择相关文件，拼接文件内容（上下文层）
//  4. 添加对话历史，超预算则压缩早期对话（易变层）
//  5. 添加用户新消息（易变层）
//  6. 检查总 token 是否超预算，超了就压缩
package context

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lix-lang/codebuddy/internal/llm"
)

// DefaultContextManager ContextManager 的默认实现
// 负责管理发给 LLM 的上下文内容
type DefaultContextManager struct {
	// mu 保护并发访问（Agent 主循环和 TUI 可能同时读写）
	mu sync.RWMutex

	// ---- 配置 ----
	model      string // 模型名，如 "glm-4"，用于选择分词器和 token 预算
	rootDir    string // 项目根目录，用于定位 SOUL.md 和读取文件
	maxContext int    // 模型最大上下文窗口（token），如 128000

	// ---- 三层 Prompt 缓存 ----
	systemPrompt string // 稳定层：SOUL.md 加载后的系统提示词（启动时加载，很少变）
	project      *ProjectContext // 上下文层：项目扫描结果（缓存）

	// ---- 易变层状态 ----
	history      []llm.Message // 对话历史（用户消息 + 助手回复 + 工具调用结果）
	workingFiles []string      // 工作集：最近几轮涉及的文件路径，保持高分不被挤掉
	addedFiles   map[string]bool // 用户手动添加的文件（AddFile），优先级最高

	// ---- Token 预算 ----
	budget TokenBudget // token 预算分配（系统、项目、工具、输出、文件、历史各多少）
}

// ContextManagerConfig 创建 ContextManager 的配置
type ContextManagerConfig struct {
	Model      string // 模型名
	RootDir    string // 项目根目录
	MaxContext int    // 最大上下文窗口
}

// NewContextManager 创建上下文管理器
// 启动时：扫描项目 + 加载 SOUL.md + 计算 token 预算
func NewContextManager(cfg ContextManagerConfig) (*DefaultContextManager, error) {
	cm := &DefaultContextManager{
		model:      cfg.Model,
		rootDir:    cfg.RootDir,
		maxContext: cfg.MaxContext,
		addedFiles: make(map[string]bool),
	}

	// 1. 计算 token 预算分配
	cm.budget = CalculateBudget(cfg.MaxContext)

	// 2. 加载 SOUL.md（稳定层）
	cm.systemPrompt = loadSOULMD(cfg.RootDir)

	// 3. 扫描项目（上下文层）
	project, err := ScanProject(cfg.RootDir)
	if err != nil {
		return nil, fmt.Errorf("扫描项目失败: %w", err)
	}
	cm.project = project

	return cm, nil
}

// ================================================================
// ContextManager 接口实现
// ================================================================

// BuildMessages 构建发给 LLM 的完整消息列表
// 这是 ContextManager 最重要的方法，Agent 每轮循环都会调用
//
// 组装顺序：
//  1. system prompt（SOUL.md + 规则）
//  2. 项目概览
//  3. 相关文件内容
//  4. 对话历史（滑动窗口）
//  5. 用户新消息
//
// 如果总 token 超预算，会自动压缩早期对话历史
func (cm *DefaultContextManager) BuildMessages(userMsg string) ([]llm.Message, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	var messages []llm.Message

	// ==== 稳定层：System Prompt ====
	// SOUL.md 的内容 + 基本规则
	systemContent := cm.buildSystemPrompt()
	messages = append(messages, llm.Message{
		Role:    llm.RoleSystem,
		Content: systemContent,
	})

	// ==== 上下文层：项目概览 ====
	// 包含项目名、依赖、文件数量、分层结构等信息
	// 约 1K token，帮助 LLM 理解项目整体架构
	projectContent := cm.buildProjectContext()
	if projectContent != "" {
		messages = append(messages, llm.Message{
			Role:    llm.RoleSystem,
			Content: projectContent,
		})
	}

	// ==== 上下文层：相关文件内容 ====
	// 用 SelectFiles 选择跟当前查询最相关的文件
	// 拼接文件内容，每个文件用 ``` 包裹
	fileMessages := cm.buildFileContext(userMsg)
	messages = append(messages, fileMessages...)

	// ==== 易变层：对话历史 ====
	// 从最早的对话开始添加，直到接近预算
	// 如果历史太长，压缩早期对话
	historyMessages := cm.buildHistory()
	messages = append(messages, historyMessages...)

	// ==== 易变层：用户新消息 ====
	messages = append(messages, llm.Message{
		Role:    llm.RoleUser,
		Content: userMsg,
	})

	// ==== Token 检查：超预算就压缩 ====
	used := CountMessagesToken(cm.model, messages)
	if used > cm.budget.Files+cm.budget.History {
		// 超预算了，压缩早期对话（只保留最近几轮）
		messages = cm.compactMessages(messages, userMsg)
	}

	// 更新工作集：用户消息里提到的文件加入工作集
	cm.updateWorkingFiles(userMsg)

	return messages, nil
}

// AddFile 把一个文件加入上下文（手动添加，优先级最高）
// 添加后会在每轮 BuildMessages 中包含这个文件
func (cm *DefaultContextManager) AddFile(path string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 转成相对路径
	relPath, err := filepath.Rel(cm.rootDir, path)
	if err != nil {
		relPath = path
	}

	// 检查文件是否存在
	fullPath := filepath.Join(cm.rootDir, relPath)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return fmt.Errorf("文件不存在: %s", relPath)
	}

	cm.addedFiles[relPath] = true
	return nil
}

// RemoveFile 从上下文中移除一个手动添加的文件
func (cm *DefaultContextManager) RemoveFile(path string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	relPath, err := filepath.Rel(cm.rootDir, path)
	if err != nil {
		relPath = path
	}
	delete(cm.addedFiles, relPath)
}

// AddHistory 把一条消息加入对话历史
// Agent 每完成一轮（LLM 回复 / 工具调用结果）都调用这个方法
func (cm *DefaultContextManager) AddHistory(msg llm.Message) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.history = append(cm.history, msg)
}

// TokenUsage 返回当前上下文的 token 使用情况
// 返回值：(已用 token 数, 弹性预算上限)
func (cm *DefaultContextManager) TokenUsage() (used int, budget int) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// 粗略估算当前历史的 token 数
	used = EstimateMessagesToken(cm.history)
	budget = cm.budget.Files + cm.budget.History
	return
}

// Compact 压缩上下文：把早期对话合并成摘要
// 当对话历史太长、接近 token 预算时调用
//
// 压缩策略：保留最近 5 轮对话，之前的合并成一条摘要消息
// 比如 20 轮对话 → 1 条摘要 + 最近 5 轮
func (cm *DefaultContextManager) Compact() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 至少保留最近 10 条消息（约 5 轮对话）
	if len(cm.history) <= 10 {
		return nil // 不需要压缩
	}

	// 取早期消息，生成摘要
	oldHistory := cm.history[:len(cm.history)-10]
	summary := cm.summarizeHistory(oldHistory)

	// 替换早期消息为摘要
	cm.history = append([]llm.Message{{
		Role:    llm.RoleSystem,
		Content: fmt.Sprintf("[对话摘要] %s", summary),
	}}, cm.history[len(cm.history)-10:]...)

	return nil
}

// Reset 清空上下文，新对话开始时调用
func (cm *DefaultContextManager) Reset() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.history = nil
	cm.workingFiles = nil
	// addedFiles 不清空，用户手动添加的文件跨对话保留
}

// ================================================================
// 内部方法：构建三层 Prompt
// ================================================================

// buildSystemPrompt 构建系统提示词（稳定层）
// 包含 SOUL.md 内容 + 工具使用规则
func (cm *DefaultContextManager) buildSystemPrompt() string {
	var sb strings.Builder

	// SOUL.md 的内容（如果有的话）
	if cm.systemPrompt != "" {
		sb.WriteString(cm.systemPrompt)
		sb.WriteString("\n\n")
	}

	// 基本规则（所有模型通用）
	sb.WriteString("## 规则\n")
	sb.WriteString("1. 你是一个编程助手，帮助用户编写和修改代码\n")
	sb.WriteString("2. 修改文件前先读取文件内容，了解当前状态\n")
	sb.WriteString("3. 优先使用 edit_file 而非 write_file，减少出错风险\n")
	sb.WriteString("4. 每次只修改必要的部分，不要重写整个文件\n")
	sb.WriteString("5. 执行命令前确认安全性，不要执行危险操作\n")

	return sb.String()
}

// buildProjectContext 构建项目概览（上下文层）
// 包含项目名、依赖、文件数量、导出符号等信息
func (cm *DefaultContextManager) buildProjectContext() string {
	if cm.project == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## 项目信息\n")
	sb.WriteString(cm.project.Summary())
	sb.WriteString("\n## 项目结构（导出符号）\n")
	sb.WriteString(cm.project.RepoMap())

	return sb.String()
}

// buildFileContext 构建文件内容上下文（上下文层）
// 用 SelectFiles 选择最相关的文件，然后读取并拼接内容
func (cm *DefaultContextManager) buildFileContext(userMsg string) []llm.Message {
	if cm.project == nil || len(cm.project.Files) == 0 {
		return nil
	}

	// 合并工作集和手动添加的文件
	workingSet := cm.getWorkingAndAddedFiles()

	// 用 SelectFiles 选择最相关的文件
	// 传入文件预算（弹性部分的 60%）
	selected := SelectFiles(userMsg, cm.project.Files, workingSet, cm.budget.Files)

	if len(selected) == 0 {
		return nil
	}

	// 读取文件内容并拼接
	var sb strings.Builder
	sb.WriteString("## 相关文件内容\n\n")

	for _, relPath := range selected {
		fullPath := filepath.Join(cm.rootDir, relPath)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		// 用 markdown 代码块包裹，标注文件路径
		ext := strings.TrimPrefix(filepath.Ext(relPath), ".")
		sb.WriteString(fmt.Sprintf("### %s\n```%s\n%s\n```\n\n", relPath, ext, string(data)))
	}

	return []llm.Message{{
		Role:    llm.RoleSystem,
		Content: sb.String(),
	}}
}

// buildHistory 构建对话历史（易变层）
// 从历史列表中按顺序添加消息
func (cm *DefaultContextManager) buildHistory() []llm.Message {
	if len(cm.history) == 0 {
		return nil
	}

	// 从最早的开始添加
	// 后面的 Token 检查会裁剪超预算的部分
	result := make([]llm.Message, len(cm.history))
	copy(result, cm.history)
	return result
}

// ================================================================
// 内部方法：压缩和历史管理
// ================================================================

// compactMessages 当消息总 token 超预算时压缩
// 策略：保留 system prompt + 项目概览 + 最近 N 条历史 + 用户消息
func (cm *DefaultContextManager) compactMessages(messages []llm.Message, userMsg string) []llm.Message {
	// 至少保留：system prompt(1) + 项目概览(1) + 文件内容(0-1) + 最近 6 条历史 + 用户消息(1)
	keepMin := 4 // 最少保留的消息条数（system + project + 2 条历史）
	if len(messages) <= keepMin+1 {
		return messages
	}

	// 找到 history 部分的起始位置
	// 消息顺序：system, project(system), files(system), history..., user
	// 从后往前数，保留最后 6 条 + 用户消息
	historyStart := -1
	for i, msg := range messages {
		if msg.Role == llm.RoleUser || msg.Role == llm.RoleAssistant || msg.Role == llm.RoleTool {
			if historyStart == -1 {
				historyStart = i
			}
		}
	}

	if historyStart == -1 {
		return messages
	}

	// 在 history 部分前面插入一条压缩摘要
	// 保留最近的 6 条，之前的生成摘要
	recentCount := 6
	historyEnd := len(messages) - 1 // 最后一条是用户消息

	if historyEnd-historyStart <= recentCount {
		return messages
	}

	// 生成早期对话的摘要
	oldHistory := messages[historyStart : historyEnd-recentCount]
	summary := cm.summarizeHistory(oldHistory)

	// 重新组装：前面固定部分 + 摘要 + 最近 N 条 + 用户消息
	var result []llm.Message

	// 固定部分（system prompt + project + files）
	result = append(result, messages[:historyStart]...)

	// 摘要
	result = append(result, llm.Message{
		Role:    llm.RoleSystem,
		Content: fmt.Sprintf("[对话摘要] %s", summary),
	})

	// 最近 N 条 + 用户消息
	result = append(result, messages[historyEnd-recentCount:]...)

	// 同步更新内部历史
	cm.history = messages[historyStart : historyEnd-recentCount]
	cm.history = append([]llm.Message{{
		Role:    llm.RoleSystem,
		Content: fmt.Sprintf("[对话摘要] %s", summary),
	}}, cm.history...)

	return result
}

// summarizeHistory 把一段对话历史压缩成简短摘要
// 目前用简单的拼接方式，后面可以改成让 LLM 来生成摘要
func (cm *DefaultContextManager) summarizeHistory(messages []llm.Message) string {
	var sb strings.Builder

	for _, msg := range messages {
		switch msg.Role {
		case llm.RoleUser:
			// 用户消息：只取前 100 字符
			content := msg.Content
			if len(content) > 100 {
				content = content[:100] + "..."
			}
			sb.WriteString(fmt.Sprintf("用户问了: %s; ", content))
		case llm.RoleAssistant:
			// 助手回复：只取前 50 字符
			content := msg.Content
			if len(content) > 50 {
				content = content[:50] + "..."
			}
			if content != "" {
				sb.WriteString(fmt.Sprintf("助手回答了: %s; ", content))
			}
			// 工具调用
			for _, tc := range msg.ToolCalls {
				sb.WriteString(fmt.Sprintf("助手调用了 %s; ", tc.Function.Name))
			}
		case llm.RoleTool:
			// 工具结果：只记录工具名
			if msg.Name != "" {
				sb.WriteString(fmt.Sprintf("%s 执行完成; ", msg.Name))
			}
		}
	}

	return sb.String()
}

// updateWorkingFiles 从用户消息中提取提到的文件，加入工作集
// 工作集里的文件在后续 SelectFiles 中会得到额外加分（工作集惯性信号）
func (cm *DefaultContextManager) updateWorkingFiles(userMsg string) {
	// 遍历所有项目文件，检查用户消息是否提到了该文件
	for _, f := range cm.project.Files {
		if isFileMentioned(userMsg, f.Path) {
			// 去重加入工作集
			found := false
			for _, wf := range cm.workingFiles {
				if wf == f.Path {
					found = true
					break
				}
			}
			if !found {
				cm.workingFiles = append(cm.workingFiles, f.Path)
			}
		}
	}

	// 工作集最多保留 20 个文件，超过就只保留最近 20 个
	if len(cm.workingFiles) > 20 {
		cm.workingFiles = cm.workingFiles[len(cm.workingFiles)-20:]
	}
}

// getWorkingAndAddedFiles 合并工作集和手动添加的文件
func (cm *DefaultContextManager) getWorkingAndAddedFiles() []string {
	seen := make(map[string]bool)
	var result []string

	// 先加手动添加的文件（优先级最高）
	for f := range cm.addedFiles {
		if !seen[f] {
			seen[f] = true
			result = append(result, f)
		}
	}

	// 再加工作集
	for _, f := range cm.workingFiles {
		if !seen[f] {
			seen[f] = true
			result = append(result, f)
		}
	}

	return result
}

// ================================================================
// SOUL.md 加载
// ================================================================

// loadSOULMD 加载 SOUL.md 系统提示词
// SOUL.md 是一个 markdown 文件，放在项目根目录或 .codebuddy/ 目录下
// 用来定义 Agent 的"性格"和行为规则
//
// 查找顺序：
//  1. .codebuddy/SOUL.md（项目级，覆盖全局）
//  2. SOUL.md（项目根目录）
//  3. 如果都不存在，返回空字符串（用默认规则）
func loadSOULMD(rootDir string) string {
	// 尝试 .codebuddy/SOUL.md
	path1 := filepath.Join(rootDir, ".codebuddy", "SOUL.md")
	if data, err := os.ReadFile(path1); err == nil {
		return string(data)
	}

	// 尝试项目根目录的 SOUL.md
	path2 := filepath.Join(rootDir, "SOUL.md")
	if data, err := os.ReadFile(path2); err == nil {
		return string(data)
	}

	// 都没有，返回空（buildSystemPrompt 会用默认规则）
	return ""
}
