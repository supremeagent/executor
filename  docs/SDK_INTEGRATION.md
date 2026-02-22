# Executor SDK 接入指南

本文档旨在帮助开发者快速接入 Executor 系统的 HTTP API。Executor 是一个统一调度 AI 智能体（如 Claude Code、Codex 等）执行任务的服务端应用，支持流式日志输出、会话恢复、人工审批等特性。

除作为独立服务通过 HTTP 提供 API 外，本系统也可作为 Golang 依赖库直接集成到其他的 Golang 项目中（详细请参考第 5 节：作为 Golang Library 使用）。

---

## 1. 核心概念

- **Session（会话）**：每次发起执行任务都会生成一个全局唯一的 `session_id`。所有的日志、状态和后续对话都基于此 ID 进行。
- **SSE (Server-Sent Events) 流式推送**：任务开始后，系统会通过 SSE 接口将 AI 思考、工具调用、结果输出等步骤实时流式推送到客户端。
- **统一消息模型 (Unified Event)**：不同的 AI 底层输出格式各异，Executor 在服务端会将其统一转换为标准化的 `Event` 和 `UnifiedContent` 结构，方便客户端渲染。

---

## 2. API 接口概览

服务器默认基于标准 HTTP 协议提供服务。以下为核心对接流程所需的 API：

| 接口说明 | HTTP 方法 | 路径 |
| --- | --- | --- |
| 发起执行任务 | `POST` | `/api/execute` |
| 获取任务流式日志 | `GET` | `/api/execute/{session_id}/stream` |
| 继续对话/补充提示 | `POST` | `/api/execute/{session_id}/continue` |
| 中断运行中的任务 | `POST` | `/api/execute/{session_id}/interrupt` |
| 发送授权/审批决策 | `POST` | `/api/execute/{session_id}/control` |

---

## 3. 核心流程与数据结构

### 3.1 发起任务 (`POST /api/execute`)

提交一个需要 AI 解决的任务 Prompt，启动一个新的会话。

**请求体 (JSON):**

```json
{
  "prompt": "帮我写一个 Hello World 脚本",
  "executor": "claude_code",
  "working_dir": "/path/to/workspace",
  "model": "claude-3-7-sonnet-20250219",
  "plan": false,
  "sandbox": "",
  "env": {
    "CUSTOM_VAR": "value"
  },
  "ask_for_approval": "never"
}
```

*说明：*
- `prompt`：(必填) 提供给 AI 的指令。
- `executor`：(必填) 执行器类型，主要包含 `"claude_code"` 或 `"codex"`。
- `working_dir`：AI 执行任务的工作目录绝对路径。
- `ask_for_approval`：是否需要人工审批。通常为 `"never"` 等配置。

**响应体 (JSON):**

```json
{
  "session_id": "8b9cad0e-72a2-4b28-8081-1f2031c5dae3",
  "status": "running"
}
```

### 3.2 接收流式消息 (`GET /api/execute/{session_id}/stream`)

发起任务后，客户端需立即连接此接口以接收 SSE 事件流。支持的 Query 参数：
- `?return_all=true`：若在任务执行中途断开重连，带上此参数可获取从第一条开始的历史完整事件。
- `?debug=true`：是否包含底层的 debug 级别事件。

**SSE 数据格式：**

```text
event: <Event Type>
data: <JSON Event Object>

event: ...
```

#### 📌 核心流消息结构详解 (Event Object)

每次 SSE 推送的 `data` 都是一个统一的 JSON 对象，结构如下：

```json
{
  "session_id": "8b9cad0e...",
  "executor": "claude_code",
  "seq": 1,
  "timestamp": "2023-10-01T12:00:00Z",
  "type": "progress",
  "content": {
    // 统一内容详情 (UnifiedContent)
  }
}
```

**外层字段说明：**
- `type`：顶层事件类型，这是**前端路由渲染最关键的字段**。主要取值包含：
  - `"message"`：常规的文本消息回复，比如 AI 的问候或者总结发言。
  - `"progress"`：过程性状态变化（比如“正在思考”、“正在启动系统”等）。
  - `"tool"`：工具相关事件（开始调用工具、读取文件、执行 Bash 等）。
  - `"approval"`：遇到需要人工审批的高危操作（如执行敏感命令）。
  - `"error"`：发生了运行错误或中断。
  - `"done"`：当前会话/任务执行彻底结束的标志。

**内层 `content` 核心结构 (UnifiedContent)：**

