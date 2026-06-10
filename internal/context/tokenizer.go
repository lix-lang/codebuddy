// Package context 实现 Token 计数和上下文管理。
//
// 本文件（tokenizer.go）实现模型分词器，每个模型用自己原生的分词器计算 token：
//   - tiktokenCounter: OpenAI 系列（GPT-4、GPT-3.5-turbo）— tiktoken 原生支持，精确
//   - qwenCounter:     通义千问（Qwen）系列 — qwen-tokenizer 库，词表内嵌
//   - hfCounter:       GLM、DeepSeek — 从 HuggingFace 下载 tokenizer.json，用 sugarme 加载
//   - GetTokenCounter: 根据模型名自动选择分词器（工厂模式 + 缓存）
//
// # 为什么不用校正系数？
//
// 之前用 tiktoken（OpenAI 的分词器）+ 校正系数来估算其他模型的 token 数，
// 但不同模型的词表完全不同（GPT ~100K 词表，GLM ~150K，Qwen ~152K），
// 乘个系数不够准确。正确做法是每个模型用自己的分词器。
//
// # 分词器缓存
//
// 分词器初始化有开销（加载词表），所以用 sync.RWMutex + map 做缓存：
//   - 同一类型的分词器只创建一次
//   - 读多写少，用 RWMutex 读锁不加写锁，性能好
//   - 双检锁防止并发创建
package context

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	tok "github.com/CharLemAznable/qwen-tokenizer"
	_ "github.com/CharLemAznable/qwen-tokenizer/tiktoken" // 触发 init()，注册内嵌的 Qwen 词表
	"github.com/pkoukk/tiktoken-go"
	"github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/pretrained"
)

// TokenCounter 分词器接口
// 不同模型用不同的分词实现，但对外统一暴露 CountTokens 方法
// 这样调用方不需要关心底层用的是 tiktoken 还是 HuggingFace
type TokenCounter interface {
	// CountTokens 计算一段文本的 token 数
	CountTokens(text string) int
}

// ================================================================
// tiktoken 分词器 —— OpenAI 系列模型
// ================================================================

// tiktokenCounter 使用 OpenAI 开源的 tiktoken BPE 分词器
// 适用于 GPT-4、GPT-3.5-turbo 等模型
// tiktoken 是这些模型的原生分词器，计算结果完全精确
type tiktokenCounter struct {
	enc *tiktoken.Tiktoken // tiktoken 编码器实例
}

// newTiktokenCounter 创建 tiktoken 分词器
// model 用于选择对应的编码方式：
//   - "gpt-4o" → o200k_base
//   - "gpt-4" → cl100k_base
//   - "gpt-3.5-turbo" → cl100k_base
//
// 如果模型名不认识，回退到 cl100k_base
// 如果 tiktoken panic（Go RE2 不支持 Perl 正则），返回 nil
func newTiktokenCounter(model string) *tiktokenCounter {
	enc := func() (result *tiktoken.Tiktoken) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[TOKENIZER] tiktoken panic for model %q: %v", model, r)
				result = nil
			}
		}()
		// tiktoken.EncodingForModel 根据模型名选择对应的编码器
		e, err := tiktoken.EncodingForModel(model)
		if err != nil {
			// 模型名不认识，回退到 cl100k_base（GPT-4 的编码，大多数模型也接近）
			e, _ = tiktoken.GetEncoding("cl100k_base")
		}
		return e
	}()
	if enc == nil {
		return nil
	}
	return &tiktokenCounter{enc: enc}
}

// CountTokens 用 tiktoken 计算文本的 token 数
// enc.Encode 把文本拆成 token ID 列表，len 就是 token 数量
func (t *tiktokenCounter) CountTokens(text string) int {
	return len(t.enc.Encode(text, nil, nil))
}

// ================================================================
// 字符估算分词器 —— 最终回退
// ================================================================

// charEstimateCounter 用字符数 / 3 估算 token 数
// 在 tiktoken panic（Go RE2 不支持 Perl 正则）时使用
type charEstimateCounter struct{}

func (c *charEstimateCounter) CountTokens(text string) int {
	return len(text) / 3
}

// ================================================================
// Qwen 分词器 —— 通义千问系列
// ================================================================

// qwenCounter 使用 qwen-tokenizer 库
// 这个库把 Qwen 的词表直接内嵌在 Go 代码里（~1.5MB），
// 不需要下载外部文件，import 就能用
type qwenCounter struct {
	tz *tok.Tokenizer // Qwen 分词器实例，零值即可用（词表在 init() 时加载）
}

// newQwenCounter 创建 Qwen 分词器
// 词表已在包的 init() 中注册，直接创建零值结构体就行
func newQwenCounter() *qwenCounter {
	return &qwenCounter{tz: &tok.Tokenizer{}}
}

