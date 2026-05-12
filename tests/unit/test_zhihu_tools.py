from agent_runtime.tools import ZhihuClient, ZhihuConfig, ZhihuToolExecutor, zhihu_config_from_runtime_config


class FakeResponse:
    def __init__(self, payload):
        self.payload = payload

    def raise_for_status(self):
        pass

    def json(self):
        return self.payload


class FakeHTTPClient:
    def __init__(self):
        self.calls = []

    async def get(self, url, params=None, headers=None):
        self.calls.append(("GET", url, params, headers))
        return FakeResponse(
            {
                "Code": 0,
                "Message": "success",
                "Data": {
                    "Total": 1,
                    "Items": [
                        {
                            "Title": "标题",
                            "Url": "https://www.zhihu.com/question/1",
                            "ContentType": "Question",
                            "Summary": "摘要",
                            "ContentText": "内容",
                            "AuthorName": "作者",
                            "VoteUpCount": 12,
                            "CommentCount": 3,
                        }
                    ],
                },
            }
        )

    async def post(self, url, json=None, headers=None):
        self.calls.append(("POST", url, json, headers))
        return FakeResponse(
            {
                "choices": [
                    {
                        "message": {
                            "content": "直答结果",
                            "reasoning_content": "简要推理",
                        }
                    }
                ]
            }
        )


async def test_zhihu_client_sets_auth_timestamp_and_clamps_params():
    http = FakeHTTPClient()
    client = ZhihuClient(
        ZhihuConfig(access_secret="secret", api_base="https://mock.zhihu", zhida_model="zhida-fast-1p5"),
        http_client=http,
    )

    zhihu_result = await client.zhihu_search("AI Agent", count=99)
    global_result = await client.global_search("AI Agent", count=99)
    hot_result = await client.hot_list(limit=0)
    zhida_result = await client.zhida("怎么理解 AI Agent")

    assert zhihu_result["items"][0]["title"] == "标题"
    assert global_result["items"][0]["vote_up_count"] == 12
    assert hot_result["limit"] == 1
    assert zhida_result["answer"] == "直答结果"

    assert http.calls[0][2] == {"Query": "AI Agent", "Count": 10}
    assert http.calls[1][2] == {"Query": "AI Agent", "Count": 20}
    assert http.calls[2][2] == {"Limit": 1}
    assert http.calls[3][2]["model"] == "zhida-fast-1p5"
    for call in http.calls:
        headers = call[3]
        assert headers["Authorization"] == "Bearer secret"
        assert headers["X-Request-Timestamp"].isdigit()


async def test_zhihu_tool_executor_returns_structured_error_for_missing_secret():
    executor = ZhihuToolExecutor(ZhihuClient(ZhihuConfig(access_secret="")))

    result = await executor.execute("hot_list", {"limit": 5})

    assert result["ok"] is False
    assert result["tool"] == "hot_list"
    assert "ZHIHU_ACCESS_SECRET" in result["error"]


def test_zhihu_config_reads_persona_runtime_config():
    config = {
        "inference": {
            "persona": {
                "persona": {
                    "tools": {
                        "zhihu": {
                            "access_secret": "secret",
                            "api_base": "https://example.com",
                            "timeout_seconds": 7,
                            "zhida_model": "zhida-agent",
                        }
                    }
                }
            }
        }
    }

    zhihu = zhihu_config_from_runtime_config(config)

    assert zhihu.access_secret == "secret"
    assert zhihu.api_base == "https://example.com"
    assert zhihu.timeout_seconds == 7
    assert zhihu.zhida_model == "zhida-agent"
