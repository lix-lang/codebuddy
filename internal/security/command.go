package security

import (
	"fmt"
	"strings"
)

// dangerousPatterns 危险命令黑名单
// 这些命令不管什么情况都不允许执行
var dangerousPatterns = []string{
	"rm -rf /",      // 删除整个根目录
	"rm -rf /*",     // 删除根目录所有文件
	"sudo",          // 提权操作
	"mkfs",          // 格式化磁盘
	"dd if=",        // 磁盘覆写
	"chmod 777",     // 危险权限设置
	"curl | sh",     // 远程脚本执行
	"wget | sh",     // 远程脚本执行
	"curl | bash",   // 远程脚本执行
	"wget | bash",   // 远程脚本执行
	":(){ :|:& };:", // fork 炸弹
}

// sensitiveFiles 敏感文件黑名单
// 这些文件即使路径合法也不允许读取
var sensitiveFiles = []string{
	".env",        // 环境变量文件，通常包含数据库密码、API Key 等
	".ssh",        // SSH 配置目录，包含私钥和已知主机列表
	"credentials", // 凭证文件，如 AWS credentials、服务账号密钥
	".pem",        // PEM 格式的证书/私钥文件
	".key",        // 密钥文件，用于加密解密或身份认证
	".p12",        // PKCS#12 格式的证书文件，包含私钥和证书链
	"id_rsa",      // RSA 格式的 SSH 私钥
	"id_ed25519",  // Ed25519 格式的 SSH 私钥（更现代的加密算法）
}

// CheckCommand 检查命令是否安全
// 返回 nil 表示安全，返回 error 表示是危险命令
// 所有命令执行前都要调用这个函数检查
func CheckCommand(cmd string) error {
	// strings.ToLower 把命令转成小写，匹配时不区分大小写
	// 比如 "SUDO rm -rf /" 也能被匹配到
	lowerCmd := strings.ToLower(cmd)

	// 遍历黑名单，检查命令是否包含危险模式
	for _, pattern := range dangerousPatterns {
		// strings.Contains 检查字符串是否包含子串
		if strings.Contains(lowerCmd, strings.ToLower(pattern)) {
			return fmt.Errorf("危险命令被拦截: 包含 %q", pattern)
		}
	}

	// 检查是否有后台执行（& 但不是 &&）
	// "sleep 10 &" 是后台执行，有隐藏风险
	// "go build && go test" 是正常的命令链，允许
	if strings.Contains(lowerCmd, " &") && !strings.Contains(lowerCmd, " &&") {
		return fmt.Errorf("不允许后台执行命令: 包含 '&'")
	}

	return nil
}

// IsSensitiveFile 检查文件名是否是敏感文件
// 返回 true 表示是敏感文件，不应该被读取或修改
func IsSensitiveFile(filename string) bool {
	// strings.ToLower 转小写，统一比较
	lower := strings.ToLower(filename)
	for _, sensitive := range sensitiveFiles {
		// strings.Contains 检查文件名是否包含敏感关键词
		// 比如 "config.key" 包含 ".key"
		if strings.Contains(lower, strings.ToLower(sensitive)) {
			return true
		}
	}
	return false
}
