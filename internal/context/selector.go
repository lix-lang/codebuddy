package context

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// FileScore 带分数的文件条目，用于排序选择
type FileScore struct {
	Path  string // 文件相对路径
	Score int    // 得分（越高越相关）
	Size  int64  // 文件大小（字节）
}

// SelectFiles 选择跟当前查询最相关的文件
// 从所有项目文件中，根据查询内容打分，选出最相关的文件
//
// 打分算法（三信号加权）：
//   - 显式提及（权重 100）：用户或 LLM 直接提到了文件名
//   - 关键词匹配（权重 50）：查询中的关键词出现在文件路径或导出符号中
//   - 工作集惯性（权重 30）：最近几轮对话中涉及的文件保持高分
//
// 选择算法（贪心装箱）：
//   - 按分数从高到低排序，逐个尝试加入
//   - 如果文件太大超预算，尝试生成摘要（只保留函数签名）
//   - 摘要后还超预算就跳过
func SelectFiles(query string, projectFiles []FileInfo, workingFiles []string, tokenBudget int) []string {
	// 1. 给每个文件打分
	scores := scoreFiles(query, projectFiles, workingFiles)

	// 2. 按分数从高到低排序
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	// 3. 贪心装箱：按分数从高到低，逐个尝试加入
	var selected []string
	usedTokens := 0

	for _, fs := range scores {
		if fs.Score == 0 {
			continue // 0 分文件不选
		}

		// 估算文件 token 数（文件大小 / 3，粗略估算）
		fileTokens := int(fs.Size) / 3

		// 如果加入这个文件会超预算，跳过
		if usedTokens+fileTokens > tokenBudget {
			continue
		}

		selected = append(selected, fs.Path)
		usedTokens += fileTokens
	}

	return selected
}

// scoreFiles 给所有文件打分
func scoreFiles(query string, projectFiles []FileInfo, workingFiles []string) []FileScore {
	scores := make([]FileScore, 0, len(projectFiles))

	// 从查询中提取关键词（去掉停用词）
	keywords := extractKeywords(query)

	for _, f := range projectFiles {
		score := 0

		// 信号 1：显式提及（权重 100）
		// 检查查询中是否直接提到了文件名
		// 比如 "看看 config.go" → config.go 得 100 分
		if isFileMentioned(query, f.Path) {
			score += 100
		}

		// 信号 2：关键词匹配（权重 50）
		// 检查查询中的关键词是否出现在文件路径或导出符号中
		// 比如 "修改 User 的 Create 方法" → user.go 得分
		for _, kw := range keywords {
			// 匹配文件路径
			if strings.Contains(strings.ToLower(f.Path), strings.ToLower(kw)) {
				score += 50
				break // 一个关键词只算一次
			}
			// 匹配导出符号
			for _, exp := range f.Exports {
				if strings.EqualFold(exp, kw) {
					score += 50
					break
				}
			}
		}

		// 信号 3：工作集惯性（权重 30）
		// 最近几轮对话中涉及的文件保持高分
		// 因为 Agent 在修改一个文件时，后续操作大概率还涉及这个文件
		for _, wf := range workingFiles {
			if wf == f.Path {
				score += 30
				break
			}
		}

		scores = append(scores, FileScore{
			Path:  f.Path,
			Score: score,
			Size:  f.Size,
		})
	}

	return scores
}

// isFileMentioned 检查查询中是否直接提到了某个文件
// 支持的格式：config.go、internal/config/config.go、`config.go`
func isFileMentioned(query, filePath string) bool {
	// 取文件名（不含路径）
	fileName := filepath.Base(filePath)

	// 用正则匹配查询中的文件名
	// 比如 "看看 config.go" → 匹配 "config.go"
	// regexp.MustCompile 编译正则表达式（只编译一次，比每次调用快）
	// \b 是单词边界，确保匹配完整文件名
	pattern := `\b` + regexp.QuoteMeta(fileName) + `\b`
	matched, _ := regexp.MatchString(pattern, query)
	if matched {
		return true
	}

	// 也检查完整路径
	if strings.Contains(query, filePath) {
		return true
	}

	return false
}

// extractKeywords 从查询中提取关键词（去掉停用词）
// 比如 "帮我看看 config.go 里的 ReadFile 函数"
// → ["config.go", "ReadFile", "函数"]（去掉 "帮我"、"看看"、"里的"）
func extractKeywords(query string) []string {
	// Go 语言停用词列表（中英文混合）
	stopWords := map[string]bool{
		"的": true, "了": true, "在": true, "是": true, "我": true,
		"你": true, "他": true, "她": true, "它": true, "们": true,
		"这": true, "那": true, "有": true, "和": true, "与": true,
		"或": true, "不": true, "也": true, "都": true, "能": true,
		"会": true, "要": true, "把": true, "被": true, "让": true,
		"给": true, "用": true, "从": true, "到": true, "里": true,
		"中": true, "上": true, "下": true, "帮": true, "看": true,
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "in": true, "on": true, "at": true, "to": true,
		"for": true, "of": true, "and": true, "or": true, "it": true,
	}

	// 按空格和标点分割
	// regexp.MustCompile(`[\s,，。.!！?？、:：;；]+`) 匹配分隔符
	re := regexp.MustCompile(`[\s,，。.!！?？、:：;；]+`)
	parts := re.Split(query, -1)

	var keywords []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || stopWords[p] {
			continue
		}
		// 去掉 `code.go` 中的反引号
		p = strings.Trim(p, "`\"'")
		if len(p) >= 2 { // 至少 2 个字符才算关键词
			keywords = append(keywords, p)
		}
	}

	return keywords
}
