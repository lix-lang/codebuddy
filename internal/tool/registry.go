package tool

import (
	"github.com/lix-lang/codebuddy/internal/llm"
)

// registry ToolRegistry 接口的具体实现
type registry struct {
	// tools 用 map 存储所有注册的工具
	// key 是工具名（如 "read_file"），value 是工具对象
	// map 查找是 O(1) 复杂度，比遍历切片快
	tools map[string]Tool
}

// NewRegistry 创建一个新的工具注册中心
// 返回 ToolRegistry 接口类型，调用方只依赖接口，不依赖具体实现
func NewRegistry() ToolRegistry {
	// make(map[string]Tool) 创建一个空的 map，必须用 make 初始化，否则是 nil
	return &registry{
		tools: make(map[string]Tool),
	}
}

// Register 注册一个工具
// 实现 ToolRegistry 接口的方法
func (r *registry) Register(t Tool) {
	// map 查找的特殊语法：val, exists := m[key]
	// val 是值，exists 是 bool（true = 存在，false = 不存在）
	// 这里只关心是否存在，用 _ 忽略值
	if _, exists := r.tools[t.Name()]; exists {
		// 同名工具已存在，不覆盖，直接返回
		return
	}
	// t.Name() 调用工具的 Name() 方法获取工具名
	// 不管 t 是 read_file 还是 search_code，都能调用 Name()
	// 这就是接口的威力：调用方不需要知道具体类型
	r.tools[t.Name()] = t
}

// Get 按名字查找工具
// 实现 ToolRegistry 接口的方法
// 返回 (工具对象, 是否找到)
func (r *registry) Get(name string) (Tool, bool) {
	// 从 map 里按 key 查找
	t, exists := r.tools[name]
	return t, exists
}

// List 返回所有已注册的工具
// 实现 ToolRegistry 接口的方法
func (r *registry) List() []Tool {
	// make([]Tool, 0, len(r.tools)) 创建切片
	// 第一个参数 0 是初始长度（空的）
	// 第二个参数 len(r.tools) 是容量（预分配空间，避免多次扩容）
	result := make([]Tool, 0, len(r.tools))
	// for range 遍历 map，每次取出 key 和 value
	// 这里只需要 value（工具对象），用 _ 忽略 key
	for _, t := range r.tools {
		// append 往切片末尾追加一个元素
		result = append(result, t)
	}
	return result
}

// GetToolDefinitions 把所有工具转成 LLM 能理解的 ToolDefinition 格式
// Agent 调用 ChatStream 时，需要把工具定义传给 LLM
// 这是个包级函数（不属于 registry），因为任何 ToolRegistry 实现都能用
func GetToolDefinitions(r ToolRegistry) []llm.ToolDefinition {
	tools := r.List()
	// 预分配切片容量，性能优化
	defs := make([]llm.ToolDefinition, 0, len(tools))
	for _, t := range tools {
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