无论底层的 AI 吐出什么奇怪的格式，Executor 都会将其封装为如下的统一字段，对接方只需关注此对象：

```json
{
  "category": "tool",
  "action": "reading",
  "phase": "started",
  "summary": "正在读取 handler.go",
  "text": "读取的文件内容或 AI 输出内容...",
  "tool_name": "ReadTool",
  "target": "handler.go",
  "request_id": "req_12345",
  "raw": {} 
}
```

**`content` 各个业务字段详解：**

1. **`category` (分类):** 进一步细分任务类别。如 `"message"`, `"tool"`, `"progress"`, `"done"`, `"error"`, `"approval"`, `"lifecycle"`。
2. **`action` (具体动作):** 当前正在干什么。
    - 常见枚举：`"thinking"`(思考), `"reading"`(读文件), `"searching"`(搜索), `"editing"`(编辑修改), `"tool_running"`(执行其他工具), `"responding"`(响应文本), `"completed"`(完成), `"failed"`(失败), `"approval_required"`(等待审批)。
3. **`phase` (阶段):** 标识当前动作处于什么阶段。
    - 常见枚举：`"started"`(开始执行), `"completed"`(完成), `"requested"`(请求中), `"failed"`(失败)。
4. **`summary` (摘要):** 服务端已经为您生成好的、可直接展示给用户的**简要中文描述**（例如：“正在查询 API 文档”、“正在深度思考”等），非常适合作为 UI 上的进度条或副标题。
5. **`text` (主体文本):** 如果有大段需要展示的 markdown 文本、报错详细信息、或是 AI 说的具体话语，都在这个字段里。
6. **`tool_name` (工具名) & `target` (目标):** 当使用了工具时，`tool_name` 可能是 `Bash`, `ViewFile`，而 `target` 一般指的是相关的文件名、搜索的关键词等（方便做 UI 卡片上的重点高亮）。
7. **`request_id` (审批请求 ID):** **极其重要**！当 `type` 为 `"approval"` 时，必须提取此字段，用于后续的 `/control` 接口提交用户的审批决定。
8. **`raw`:** 原始的底层 AI 节点数据（调试和高级自定义需求使用）。

### 3.3 人工审批 (`POST /api/execute/{session_id}/control`)

如果在流中收到了 `type: "approval"` 的事件，意味着 AI 卡住了，正在等待用户的授权。客户端应弹出提示框，让用户选择是否同意，然后调用此接口：

**请求体 (JSON):**

```json
{
  "request_id": "req_123456", 
  "decision": "approve",       
  "reason": ""                 
}
```
*说明：* 
- `request_id` 来源于上文 SSE 流中 `content.request_id`。
- `decision` 只能是 `"approve"`(同意) 或 `"deny"`(拒绝)。
- 如果拒绝，可以在 `reason` 中告诉 AI 为什么拒绝（比如“不要删除这个文件”）。

### 3.4 追加对话或继续执行 (`POST /api/execute/{session_id}/continue`)

当会话中断、出现错误需要人工纠正，或者 `done` 之后用户想提出进一步修改意见时（例如：“帮我把刚才页面的主色调换成蓝色”）：

**请求体 (JSON):**

```json
{
  "message": "帮我把刚才页面的主色调换成蓝色"
}
```
*备注：调用此接口后，原本连着的 `/stream` 接口会继续源源不断地吐出新的事件。*

### 3.5 中断任务 (`POST /api/execute/{session_id}/interrupt`)

客户端点击“停止执行”按钮时调用。
调用后服务端会强行杀死底层的 AI 进程，相关的 `/stream` 会收到最终的一个 `error` 或 `done` 事件即可关闭。

---

## 4. 最佳实践建议

1. **界面渲染逻辑：**
   - 监听 SSE 流的过程中，利用 `content.summary` 作为流水的标题。
   - 当遇到 `category: "tool"` 且 `phase: "started"` 可以展示加载圈，在收到 `phase: "completed"` 对应 `tool_name` 相同时打上绿色的勾。
   - 大段文本直接读取 `content.text`，并使用 Markdown 渲染。
2. **断线重连体验：**
   - 如果网络断开，重新访问 `/stream?return_all=true` 会把当前会话的所有历史重新快速发一遍，前端应当根据 `seq` 字段做简单的去重和回放覆盖。

---

## 5. 作为 Golang Library 使用

