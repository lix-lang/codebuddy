package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lix-lang/codebuddy/internal/agent"
	"github.com/lix-lang/codebuddy/internal/config"
	"github.com/lix-lang/codebuddy/internal/context"
	"github.com/lix-lang/codebuddy/internal/llm"
	"github.com/lix-lang/codebuddy/internal/tool"
	"github.com/lix-lang/codebuddy/internal/tui"
	"github.com/spf13/cobra"
)

// rootCmd 根命令：直接运行 codebuddy 时执行
var rootCmd = &cobra.Command{
	Use:   "codebuddy",
	Short: "AI 编程助手",
	Run: func(cmd *cobra.Command, args []string) {
		// 1. 加载配置（没配置文件时走首次引导）
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, "加载配置失败:", err)
			os.Exit(1)
		}

		// 如果没有 API Key，走首次配置引导
		if cfg.LLM.APIKey == "" {
			fmt.Println("首次使用，需要配置 LLM API Key。")
			fmt.Println()
			cfg = runInit()
			if cfg.LLM.APIKey == "" {
				fmt.Fprintln(os.Stderr, "未配置 API Key，退出。")
				os.Exit(1)
			}
		}

		// 如果配置是零值（没加载到文件），用默认值填充
		if cfg.LLM.Model == "" {
			defaults := config.DefaultConfig()
			if cfg.LLM.Provider == "" {
				cfg.LLM.Provider = defaults.LLM.Provider
			}
			if cfg.LLM.Model == "" {
				cfg.LLM.Model = defaults.LLM.Model
			}
			if cfg.LLM.BaseURL == "" {
				cfg.LLM.BaseURL = defaults.LLM.BaseURL
			}
			if cfg.LLM.MaxTokens == 0 {
				cfg.LLM.MaxTokens = defaults.LLM.MaxTokens
			}
			if cfg.LLM.Temperature == 0 {
				cfg.LLM.Temperature = defaults.LLM.Temperature
			}
		}

		// 2. 初始化 LLM 客户端（根据 provider 选择）
		var llmClient llm.LLMClient
		switch cfg.LLM.Provider {
		case "anthropic":
			llmClient = llm.NewAnthropicClient(llm.AnthropicConfig{
				APIKey:  cfg.LLM.APIKey,
				BaseURL: cfg.LLM.BaseURL,
				Model:   cfg.LLM.Model,
			})
		default:
			llmClient = llm.NewOpenAIClient(llm.OpenAIConfig{
				APIKey:  cfg.LLM.APIKey,
				BaseURL: cfg.LLM.BaseURL,
				Model:   cfg.LLM.Model,
			})
		}

		// 3. 初始化上下文管理器
		ctxManager, err := context.NewContextManager(context.ContextManagerConfig{
			Model:      cfg.LLM.Model,
			RootDir:    ".",
			MaxContext: llmClient.MaxContextTokens(),
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "初始化上下文失败:", err)
			os.Exit(1)
		}

		// 4. 确保项目 .gitignore 包含 .codebuddy/
		ensureGitignore(".")

		// 5. 注册内置工具
		registry := tool.NewRegistry()
		registry.Register(tool.NewReadFileTool("."))
		registry.Register(tool.NewWriteFileTool(".", nil))
		registry.Register(tool.NewEditFileTool(".", nil))
		registry.Register(tool.NewListDirTool("."))
		registry.Register(tool.NewSearchCodeTool("."))
		registry.Register(tool.NewRunCommandTool("."))

		// 6. 创建并启动
		a := agent.NewAgent(agent.AgentConfig{
			LLMClient:  llmClient,
			Registry:   registry,
			CtxManager: ctxManager,
			Config:     *cfg,
		})

		if err := tui.Run(a, *cfg); err != nil {
			fmt.Fprintln(os.Stderr, "TUI 错误:", err)
			os.Exit(1)
		}
	},
}

// initCmd init 子命令：交互式创建配置
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "初始化配置",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := runInit()
		if cfg.LLM.APIKey == "" {
			fmt.Fprintln(os.Stderr, "未配置 API Key")
			os.Exit(1)
		}
		fmt.Println("配置完成！运行 codebuddy 开始使用。")
		_ = cfg
	},
}

// configCmd config 子命令组
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "管理配置",
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "查看所有配置",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, "加载配置失败:", err)
			os.Exit(1)
		}
		fmt.Printf("Provider:    %s\n", cfg.LLM.Provider)
		fmt.Printf("Model:       %s\n", cfg.LLM.Model)
		fmt.Printf("BaseURL:     %s\n", cfg.LLM.BaseURL)
		fmt.Printf("MaxSteps:    %d\n", cfg.Agent.MaxSteps)
		fmt.Printf("Language:    %s\n", cfg.PrimaryLanguage)
	},
}

func init() {
	configCmd.AddCommand(configListCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(configCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// ================================================================
// 首次配置引导
// ================================================================

// runInit 交互式引导用户配置 API Key 和模型
func runInit() *config.Config {
	reader := bufio.NewReader(os.Stdin)

	// 选择 API 格式
	fmt.Println("选择 API 格式：")
	fmt.Println("  1. OpenAI 兼容（OpenAI / GLM / DeepSeek / Qwen 等均支持）")
	fmt.Println("  2. Anthropic 兼容（Claude / GLM 支持）")
	fmt.Print("请选择 [1-2]: ")

	var provider string
	choice := readLine(reader)
	switch strings.TrimSpace(choice) {
	case "2":
		provider = "anthropic"
	default:
		provider = "openai"
	}

	// API Base URL
	fmt.Print("API Base URL: ")
	baseURL := readLine(reader)

	// 模型名
	fmt.Print("模型名: ")
	model := readLine(reader)

	// API Key
	fmt.Print("API Key: ")
	apiKey := readLine(reader)

	// 构建 config
	cfg := config.DefaultConfig()
	cfg.LLM.Provider = provider
	cfg.LLM.Model = model
	cfg.LLM.APIKey = apiKey
	cfg.LLM.BaseURL = baseURL

	// 保存到 ~/.codebuddy/config.json
	saveConfig(&cfg)

	return &cfg
}

// readLine 读一行输入
func readLine(reader *bufio.Reader) string {
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

// saveConfig 保存配置到 ~/.codebuddy/config.json
func saveConfig(cfg *config.Config) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}

	configDir := filepath.Join(homeDir, ".codebuddy")
	os.MkdirAll(configDir, 0755)

	configPath := filepath.Join(configDir, "config.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "序列化配置失败: %v\n", err)
		return
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "保存配置失败: %v\n", err)
		return
	}

	fmt.Printf("配置已保存到 %s\n", configPath)
}

// ensureGitignore 确保 .gitignore 里包含指定条目
func ensureGitignore(projectDir string) {
	gitignorePath := filepath.Join(projectDir, ".gitignore")

	// 读取现有 .gitignore
	var content string
	data, err := os.ReadFile(gitignorePath)
	if err == nil {
		content = string(data)
	}

	entries := []string{".codebuddy/"}
	var added []string
	for _, entry := range entries {
		if !strings.Contains(content, entry) {
			content += entry + "\n"
			added = append(added, entry)
		}
	}

	if len(added) > 0 {
		os.WriteFile(gitignorePath, []byte(content), 0644)
		fmt.Printf("已添加 %v 到 .gitignore\n", added)
	}
}
