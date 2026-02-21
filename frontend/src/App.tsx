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
  listSessions,
} from "@/lib/api";
import type { EventItem, ExecutorType, SessionItem } from "@/types/chat";

function summarizeContent(content: unknown): string {
  if (typeof content === "string") return content;
  if (content == null) return "";
  try {
    return JSON.stringify(content);
  } catch {
    return String(content);
  }
}

function formatTime(v: string): string {
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return v;
  return d.toLocaleString();
}

export default function App() {
  const [sessions, setSessions] = useState<SessionItem[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [messages, setMessages] = useState<Record<string, EventItem[]>>({});
  const [prompt, setPrompt] = useState("");
  const [followup, setFollowup] = useState("");
  const [executor, setExecutor] = useState<ExecutorType>("codex");
  const [workingDir, setWorkingDir] = useState(".");
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const streamRef = useRef<{ close: () => void } | null>(null);

  useEffect(() => {
    return () => {
      streamRef.current?.close();
    };
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
    const trimmed = prompt.trim();
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
      setPrompt("");

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
    if (selectedSession.status !== "running") {
      setError("当前会话已结束，无法继续对话，请新建会话。");
      return;
    }
    const text = followup.trim();
    if (!text) return;

    setBusy(true);
    setError(null);
    try {
      await continueTask(selectedSession.id, { message: text });
      setFollowup("");
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
      if (message.includes("session not found")) {
        setSessions((prev) =>
          prev.map((item) =>
            item.id === selectedSession.id
              ? {
                  ...item,
                  status: "done",
                  updatedAt: new Date().toISOString(),
                }
              : item,
          ),
        );
        setError("当前会话已结束，无法继续对话，请新建会话。");
      } else {
        setError(message);
      }
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

  return (
    <div className="page">
      <header className="hero">
        <div>
          <p className="eyebrow">Creative Tim x shadcn/ui Playground</p>
          <h1>Executor 会话测试页面</h1>
          <p>用于发起任务、查看会话列表、追踪流式消息、追加提问和中断执行。</p>
        </div>
      </header>

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
                placeholder="例如：帮我总结当前仓库结构并提出优化建议"
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                rows={4}
              />
              <div className="inline-fields">
                <label>
                  Executor
                  <select
                    value={executor}
                    onChange={(e) =>
                      setExecutor(e.target.value as ExecutorType)
                    }
                    className="select"
                  >
                    <option value="codex">codex</option>
                    <option value="claude_code">claude_code</option>
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
              <Button
                onClick={() => void handleCreateSession()}
                disabled={busy || !prompt.trim()}
              >
                <Sparkles size={16} /> 发起会话
              </Button>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>会话消息</CardTitle>
              <CardDescription>
                {selectedSession
                  ? `当前会话：${selectedSession.id}`
                  : "请从左侧选择会话，或先创建一个新会话"}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="actions">
                <Input
                  placeholder="追加问题，例如：请给出更详细的实施步骤"
                  value={followup}
                  onChange={(e) => setFollowup(e.target.value)}
                  disabled={!selectedSession}
                />
                <Button
                  variant="secondary"
                  onClick={() => void handleFollowup()}
                  disabled={
                    !selectedSession ||
                    selectedSession.status !== "running" ||
                    !followup.trim() ||
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

              {error && <div className="error-box">{error}</div>}

              <div className="message-panel">
                {loading ? (
                  <div className="empty">加载中...</div>
                ) : activeMessages.length === 0 ? (
                  <div className="empty">当前会话暂无消息</div>
                ) : (
                  activeMessages.map((event, idx) => (
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
                      <pre>{summarizeContent(event.content)}</pre>
                    </article>
                  ))
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
          disabled={busy || !prompt.trim()}
        >
          <SendHorizontal size={14} /> 快速发送
        </Button>
      </footer>
    </div>
  );
}
