package security

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BackupManager 备份管理器
// 负责文件备份、恢复、清理
type BackupManager struct {
	// rootDir 项目根目录，备份存放在 rootDir/.codebuddy/backup/ 下
	rootDir string
	// maxKeep 最多保留多少份备份，超过的自动清理
	maxKeep int
}

// NewBackupManager 创建备份管理器
// rootDir 是项目根目录，maxKeep 是最大保留备份数（默认 10）
func NewBackupManager(rootDir string, maxKeep int) *BackupManager {
	if maxKeep <= 0 {
		maxKeep = 10
	}
	return &BackupManager{
		rootDir: rootDir,
		maxKeep: maxKeep,
	}
}

// Backup 备份一个文件
// 把原文件复制到 .codebuddy/backup/YYYYMMDD_HHMMSS_文件名
// 返回备份文件的完整路径
func (bm *BackupManager) Backup(filePath string) (string, error) {
	// os.ReadFile 读取原文件全部内容到 []byte
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("读取文件失败: %w", err)
	}

	// 创建备份目录（如果不存在）
	// filepath.Join 拼接：项目根目录/.codebuddy/backup/
	backupDir := filepath.Join(bm.rootDir, ".codebuddy", "backup")
	// os.MkdirAll 递归创建目录，不存在的层级都创建，已存在不报错
	// os.ModePerm 是默认权限 0755（用户可读写执行，其他可读执行）
	if err := os.MkdirAll(backupDir, os.ModePerm); err != nil {
		return "", fmt.Errorf("创建备份目录失败: %w", err)
	}

	// 生成备份文件名：20240101_150405_main.go
	// time.Now().Format 按指定格式输出当前时间字符串
	// Go 的时间格式必须用 "2006-01-02 15:04:05" 这个固定基准时间
	timestamp := time.Now().Format("20060102_150405")
	// filepath.Base 取路径的最后一部分（文件名）
	// 比如 "/Users/app/main.go" → "main.go"
	backupName := fmt.Sprintf("%s_%s", timestamp, filepath.Base(filePath))
	backupPath := filepath.Join(backupDir, backupName)

	// 把原文件内容写入备份文件
	// 0644 是文件权限：用户可读写，其他用户只读
	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return "", fmt.Errorf("写入备份失败: %w", err)
	}

	return backupPath, nil
}

// Restore 从最近的备份恢复文件
// 找到该文件的最新备份，覆盖当前文件
func (bm *BackupManager) Restore(filePath string) error {
	backupDir := filepath.Join(bm.rootDir, ".codebuddy", "backup")
	// filepath.Base 取文件名，用来匹配对应的备份文件
	fileName := filepath.Base(filePath)

	// os.ReadDir 读取目录下的所有文件和子目录
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return fmt.Errorf("读取备份目录失败: %w", err)
	}

	// 找到匹配该文件名的所有备份
	var backups []string
	for _, entry := range entries {
		// strings.HasSuffix 检查字符串是否以指定后缀结尾
		// 比如 "20240101_150405_main.go" 以 "_main.go" 结尾
		if strings.HasSuffix(entry.Name(), "_"+fileName) {
			backups = append(backups, entry.Name())
		}
	}

	if len(backups) == 0 {
		return fmt.Errorf("没有找到 %s 的备份", fileName)
	}

	// sort.Sort(sort.Reverse(sort.StringSlice(backups))) 三层嵌套：
	// 1. sort.StringSlice(backups) 把 []string 转成可排序类型
	// 2. sort.Reverse(...) 反转排序规则（正序变倒序）
	// 3. sort.Sort(...) 执行排序
	// 因为文件名以时间戳开头，倒序排列后最新的在前面
	sort.Sort(sort.Reverse(sort.StringSlice(backups)))

	// 读取最新备份的内容（backups[0] 是最新的）
	latestBackup := filepath.Join(backupDir, backups[0])
	data, err := os.ReadFile(latestBackup)
	if err != nil {
		return fmt.Errorf("读取备份文件失败: %w", err)
	}

	// 覆盖当前文件，完成恢复
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("恢复文件失败: %w", err)
	}

	return nil
}

// Clean 清理旧备份，只保留最近 maxKeep 份
func (bm *BackupManager) Clean() error {
	backupDir := filepath.Join(bm.rootDir, ".codebuddy", "backup")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		// 备份目录不存在，不需要清理
		return nil
	}

	// sort.Slice 自定义排序，按文件名正序排列（时间戳开头，正序 = 从旧到新）
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	// 如果备份数量超过 maxKeep，删除多余的（从最旧的开始删）
	if len(entries) > bm.maxKeep {
		deleteCount := len(entries) - bm.maxKeep
		for i := 0; i < deleteCount; i++ {
			deletePath := filepath.Join(backupDir, entries[i].Name())
			// os.Remove 删除一个文件
			if err := os.Remove(deletePath); err != nil {
				return fmt.Errorf("清理备份失败: %w", err)
			}
		}
	}

	return nil
}
