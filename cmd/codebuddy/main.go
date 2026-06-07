// cmd/codebuddy 是程序的入口点。
// 用户在终端执行 `codebuddy` 命令时，就是运行这里的 main() 函数。
// 它负责：加载配置 → 初始化各组件 → 启动 TUI 交互界面。
package main

import (
	"fmt"
	"os"

	"github.com/lix-lang/codebuddy/internal/config"
	"github.com/spf13/cobra"
)

// rootCmd 根命令：直接运行 codebuddy 时执行
// cobra.Command 定义一个 CLI 命令
// Use 是用法提示，Short 是简短描述，Run 是执行函数
var rootCmd = &cobra.Command{
	Use:   "codebuddy",
	Short: "AI 编程助手",
	Run: func(cmd *cobra.Command, args []string) {
		// 1. 加载配置（合并全局 + 项目级，替换环境变量）
		cfg, err := config.Load(".")
		if err != nil {
			// fmt.Fprintln(os.Stderr, ...) 输出到标准错误流（用于报错信息）
			// os.Stderr 是标准错误输出，和 fmt.Println（标准输出）分开
			fmt.Fprintln(os.Stderr, "加载配置失败:", err)
			// os.Exit(1) 立即终止程序，1 表示异常退出，0 表示正常退出
			os.Exit(1)
		}

		// 2. 校验配置（检查必填字段、API Key、URL 格式）
		if err := config.Validate(cfg); err != nil {
			fmt.Fprintln(os.Stderr, "配置校验失败:", err)
			os.Exit(1)
		}

		// TODO: 初始化 Agent → 启动 TUI（后面的 Day 写）
		fmt.Println("Agent 启动成功，模型:", cfg.LLM.Model)
	},
}

// initCmd init 子命令：交互式创建配置文件
// 用户运行 codebuddy init 时执行
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "初始化配置",
	Run: func(cmd *cobra.Command, args []string) {
		// TODO: 后面换成 bubbletea 交互式 UI（Day 17）
		fmt.Println("初始化配置...")
		fmt.Println("请运行: codebuddy config set llm.provider openai")
	},
}

// configCmd config 子命令组（本身不执行，只是挂载 config list 等子命令）
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "管理配置",
}

// configListCmd config list 子命令：查看当前所有配置
var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "查看所有配置",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load(".")
		if err != nil {
			fmt.Fprintln(os.Stderr, "加载配置失败:", err)
			os.Exit(1)
		}
		// 打印当前配置（隐藏 API Key，不泄露敏感信息）
		fmt.Printf("Provider:    %s\n", cfg.LLM.Provider)
		fmt.Printf("Model:       %s\n", cfg.LLM.Model)
		fmt.Printf("BaseURL:     %s\n", cfg.LLM.BaseURL)
		fmt.Printf("MaxSteps:    %d\n", cfg.Agent.MaxSteps)
	},
}

// init() 是 Go 的特殊函数，在 main() 之前自动执行
// 这里用来注册子命令的层级关系
func init() {
	// AddCommand 把子命令挂到父命令下
	// 执行效果：codebuddy config list
	configCmd.AddCommand(configListCmd)
	// 执行效果：codebuddy init、codebuddy config
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(configCmd)
}

func main() {
	// Execute 解析命令行参数，找到对应的命令并执行它的 Run 函数
	// 比如用户输入 codebuddy init → 找到 initCmd → 执行 initCmd.Run
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
