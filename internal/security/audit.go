package security

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AuditEntry 一条审计日志记录
type AuditEntry struct {
	Timestamp string `json:"timestamp"` // 操作时间
	Tool      string `json:"tool"`      // 工具名，如 "write_file"
	Args      string `json:"args"`      // 操作参数摘要（不含文件内容）
	Result    string `json:"result"`    // 结果：success 或 fail
	Duration  int64  `json:"duration"`  // 执行耗时（毫秒）
}

// AuditLogger 审计日志记录器
// 专门记录工具操作，和普通应用日志（zap）分开
type AuditLogger struct {
	// logFile 日志文件的完整路径
	logFile string
}

// NewAuditLogger 创建审计日志记录器
// rootDir 是项目根目录，日志文件存放在 rootDir/.codebuddy/audit.log
func NewAuditLogger(rootDir string) *AuditLogger {
	// rootDir 是项目根目录，日志文件存放在 rootDir/.codebuddy/audit.log

	return &AuditLogger{
		logFile: filepath.Join(rootDir, ".codebuddy", "audit.log"),
	}
}

// Log 记录一条审计日志
// 采用 JSON Lines 格式（每行一个 JSON），方便用 grep/jq 筛选
// 调用方式：logger.Log(AuditEntry{Tool: "write_file", Args: "main.go", Result: "success"})
func (al *AuditLogger) Log(entry AuditEntry) error {
	// entry 是要记录的审计日志条目，包含工具名、参数、结果等信息

	// 确保日志目录存在
	logDir := filepath.Dir(al.logFile) // logDir 是日志文件所在的目录路径
	// os.MkdirAll 递归创建目录，不存在的层级都创建，已存在不报错
	if err := os.MkdirAll(logDir, os.ModePerm); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}

	// 如果没传时间，自动填入当前时间
	if entry.Timestamp == "" {
		// Go 的时间格式必须用 "2006-01-02 15:04:05" 这个固定基准时间
		entry.Timestamp = time.Now().Format("2006-01-02 15:04:05")
	}

	// json.Marshal 把结构体转成 JSON 字节流（json.Unmarshal 的反操作）
	data, err := json.Marshal(entry) // data 是序列化后的 JSON 字节流
	if err != nil {
		return fmt.Errorf("序列化日志失败: %w", err)
	}

	// os.OpenFile 打开文件，支持多种模式组合：
	// os.O_APPEND  追加写入（不覆盖已有内容）
	// os.O_CREATE  文件不存在就创建
	// os.O_WRONLY  只写模式
	// 0644 是文件权限：用户可读写，其他用户只读
	f, err := os.OpenFile(al.logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %w", err)
	}
	// defer 延迟执行，函数结束时自动关闭文件
	// 不管函数正常返回还是出错，都会执行
	defer f.Close()

	// 写入 JSON + 换行符（JSON Lines 格式要求每行一条记录）
	// append(data, '\n') 在 JSON 字节后面追加一个换行符
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("写入日志失败: %w", err)
	}

	return nil
}