// CountTokens 用 Qwen 分词器计算 token 数
// tz.Encode 返回 token ID 列表，len 就是数量
func (q *qwenCounter) CountTokens(text string) int {
	return len(q.tz.Encode(text))
}

// ================================================================
// HuggingFace 分词器 —— GLM、DeepSeek
// ================================================================

// hfCounter 使用 sugarme/tokenizer 加载 HuggingFace 格式的 tokenizer.json
// GLM 和 DeepSeek 没有专门的 Go 分词器库，
// 但它们的分词器文件（tokenizer.json）是标准的 HuggingFace 格式，
// 可以用 sugarme/tokenizer 这个通用库加载
//
// 首次使用时从 HuggingFace 下载 tokenizer.json（~1-3MB），缓存到本地：
//
//	~/.codebuddy/tokenizers/hf-glm.json
//	~/.codebuddy/tokenizers/hf-deepseek.json
type hfCounter struct {
	tz *tokenizer.Tokenizer // sugarme 分词器实例
}

// hfModelConfig HuggingFace 模型下载配置
type hfModelConfig struct {
	ModelName string // HuggingFace 上的模型路径，如 "zai-org/glm-4-9b-chat-hf"
	FileName  string // 分词器文件名，通常是 "tokenizer.json"
}

// hfModelMap 分词器类型 → HuggingFace 模型配置
// key 是分词器类型（如 "hf-glm"），value 是从哪个 HuggingFace 模型下载 tokenizer.json
var hfModelMap = map[string]hfModelConfig{
	// GLM：用 HuggingFace 兼容版的 GLM-4 模型
	"hf-glm": {
		ModelName: "zai-org/glm-4-9b-chat-hf",
		FileName:  "tokenizer.json",
	},
	// DeepSeek：用 DeepSeek-Coder 的分词器（DeepSeek 各系列分词器通用）
	"hf-deepseek": {
		ModelName: "deepseek-ai/deepseek-coder-6.7b-instruct",
		FileName:  "tokenizer.json",
	},
}

// newHFCounter 创建 HuggingFace 分词器
// 会自动下载 tokenizer.json 并缓存，如果下载失败则回退到 tiktoken
func newHFCounter(tokenizerType string) TokenCounter {
	config, ok := hfModelMap[tokenizerType]
	if !ok {
		// 没有对应的 HuggingFace 配置，回退到 tiktoken
		log.Printf("[TOKENIZER] no HF config for %q, falling back to tiktoken", tokenizerType)
		tc := newTiktokenCounter("gpt-4")
		if tc != nil {
			return tc
		}
		return nil
	}

	// 缓存路径: ~/.codebuddy/tokenizers/hf-glm.json
	homeDir, _ := os.UserHomeDir()
	cacheDir := filepath.Join(homeDir, ".codebuddy", "tokenizers")
	cachePath := filepath.Join(cacheDir, tokenizerType+".json")

	// 如果本地没有缓存，从 HuggingFace 下载
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		log.Printf("[TOKENIZER] no cache at %q, downloading from HuggingFace", cachePath)
		if err := downloadHFTokenizer(config, cacheDir, cachePath); err != nil {
			// 下载失败（没网络等），回退到 tiktoken
			log.Printf("[TOKENIZER] download failed: %v, falling back to tiktoken", err)
			tc := newTiktokenCounter("gpt-4")
			if tc != nil {
				return tc
			}
			return nil
		}
	}

	// 用 sugarme/tokenizer 加载 tokenizer.json
	// 注意：sugarme 内部用 Go 标准 regexp 编译 tokenizer.json 中的正则，
	// GLM 的 tokenizer.json 包含 (?!\S) 负向前瞻（Perl 语法，Go RE2 不支持），
	// 会导致 panic，所以必须用 recover 兜住
	tz, err := func() (*tokenizer.Tokenizer, error) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[TOKENIZER] pretrained.FromFile panic for %q: %v", cachePath, r)
			}
		}()
		return pretrained.FromFile(cachePath)
	}()
	if err != nil || tz == nil {
		log.Printf("[TOKENIZER] HF load %q failed (err=%v), falling back to tiktoken", cachePath, err)
		tc := newTiktokenCounter("gpt-4")
		if tc != nil {
			return tc
		}
		return nil
	}

	log.Printf("[TOKENIZER] HF tokenizer loaded: %q", cachePath)
	return &hfCounter{tz: tz}
}

// CountTokens 用 HuggingFace 分词器计算 token 数
func (h *hfCounter) CountTokens(text string) int {
	// recover 防止 sugarme 内部正则编译 panic（如负向前瞻等 Perl 语法）
	result := func() int {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[TOKENIZER] hfCounter.CountTokens panic: %v", r)
			}
		}()
		enc, err := h.tz.EncodeSingle(text)
		if err != nil {
			return 0
		}
		return enc.Len()
	}()
	if result > 0 {
		return result
	}
	return len(text) / 3 // 编码失败，粗略估算
}

