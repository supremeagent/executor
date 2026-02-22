import { useEffect, useMemo, useRef, useState } from "react";
import {
  CircleStop,
  MessageCircleMore,
  Play,
  SendHorizontal,
  Sparkles,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import JsonCodeView from "@/components/json-code-view";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  continueTask,
  createStream,
  executeTask,
  interruptTask,
  listEvents,
  listExecutors,
  listSessions,
  respondControl,
} from "@/lib/api";
import { buildEventViewModel, toUnifiedContent } from "@/lib/event-display";
import type { EventItem, ExecutorType, SessionItem, UnifiedEventContent } from "@/types/chat";

function formatTime(v: string): string {
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return v;
  return d.toLocaleString();
}

export default function App() {
  type FilterState = {
    types: string[];
    categories: string[];
    actions: string[];
  };

  const [sessions, setSessions] = useState<SessionItem[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [messages, setMessages] = useState<Record<string, EventItem[]>>({});
  const [draftMessage, setDraftMessage] = useState("");
  const [executor, setExecutor] = useState<string>("");
  const [availableExecutors, setAvailableExecutors] = useState<string[]>([]);
  const [workingDir, setWorkingDir] = useState(".");
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [filters, setFilters] = useState<FilterState>({
    types: [],
    categories: [],
    actions: [],
  });
  const [filterPanelOpen, setFilterPanelOpen] = useState(false);
  const streamRef = useRef<{ close: () => void } | null>(null);

  useEffect(() => {
    return () => {
      streamRef.current?.close();
    };
  }, []);

  useEffect(() => {
    async function initExecutors() {
      try {
        const list = await listExecutors();
        setAvailableExecutors(list);
        if (list.length > 0) {
          setExecutor(list[0]);
        }
      } catch (e) {
        console.error("Failed to load executors:", e);
      }
    }
    void initExecutors();
  }, []);

  useEffect(() => {
    async function initSessions() {
      setError(null);
      try {
        const listedSessions = await listSessions();
        setSessions(listedSessions);
        if (listedSessions.length === 0) {
          setSelectedId(null);
          return;
        }

        const firstSession = listedSessions[0];
        setSelectedId(firstSession.id);
        await loadSessionMessages(firstSession.id);
        if (firstSession.status === "running") {
          attachStream(firstSession.id);
        }
      } catch (e) {
        setError(e instanceof Error ? e.message : "加载会话列表失败");
      }
    }

    void initSessions();
  }, []);

  const selectedSession = useMemo(
    () => sessions.find((s) => s.id === selectedId) ?? null,
    [sessions, selectedId],
  );

  async function loadSessionMessages(sessionId: string) {
    setLoading(true);
    setError(null);
    try {
      const events = await listEvents(sessionId);
      setMessages((prev) => ({ ...prev, [sessionId]: events }));
    } catch (e) {
      setError(e instanceof Error ? e.message : "加载会话消息失败");
    } finally {
      setLoading(false);
    }
  }

  function attachStream(sessionId: string) {
    streamRef.current?.close();

    const handleIncomingEvent = (evt: MessageEvent) => {
      try {
        const parsed = JSON.parse(evt.data) as EventItem;
        setMessages((prev) => {
          const current = prev[sessionId] ?? [];
          if (
            current.some(
              (m) => m.seq === parsed.seq && parsed.seq !== undefined,
            )
          )
            return prev;
          return { ...prev, [sessionId]: [...current, parsed] };
        });

        if (parsed.type === "done") {
          setSessions((prev) =>
            prev.map((item) =>
              item.id === sessionId
                ? {
                  ...item,
                  status: "done",
                  updatedAt: new Date().toISOString(),
                }
                : item,
            ),
          );
        }
      } catch {
        // ignore malformed events
      }
    };

    streamRef.current = createStream(sessionId, handleIncomingEvent, () => {
      streamRef.current?.close();
      setSessions((prev) =>
        prev.map((item) =>
          item.id === sessionId && item.status === "running"
            ? {
              ...item,
              status: "interrupted",
              updatedAt: new Date().toISOString(),
            }
            : item,
        ),
      );
    });
  }

  async function handleCreateSession() {
    const trimmed = draftMessage.trim();
    if (!trimmed) return;

    setBusy(true);
    setError(null);
    try {
      const res = await executeTask({
        prompt: trimmed,
        executor,
        working_dir: workingDir || ".",
      });

      const now = new Date().toISOString();
      const session: SessionItem = {
        id: res.session_id,
        title: trimmed.slice(0, 36),
        status: "running",
        createdAt: now,
        updatedAt: now,
        executor,
      };

      setSessions((prev) => [session, ...prev]);
      setSelectedId(session.id);
      setMessages((prev) => ({ ...prev, [session.id]: [] }));
      setDraftMessage("");

      await loadSessionMessages(session.id);
      attachStream(session.id);
    } catch (e) {
      setError(e instanceof Error ? e.message : "创建会话失败");
    } finally {
      setBusy(false);
    }
  }

  async function handleSelectSession(sessionId: string) {
    setSelectedId(sessionId);
    await loadSessionMessages(sessionId);
    const target = sessions.find((s) => s.id === sessionId);
    if (target?.status === "running") {
      attachStream(sessionId);
    } else {
      streamRef.current?.close();
    }
  }

  async function handleFollowup() {
    if (!selectedSession) return;
    const text = draftMessage.trim();
    if (!text) return;

    setBusy(true);
    setError(null);
    try {
      await continueTask(selectedSession.id, { message: text });
      setDraftMessage("");
      setSessions((prev) =>
        prev.map((item) =>
          item.id === selectedSession.id
            ? {
              ...item,
              status: "running",
              updatedAt: new Date().toISOString(),
            }
            : item,
        ),
      );
      attachStream(selectedSession.id);
    } catch (e) {
      const message = e instanceof Error ? e.message : "追加问题失败";
      setError(message);
    } finally {
      setBusy(false);
    }
  }

  async function handleControlDecision(requestId: string, decision: "approve" | "deny") {
    if (!selectedSession) return;
    setBusy(true);
    setError(null);
    try {
      await respondControl(selectedSession.id, { request_id: requestId, decision });
    } catch (e) {
      setError(e instanceof Error ? e.message : "审批操作失败");
    } finally {
      setBusy(false);
    }
  }

  async function handleInterrupt() {
    if (!selectedSession) return;
    setBusy(true);
    setError(null);
    try {
      await interruptTask(selectedSession.id);
      setSessions((prev) =>
        prev.map((item) =>
          item.id === selectedSession.id
            ? {
              ...item,
              status: "interrupted",
              updatedAt: new Date().toISOString(),
            }
            : item,
        ),
      );
      streamRef.current?.close();
    } catch (e) {
      setError(e instanceof Error ? e.message : "中断失败");
    } finally {
      setBusy(false);
    }
  }

  const activeMessages = selectedId ? (messages[selectedId] ?? []) : [];

  const filterOptions = useMemo(() => {
    const typeSet = new Set<string>();
    const categorySet = new Set<string>();
    const actionSet = new Set<string>();

    for (const event of activeMessages) {
      typeSet.add(event.type);
      const content = toUnifiedContent(event.content);
      if (!content) continue;
      if (content.category) categorySet.add(content.category);
      if (content.action) actionSet.add(content.action);
    }

    return {
      types: Array.from(typeSet).sort(),
      categories: Array.from(categorySet).sort(),
      actions: Array.from(actionSet).sort(),
    };
  }, [activeMessages]);

  const filteredMessages = useMemo(() => {
    return activeMessages.filter((event) => {
      const content = toUnifiedContent(event.content);
      if (filters.types.length > 0 && !filters.types.includes(event.type)) {
        return false;
      }
      if (filters.categories.length > 0) {
        const category = content?.category ?? "";
        if (!filters.categories.includes(category)) return false;
      }
      if (filters.actions.length > 0) {
        const action = content?.action ?? "";
        if (!filters.actions.includes(action)) return false;
      }
      return true;
    });
  }, [activeMessages, filters]);

  function toggleFilter(key: keyof FilterState, value: string) {
    setFilters((prev) => {
      const exists = prev[key].includes(value);
      const nextValues = exists
        ? prev[key].filter((item) => item !== value)
        : [...prev[key], value];
      return { ...prev, [key]: nextValues };
    });
  }

  function clearFilters() {
    setFilters({
      types: [],
      categories: [],
      actions: [],
    });
  }

  return (
    <div className="page">
      <main className="layout">
        <Card className="sidebar">
          <CardHeader>
            <CardTitle>会话列表</CardTitle>
            <CardDescription>
              点击任意会话可查看完整消息和实时输出
            </CardDescription>
          </CardHeader>
          <CardContent className="list-wrap">
            {sessions.length === 0 ? (
              <div className="empty">暂无会话，先在右侧发起一个需求。</div>
            ) : (
              sessions.map((session) => (
                <button
                  key={session.id}
                  className={`session-item ${selectedId === session.id ? "selected" : ""}`}
                  onClick={() => void handleSelectSession(session.id)}
                >
                  <div className="session-row">
                    <strong>{session.title || session.id.slice(0, 8)}</strong>
                    <Badge
                      tone={
                        session.status === "running" ? "running" : "stopped"
                      }
                    >
                      {session.status}
                    </Badge>
                  </div>
                  <p>{session.id}</p>
                  <small>{formatTime(session.updatedAt)}</small>
                </button>
              ))
            )}
          </CardContent>
        </Card>

        <section className="content">
          <Card>
            <CardHeader>
              <CardTitle>新建对话</CardTitle>
              <CardDescription>
                输入你的需求，创建一个新会话并立即开始流式监听
              </CardDescription>
            </CardHeader>
            <CardContent className="form-grid">
              <Textarea
                placeholder="输入内容：可发起新会话，也可对当前会话追加问题"
                value={draftMessage}
                onChange={(e) => setDraftMessage(e.target.value)}
                rows={4}
              />
              <div className="inline-fields">
                <label>
                  Executor
                  <select
                    value={executor}
                    onChange={(e) =>
                      setExecutor(e.target.value)
                    }
                    className="select"
                  >
                    {availableExecutors.map((ex) => (
                      <option key={ex} value={ex}>
                        {ex}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  Working Dir
                  <Input
                    value={workingDir}
                    onChange={(e) => setWorkingDir(e.target.value)}
                  />
                </label>
              </div>
              <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
                <Button
                  onClick={() => void handleCreateSession()}
                  disabled={busy || !draftMessage.trim()}
                >
                  <Sparkles size={16} /> 发起会话
                </Button>
                <Button
                  variant="secondary"
                  onClick={() => void handleFollowup()}
                  disabled={
                    !selectedSession ||
                    !draftMessage.trim() ||
                    busy
                  }
                >
                  <MessageCircleMore size={16} /> 追加问题
                </Button>
                <Button
                  variant="destructive"
                  onClick={() => void handleInterrupt()}
                  disabled={!selectedSession || busy}
                >
                  <CircleStop size={16} /> 中断
                </Button>
                <Button
                  variant="outline"
                  onClick={() =>
                    selectedId && void handleSelectSession(selectedId)
                  }
                  disabled={!selectedSession || loading}
                >
                  <Play size={16} /> 刷新
                </Button>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 8 }}>
                <CardTitle>会话消息</CardTitle>
                <div style={{ display: "flex", gap: 8 }}>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => setFilterPanelOpen((v) => !v)}
                  >
                    {filterPanelOpen ? "收起筛选" : "展开筛选"}
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={clearFilters}
                    disabled={filters.types.length === 0 && filters.categories.length === 0 && filters.actions.length === 0}
                  >
                    清空筛选
                  </Button>
                </div>
              </div>
              <CardDescription>
                {selectedSession
                  ? `当前会话：${selectedSession.id}`
                  : "请从左侧选择会话，或先创建一个新会话"}
              </CardDescription>
            </CardHeader>
            <CardContent>
              {error && <div className="error-box">{error}</div>}

              {filterPanelOpen ? (
                <div style={{ marginBottom: 12, display: "grid", gap: 8 }}>
                  <div>
                    <small>类型（type，多选）</small>
                    <div style={{ display: "flex", flexWrap: "wrap", gap: 8, marginTop: 6 }}>
                      {filterOptions.types.map((type) => (
                        <label key={type} style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
                          <input
                            type="checkbox"
                            checked={filters.types.includes(type)}
                            onChange={() => toggleFilter("types", type)}
                          />
                          <span>{type}</span>
                        </label>
                      ))}
                    </div>
                  </div>

                  <div>
                    <small>类别（category，多选）</small>
                    <div style={{ display: "flex", flexWrap: "wrap", gap: 8, marginTop: 6 }}>
                      {filterOptions.categories.map((category) => (
                        <label key={category} style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
                          <input
                            type="checkbox"
                            checked={filters.categories.includes(category)}
                            onChange={() => toggleFilter("categories", category)}
                          />
                          <span>{category}</span>
                        </label>
                      ))}
                    </div>
                  </div>

                  <div>
                    <small>动作（action，多选）</small>
                    <div style={{ display: "flex", flexWrap: "wrap", gap: 8, marginTop: 6 }}>
                      {filterOptions.actions.map((action) => (
                        <label key={action} style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
                          <input
                            type="checkbox"
                            checked={filters.actions.includes(action)}
                            onChange={() => toggleFilter("actions", action)}
                          />
                          <span>{action}</span>
                        </label>
                      ))}
                    </div>
                  </div>
                </div>
              ) : null}

              <div className="message-panel">
                {loading ? (
                  <div className="empty">加载中...</div>
                ) : activeMessages.length === 0 ? (
                  <div className="empty">当前会话暂无消息</div>
                ) : filteredMessages.length === 0 ? (
                  <div className="empty">没有符合当前筛选条件的消息</div>
                ) : (
                  filteredMessages.map((event, idx) => {
                    const view = buildEventViewModel(event);
                    const content = view.unified as UnifiedEventContent | null;

                    return (
                      <article
                        key={`${event.seq ?? "n"}-${idx}`}
                        className="message-item"
                      >
                        <header>
                          <Badge>{event.type}</Badge>
                          <small>
                            {event.timestamp
                              ? formatTime(event.timestamp)
                              : "streaming..."}
                          </small>
                        </header>
                        <p style={{ margin: "6px 0", fontWeight: 600 }}>{view.headline}</p>
                        {content ? (
                          <div style={{ marginBottom: 6, display: "flex", gap: 8, flexWrap: "wrap" }}>
                            <Badge>{content.category}</Badge>
                            {content.action ? <Badge>{content.action}</Badge> : null}
                            {content.phase ? <Badge>{content.phase}</Badge> : null}
                            {content.tool_name ? <Badge>{content.tool_name}</Badge> : null}
                            {content.target ? <Badge>{content.target}</Badge> : null}
                          </div>
                        ) : null}
                        {view.details.length > 0 ? <small>{view.details.join(" | ")}</small> : null}
                        {view.rawText ? <JsonCodeView value={view.rawText} /> : null}
                        {content && content.category === "approval" && content.request_id ? (
                          <div style={{ marginTop: 8, display: "flex", gap: 8 }}>
                            <Button
                              size="sm"
                              variant="secondary"
                              disabled={busy}
                              onClick={() => void handleControlDecision(content.request_id!, "approve")}
                            >
                              Approve
                            </Button>
                            <Button
                              size="sm"
                              variant="destructive"
                              disabled={busy}
                              onClick={() => void handleControlDecision(content.request_id!, "deny")}
                            >
                              Deny
                            </Button>
                          </div>
                        ) : null}
                      </article>
                    );
                  })
                )}
              </div>
            </CardContent>
          </Card>
        </section>
      </main>

      <footer className="footer">
        <span>
          React + TypeScript + shadcn-style components + Creative Tim theme
          flavor
        </span>
        <Button
          size="sm"
          variant="outline"
          onClick={() => void handleCreateSession()}
          disabled={busy || !draftMessage.trim()}
        >
          <SendHorizontal size={14} /> 快速发送
        </Button>
      </footer>
    </div>
  );
}
