package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lix-lang/codebuddy/internal/security"
)

// ListDirTool list_dir 工具，列出目录结构
type ListDirTool struct {
	// rootDir 项目根目录，用于路径校验
	rootDir string
}

// NewListDirTool 创建 list_dir 工具
func NewListDirTool(rootDir string) *ListDirTool {
	return &ListDirTool{rootDir: rootDir}
}

// Name 返回工具名（实现 Tool 接口）
func (t *ListDirTool) Name() string {
	return "list_dir"
}

// Description 返回工具描述（实现 Tool 接口）
func (t *ListDirTool) Description() string {
	return "列出指定目录的文件和子目录。当用户想了解项目结构时使用。"
}

// Parameters 返回参数定义（实现 Tool 接口）
// path 是可选参数，不传就列出项目根目录
func (t *ListDirTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				// "type": "string" 参数值是字符串
				"type": "string",
				// "description" 告诉 LLM 这个参数的含义
				"description": "要列出的目录路径，默认为项目根目录",
			},
		},
		// 没有 "required" 字段，表示 path 是可选参数
	}
}

// Validate 校验参数（实现 Tool 接口）
func (t *ListDirTool) Validate(args map[string]any) error {
	// path 是可选参数，不传就是根目录，不需要校验
	pathVal, ok := args["path"]
	if !ok {
		return nil
	}

	// 如果传了 path，检查类型和路径合法性
	path, ok := pathVal.(string)
	if !ok {
		return fmt.Errorf("path 参数必须是字符串")
	}

	// security.IsSubPath 检查路径是否在项目目录内（防路径穿越）
	return security.IsSubPath(t.rootDir, path)
}

// Execute 执行列出目录（实现 Tool 接口）
func (t *ListDirTool) Execute(ctx context.Context, args map[string]any) (*ToolResult, error) {
	// path 可选，默认 "."（项目根目录）
	// args["path"].(string) 类型断言取值，ok 判断是否存在
	path := "."
	if pathVal, ok := args["path"].(string); ok && pathVal != "" {
		path = pathVal
	}

	// filepath.Join 拼接完整路径
	fullPath := filepath.Join(t.rootDir, path)

	// os.ReadDir 读取目录下的所有文件和子目录
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return &ToolResult{Output: fmt.Sprintf("读取目录失败: %v", err), IsError: true}, nil
	}

	// sort.Slice 自定义排序，按文件名字母顺序排列
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	// 格式化输出：目录后加 /，文件显示大小
	var lines []string
	for _, entry := range entries {
		if entry.IsDir() {
			lines = append(lines, entry.Name()+"/")
		} else {
			// entry.Info() 获取文件的详细信息（大小、权限、修改时间等）
			info, _ := entry.Info()
			lines = append(lines, fmt.Sprintf("%s (%d bytes)", entry.Name(), info.Size()))
		}
	}

	// 过滤掉 .codebuddy 目录（内部目录，不需要给 LLM 看）
	var filtered []string
	for _, line := range lines {
		if !strings.Contains(line, ".codebuddy") {
			filtered = append(filtered, line)
		}
	}

	// strings.Join 用换行符拼接所有行
	output := fmt.Sprintf("目录: %s\n%s", path, strings.Join(filtered, "\n"))
	return &ToolResult{Output: output, IsError: false}, nil
}

// IsDestructive 标记为只读工具（实现 Tool 接口）
func (t *ListDirTool) IsDestructive() bool {
	return false
}
