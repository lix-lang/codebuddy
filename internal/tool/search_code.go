package tool

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lix-lang/codebuddy/internal/security"
)

// SearchCodeTool search_code 工具，搜索代码内容
type SearchCodeTool struct {
	// rootDir 项目根目录，用于路径校验和拼接绝对路径
	rootDir string
}

// NewSearchCodeTool 创建 search_code 工具
func NewSearchCodeTool(rootDir string) *SearchCodeTool {
	return &SearchCodeTool{rootDir: rootDir}
}

// Name 返回工具名（实现 Tool 接口）
func (t *SearchCodeTool) Name() string {
	return "search_code"
}

// Description 返回工具描述（实现 Tool 接口）
func (t *SearchCodeTool) Description() string {
	return "在项目代码中搜索包含指定关键词的文件和行。当用户想查找某个函数、变量或关键词时使用。"
}

// Parameters 返回参数定义（实现 Tool 接口）
func (t *SearchCodeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "要搜索的关键词或文本",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "搜索范围（目录或文件路径），默认为项目根目录",
			},
			"file_type": map[string]any{
				"type":        "string",
				"description": "限定文件类型，如 go、py、js、ts 等（不含点号），默认搜索所有代码文件",
			},
		},
		"required": []string{"query"},
	}
}

// Validate 校验参数（实现 Tool 接口）
func (t *SearchCodeTool) Validate(args map[string]any) error {
	// query 是必填参数
	queryVal, ok := args["query"]
	if !ok {
		return fmt.Errorf("缺少 query 参数")
	}
	if _, ok := queryVal.(string); !ok {
		return fmt.Errorf("query 参数必须是字符串")
	}

	// path 可选，如果传了就校验
	if pathVal, ok := args["path"]; ok {
		if path, ok := pathVal.(string); ok && path != "" {
			return security.IsSubPath(t.rootDir, path)
		}
	}

	return nil
}

// searchResult 一条搜索结果
type searchResult struct {
	File    string // 文件路径
	Line    int    // 行号
	Content string // 匹配的行内容
}

// Execute 执行搜索代码（实现 Tool 接口）
func (t *SearchCodeTool) Execute(ctx context.Context, args map[string]any) (*ToolResult, error) {
	query, _ := args["query"].(string)
	searchPath := "."
	if pathVal, ok := args["path"].(string); ok && pathVal != "" {
		searchPath = pathVal
	}

	// file_type 参数：限定搜索的文件类型
	var fileType string
	if ftVal, ok := args["file_type"].(string); ok && ftVal != "" {
		fileType = strings.TrimPrefix(ftVal, ".")
	}

	fullPath := filepath.Join(t.rootDir, searchPath)

	// 搜索匹配的结果
	var results []searchResult
	maxResults := 50

	filepath.Walk(fullPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		// 如果指定了 file_type，只搜索对应扩展名的文件
		if fileType != "" {
			ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
			if ext != strings.ToLower(fileType) {
				return nil
			}
		} else if !isCodeFile(path) {
			// 未指定 file_type 时，使用默认的代码文件过滤
			return nil
		}

		matches := searchInFile(path, query)
		results = append(results, matches...)
		if len(results) >= maxResults {
			results = results[:maxResults]
			return fmt.Errorf("达到最大结果数")
		}

		return nil
	})

	// 格式化输出
	if len(results) == 0 {
		return &ToolResult{Output: fmt.Sprintf("没有找到包含 %q 的代码", query), IsError: false}, nil
	}

	// 转换为相对路径显示
	var lines []string
	for _, r := range results {
		// 把绝对路径转成相对于项目根目录的路径
		relPath, _ := filepath.Rel(t.rootDir, r.File)
		lines = append(lines, fmt.Sprintf("%s:%d: %s", relPath, r.Line, strings.TrimSpace(r.Content)))
	}

	output := strings.Join(lines, "\n")
	return &ToolResult{Output: output, IsError: false}, nil
}

// IsDestructive 标记为只读工具（实现 Tool 接口）
func (t *SearchCodeTool) IsDestructive() bool {
	return false
}

// IsAvailable 基础工具始终可用（实现 Tool 接口）
func (t *SearchCodeTool) IsAvailable() bool {
	return true
}
func isCodeFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	codeExts := map[string]bool{
		".go": true, ".py": true, ".js": true, ".ts": true, ".tsx": true,
		".java": true, ".c": true, ".cpp": true, ".h": true, ".rs": true,
		".rb": true, ".php": true, ".swift": true, ".kt": true,
		".json": true, ".yaml": true, ".yml": true, ".toml": true,
		".md": true, ".txt": true, ".sh": true, ".sql": true,
		".html": true, ".css": true, ".scss": true,
	}
	return codeExts[ext]
}

// searchInFile 在单个文件中搜索关键词
func searchInFile(path string, query string) []searchResult {
	// os.Open 打开文件
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var results []searchResult
	// bufio.NewScanner 按行读取文件，比 ReadFile 更省内存
	scanner := bufio.NewScanner(f)
	lineNum := 0

	// scanner.Scan() 逐行读取，读到文件末尾返回 false
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// strings.Contains 检查这行是否包含搜索关键词
		// strings.ToLower 转小写，实现不区分大小写搜索
		if strings.Contains(strings.ToLower(line), strings.ToLower(query)) {
			results = append(results, searchResult{
				File:    path,
				Line:    lineNum,
				Content: line,
			})
			// 每个文件最多返回 10 条匹配
			if len(results) >= 10 {
				break
			}
		}
	}

	return results
}
