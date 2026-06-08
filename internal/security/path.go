package security

import (
	"fmt"
	"path/filepath"
	"strings"
)

// IsSubPath 检查 target 路径是否在 root 目录下
// 防止路径穿越攻击，比如 ../../etc/passwd
// 返回 nil 表示安全，返回 error 表示路径不合法
func IsSubPath(root, target string) error {
	// root 是项目根目录的路径，用作安全边界
	// target 是要检查的目标路径，需要确认它在 root 目录内

	// filepath.Abs 把相对路径转成绝对路径
	// 比如 "." → "/Users/lixiaoyang/Projects/myapp"
	absRoot, err := filepath.Abs(root) // absRoot 是 root 转换后的绝对路径
	if err != nil {
		return fmt.Errorf("解析根路径失败: %w", err)
	}

	absTarget, err := filepath.Abs(target) // absTarget 是 target 转换后的绝对路径
	if err != nil {
		return fmt.Errorf("解析目标路径失败: %w", err)
	}

	// filepath.EvalSymlinks 解析软链接的真实路径
	// 防止通过软链接跳到项目外的目录
	// 比如 /project/link → /etc/passwd
	evalRoot, err := filepath.EvalSymlinks(absRoot) // evalRoot 是 root 解析软链接后的真实路径
	if err != nil {
		return fmt.Errorf("解析根路径软链接失败: %w", err)
	}

	evalTarget, err := filepath.EvalSymlinks(absTarget) // evalTarget 是 target 解析软链接后的真实路径
	if err != nil {
		// 目标文件可能还不存在（比如 write_file 要创建新文件），不算错误
		// 这种情况用未解析的绝对路径继续检查
		evalTarget = absTarget
	}

	// 确保路径末尾有分隔符，防止 /app 匹配到 /application
	// filepath.Join(path, "") 会在末尾加 "/"
	// 比如 "/Users/app" → "/Users/app/"
	evalRoot = filepath.Join(evalRoot, "")

	// strings.HasPrefix 检查目标路径是否以根路径开头
	// 如果是，说明目标在项目目录内，安全
	if !strings.HasPrefix(evalTarget, evalRoot) {
		return fmt.Errorf("路径越界: %s 不在项目目录 %s 内", target, root)
	}

	return nil
}
