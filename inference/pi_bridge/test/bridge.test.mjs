import assert from "node:assert/strict";
import { mkdtemp } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { handleLine, runTask } from "../src/bridge.mjs";

async function makeTempDir(prefix) {
  return mkdtemp(path.join(os.tmpdir(), prefix));
}

function makeMockDeps(options = {}) {
  const calls = {
    resourceOptions: undefined,
    reloadOptions: undefined,
    createAgentSession: undefined,
    registerProvider: undefined,
    runtimeApiKey: undefined,
    prompt: undefined,
    aborted: false,
    disposed: false,
  };
  let listener;
  let resolvePrompt;

  class MockResourceLoader {
    constructor(resourceOptions) {
      calls.resourceOptions = resourceOptions;
    }

    async reload(reloadOptions) {
      calls.reloadOptions = reloadOptions;
    }

    getExtensions() {
      return { errors: [] };
    }
  }

  const session = {
    subscribe(nextListener) {
      listener = nextListener;
      return () => {};
    },
    async prompt(text, promptOptions) {
      calls.prompt = { text, promptOptions };
      listener?.({ type: "agent_start" });
      if (options.blockPrompt) {
        await new Promise((resolve) => {
          resolvePrompt = resolve;
        });
      }
      if (options.messageEndContent !== undefined) {
        listener?.({
          type: "message_end",
          message: {
            role: "assistant",
            content: options.messageEndContent,
          },
        });
      }
      const agentEndMessages = options.agentEndMessages ?? [
        {
          role: "assistant",
          content: JSON.stringify({
            summary: "done",
            artifacts: [
              {
                title: "Report",
                type: "markdown",
                mime_type: "text/markdown; charset=utf-8",
                content: "# done",
              },
            ],
          }),
        },
      ];
      listener?.({
        type: "agent_end",
        messages: agentEndMessages,
      });
    },
    async abort() {
      calls.aborted = true;
      resolvePrompt?.();
    },
    dispose() {
      calls.disposed = true;
    },
  };

  return {
    calls,
    deps: {
      AuthStorage: {
        create(authPath) {
          return {
            authPath,
            setRuntimeApiKey(provider, apiKey) {
              calls.runtimeApiKey = { provider, apiKey };
            },
          };
        },
      },
      createAgentSession: async (sessionOptions) => {
        calls.createAgentSession = sessionOptions;
        return { session, modelFallbackMessage: "" };
      },
      DefaultResourceLoader: MockResourceLoader,
      getModel(provider, modelId) {
        return { provider, id: modelId, source: "builtin" };
      },
      ModelRegistry: {
        create(_authStorage, modelsPath) {
          return {
            modelsPath,
            find(provider, modelId) {
              return { provider, id: modelId, source: "registry" };
            },
            registerProvider(provider, config) {
              calls.registerProvider = { provider, config };
            },
          };
        },
      },
      SessionManager: {
        create(cwd, sessionDir, sessionOptions) {
          return { cwd, sessionDir, sessionOptions };
        },
      },
      SettingsManager: {
        inMemory(settings) {
          return { settings };
        },
      },
    },
  };
}

function baseRequest(paths) {
  return {
    id: "task-1",
    type: "run_task",
    task: {
      id: "task-1",
      title: "Research",
      user_request: "Summarize materials",
      locale: "zh-CN",
    },
    context: {
      character_id: "char-1",
      workspace: paths.workspace,
      session_dir: paths.sessionDir,
      session_id: "role-char-1",
      agent_dir: paths.agentDir,
      allowed_packages: ["npm:@pi/allowed"],
      allowed_skills: ["skill-a"],
      allowed_tools: ["custom_tool"],
      extension_paths: [paths.extensionPath],
      extension_package_urls: ["npm:@pi/research"],
      provider: "openai",
      model: "gpt-test",
      no_builtin_tools: true,
      settings: {
        compaction: { enabled: false, reserveTokens: 4096 },
        terminal: { showImages: false },
      },
    },
    prompt: "Do the task",
  };
}

