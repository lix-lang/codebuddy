package context

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ProjectContext 项目级上下文，启动时扫描一次，结果缓存
type ProjectContext struct {
	RootDir string        // 项目根目录
	Module  *ModuleInfo   // go.mod 解析结果
	Tree    []DirEntry    // 目录树（3 层深度）
	Files   []FileInfo    // 所有代码文件信息
	Style   *CodeStyle    // 代码风格分析结果
}

// ModuleInfo go.mod 解析结果
type ModuleInfo struct {
	Name     string   // module 名，如 "github.com/lix-lang/codebuddy"
	GoVersion string  // Go 版本，如 "1.22"
	Dependencies []string // 主要依赖名（如 "gin"、"gorm"）
}

// DirEntry 目录树中的一个条目
type DirEntry struct {
	Path  string // 相对路径，如 "internal/config"
	IsDir bool   // 是否是目录
	Size  int64  // 文件大小（目录为 0）
}

// FileInfo 一个代码文件的信息
type FileInfo struct {
	Path      string   // 相对路径，如 "internal/llm/types.go"
	Exports   []string // 导出的符号名（函数名、结构体名、接口名）
	Package   string   // 包名
	Size      int64    // 文件大小（字节）
	ModTime   int64    // 最后修改时间（Unix 时间戳）
}

// CodeStyle 代码风格分析结果
type CodeStyle struct {
	ErrorPattern  string // 错误处理模式："return err" 或 "log.Fatal"
	NamingStyle   string // 命名风格："camelCase" 或 "snake_case"
	CommentStyle  string // 注释风格："full"（完整 godoc）或 "minimal"
	Layers        []string // 分层结构，如 ["handler", "service", "repo"]
}

// cacheFile 缓存文件路径
const cacheFile = ".codebuddy/cache/project.json"

// ScanProject 扫描项目，生成 ProjectContext
// 结果会缓存到 .codebuddy/cache/project.json，下次启动时增量更新
func ScanProject(rootDir string) (*ProjectContext, error) {
	ctx := &ProjectContext{RootDir: rootDir}

	// 1. 解析 go.mod
	ctx.Module = parseGoMod(rootDir)

	// 2. 生成目录树（3 层深度）
	ctx.Tree = buildDirTree(rootDir, 3)

	// 3. 扫描所有代码文件（并发解析 AST）
	files := collectCodeFiles(rootDir)
	ctx.Files = analyzeFilesConcurrently(rootDir, files)

	// 4. 代码风格分析（采样 5 个文件）
	ctx.Style = analyzeCodeStyle(rootDir, ctx.Files)

	// 5. 缓存结果
	saveCache(rootDir, ctx)

	return ctx, nil
}

// parseGoMod 解析 go.mod 文件
func parseGoMod(rootDir string) *ModuleInfo {
	// os.ReadFile 读取 go.mod 文件
	data, err := os.ReadFile(filepath.Join(rootDir, "go.mod"))
	if err != nil {
		return nil // go.mod 不存在，不是 Go 项目
	}

	info := &ModuleInfo{}
	content := string(data)

	// 按行解析（不引入 gomod 解析库，简单处理够用）
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)

		// module github.com/lix-lang/codebuddy
		if strings.HasPrefix(line, "module ") {
			info.Name = strings.TrimPrefix(line, "module ")
		}

		// go 1.22
		if strings.HasPrefix(line, "go ") {
			info.GoVersion = strings.TrimPrefix(line, "go ")
		}

		// 收集间接依赖（require 块中的）
		if strings.Contains(line, " ") && !strings.HasPrefix(line, "//") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				dep := parts[0]
				// 只取依赖名最后一部分，如 "github.com/gin-gonic/gin" → "gin"
				if strings.Contains(dep, "/") {
					dep = dep[strings.LastIndex(dep, "/")+1:]
				}
				info.Dependencies = append(info.Dependencies, dep)
			}
		}
	}

	return info
}

// buildDirTree 生成目录树
// maxDepth 控制递归深度（默认 3 层）
func buildDirTree(rootDir string, maxDepth int) []DirEntry {
	var entries []DirEntry

	// walkDir 递归遍历目录
	var walkDir func(dir string, depth int)
	walkDir = func(dir string, depth int) {
		if depth > maxDepth {
			return
		}

		// os.ReadDir 读取目录内容
		items, err := os.ReadDir(dir)
		if err != nil {
			return
		}

		// 忽略的目录
		skipDirs := map[string]bool{
			"vendor": true, ".git": true, "node_modules": true,
			".idea": true, "__pycache__": true, ".codebuddy": true,
		}

		for _, item := range items {
			// 跳过隐藏文件和忽略目录
			name := item.Name()
			if strings.HasPrefix(name, ".") || skipDirs[name] {
				continue
			}

			relPath, _ := filepath.Rel(rootDir, filepath.Join(dir, name))
			entry := DirEntry{
				Path:  relPath,
				IsDir: item.IsDir(),
			}

			if !item.IsDir() {
				info, _ := item.Info()
				entry.Size = info.Size()
			}

			entries = append(entries, entry)

			// 递归处理子目录
			if item.IsDir() {
				walkDir(filepath.Join(dir, name), depth+1)
			}
		}
	}

	walkDir(rootDir, 0)
	return entries
}

// collectCodeFiles 收集所有代码文件路径
func collectCodeFiles(rootDir string) []string {
	var files []string

	filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			// 跳过不需要的目录
			name := info.Name()
			if info.IsDir() && (strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}

		// 只收集 Go 文件（其他语言后期扩展）
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			relPath, _ := filepath.Rel(rootDir, path)
			files = append(files, relPath)
		}
		return nil
	})

	return files
}

