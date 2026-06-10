package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lix-lang/codebuddy/internal/security"
)

// ReadFileTool read_file 工具，读取文件内容
type ReadFileTool struct {
	// rootDir 项目根目录，用于路径校验
	rootDir string
}

// NewReadFileTool 创建 read_file 工具
func NewReadFileTool(rootDir string) *ReadFileTool {
	return &ReadFileTool{rootDir: rootDir}
}

// Name 返回工具名（实现 Tool 接口）
func (t *ReadFileTool) Name() string {
	return "read_file"
}

// Description 返回工具描述（实现 Tool 接口）
// 这个描述会发给 LLM，告诉它什么时候该用这个工具
func (t *ReadFileTool) Description() string {
	return "读取指定文件的内容。当用户提到查看文件、阅读代码时使用。"
}

// Parameters 返回参数定义（实现 Tool 接口）
// Parameters 返回参数定义（实现 Tool 接口）
// 这是 JSON Schema 格式，发给 LLM 告诉它这个工具需要什么参数
// LLM 看了这个定义，就知道要传 {"path": "main.go"} 这样的参数
func (t *ReadFileTool) Parameters() map[string]any {
	return map[string]any{
		// "type": "object" 表示参数是一个 JSON 对象（键值对）
		"type": "object",
		// "properties" 定义每个参数的详细信息
		"properties": map[string]any{
			// "path" 是参数名，LLM 调用时用这个 key
			"path": map[string]any{
				// "type": "string" 表示这个参数的值是字符串
				"type": "string",
				// "description" 告诉 LLM 这个参数是什么意思
				"description": "要读取的文件路径，相对于项目根目录",
			},
		},
		// "required" 列出必填参数，LLM 必须传这些参数才能调用
		"required": []string{"path"},
	}
}

// Validate 校验参数（实现 Tool 接口）
// 检查参数类型、路径合法性、是否敏感文件
func (t *ReadFileTool) Validate(args map[string]any) error {
	// 检查 path 参数是否存在
	pathVal, ok := args["path"]
	if !ok {
		return fmt.Errorf("缺少 path 参数")
	}

	// pathVal.(string) 是类型断言，把 any 类型转成 string
	// ok 为 true 表示转换成功，false 表示类型不匹配
	path, ok := pathVal.(string)
	if !ok {
		return fmt.Errorf("path 参数必须是字符串")
	}

	// 检查是否是敏感文件（.env、.key 等）
	if security.IsSensitiveFile(path) {
		return fmt.Errorf("不允许读取敏感文件: %s", path)
	}

	// 检查路径是否在项目目录内（防路径穿越）
	if err := security.IsSubPath(t.rootDir, path); err != nil {
		return err
	}

	return nil
}

// Execute 执行读取文件（实现 Tool 接口）
func (t *ReadFileTool) Execute(ctx context.Context, args map[string]any) (*ToolResult, error) {
	// 获取 path 参数，已经过 Validate 校验，安全
	path, _ := args["path"].(string)

	// filepath.Join 拼接完整路径
	fullPath := filepath.Join(t.rootDir, path)

	// os.Stat 获取文件信息（大小、是否是目录、修改时间等）
	info, err := os.Stat(fullPath)
	if err != nil {
		// 文件不存在，返回错误结果（不是程序错误，是用户输入问题）
		return &ToolResult{Output: fmt.Sprintf("文件不存在: %s", path), IsError: true}, nil
	}

	// 如果是目录，提示用户用 list_dir
	if info.IsDir() {
		return &ToolResult{Output: fmt.Sprintf("%s 是目录，不是文件，请使用 list_dir 工具", path), IsError: true}, nil
	}

	// os.ReadFile 读取文件全部内容
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return &ToolResult{Output: fmt.Sprintf("读取文件失败: %v", err), IsError: true}, nil
	}

	// 处理大文件：超过 100KB 只返回前 100 行
	content := string(data)
	if len(data) > 100*1024 {
		// strings.Split 按换行符拆分成行数组
		lines := strings.Split(content, "\n")
		if len(lines) > 100 {
			// strings.Join 用换行符拼接前 100 行
			content = strings.Join(lines[:100], "\n")
			content += fmt.Sprintf("\n\n... 文件过大，只显示前 100 行（共 %d 行）", len(lines))
		}
	}

	return &ToolResult{Output: content, IsError: false}, nil
}

// IsDestructive 标记为只读工具（实现 Tool 接口）
func (t *ReadFileTool) IsDestructive() bool {
	return false
}

// IsAvailable 基础工具始终可用（实现 Tool 接口）
func (t *ReadFileTool) IsAvailable() bool {
	return true
}
