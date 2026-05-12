from agent_runtime.graph import run_subagent, run_task_with_langgraph
from agent_runtime.schemas import Task
from agent_runtime.tools import NullSearchTool
from langchain.messages import AIMessage
from langchain.tools import tool


class FakeCallbacks:
    def __init__(self):
        self.events = []
        self.artifacts = []

    async def event(self, task_id, event):
        self.events.append((task_id, event))

    async def artifact(self, task_id, artifact):
        self.artifacts.append((task_id, artifact))
        return {"id": "artifact-1"}


class FakeToolLLM:
    provider = "fake"
    model = "fake-tool-llm"

    def __init__(self):
        self.calls = 0
        self.bound_tools = []

    def bind_tools(self, tools):
        self.bound_tools = [tool.name for tool in tools]
        return self

    async def ainvoke(self, messages):
        self.calls += 1
        if self.calls == 1:
            return AIMessage(
                content="",
                tool_calls=[{"id": "call-1", "name": "hot_list", "args": {"limit": 5}}],
            )
        if self.calls == 2:
            return AIMessage(
                content="",
                tool_calls=[{"id": "call-2", "name": "zhihu_search", "args": {"query": "AI Agent", "count": 3}}],
            )
        return AIMessage(
            content="",
            tool_calls=[
                {
                    "id": "call-3",
                    "name": "create_html_report",
                    "args": {
                        "title": "知乎热点整理",
                        "summary": "已经整理好知乎热点。",
                        "sections": [
                            {
                                "heading": "重点观察",
                                "paragraphs": ["热榜显示 AI Agent 仍是高关注方向。"],
                                "bullets": ["关注讨论热度", "保留来源链接"],
                            }
                        ],
                        "sources": [
                            {
                                "title": "危险链接会被移除",
                                "url": "javascript:alert(1)",
                                "source_type": "zhihu",
                                "author": "测试作者",
                            },
                            {
                                "title": "知乎问题",
                                "url": "https://www.zhihu.com/question/1",
                                "source_type": "zhihu",
                            },
                        ],
                        "caveats": ["热榜会随时间变化。"],
                    },
                }
            ]
        )


class FakeToolExecutor:
    def __init__(self):
        self.calls = []

    async def execute(self, name, arguments):
        self.calls.append((name, arguments))
        return {
            "ok": True,
            "tool": name,
            "items": [
                {
                    "title": "AI Agent 热点",
                    "url": "https://www.zhihu.com/question/1",
                    "content_text": "摘要",
                }
            ],
        }


async def test_persona_subagent_uses_model_chosen_tools_and_creates_html_artifact():
    callbacks = FakeCallbacks()
    llm = FakeToolLLM()
    executor = FakeToolExecutor()
    task = Task(
        id="task-1",
        session_id="session-1",
        title="知乎热点",
        user_request="今天知乎有哪些热门信息，帮我整理成网页",
        locale="zh-CN",
    )

    await run_task_with_langgraph(
        task,
        NullSearchTool(),
        callbacks,
        llm=llm,
        tool_executor=executor,
        max_agent_iterations=5,
    )

    assert executor.calls == [
        ("hot_list", {"limit": 5}),
        ("zhihu_search", {"query": "AI Agent", "count": 3}),
    ]
    assert [event.event_type for _, event in callbacks.events] == [
        "plan.created",
        "agent.tool_call",
        "agent.tool_call",
        "agent.tool_call",
        "task.completed",
    ]
    artifact = callbacks.artifacts[0][1]
    assert artifact.type == "html"
    assert artifact.mime_type == "text/html; charset=utf-8"
    assert artifact.content.startswith("<!doctype html>")
    assert "热榜显示 AI Agent" in artifact.content
    assert 'href="javascript:alert(1)"' not in artifact.content
    assert 'href="https://www.zhihu.com/question/1"' in artifact.content
    assert callbacks.events[-1][1].payload == {"artifact_id": "artifact-1"}


async def test_generic_subagent_runs_with_replaced_tool_set():
    calls = []

    @tool
    async def echo_topic(topic: str) -> dict:
        """Echo a topic.

        Args:
            topic: Topic to echo.
        """
        calls.append(topic)
        return {"topic": topic}

    class FakeGenericLLM:
        provider = "fake"
        model = "fake-generic-llm"

        def __init__(self):
            self.calls = 0
            self.bound_tools = []

        def bind_tools(self, tools):
            self.bound_tools = [tool.name for tool in tools]
            return self

        async def ainvoke(self, messages):
            self.calls += 1
            if self.calls == 1:
                return AIMessage(
                    content="",
                    tool_calls=[{"id": "echo-1", "name": "echo_topic", "args": {"topic": "通用任务"}}],
                )
            return AIMessage(content="done")

    callbacks = FakeCallbacks()
    llm = FakeGenericLLM()
    task = Task(
        id="task-2",
        session_id="session-1",
        title="通用任务",
        user_request="用可用工具处理这个任务",
        locale="zh-CN",
    )

    await run_subagent(
        task=task,
        model=llm,
        tools=[echo_topic],
        callbacks=callbacks,
        max_agent_iterations=3,
    )

    assert llm.bound_tools == ["echo_topic"]
    assert calls == ["通用任务"]
    assert [event.event_type for _, event in callbacks.events] == [
        "plan.created",
        "agent.tool_call",
    ]