除了通过 API Server 调用，你还可以直接将此项目作为普通的 Go 模块引入到你自己的工程中。你需要直接使用 SDK 包 `github.com/supremeagent/executor/pkg/sdk`。

### 5.1 初始化客户端

你可以使用默认配置初始化 SDK Client，这样会自动加载内置的执行器工厂以及内存事件存储和流管理器：

```go
package main

import (
	"context"
	"fmt"
	"github.com/supremeagent/executor/pkg/sdk"
	"github.com/supremeagent/executor/pkg/executor"
)

func main() {
	// 初始化 SDK Client
	client := sdk.New()
	defer client.Shutdown()
    
	// 详见后文的使用
}
```

或者使用自定义组件初始化，例如需要对接你自己的持久化数据库或者注册钩子函数时：

```go
client := sdk.NewWithOptions(sdk.ClientOptions{
	Registry:      myCustomRegistry,
	StreamManager: myCustomStreamManager,
	EventStore:    myPersistentStore,
	Hooks: executor.Hooks{
		OnSessionStart: func(ctx context.Context, sessionID string, req executor.ExecuteRequest) {
			fmt.Println("Session Started:", sessionID)
		},
		OnSessionEnd: func(ctx context.Context, sessionID string) {
			fmt.Println("Session Ended:", sessionID)
		},
	},
})
```

### 5.2 发起与流式监听任务

你需要提供完整的 `context`，并通过 SDK 提供的订阅机制捕获所有 AI 执行时吐出的结构化数据。

```go
func runTask(client *sdk.Client) {
	ctx := context.Background()
    
	// 1. 发起任务执行请求
	resp, err := client.Execute(ctx, executor.ExecuteRequest{
		Prompt:     "帮我写一个 Hello World 脚本",
		Executor:   executor.ExecutorClaudeCode, // "claude_code" 或 "codex"
		WorkingDir: "/tmp/my-project",
	})
	if err != nil {
		panic(err)
	}

	sessionID := resp.SessionID
	fmt.Printf("Started Agent session: %s\n", sessionID)

	// 2. 及时订阅事件流通道
	events, unsubscribe := client.Subscribe(sessionID, executor.SubscribeOptions{
		ReturnAll:    true,  // 顺带拉取在订阅前可能已经产生的历史事件
		IncludeDebug: false,
	})
	defer unsubscribe()

	// 3. 消费输出事件
	for evt := range events {
		// 在这里您可以解析 Event 结构，打印相应的 type 和 content 等。
		fmt.Printf("[Event:%s] %#v\n", evt.Type, evt.Content)

		// 代表整个任务已彻底完成
		if evt.Type == "done" {
			fmt.Println("Agent task completed successfully!")
			break
		}
	}
}
```

### 5.3 会话控制（中断与恢复）

在代码中你也可以轻松调用相应的方法控制当前会话的进度，或是直接发送继续执行的消息，完全抛弃 HTTP Server 的束缚。

```go
// 中断执行
err := client.PauseTask(sessionID)

// 发送继续的提示词给 AI
err := client.ContinueTask(context.Background(), sessionID, "刚才的颜色不够亮，麻烦换一个")

// 回应 AI 本地工具请求的人工审批
err := client.RespondControl(context.Background(), sessionID, executor.ControlResponse{
	RequestID: "req_xyz123",
	Decision:  executor.ControlDecisionApprove,
})
```

### 5.4 查看历史与会话管理

如果你需要在本地缓存、展示所有的历史对话，或是查看当前有哪些正在运行的 Agent 会话，可以使用以下提供的方法查询：

```go
// 1. 获取所有的会话列表（按最新更新时间倒序排列）
sessions := client.ListSessions(context.Background())
for _, s := range sessions {
	fmt.Printf("Session %s [%s]: %s\n", s.SessionID, s.Status, s.Title)
}

// 2. 检查某个会话是否仍在运行中
isRunning := client.SessionRunning(sessionID)

// 3. 获取某个会话所有已经产生的历史 Event 记录
events, ok := client.GetSessionEvents(sessionID)
if ok {
	fmt.Printf("共找到 %d 条历史事件\n", len(events))
}

// 4. 分页或从特定序列号开始获取部分历史记录
partialEvents, err := client.ListEvents(context.Background(), sessionID, 10 /* afterSeq */, 50 /* limit */)
```

至此，通过这套 SDK API，你不仅能够快速驱动起强大的 AI 执行能力，还能够完全将所有的中间过程无缝嵌入到自己的产品 UI 之中！
