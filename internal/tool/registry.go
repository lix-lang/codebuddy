package tool

import (
	"fmt"
	"sort"

	"github.com/lix-lang/codebuddy/internal/llm"
)

// registry ToolRegistry 接口的具体实现
type registry struct {
	// tools 用 map 存储所有注册的工具
	// key 是工具名（如 "read_file"），value 是工具对象
	tools map[string]Tool
}

// NewRegistry 创建一个新的工具注册中心
func NewRegistry() ToolRegistry {
	return &registry{
		tools: make(map[string]Tool),
	}
}

// Register 注册一个工具
func (r *registry) Register(t Tool) {
	if _, exists := r.tools[t.Name()]; exists {
		return // 同名工具已存在，不覆盖
	}
	r.tools[t.Name()] = t
}

// Get 按名字查找工具
func (r *registry) Get(name string) (Tool, bool) {
	t, exists := r.tools[name]
	return t, exists
}

// List 返回所有已注册的工具
func (r *registry) List() []Tool {
	result := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	// 按名字排序，保证输出稳定
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name() < result[j].Name()
	})
	return result
}

// ListNames 返回所有已注册工具的名字列表
func (r *registry) ListNames() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ValidateParams 校验工具调用参数（Schema 校验 + 业务校验）
func (r *registry) ValidateParams(name string, args map[string]any) error {
	t, ok := r.tools[name]
	if !ok {
		return fmt.Errorf("工具 %s 不存在，可用工具: %v", name, r.ListNames())
	}
	// Schema 校验：检查必填参数是否存在
	params := t.Parameters()
	if props, ok := params["properties"].(map[string]any); ok {
		if required, ok := params["required"].([]string); ok {
			for _, req := range required {
				if _, exists := args[req]; !exists {
					return fmt.Errorf("缺少必填参数: %s", req)
				}
			}
		}
		// 类型检查
		for key, val := range args {
			if propDef, exists := props[key]; exists {
				if propMap, ok := propDef.(map[string]any); ok {
					if expectedType, ok := propMap["type"].(string); ok {
						if err := checkParamType(key, val, expectedType); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	// 业务校验：调用工具自己的 Validate
	return t.Validate(args)
}

// checkParamType 检查参数值类型是否匹配 Schema 定义
func checkParamType(key string, val any, expectedType string) error {
	switch expectedType {
	case "string":
		if _, ok := val.(string); !ok {
			return fmt.Errorf("参数 %s 必须是字符串", key)
		}
	case "integer":
		switch val.(type) {
		case int, int64, float64:
			// JSON 数字默认解析为 float64，整数也算匹配
		default:
			return fmt.Errorf("参数 %s 必须是整数", key)
		}
	case "number":
		switch val.(type) {
		case int, int64, float64:
		default:
			return fmt.Errorf("参数 %s 必须是数字", key)
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("参数 %s 必须是布尔值", key)
		}
	}
	return nil
}

// ToOpenAITools 返回所有可用工具的 OpenAI Function Calling 格式定义
// 只返回 IsAvailable() == true 的工具（不可用的不发 LLM，省 token）
func (r *registry) ToOpenAITools() []map[string]any {
	tools := r.List()
	defs := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		if !t.IsAvailable() {
			continue // 不可用的工具跳过
		}
		defs = append(defs, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name(),
				"description": t.Description(),
				"parameters":  t.Parameters(),
			},
		})
	}
	return defs
}

// GetToolDefinitions 把所有可用工具转成 llm.ToolDefinition 格式
// Agent 调用 ChatStream 时，需要把工具定义传给 LLM
func GetToolDefinitions(r ToolRegistry) []llm.ToolDefinition {
	tools := r.List()
	defs := make([]llm.ToolDefinition, 0, len(tools))
	for _, t := range tools {
		if !t.IsAvailable() {
			continue
		}
		defs = append(defs, llm.ToolDefinition{
			Type: "function",
			Function: llm.ToolFunction{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			},
		})
	}
	return defs
}