// downloadHFTokenizer 从 HuggingFace 下载 tokenizer.json 到本地缓存
// HuggingFace 的文件下载 URL 格式：https://huggingface.co/{model}/resolve/main/{filename}
func downloadHFTokenizer(config hfModelConfig, cacheDir, cachePath string) error {
	// os.MkdirAll 递归创建目录，已存在不报错
	os.MkdirAll(cacheDir, os.ModePerm)

	// 拼接下载地址
	url := fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", config.ModelName, config.FileName)

	// http.Get 发送 GET 请求下载文件
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("网络请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HuggingFace 返回 HTTP %d", resp.StatusCode)
	}

	// io.ReadAll 读取整个响应体到内存
	// tokenizer.json 通常 1-3MB，不大，一次性读取没问题
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	// os.WriteFile 写入缓存文件
	// 0644 = 用户读写，其他用户只读
	return os.WriteFile(cachePath, data, 0644)
}

// ================================================================
// 分词器工厂（工厂模式 + 缓存）
// ================================================================

var (
	// counterCache 已创建的分词器缓存，按类型缓存
	// 比如 "tiktoken" 类型只创建一个实例，所有 GPT 模型共用
	counterCache = make(map[string]TokenCounter)
	// counterMu 保护 counterCache 的并发读写
	counterMu sync.RWMutex
)

// modelTokenizerType 模型名前缀 → 分词器类型
// 通过前缀匹配模型名，决定用哪种分词器
// 比如 "glm-4-flash" 匹配 "glm-" 前缀 → 用 "hf-glm" 分词器
var modelTokenizerType = map[string]string{
	// OpenAI 系列 → tiktoken（OpenAI 原生分词器，完全精确）
	"gpt-4o":        "tiktoken",
	"gpt-4-turbo":   "tiktoken",
	"gpt-4-":        "tiktoken",
	"gpt-4":         "tiktoken",
	"gpt-3.5-turbo": "tiktoken",

	// Qwen 系列 → qwen-tokenizer（专用库，精确）
	"qwen": "qwen",

	// GLM 系列 → 从 HuggingFace 下载 tokenizer.json
	"glm-":    "hf-glm",
	"chatglm": "hf-glm",

	// DeepSeek 系列 → 从 HuggingFace 下载 tokenizer.json
	"deepseek-": "hf-deepseek",

	// Claude 系列 → tiktoken 近似
	// Anthropic 没有公开本地分词器，只有 API 端点可以远程计数
	// tiktoken cl100k_base 跟 Claude 误差约 5%，是目前最好的本地选择
	"claude-": "tiktoken",

	// Ollama 本地模型 → tiktoken 近似
	"llama":   "tiktoken",
	"mistral": "tiktoken",
}

// GetTokenCounter 获取模型对应的分词器（带缓存）
// model 是模型名，如 "glm-4"、"qwen-plus"、"gpt-4o"
//
// 内部流程：
//  1. 根据模型名匹配分词器类型（如 "hf-glm"）
//  2. 查缓存，命中直接返回
//  3. 未命中，创建新分词器，存入缓存
//  4. 使用双检锁（double-checked locking）防止并发重复创建
func GetTokenCounter(model string) TokenCounter {
	tzType := getTokenizerType(model)

	// 先用读锁查缓存（读多写少，读锁性能更好）
	counterMu.RLock()
	c, ok := counterCache[tzType]
	counterMu.RUnlock()
	if ok {
		return c
	}

	// 缓存未命中，加写锁创建
	counterMu.Lock()
	defer counterMu.Unlock()

	// 双检锁：再次检查，防止多个 goroutine 同时进入创建逻辑
	if c, ok := counterCache[tzType]; ok {
		return c
	}

	// 根据类型创建对应的分词器
	var counter TokenCounter
	switch {
	case tzType == "qwen":
		counter = newQwenCounter()
	case strings.HasPrefix(tzType, "hf-"):
		counter = newHFCounter(tzType)
		if counter == nil {
			// HF 分词器创建失败，回退到 tiktoken
			counter = newTiktokenCounter(model)
		}
	default:
		counter = newTiktokenCounter(model)
	}

	// tiktoken 也 panic 了（Go RE2 不支持 Perl 正则），用字符估算
	if counter == nil {
		log.Printf("[TOKENIZER] all tokenizers failed for %q, using char estimator", model)
		counter = &charEstimateCounter{}
	}

	// 存入缓存，下次直接命中
	counterCache[tzType] = counter
	return counter
}

// getTokenizerType 根据模型名匹配分词器类型
// 比如 "glm-4-flash" → "hf-glm"，"qwen-plus" → "qwen"
func getTokenizerType(model string) string {
	lower := strings.ToLower(model)
	for prefix, tzType := range modelTokenizerType {
		if strings.HasPrefix(lower, prefix) {
			return tzType
		}
	}
	return "tiktoken" // 默认用 tiktoken
}
