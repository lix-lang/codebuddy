package tool

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/lix-lang/codebuddy/internal/security"
)

// RunCommandTool run_command 工具，执行 shell 命令
type RunCommandTool struct {
	// rootDir 项目根目录，作为命令执行的工作目录
	rootDir string
}

// NewRunCommandTool 创建 run_command 工具
func NewRunCommandTool(rootDir string) *RunCommandTool {
	return &RunCommandTool{rootDir: rootDir}
}

// Name 返回工具名（实现 Tool 接口）
func (t *RunCommandTool) Name() string {
	return "run_command"
}

// Description 返回工具描述（实现 Tool 接口）
func (t *RunCommandTool) Description() string {
	return "执行 shell 命令。需要用户确认后才会执行。用于运行测试、编译、安装依赖等操作。"
}

// Parameters 返回参数定义（实现 Tool 接口）
func (t *RunCommandTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "要执行的 shell 命令",
			},
		},
		"required": []string{"command"},
	}
}

// Validate 校验参数（实现 Tool 接口）
func (t *RunCommandTool) Validate(args map[string]any) error {
	cmdVal, ok := args["command"]
	if !ok {
		return fmt.Errorf("缺少 command 参数")
	}
	cmd, ok := cmdVal.(string)
	if !ok {
		return fmt.Errorf("command 参数必须是字符串")
	}

	// security.CheckCommand 检查命令是否包含危险模式（sudo、rm -rf / 等）
	return security.CheckCommand(cmd)
}

// Execute 执行 shell 命令（实现 Tool 接口）
func (t *RunCommandTool) Execute(ctx context.Context, args map[string]any) (*ToolResult, error) {
	command, _ := args["command"].(string)

	// 设置超时：30 秒
	// context.WithTimeout 在 ctx 的基础上创建一个 30 秒后自动取消的 context
	// cancel() 用来手动释放资源，用 defer 确保函数结束时一定执行
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// exec.CommandContext 创建一个带超时控制的命令执行对象
	// "bash" 是执行的程序，"-c" 表示后面的字符串是命令内容
	// 比如 exec.CommandContext(ctx, "bash", "-c", "go test ./...")
	cmd := exec.CommandContext(timeoutCtx, "bash", "-c", command)

	// 设置工作目录为项目根目录
	cmd.Dir = t.rootDir

	// bytes.Buffer 是一个可变大小的字节缓冲区，用来收集命令的输出
	var stdout, stderr bytes.Buffer
	// cmd.Stdout 把命令的标准输出重定向到 stdout 缓冲区
	cmd.Stdout = &stdout
	// cmd.Stderr 把命令的错误输出重定向到 stderr 缓冲区
	cmd.Stderr = &stderr

	// cmd.Run 执行命令并等待完成
	err := cmd.Run()

	// 组装输出
	var output string
	// stdout.String() 获取缓冲区里的字符串内容
	if stdout.Len() > 0 {
		output += "输出:\n" + stdout.String()
	}
	if stderr.Len() > 0 {
		output += "错误:\n" + stderr.String()
	}

	// 限制输出长度，防止超长输出占用太多 token
	maxOutputLen := 10000
	if len(output) > maxOutputLen {
		output = output[:maxOutputLen] + fmt.Sprintf("\n\n... 输出过长，已截断（共 %d 字符）", len(output))
	}

	if err != nil {
		// 命令执行失败（非零退出码或超时）
		return &ToolResult{
			Output:  fmt.Sprintf("命令执行失败: %v\n%s", err, output),
			IsError: true,
		}, nil
	}

	return &ToolResult{Output: output, IsError: false}, nil
}

// IsDestructive 标记为破坏性操作（命令可能修改文件系统）
func (t *RunCommandTool) IsDestructive() bool {
	return true
}
