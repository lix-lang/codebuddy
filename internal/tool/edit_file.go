package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lix-lang/codebuddy/internal/security"
)

// EditFileTool edit_file 工具，精确替换文件中的内容
// 跟 write_file 的区别：write_file 覆盖整个文件，edit_file 只替换指定部分
type EditFileTool struct {
	rootDir   string
	backupMgr *security.BackupManager
}

func NewEditFileTool(rootDir string, backupMgr *security.BackupManager) *EditFileTool {
	return &EditFileTool{
		rootDir:   rootDir,
		backupMgr: backupMgr,
	}
}

func (t *EditFileTool) Name() string {
	return "edit_file"
}

func (t *EditFileTool) Description() string {
	return "精确替换文件中的指定内容。需要用户确认后才会执行。用于修改代码的一部分而不是重写整个文件。"
}

// Parameters 三个参数：path + old_string + new_string
// old_string 是要被替换的原文本，new_string 是替换后的新文本
func (t *EditFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "要编辑的文件路径",
			},
			"old_string": map[string]any{
				"type":        "string",
				"description": "要被替换的原文本（必须与文件中的内容完全一致）",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "替换后的新文本",
			},
		},
		"required": []string{"path", "old_string", "new_string"},
	}
}

func (t *EditFileTool) Validate(args map[string]any) error {
	// 三个必填参数都要检查
	for _, key := range []string{"path", "old_string", "new_string"} {
		val, ok := args[key]
		if !ok {
			return fmt.Errorf("缺少 %s 参数", key)
		}
		if _, ok := val.(string); !ok {
			return fmt.Errorf("%s 参数必须是字符串", key)
		}
	}

	path, _ := args["path"].(string)

	if security.IsSensitiveFile(path) {
		return fmt.Errorf("不允许编辑敏感文件: %s", path)
	}

	return security.IsSubPath(t.rootDir, path)
}

func (t *EditFileTool) Execute(ctx context.Context, args map[string]any) (*ToolResult, error) {
	path, _ := args["path"].(string)
	oldString, _ := args["old_string"].(string)
	newString, _ := args["new_string"].(string)
	fullPath := filepath.Join(t.rootDir, path)

	// 读取原文件内容
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return &ToolResult{Output: fmt.Sprintf("读取文件失败: %v", err), IsError: true}, nil
	}

	content := string(data)

	// 检查 old_string 是否存在于文件中
	// strings.Count 统计 old_string 在文件中出现了几次
	count := strings.Count(content, oldString)
	if count == 0 {
		return &ToolResult{
			Output:  fmt.Sprintf("在 %s 中找不到要替换的内容，请检查 old_string 是否与文件内容完全一致", path),
			IsError: true,
		}, nil
	}

	// 如果 old_string 出现多次，报错（避免误替换不该改的地方）
	if count > 1 {
		return &ToolResult{
			Output:  fmt.Sprintf("在 %s 中找到 %d 处匹配，old_string 必须唯一才能安全替换。请提供更多上下文让匹配唯一", path, count),
			IsError: true,
		}, nil
	}

	// 备份原文件
	if t.backupMgr != nil {
		if _, err := t.backupMgr.Backup(fullPath); err != nil {
			return &ToolResult{Output: fmt.Sprintf("备份失败: %v", err), IsError: true}, nil
		}
	}

	// strings.Replace 替换：把 old_string 替换成 new_string
	// 最后一个参数 1 表示只替换第一个匹配（虽然上面已经确认只有一处）
	newContent := strings.Replace(content, oldString, newString, 1)

	// 写回文件
	if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
		return &ToolResult{Output: fmt.Sprintf("写入文件失败: %v", err), IsError: true}, nil
	}

	return &ToolResult{
		Output:  fmt.Sprintf("文件编辑成功: %s（替换了 %d 个字符 → %d 个字符）", path, len(oldString), len(newString)),
		IsError: false,
	}, nil
}

// IsDestructive 标记为破坏性操作（会修改文件）
func (t *EditFileTool) IsDestructive() bool {
	return true
}
