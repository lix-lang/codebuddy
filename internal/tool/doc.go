// Package tool 定义工具接口和所有内置工具的实现。
//
// 工具是 Agent 的"手"——LLM 本身只能生成文字，但通过工具可以：
//   - 读写文件（read_file, write_file, edit_file）
//   - 执行命令（run_command, go_test）
//   - 搜索代码（search_code）
//   - 分析代码结构（analyze — 按文件扩展名自动选择分析器，Go 用 go/ast）
//   - 识别图片（read_image）
//
// 每个工具实现 Tool 接口，提供：
//   - Name/Description/Parameters → 告诉 LLM "我是谁、怎么用我"
//   - Validate → 校验参数（路径合法吗？文件存在吗？）
//   - Execute → 真正执行操作
//   - IsDestructive → 标记是否修改文件系统（需要用户确认）
package tool
