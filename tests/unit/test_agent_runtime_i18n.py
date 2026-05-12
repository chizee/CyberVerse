from agent_runtime.i18n import Localizer, normalize_locale
from agent_runtime.graph import _draft_markdown, run_task_with_langgraph
from agent_runtime.schemas import Task
from agent_runtime.tools import MockSearchTool, NullSearchTool
from langchain.messages import AIMessage


class FakeCallbacks:
    def __init__(self):
        self.events = []
        self.artifacts = []

    async def event(self, task_id, event):
        self.events.append((task_id, event))

    async def artifact(self, task_id, artifact):
        self.artifacts.append((task_id, artifact))
        return {"id": "artifact-1"}


class FakeAgentLLM:
    provider = "fake"
    model = "fake-agent-llm"

    def __init__(self, responses=None):
        self.calls = []
        self.responses = list(
            responses
            or [
                AIMessage(
                    content="",
                    tool_calls=[
                        {
                            "id": "report-1",
                            "name": "create_html_report",
                            "args": {
                                "title": "Hot topics",
                                "summary": "I have prepared the materials.",
                                "sections": [{"heading": "Summary", "paragraphs": ["A localized report."]}],
                                "sources": [{"title": "Test source"}],
                            },
                        }
                    ]
                )
            ]
        )

    def bind_tools(self, tools):
        self.bound_tools = [tool.name for tool in tools]
        return self

    async def ainvoke(self, messages):
        self.calls.append(("ainvoke", messages, self.bound_tools))
        return self.responses.pop(0)


class FakeToolExecutor:
    client = None

    def __init__(self):
        self.calls = []

    async def execute(self, name, arguments):
        self.calls.append((name, arguments))
        return {"ok": True, "tool": name, "items": [{"title": "Mock result"}]}


def test_normalize_locale_aliases():
    assert normalize_locale("zh") == "zh-CN"
    assert normalize_locale("en-US") == "en"
    assert normalize_locale("ja-JP") == "ja"
    assert normalize_locale("ko-KR") == "ko"


def test_agent_markdown_uses_task_locale():
    task = Task(
        id="task-1",
        session_id="session-1",
        title="Hot topics",
        user_request="What is trending?",
        locale="en",
    )

    content = _draft_markdown(task, [], Localizer(task.locale))

    assert "User request: What is trending?" in content
    assert "Current status" in content
    assert "搜索工具" not in content


async def test_persona_subagent_task_uses_localized_messages(monkeypatch, tmp_path):
    monkeypatch.setenv("LANGGRAPH_CHECKPOINT_DB", str(tmp_path / "checkpoints.db"))
    callbacks = FakeCallbacks()
    task = Task(
        id="task-1",
        session_id="session-1",
        title="Hot topics",
        user_request="What is trending?",
        locale="en",
    )

    llm = FakeAgentLLM()

    await run_task_with_langgraph(task, NullSearchTool(), callbacks, llm=llm, tool_executor=FakeToolExecutor())

    assert len(callbacks.events) == 3
    assert [event.event_type for _, event in callbacks.events] == [
        "plan.created",
        "agent.tool_call",
        "task.completed",
    ]
    assert callbacks.events[-1][1].status == "completed"
    assert "I have prepared the materials" in callbacks.events[-1][1].message
    assert "A localized report" in callbacks.artifacts[0][1].content
    assert [call[0] for call in llm.calls] == ["ainvoke"]
    assert "create_html_report" in llm.calls[0][2]
    assert callbacks.events[0][1].payload["llm_provider"] == "fake"
    assert callbacks.artifacts[0][1].metadata["llm_model"] == "fake-agent-llm"


async def test_persona_subagent_task_mock_search_success(monkeypatch, tmp_path):
    monkeypatch.setenv("LANGGRAPH_CHECKPOINT_DB", str(tmp_path / "checkpoints.db"))
    callbacks = FakeCallbacks()
    task = Task(
        id="task-1",
        session_id="session-1",
        title="知乎热点",
        user_request="今天知乎有哪些热门信息",
        locale="zh-CN",
    )

    llm = FakeAgentLLM(
        responses=[
            AIMessage(
                content="",
                tool_calls=[{"id": "hot-1", "name": "hot_list", "args": {"limit": 5}}],
            ),
            AIMessage(
                content="",
                tool_calls=[
                    {
                        "id": "report-1",
                        "name": "create_html_report",
                        "args": {
                            "title": "知乎热点",
                            "summary": "已经整理好知乎热点。",
                            "sections": [{"heading": "摘要", "paragraphs": ["热榜整理完成。"]}],
                            "sources": [{"title": "知乎热榜"}],
                        },
                    }
                ]
            ),
        ]
    )
    executor = FakeToolExecutor()

    await run_task_with_langgraph(task, MockSearchTool(), callbacks, llm=llm, tool_executor=executor)

    assert [event.event_type for _, event in callbacks.events] == [
        "plan.created",
        "agent.tool_call",
        "agent.tool_call",
        "task.completed",
    ]
    assert callbacks.artifacts[0][1].metadata["source_count"] == 1
    assert executor.calls == [("hot_list", {"limit": 5})]
    assert "热榜整理完成" in callbacks.artifacts[0][1].content
    assert [call[0] for call in llm.calls] == ["ainvoke", "ainvoke"]
