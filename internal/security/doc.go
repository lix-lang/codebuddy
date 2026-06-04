// Package security 提供安全相关的工具函数，被各 Tool 和 Agent 直接调用。
//
// 三大核心能力：
//
//  1. 路径校验（IsSubPath）
//     防止路径穿越攻击，确保所有文件操作都在项目目录内。
//     例：../../etc/passwd → 拒绝
//
//  2. 备份管理（BackupManager）
//     修改文件前自动备份到 .codebuddy/backup/，支持 /undo 回退。
//     保留最近 10 份备份。
//
//  3. 命令安全（CheckCommand）
//     黑名单拦截危险命令（rm -rf /、sudo、mkfs 等）。
//     所有命令执行前都要展示给用户确认。
package security
