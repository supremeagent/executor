# Claude Code 和 Codex API 调用指南

本文档详细介绍了 Vibe Kanban 项目中如何通过 API 调用 Claude Code 和 Codex 执行任务并获取执行结果。

## 目录

1. [架构概述](#架构概述)
2. [Claude Code 执行器](#claude-code-执行器)
3. [Codex 执行器](#codex-执行器)
4. [任务执行流程](#任务执行流程)
5. [MCP 工具接口](#mcp-工具接口)
6. [日志流与结果获取](#日志流与结果获取)
7. [配置与变体](#配置与变体)
8. [API 端点参考](#api-端点参考)

---

## 架构概述

Vibe Kanban 采用执行器（Executor）模式来统一管理不同的 AI 编码代理。核心组件包括：

```
┌─────────────────────────────────────────────────────────────────┐
│                         API Layer                                │
│  ┌───────────────┐  ┌───────────────┐  ┌───────────────────┐   │
│  │  /api/tasks   │  │ /api/sessions │  │ /api/execution-   │   │
│  │               │  │               │  │    processes      │   │
│  └───────┬───────┘  └───────┬───────┘  └─────────┬─────────┘   │
└──────────┼──────────────────┼────────────────────┼─────────────┘
           │                  │                    │
           ▼                  ▼                    ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Container Service                             │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │  start_workspace() / start_execution() / stream_logs()     ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
           │
           ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Executor Layer                                │
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐   │
│  │ ClaudeCode │ │   Codex    │ │   Gemini   │ │   Amp      │   │
│  └────────────┘ └────────────┘ └────────────┘ └────────────┘   │
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐   │
│  │  Opencode  │ │ QwenCode   │ │ CursorAgent│ │  Copilot   │   │
│  └────────────┘ └────────────┘ └────────────┘ └────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

### 核心模块

| 模块路径 | 描述 |
|---------|------|
| `crates/executors/src/executors/` | 所有执行器实现 |
| `crates/executors/src/executors/claude/` | Claude Code 执行器 |
| `crates/executors/src/executors/codex/` | Codex 执行器 |
| `crates/executors/src/actions/` | 执行动作定义 |
| `crates/executors/src/profile.rs` | 执行器配置管理 |
| `crates/server/src/mcp/task_server.rs` | MCP 工具服务器 |

---

## Claude Code 执行器

Claude Code 执行器通过 **SDK Control Protocol** 与 Claude Code CLI 进行双向通信。

### 基本命令

```rust
// crates/executors/src/executors/claude.rs
pub fn base_command() -> &'static str {
    "npx -y @anthropic-ai/claude-code@latest"
}
```

### 执行流程

1. **启动进程**：生成 Claude Code CLI 子进程，使用 `--stdio` 模式
2. **初始化协议**：发送 `Initialize` 控制请求建立 SDK 连接
3. **发送消息**：通过 `send_user_message()` 发送用户提示
4. **处理请求**：监听并响应 `CanUseTool` 和 `HookCallback` 请求
5. **流式输出**：实时接收 stdout 中的执行日志

### 协议类型定义

```rust
// crates/executors/src/executors/claude/types.rs

/// CLI 发出的控制请求类型
pub enum ControlRequestType {
    CanUseTool {
        tool_name: String,
        input: Value,
        permission_suggestions: Option<Vec<PermissionUpdate>>,
        blocked_paths: Option<String>,
        tool_use_id: Option<String>,
    },
    HookCallback {
        callback_id: String,
        input: Value,
        tool_use_id: Option<String>,
    },
}

/// 权限检查结果
pub enum PermissionResult {
    Allow {
        updated_input: Value,
        updated_permissions: Option<Vec<PermissionUpdate>>,
    },
    Deny {
        message: String,
        interrupt: Option<bool>,
    },
}

/// SDK 发出的控制请求类型
pub enum SDKControlRequestType {
    SetPermissionMode { mode: PermissionMode },
    Initialize { hooks: Option<Value> },
    Interrupt {},
}

/// 权限模式
pub enum PermissionMode {
    Default,
    AcceptEdits,
    Plan,
    BypassPermissions,
}
```

### ProtocolPeer 实现

```rust
// crates/executors/src/executors/claude/protocol.rs

pub struct ProtocolPeer {
    stdin: Arc<Mutex<ChildStdin>>,
}

impl ProtocolPeer {
    /// 发送用户消息
    pub async fn send_user_message(&self, content: String) -> Result<(), ExecutorError> {
        let message = Message::new_user(content);
        self.send_json(&message).await
    }

    /// 初始化 SDK 连接
    pub async fn initialize(&self, hooks: Option<serde_json::Value>) -> Result<(), ExecutorError> {
        self.send_json(&SDKControlRequest::new(SDKControlRequestType::Initialize {
            hooks,
        }))
        .await
    }

    /// 中断当前执行
    pub async fn interrupt(&self) -> Result<(), ExecutorError> {
        self.send_json(&SDKControlRequest::new(SDKControlRequestType::Interrupt {}))
            .await
    }

    /// 设置权限模式
    pub async fn set_permission_mode(&self, mode: PermissionMode) -> Result<(), ExecutorError> {
        self.send_json(&SDKControlRequest::new(
            SDKControlRequestType::SetPermissionMode { mode },
        ))
        .await
    }
}
```

### ClaudeAgentClient 实现

```rust
// crates/executors/src/executors/claude/client.rs

pub struct ClaudeAgentClient {
    log_writer: LogWriter,
    approvals: Option<Arc<dyn ExecutorApprovalService>>,
    auto_approve: bool,
    repo_context: RepoContext,
    commit_reminder_prompt: String,
    cancel: CancellationToken,
}

impl ClaudeAgentClient {
    /// 处理工具使用权限请求
    pub async fn on_can_use_tool(
        &self,
        tool_name: String,
        input: serde_json::Value,
        permission_suggestions: Option<Vec<PermissionUpdate>>,
        tool_use_id: Option<String>,
    ) -> Result<PermissionResult, ExecutorError> {
        if self.auto_approve {
            Ok(PermissionResult::Allow {
                updated_input: input,
                updated_permissions: None,
            })
        } else if let Some(latest_tool_use_id) = tool_use_id {
            self.handle_approval(latest_tool_use_id, tool_name, input).await
        } else {
            Ok(PermissionResult::Allow {
                updated_input: input,
                updated_permissions: None,
            })
        }
    }

    /// 处理 Hook 回调
    pub async fn on_hook_callback(
        &self,
        callback_id: String,
        input: serde_json::Value,
        tool_use_id: Option<String>,
    ) -> Result<serde_json::Value, ExecutorError> {
        // 处理特定的回调ID，如 git 检查等
        // ...
    }
}
```

### Claude Code 配置选项

```rust
// crates/executors/default_profiles.json
{
  "CLAUDE_CODE": {
    "DEFAULT": {
      "CLAUDE_CODE": {
        "dangerously_skip_permissions": true  // 跳过权限检查
      }
    },
    "PLAN": {
      "CLAUDE_CODE": {
        "plan": true  // 计划模式
      }
    },
    "OPUS": {
      "CLAUDE_CODE": {
        "model": "opus"  // 使用 Opus 模型
      }
    },
    "APPROVALS": {
      "CLAUDE_CODE": {
        "approvals": true  // 启用审批流程
      }
    }
  }
}
```

---

## Codex 执行器

Codex 执行器通过 **JSON-RPC 协议** 与 Codex App Server 进行通信。

### 基本命令

```rust
// crates/executors/src/executors/codex.rs
pub fn base_command() -> &'static str {
    "npx -y @openai/codex@0.101.0"
}
```

### 执行流程

1. **启动 App Server**：运行 `npx -y @openai/codex@0.101.0 app-server`
2. **初始化连接**：发送 `Initialize` JSON-RPC 请求
3. **创建/恢复会话**：调用 `new_conversation` 或 `resume_conversation`
4. **发送消息**：使用 `send_user_message` 发送用户提示
5. **监听事件**：通过 `add_conversation_listener` 接收实时事件

### JSON-RPC 协议实现

```rust
// crates/executors/src/executors/codex/jsonrpc.rs

pub struct JsonRpcPeer {
    stdin: Arc<Mutex<ChildStdin>>,
    pending: Arc<Mutex<HashMap<RequestId, oneshot::Sender<PendingResponse>>>>,
    id_counter: Arc<AtomicI64>,
}

impl JsonRpcPeer {
    pub fn spawn(
        stdin: ChildStdin,
        stdout: ChildStdout,
        callbacks: Arc<dyn JsonRpcCallbacks>,
        exit_tx: ExitSignalSender,
        cancel: CancellationToken,
    ) -> Self {
        // 启动读取循环，处理服务器消息
        tokio::spawn(async move {
            let mut reader = BufReader::new(stdout);
            loop {
                match serde_json::from_str::<JSONRPCMessage>(line) {
                    Ok(JSONRPCMessage::Response(response)) => {
                        callbacks.on_response(&peer, line, &response).await;
                        reader_peer.resolve(request_id, PendingResponse::Result(result)).await;
                    }
                    Ok(JSONRPCMessage::Request(request)) => {
                        callbacks.on_request(&peer, line, request).await;
                    }
                    Ok(JSONRPCMessage::Notification(notification)) => {
                        callbacks.on_notification(&peer, line, notification).await;
                    }
                    // ...
                }
            }
        });
    }

    pub async fn request<R, T>(
        &self,
        request_id: RequestId,
        message: &T,
        label: &str,
        cancel: CancellationToken,
    ) -> Result<R, ExecutorError>
    where
        R: DeserializeOwned + Debug,
        T: Serialize + Sync,
    {
        let receiver = self.register(request_id).await;
        self.send(message).await?;
        await_response(receiver, label, cancel).await
    }
}
```

### AppServerClient 实现

```rust
// crates/executors/src/executors/codex/client.rs

pub struct AppServerClient {
    rpc: OnceLock<JsonRpcPeer>,
    log_writer: LogWriter,
    approvals: Option<Arc<dyn ExecutorApprovalService>>,
    conversation_id: Mutex<Option<ThreadId>>,
    auto_approve: bool,
    cancel: CancellationToken,
}

impl AppServerClient {
    /// 初始化连接
    pub async fn initialize(&self) -> Result<(), ExecutorError> {
        let request = ClientRequest::Initialize {
            request_id: self.next_request_id(),
            params: InitializeParams {
                client_info: ClientInfo {
                    name: "vibe-codex-executor".to_string(),
                    version: env!("CARGO_PKG_VERSION").to_string(),
                },
                capabilities: None,
            },
        };
        self.send_request::<InitializeResponse>(request, "initialize").await?;
        self.send_message(&ClientNotification::Initialized).await
    }

    /// 创建新会话
    pub async fn new_conversation(
        &self,
        params: NewConversationParams,
    ) -> Result<NewConversationResponse, ExecutorError> {
        let request = ClientRequest::NewConversation {
            request_id: self.next_request_id(),
            params,
        };
        self.send_request(request, "newConversation").await
    }

    /// 恢复会话
    pub async fn resume_conversation(
        &self,
        rollout_path: std::path::PathBuf,
        overrides: NewConversationParams,
    ) -> Result<ResumeConversationResponse, ExecutorError> {
        let request = ClientRequest::ResumeConversation {
            request_id: self.next_request_id(),
            params: ResumeConversationParams {
                path: Some(rollout_path),
                overrides: Some(overrides),
                conversation_id: None,
                history: None,
            },
        };
        self.send_request(request, "resumeConversation").await
    }

    /// 发送用户消息
    pub async fn send_user_message(
        &self,
        conversation_id: codex_protocol::ThreadId,
        message: String,
    ) -> Result<SendUserMessageResponse, ExecutorError> {
        let request = ClientRequest::SendUserMessage {
            request_id: self.next_request_id(),
            params: SendUserMessageParams {
                conversation_id,
                items: vec![InputItem::Text {
                    text: message,
                    text_elements: vec![],
                }],
            },
        };
        self.send_request(request, "sendUserMessage").await
    }

    /// 添加会话监听器
    pub async fn add_conversation_listener(
        &self,
        conversation_id: codex_protocol::ThreadId,
    ) -> Result<AddConversationSubscriptionResponse, ExecutorError> {
        let request = ClientRequest::AddConversationListener {
            request_id: self.next_request_id(),
            params: AddConversationListenerParams {
                conversation_id,
                experimental_raw_events: false,
            },
        };
        self.send_request(request, "addConversationListener").await
    }
}
```

### Codex 配置选项

```rust
// crates/executors/src/executors/codex.rs

/// 沙箱策略模式
pub enum SandboxMode {
    Auto,
    ReadOnly,
    WorkspaceWrite,
    DangerFullAccess,
}

/// 审批策略
pub enum AskForApproval {
    UnlessTrusted,  // 只读命令自动审批，其他需用户确认
    OnFailure,      // 先在沙箱中运行，失败时询问
    OnRequest,      // 模型决定何时询问
    Never,          // 从不询问审批
}

/// 推理努力程度
pub enum ReasoningEffort {
    Low,
    Medium,
    High,
    Xhigh,
}

pub struct Codex {
    pub append_prompt: AppendPrompt,
    pub sandbox: Option<SandboxMode>,
    pub ask_for_approval: Option<AskForApproval>,
    pub oss: Option<bool>,
    pub model: Option<String>,
    pub model_reasoning_effort: Option<ReasoningEffort>,
    pub profile: Option<String>,
    pub base_instructions: Option<String>,
    // ...
}
```

### Codex 配置变体

```json
// crates/executors/default_profiles.json
{
  "CODEX": {
    "DEFAULT": {
      "CODEX": {
        "sandbox": "danger-full-access"
      }
    },
    "HIGH": {
      "CODEX": {
        "sandbox": "danger-full-access",
        "model_reasoning_effort": "high"
      }
    },
    "APPROVALS": {
      "CODEX": {
        "sandbox": "workspace-write",
        "ask_for_approval": "unless-trusted"
      }
    },
    "MAX": {
      "CODEX": {
        "model": "gpt-5.1-codex-max",
        "sandbox": "danger-full-access"
      }
    },
    "GPT_5_3_CODEX": {
      "CODEX": {
        "model": "gpt-5.3-codex",
        "sandbox": "danger-full-access"
      }
    }
  }
}
```

---

## 任务执行流程

### 1. 创建并启动任务

```rust
// crates/server/src/routes/tasks.rs

pub struct CreateAndStartTaskRequest {
    pub task: CreateTask,
    pub executor_profile_id: ExecutorProfileId,
    pub repos: Vec<WorkspaceRepoInput>,
    pub linked_issue: Option<LinkedIssueInfo>,
}

pub async fn create_task_and_start(
    State(deployment): State<DeploymentImpl>,
    Json(payload): Json<CreateAndStartTaskRequest>,
) -> Result<ResponseJson<ApiResponse<TaskWithAttemptStatus>>, ApiError> {
    // 1. 创建任务
    let task = Task::create(pool, &payload.task, task_id).await?;

    // 2. 创建工作空间
    let workspace = Workspace::create(pool, &CreateWorkspace {
        branch: git_branch_name,
        agent_working_dir,
    }, attempt_id, task.id).await?;

    // 3. 关联仓库
    WorkspaceRepo::create_many(&deployment.db().pool, workspace.id, &workspace_repos).await?;

    // 4. 启动执行
    let is_attempt_running = deployment
        .container()
        .start_workspace(&workspace, payload.executor_profile_id.clone())
        .await
        .is_ok();

    Ok(ResponseJson(ApiResponse::success(TaskWithAttemptStatus {
        task,
        has_in_progress_attempt: is_attempt_running,
        // ...
    })))
}
```

### 2. 执行动作类型

```rust
// crates/executors/src/actions/mod.rs

pub enum ExecutorActionType {
    CodingAgentInitialRequest(CodingAgentInitialRequest),
    CodingAgentFollowUpRequest(CodingAgentFollowUpRequest),
    Script(ScriptRequest),
    Review(ReviewRequest),
}

/// 初始执行请求
pub struct CodingAgentInitialRequest {
    pub prompt: String,
    pub executor_profile_id: ExecutorProfileId,
    pub working_dir: Option<String>,
}

/// 后续执行请求
pub struct CodingAgentFollowUpRequest {
    pub prompt: String,
    pub session_id: String,
    pub reset_to_message_id: Option<String>,
    pub executor_profile_id: ExecutorProfileId,
    pub working_dir: Option<String>,
}
```

### 3. 执行器选择

```rust
// crates/executors/src/actions/coding_agent_initial.rs

impl Executable for CodingAgentInitialRequest {
    async fn spawn(
        &self,
        current_dir: &Path,
        approvals: Arc<dyn ExecutorApprovalService>,
        env: &ExecutionEnv,
    ) -> Result<SpawnedChild, ExecutorError> {
        // 从配置中获取执行器
        let mut agent = ExecutorConfigs::get_cached()
            .get_coding_agent(&self.executor_profile_id)
            .ok_or(ExecutorError::UnknownExecutorType(
                executor_profile_id.to_string(),
            ))?;

        // 设置审批服务
        agent.use_approvals(approvals.clone());

        // 执行任务
        agent.spawn(&effective_dir, &self.prompt, env).await
    }
}
```

---

## MCP 工具接口

Vibe Kanban 通过 MCP (Model Context Protocol) 暴露工具接口，允许其他 AI 代理调用。

### 启动工作空间会话

```rust
// crates/server/src/mcp/task_server.rs

pub struct StartWorkspaceSessionRequest {
    pub title: String,
    pub executor: String,  // 'CLAUDE_CODE', 'AMP', 'GEMINI', 'CODEX', 等
    pub variant: Option<String>,
    pub repos: Vec<McpWorkspaceRepoInput>,
    pub issue_id: Option<Uuid>,
}

#[tool(description = "Start a new workspace session. A local task is auto-created.")]
async fn start_workspace_session(
    &self,
    Parameters(request): Parameters<StartWorkspaceSessionRequest>,
) -> Result<CallToolResult, ErrorData> {
    // 解析执行器类型
    let base_executor = BaseCodingAgent::from_str(&normalized_executor)?;

    // 获取项目
    let projects: Vec<Project> = self.send_json(self.client.get(self.url("/api/projects"))).await?;

    // 创建并启动任务
    let payload = CreateAndStartTaskRequest {
        task: CreateTask::from_title_description(project.id, title, None),
        executor_profile_id,
        repos: workspace_repos,
        linked_issue: None,
    };

    let task = self.send_json(self.client.post(url).json(&payload)).await?;

    // 返回工作空间 ID
    TaskServer::success(&StartWorkspaceSessionResponse {
        workspace_id: workspace.id.to_string(),
    })
}
```

### 其他 MCP 工具

| 工具名称 | 描述 |
|---------|------|
| `list_organizations` | 列出所有可用组织 |
| `list_projects` | 列出项目 |
| `list_issues` | 列出项目中的问题 |
| `create_issue` | 创建新问题 |
| `update_issue` | 更新问题 |
| `delete_issue` | 删除问题 |
| `get_issue` | 获取问题详情 |
| `get_context` | 获取当前工作空间上下文 |
| `link_workspace` | 关联工作空间到远程问题 |
| `list_repos` | 列出仓库 |
| `get_repo` | 获取仓库详情 |
| `update_setup_script` | 更新设置脚本 |
| `update_cleanup_script` | 更新清理脚本 |
| `update_dev_server_script` | 更新开发服务器脚本 |

---

## 日志流与结果获取

### WebSocket 日志流

```rust
// crates/server/src/routes/execution_processes.rs

/// 原始日志流
pub async fn stream_raw_logs_ws(
    ws: WebSocketUpgrade,
    State(deployment): State<DeploymentImpl>,
    Path(exec_id): Path<Uuid>,
) -> Result<impl IntoResponse, ApiError> {
    let _stream = deployment
        .container()
        .stream_raw_logs(&exec_id)
        .await
        .ok_or_else(|| ApiError::ExecutionProcessNotFound)?;

    Ok(ws.on_upgrade(move |socket| async move {
        handle_raw_logs_ws(socket, deployment, exec_id).await
    }))
}

/// 归一化日志流
pub async fn stream_normalized_logs_ws(
    ws: WebSocketUpgrade,
    State(deployment): State<DeploymentImpl>,
    Path(exec_id): Path<Uuid>,
) -> Result<impl IntoResponse, ApiError> {
    let stream = deployment
        .container()
        .stream_normalized_logs(&exec_id)
        .await?;

    Ok(ws.on_upgrade(move |socket| async move {
        handle_normalized_logs_ws(socket, stream).await
    }))
}
```

### 日志消息类型

```rust
// crates/executors/src/logs/mod.rs

/// 归一化会话
pub struct NormalizedConversation {
    pub entries: Vec<NormalizedEntry>,
    pub session_id: Option<String>,
    pub executor_type: String,
    pub prompt: Option<String>,
    pub summary: Option<String>,
}

/// 归一化条目
pub struct NormalizedEntry {
    pub timestamp: Option<String>,
    pub entry_type: NormalizedEntryType,
    pub content: String,
    pub metadata: Option<serde_json::Value>,
}

/// 条目类型
pub enum NormalizedEntryType {
    UserMessage,
    UserFeedback { denied_tool: String },
    AssistantMessage,
    ToolUse {
        tool_name: String,
        action_type: ActionType,
        status: ToolStatus,
    },
    SystemMessage,
    ErrorMessage { error_type: NormalizedEntryError },
    Thinking,
    Loading,
    NextAction {
        failed: bool,
        execution_processes: usize,
        needs_setup: bool,
    },
    TokenUsageInfo(TokenUsageInfo),
}

/// 工具状态
pub enum ToolStatus {
    Created,
    Success,
    Failed,
    Denied { reason: Option<String> },
    PendingApproval {
        approval_id: String,
        requested_at: DateTime<Utc>,
        timeout_at: DateTime<Utc>,
    },
    TimedOut,
}

/// 动作类型
pub enum ActionType {
    FileRead { path: String },
    FileEdit { path: String, changes: Vec<FileChange> },
    CommandRun {
        command: String,
        result: Option<CommandRunResult>,
        category: CommandCategory,
    },
    Search { query: String },
    WebFetch { url: String },
    Tool {
        tool_name: String,
        arguments: Option<serde_json::Value>,
        result: Option<ToolResult>,
    },
    TaskCreate { description: String, subagent_type: Option<String> },
    PlanPresentation { plan: String },
    TodoManagement { todos: Vec<TodoItem>, operation: String },
    Other { description: String },
}
```

### JSON Patch 格式

日志通过 JSON Patch 格式进行增量更新：

```rust
// crates/executors/src/logs/utils/patch.rs

pub struct ConversationPatch;

impl ConversationPatch {
    /// 添加 stdout 条目
    pub fn add_stdout(index: usize, content: String) -> Self {
        // 生成 JSON Patch
    }

    /// 添加 stderr 条目
    pub fn add_stderr(index: usize, content: String) -> Self {
        // 生成 JSON Patch
    }

    /// 更新工具状态
    pub fn update_tool_status(index: usize, status: ToolStatus) -> Self {
        // 生成 JSON Patch
    }
}
```

---

## 配置与变体

### ExecutorProfileId 结构

```rust
// crates/executors/src/profile.rs

pub struct ExecutorProfileId {
    pub executor: BaseCodingAgent,
    pub variant: Option<String>,
}

impl ExecutorProfileId {
    pub fn new(executor: BaseCodingAgent) -> Self {
        Self { executor, variant: None }
    }

    pub fn with_variant(executor: BaseCodingAgent, variant: String) -> Self {
        Self { executor, variant: Some(variant) }
    }
}
```

### 支持的执行器类型

```rust
// crates/executors/src/executors/mod.rs

pub enum BaseCodingAgent {
    ClaudeCode,
    Amp,
    Gemini,
    Codex,
    Opencode,
    CursorAgent,
    QwenCode,
    Copilot,
    Droid,
}
```

### 执行器能力

```rust
impl CodingAgent {
    pub fn capabilities(&self) -> Vec<BaseAgentCapability> {
        match self {
            Self::ClaudeCode(_) => vec![
                BaseAgentCapability::SessionFork,
                BaseAgentCapability::ContextUsage,
            ],
            Self::Codex(_) => vec![
                BaseAgentCapability::SessionFork,
                BaseAgentCapability::SetupHelper,
                BaseAgentCapability::ContextUsage,
            ],
            // ...
        }
    }
}
```

---

## API 端点参考

### 任务管理

| 端点 | 方法 | 描述 |
|-----|------|------|
| `/api/tasks` | GET | 获取项目任务列表 |
| `/api/tasks` | POST | 创建任务 |
| `/api/tasks/create-and-start` | POST | 创建并启动任务 |
| `/api/tasks/stream/ws` | GET | WebSocket 流式任务更新 |
| `/api/tasks/{task_id}` | GET | 获取任务详情 |
| `/api/tasks/{task_id}` | PUT | 更新任务 |
| `/api/tasks/{task_id}` | DELETE | 删除任务 |

### 会话管理

| 端点 | 方法 | 描述 |
|-----|------|------|
| `/api/sessions` | GET | 获取工作空间的会话 |
| `/api/sessions` | POST | 创建会话 |
| `/api/sessions/{session_id}` | GET | 获取会话详情 |
| `/api/sessions/{session_id}/follow-up` | POST | 发送后续提示 |
| `/api/sessions/{session_id}/reset` | POST | 重置到指定进程 |

### 执行进程

| 端点 | 方法 | 描述 |
|-----|------|------|
| `/api/execution-processes/{id}` | GET | 获取执行进程 |
| `/api/execution-processes/{id}/stop` | POST | 停止执行进程 |
| `/api/execution-processes/{id}/raw-logs/ws` | GET | WebSocket 原始日志流 |
| `/api/execution-processes/{id}/normalized-logs/ws` | GET | WebSocket 归一化日志流 |
| `/api/execution-processes/stream/session/ws` | GET | WebSocket 会话进程流 |

### 审批管理

| 端点 | 方法 | 描述 |
|-----|------|------|
| `/api/approvals/{id}` | GET | 获取审批请求 |
| `/api/approvals/{id}/approve` | POST | 批准请求 |
| `/api/approvals/{id}/deny` | POST | 拒绝请求 |

---

## 使用示例

### 1. 通过 API 创建并启动任务

```bash
# 创建并启动任务
curl -X POST http://localhost:3000/api/tasks/create-and-start \
  -H "Content-Type: application/json" \
  -d '{
    "task": {
      "title": "实现用户登录功能",
      "project_id": "00000000-0000-0000-0000-000000000001"
    },
    "executor_profile_id": {
      "executor": "CLAUDE_CODE"
    },
    "repos": [
      {
        "repo_id": "00000000-0000-0000-0000-000000000001",
        "target_branch": "main"
      }
    ]
  }'
```

### 2. 使用 Codex 执行器

```bash
curl -X POST http://localhost:3000/api/tasks/create-and-start \
  -H "Content-Type: application/json" \
  -d '{
    "task": {
      "title": "修复登录 bug",
      "project_id": "00000000-0000-0000-0000-000000000001"
    },
    "executor_profile_id": {
      "executor": "CODEX",
      "variant": "HIGH"
    },
    "repos": [
      {
        "repo_id": "00000000-0000-0000-0000-000000000001",
        "target_branch": "main"
      }
    ]
  }'
```

### 3. 发送后续提示

```bash
curl -X POST http://localhost:3000/api/sessions/{session_id}/follow-up \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "请添加单元测试",
    "executor_profile_id": {
      "executor": "CLAUDE_CODE"
    }
  }'
```

### 4. WebSocket 日志流

```javascript
// 连接 WebSocket
const ws = new WebSocket('ws://localhost:3000/api/execution-processes/{id}/normalized-logs/ws');

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log('Log entry:', data);
};

ws.onclose = () => {
  console.log('Connection closed');
};
```

### 5. 通过 MCP 工具调用

MCP 工具可通过 `mcp__codex__codex` 工具直接调用 Codex：

```json
{
  "prompt": "实现用户认证功能",
  "cwd": "/path/to/project",
  "sandbox": "workspace-write",
  "approval-policy": "on-request"
}
```

继续对话使用 `mcp__codex__codex-reply`：

```json
{
  "threadId": "previous-thread-id",
  "prompt": "请添加错误处理"
}
```

---

## 附录

### 文件路径参考

| 文件 | 描述 |
|-----|------|
| `crates/executors/src/executors/claude.rs` | Claude Code 主执行器 |
| `crates/executors/src/executors/claude/client.rs` | Claude SDK 客户端 |
| `crates/executors/src/executors/claude/protocol.rs` | 协议通信层 |
| `crates/executors/src/executors/claude/types.rs` | 类型定义 |
| `crates/executors/src/executors/codex.rs` | Codex 主执行器 |
| `crates/executors/src/executors/codex/client.rs` | Codex 客户端 |
| `crates/executors/src/executors/codex/jsonrpc.rs` | JSON-RPC 实现 |
| `crates/executors/src/executors/codex/session.rs` | 会话管理 |
| `crates/executors/default_profiles.json` | 默认执行器配置 |
| `crates/server/src/mcp/task_server.rs` | MCP 工具服务器 |
| `crates/server/src/routes/tasks.rs` | 任务 API 路由 |
| `crates/server/src/routes/sessions/mod.rs` | 会话 API 路由 |
| `crates/server/src/routes/execution_processes.rs` | 执行进程 API |

### 相关文档

- [Claude Code SDK 文档](https://docs.claude.com/en/api/agent-sdk)
- [Codex App Server 协议](https://github.com/openai/codex)
- [MCP 规范](https://modelcontextprotocol.io/)