// analyzeFilesConcurrently 并发解析所有 Go 文件的 AST
// sync.WaitGroup 等待所有 goroutine 完成
func analyzeFilesConcurrently(rootDir string, files []string) []FileInfo {
	// sync.WaitGroup 是 Go 的并发同步工具
	// Add(n) 表示要等 n 个任务完成
	// Done() 表示一个任务完成了
	// Wait() 阻塞直到所有任务完成
	var wg sync.WaitGroup
	// mu 保护 results 的并发写入
	var mu sync.Mutex
	results := make([]FileInfo, 0, len(files))

	// 每个文件启动一个 goroutine 并发解析
	for _, file := range files {
		wg.Add(1)
		go func(relPath string) {
			defer wg.Done()
			info := analyzeGoFile(rootDir, relPath)

			mu.Lock()         // 加锁，防止并发写 results
			results = append(results, info)
			mu.Unlock()       // 解锁
		}(file)
	}

	wg.Wait() // 等待所有 goroutine 完成
	return results
}

// analyzeGoFile 解析单个 Go 文件，提取导出符号
func analyzeGoFile(rootDir, relPath string) FileInfo {
	fullPath := filepath.Join(rootDir, relPath)
	info := FileInfo{Path: relPath}

	// 获取文件信息
	stat, err := os.Stat(fullPath)
	if err != nil {
		return info
	}
	info.Size = stat.Size()
	info.ModTime = stat.ModTime().Unix()

	// go/parser.ParseFile 解析 Go 文件的 AST（抽象语法树）
	// token.NewFileSet() 创建文件集，记录位置信息
	// parser.ImportsOnly 只解析 import 和包声明（更快）
	// 这里用完整解析，提取导出符号
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, fullPath, nil, parser.PackageClauseOnly)
	if err != nil {
		return info
	}

	info.Package = f.Name.Name // 包名

	// 提取导出的函数名和类型名
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			// ast.FuncDecl 是函数声明节点
			// d.Name.Name 是函数名，d.Name.IsExported() 判断是否导出（首字母大写）
			if d.Name.IsExported() {
				info.Exports = append(info.Exports, d.Name.Name)
			}
		}
	}

	return info
}

// analyzeCodeStyle 采样分析代码风格
func analyzeCodeStyle(rootDir string, files []FileInfo) *CodeStyle {
	style := &CodeStyle{
		ErrorPattern: "return err", // 默认
		NamingStyle:  "camelCase",  // Go 默认
		CommentStyle: "minimal",
	}

	// 采样最多 5 个文件分析
	sampleCount := 5
	if len(files) < sampleCount {
		sampleCount = len(files)
	}

	for i := 0; i < sampleCount; i++ {
		fullPath := filepath.Join(rootDir, files[i].Path)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		content := string(data)

		// 检测错误处理模式
		if strings.Contains(content, "log.Fatal") {
			style.ErrorPattern = "log.Fatal"
		}
	}

	// 检测分层结构（从目录名推断）
	layers := map[string]bool{}
	for _, entry := range files {
		parts := strings.Split(entry.Path, "/")
		for _, part := range parts {
			if part == "handler" || part == "service" || part == "repo" ||
				part == "controller" || part == "dao" || part == "repository" {
				layers[part] = true
			}
		}
	}
	for layer := range layers {
		style.Layers = append(style.Layers, layer)
	}

	return style
}

// saveCache 把扫描结果缓存到文件
func saveCache(rootDir string, ctx *ProjectContext) {
	cacheDir := filepath.Join(rootDir, ".codebuddy", "cache")
	os.MkdirAll(cacheDir, os.ModePerm)

	data, err := json.Marshal(ctx)
	if err != nil {
		return
	}
	os.WriteFile(filepath.Join(rootDir, cacheFile), data, 0644)
}

// Summary 返回项目概览文本（约 1K token，注入上下文给 LLM）
func (pc *ProjectContext) Summary() string {
	var sb strings.Builder

	// 项目基本信息
	if pc.Module != nil {
		sb.WriteString(fmt.Sprintf("项目: %s (Go %s)\n", pc.Module.Name, pc.Module.GoVersion))
		if len(pc.Module.Dependencies) > 0 {
			sb.WriteString(fmt.Sprintf("依赖: %s\n", strings.Join(pc.Module.Dependencies[:min(10, len(pc.Module.Dependencies))], ", ")))
		}
	}

	// 分层结构
	if pc.Style != nil && len(pc.Style.Layers) > 0 {
		sb.WriteString(fmt.Sprintf("分层: %s\n", strings.Join(pc.Style.Layers, " → ")))
	}

	// 文件数量
	goFiles := 0
	for _, f := range pc.Files {
		if strings.HasSuffix(f.Path, ".go") {
			goFiles++
		}
	}
	sb.WriteString(fmt.Sprintf("文件数: %d 个 Go 文件\n", goFiles))

	return sb.String()
}

// RepoMap 返回仓库地图（AST 导出符号），用于 LLM 快速了解项目结构
func (pc *ProjectContext) RepoMap() string {
	var sb strings.Builder
	sb.WriteString("=== 项目结构 ===\n")

	for _, f := range pc.Files {
		if len(f.Exports) > 0 {
			sb.WriteString(fmt.Sprintf("%s (%s): %s\n",
				f.Path, f.Package, strings.Join(f.Exports, ", ")))
		}
	}

	return sb.String()
}
