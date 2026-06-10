package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lix-lang/codebuddy/internal/security"
)

// WriteFileTool write_file 工具，创建或覆盖文件
type WriteFileTool struct {
	// rootDir 项目根目录，用于路径校验和拼接绝对路径
	rootDir string
	// backupMgr 备份管理器，写文件前自动备份
	backupMgr *security.BackupManager
}

// NewWriteFileTool 创建 write_file 工具
func NewWriteFileTool(rootDir string, backupMgr *security.BackupManager) *WriteFileTool {
	return &WriteFileTool{
		rootDir:   rootDir,
		backupMgr: backupMgr,
	}
}

// Name 返回工具名（实现 Tool 接口）
func (t *WriteFileTool) Name() string {
	return "write_file"
}

// Description 返回工具描述（实现 Tool 接口）
func (t *WriteFileTool) Description() string {
	return "创建或覆盖文件。需要用户确认后才会执行。用于创建新文件或完全重写文件内容。"
}

// Parameters 返回参数定义（实现 Tool 接口）
// 两个必填参数：path（文件路径）和 content（文件内容）
func (t *WriteFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "要写入的文件路径，相对于项目根目录",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "要写入的文件内容（完整内容，会覆盖整个文件）",
			},
		},
		"required": []string{"path", "content"},
	}
}

// Validate 校验参数（实现 Tool 接口）
func (t *WriteFileTool) Validate(args map[string]any) error {
	// path 必填
	pathVal, ok := args["path"]
	if !ok {
		return fmt.Errorf("缺少 path 参数")
	}
	path, ok := pathVal.(string)
	if !ok {
		return fmt.Errorf("path 参数必须是字符串")
	}

	// content 必填
	contentVal, ok := args["content"]
	if !ok {
		return fmt.Errorf("缺少 content 参数")
	}
	if _, ok := contentVal.(string); !ok {
		return fmt.Errorf("content 参数必须是字符串")
	}

	// 敏感文件检查
	if security.IsSensitiveFile(path) {
		return fmt.Errorf("不允许写入敏感文件: %s", path)
	}

	// 路径越界检查
	return security.IsSubPath(t.rootDir, path)
}

// Execute 执行写入文件（实现 Tool 接口）
func (t *WriteFileTool) Execute(ctx context.Context, args map[string]any) (*ToolResult, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	fullPath := filepath.Join(t.rootDir, path)

	// 如果文件已存在，先备份
	if _, err := os.Stat(fullPath); err == nil {
		if t.backupMgr != nil {
			// Backup 在文件修改前创建备份副本
			backupPath, err := t.backupMgr.Backup(fullPath)
			if err != nil {
				return &ToolResult{Output: fmt.Sprintf("备份失败: %v", err), IsError: true}, nil
			}
			_ = backupPath // 备份成功，继续写入
		}
	}

	// 确保目录存在（比如写 internal/new/file.go，new 目录可能不存在）
	dir := filepath.Dir(fullPath)
	// os.MkdirAll 递归创建所有不存在的目录层级
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return &ToolResult{Output: fmt.Sprintf("创建目录失败: %v", err), IsError: true}, nil
	}

	// os.WriteFile 写入文件（如果文件存在会覆盖，不存在会创建）
	// 0644 是文件权限：用户可读写，其他只读
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return &ToolResult{Output: fmt.Sprintf("写入文件失败: %v", err), IsError: true}, nil
	}

	return &ToolResult{Output: fmt.Sprintf("文件写入成功: %s (%d bytes)", path, len(content)), IsError: false}, nil
}

// IsDestructive 标记为破坏性操作（会修改文件）
func (t *WriteFileTool) IsDestructive() bool {
	return true
}

// IsAvailable 基础工具始终可用（实现 Tool 接口）
func (t *WriteFileTool) IsAvailable() bool {
	return true
}
