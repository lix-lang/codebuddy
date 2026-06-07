// Package search 实现多引擎 Web 搜索能力。
//
// 三层搜索架构：
//  1. 智能路由：判断是否需要搜索（时效性问题搜索，代码问题优先用本地索引）
//  2. 搜索 + 摘要：多引擎查询，结果压缩为摘要（每条 150 token）
//  3. 精准注入：只把摘要注入上下文，用户/LLM 需要详情时再 web_fetch 全文
//
// 支持引擎：DuckDuckGo（免费，默认）、Google（API）、Bing（API）、SearXNG（自建）。
// 缓存机制：相同查询不重复搜索，TTL 默认 1 小时。
package search