test("runTask passes role-scoped SDK options and only explicit resources", async () => {
  const workspace = await makeTempDir("cyberverse-pi-workspace-");
  const agentDir = await makeTempDir("cyberverse-pi-agent-");
  const sessionDir = await makeTempDir("cyberverse-pi-session-");
  const request = baseRequest({
    workspace,
    agentDir,
    sessionDir,
    extensionPath: "/explicit/extension.mjs",
  });
  const { calls, deps } = makeMockDeps();
  const events = [];

  await runTask(request, undefined, deps, (event) => events.push(event));

  assert.equal(calls.createAgentSession.cwd, workspace);
  assert.equal(calls.createAgentSession.agentDir, agentDir);
  assert.equal(calls.createAgentSession.sessionManager.cwd, workspace);
  assert.equal(calls.createAgentSession.sessionManager.sessionDir, sessionDir);
  assert.equal(calls.createAgentSession.sessionManager.sessionOptions.id, "role-char-1-task-1");
  assert.deepEqual(calls.createAgentSession.model, {
    provider: "openai",
    id: "gpt-test",
    source: "registry",
  });
  assert.deepEqual(calls.createAgentSession.settingsManager.settings, {
    defaultProjectTrust: "never",
    compaction: { enabled: false, reserveTokens: 4096 },
    terminal: { showImages: false },
  });
  assert.deepEqual(calls.createAgentSession.tools, [
    "custom_tool",
    "cyberverse_progress",
    "cyberverse_create_artifact",
  ]);
  assert.equal(calls.createAgentSession.noTools, undefined);
  assert.deepEqual(calls.resourceOptions.additionalExtensionPaths, [
    "/explicit/extension.mjs",
    "npm:@pi/research",
    "npm:@pi/allowed",
  ]);
  assert.deepEqual(calls.resourceOptions.additionalSkillPaths, ["skill-a"]);
  assert.equal(calls.resourceOptions.noExtensions, true);
  assert.equal(calls.resourceOptions.noSkills, true);
  assert.equal(calls.resourceOptions.noPromptTemplates, true);
  assert.equal(calls.resourceOptions.noContextFiles, true);
  assert.equal(typeof calls.resourceOptions.extensionFactories[0], "function");
  assert.equal(calls.prompt.text, "Do the task");
  assert.deepEqual(calls.prompt.promptOptions, { expandPromptTemplates: true, source: "rpc" });
  assert.equal(calls.disposed, true);
  assert.equal(events.some((event) => event.type === "artifact" && event.artifact.title === "Report"), true);
  assert.equal(events.at(-1).type, "completed");
});

test("runTask registers configured OpenAI-compatible provider with runtime key", async () => {
  const workspace = await makeTempDir("cyberverse-pi-workspace-");
  const agentDir = await makeTempDir("cyberverse-pi-agent-");
  const sessionDir = await makeTempDir("cyberverse-pi-session-");
  const request = baseRequest({
    workspace,
    agentDir,
    sessionDir,
    extensionPath: "/explicit/extension.mjs",
  });
  request.context.provider = "qwen";
  request.context.model = "qwen3.6-plus";
  request.context.provider_api = "openai-completions";
  request.context.provider_base_url = "${CYBERVERSE_TEST_QWEN_BASE_URL}";
  request.context.provider_api_key_env = "CYBERVERSE_TEST_QWEN_KEY";
  const previousKey = process.env.CYBERVERSE_TEST_QWEN_KEY;
  const previousBaseUrl = process.env.CYBERVERSE_TEST_QWEN_BASE_URL;
  process.env.CYBERVERSE_TEST_QWEN_KEY = "test-key";
  process.env.CYBERVERSE_TEST_QWEN_BASE_URL = "https://dashscope.test/compatible-mode/v1";
  const { calls, deps } = makeMockDeps();

  try {
    await runTask(request, undefined, deps, () => {});
  } finally {
    if (previousKey === undefined) {
      delete process.env.CYBERVERSE_TEST_QWEN_KEY;
    } else {
      process.env.CYBERVERSE_TEST_QWEN_KEY = previousKey;
    }
    if (previousBaseUrl === undefined) {
      delete process.env.CYBERVERSE_TEST_QWEN_BASE_URL;
    } else {
      process.env.CYBERVERSE_TEST_QWEN_BASE_URL = previousBaseUrl;
    }
  }

  assert.deepEqual(calls.runtimeApiKey, { provider: "qwen", apiKey: "test-key" });
  assert.equal(calls.registerProvider.provider, "qwen");
  assert.equal(calls.registerProvider.config.api, "openai-completions");
  assert.equal(calls.registerProvider.config.baseUrl, "https://dashscope.test/compatible-mode/v1");
  assert.equal(calls.registerProvider.config.apiKey, "$CYBERVERSE_TEST_QWEN_KEY");
  assert.equal(calls.registerProvider.config.models[0].id, "qwen3.6-plus");
  assert.equal(calls.createAgentSession.model.provider, "qwen");
});

test("runTask creates a fallback result artifact from final assistant text", async () => {
  const workspace = await makeTempDir("cyberverse-pi-workspace-");
  const agentDir = await makeTempDir("cyberverse-pi-agent-");
  const sessionDir = await makeTempDir("cyberverse-pi-session-");
  const request = baseRequest({
    workspace,
    agentDir,
    sessionDir,
    extensionPath: "/explicit/extension.mjs",
  });
  const { deps } = makeMockDeps({
    messageEndContent: "风险：权限过宽。验证：检查 allowlist。",
    agentEndMessages: [],
  });
  const events = [];

  await runTask(request, undefined, deps, (event) => events.push(event));

  const artifact = events.find((event) => event.type === "artifact")?.artifact;
  assert.equal(artifact?.title, "Research");
  assert.equal(artifact?.type, "markdown");
  assert.match(artifact?.content, /风险：权限过宽/);
  assert.equal(events.at(-1).type, "completed");
  assert.equal(events.at(-1).artifact_count, 1);
});

test("runTask rejects empty successful Pi SDK runs", async () => {
  const workspace = await makeTempDir("cyberverse-pi-workspace-");
  const agentDir = await makeTempDir("cyberverse-pi-agent-");
  const sessionDir = await makeTempDir("cyberverse-pi-session-");
  const request = baseRequest({
    workspace,
    agentDir,
    sessionDir,
    extensionPath: "/explicit/extension.mjs",
  });
  const { deps } = makeMockDeps({ agentEndMessages: [] });

  await assert.rejects(
    () => runTask(request, undefined, deps, () => {}),
    /without assistant output or artifacts/,
  );
});

test("handleLine accepts cancel while run_task is active", async () => {
  const workspace = await makeTempDir("cyberverse-pi-workspace-");
  const agentDir = await makeTempDir("cyberverse-pi-agent-");
  const sessionDir = await makeTempDir("cyberverse-pi-session-");
  const request = baseRequest({
    workspace,
    agentDir,
    sessionDir,
    extensionPath: "/explicit/extension.mjs",
  });
  request.id = "task-cancel";
  const { calls, deps } = makeMockDeps({ blockPrompt: true });
  const events = [];
  const activeRuns = new Map();

  await handleLine(JSON.stringify(request), activeRuns, deps, (event) => events.push(event));
  assert.equal(activeRuns.has("task-cancel"), true);

  await waitFor(() => calls.createAgentSession !== undefined);
  const runState = activeRuns.get("task-cancel");
  await handleLine(JSON.stringify({ id: "task-cancel", type: "cancel" }), activeRuns, deps, (event) => events.push(event));
  await runState.promise;

  assert.equal(calls.aborted, true);
  assert.equal(calls.disposed, true);
  assert.equal(events.some((event) => event.type === "progress" && event.event_type === "subagent.cancel_requested"), true);
  assert.deepEqual(events.at(-1), { id: "task-cancel", type: "failed", error: "cancelled" });
});

async function waitFor(predicate) {
  for (let i = 0; i < 50; i += 1) {
    if (predicate()) {
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  assert.fail("condition was not met");
}
