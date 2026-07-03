import { mkdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { getModel } from "@earendil-works/pi-ai/compat";
import {
  AuthStorage,
  createAgentSession,
  DefaultResourceLoader,
  defineTool,
  ModelRegistry,
  SessionManager,
  SettingsManager,
} from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const defaultAgentDir = path.resolve(__dirname, "../.pi-agent");
const defaultDeps = {
  AuthStorage,
  createAgentSession,
  DefaultResourceLoader,
  getModel,
  ModelRegistry,
  SessionManager,
  SettingsManager,
};

export function emit(event) {
  process.stdout.write(`${JSON.stringify(event)}\n`);
}

function emitProgress(message, progress, payload = undefined, eventType = "subagent.progress", emitEvent = emit) {
  emitEvent({
    type: "progress",
    event_type: eventType,
    message,
    progress: clampProgress(progress),
    ...(payload && typeof payload === "object" ? { payload } : {}),
  });
}

function clampProgress(value) {
  const parsed = Number.parseInt(String(value ?? ""), 10);
  if (!Number.isFinite(parsed)) return 30;
  return Math.max(1, Math.min(99, parsed));
}

function asArray(value) {
  if (!Array.isArray(value)) return [];
  return value.map((item) => String(item ?? "").trim()).filter(Boolean);
}

function asObject(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : {};
}

function asString(value) {
  return String(value ?? "").trim();
}

function safeSessionId(value) {
  const clean = String(value ?? "")
    .replace(/[^A-Za-z0-9_.-]+/g, "_")
    .replace(/^[._-]+|[._-]+$/g, "");
  return clean || `cyberverse-${Date.now()}`;
}

function isPlaceholderCredential(value) {
  const text = String(value ?? "").trim().toLowerCase();
  if (!text) return true;
  return (
    text === "your_api_key" ||
    text.startsWith("your_") ||
    text.endsWith("_api_key") ||
    text.includes("placeholder")
  );
}

function mergeSettings(base, override) {
  const merged = { ...asObject(base) };
  for (const [key, value] of Object.entries(asObject(override))) {
    if (value && typeof value === "object" && !Array.isArray(value) && merged[key] && typeof merged[key] === "object" && !Array.isArray(merged[key])) {
      merged[key] = mergeSettings(merged[key], value);
    } else {
      merged[key] = value;
    }
  }
  return merged;
}

function resolveSimpleEnvValue(value) {
  const text = asString(value);
  const match = text.match(/^\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?$/);
  if (!match) return text;
  return asString(process.env[match[1]] || text);
}

function textFromContent(content) {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  return content
    .map((part) => {
      if (typeof part === "string") return part;
      if (part && typeof part === "object" && typeof part.text === "string") return part.text;
      return "";
    })
    .join("");
}

function messageText(message) {
  if (!message || typeof message !== "object") return "";
  return textFromContent(message.content ?? message.parts ?? message.text ?? "");
}

function assistantErrorText(message) {
  if (!message || typeof message !== "object") return "";
  if (message.role !== "assistant") return "";
  const stopReason = asString(message.stopReason);
  const errorMessage = asString(message.errorMessage);
  if (stopReason === "error" && errorMessage) return errorMessage;
  return "";
}

function assistantEventText(value) {
  const event = asObject(value);
  if (typeof event.delta === "string") return event.delta;
  if (typeof event.text === "string") return event.text;
  if (typeof event.content === "string") return event.content;
  const delta = asObject(event.delta);
  if (typeof delta.text === "string") return delta.text;
  return "";
}

function lastAssistantText(messages, fallback) {
  for (let i = messages.length - 1; i >= 0; i -= 1) {
    const message = messages[i];
    if (message && typeof message === "object" && message.role === "assistant") {
      const text = messageText(message).trim();
      if (text) return text;
    }
  }
  return fallback.trim();
}

function parseJsonObject(text) {
  const trimmed = String(text ?? "").trim();
  if (!trimmed) return null;
  const candidates = [trimmed];
  if (trimmed.startsWith("```")) {
    candidates.push(trimmed.replace(/^```(?:json)?\s*/i, "").replace(/\s*```$/i, ""));
  }
  const start = trimmed.indexOf("{");
  const end = trimmed.lastIndexOf("}");
  if (start >= 0 && end > start) {
    candidates.push(trimmed.slice(start, end + 1));
  }
  for (const candidate of candidates) {
    try {
      const parsed = JSON.parse(candidate);
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) return parsed;
    } catch {
      // Keep trying looser candidates.
    }
  }
  return null;
}

function normalizeArtifact(value) {
  const artifact = asObject(value);
  const content = String(artifact.content ?? "");
  if (!content.trim()) return null;
  return {
    title: String(artifact.title ?? "SubAgent artifact").trim() || "SubAgent artifact",
    type: String(artifact.type ?? "markdown").trim() || "markdown",
    mime_type: String(artifact.mime_type ?? artifact.mimeType ?? "text/markdown; charset=utf-8").trim(),
    content,
  };
}

function createCyberVerseExtension(state, emitEvent = emit) {
  const progressTool = defineTool({
    name: "cyberverse_progress",
    label: "CyberVerse Progress",
    description: "Report visible progress for the current CyberVerse background task.",
    parameters: Type.Object({
      message: Type.String({ description: "Short progress message for the user." }),
      progress: Type.Optional(Type.Number({ minimum: 1, maximum: 99 })),
    }),
    async execute(_toolCallId, params) {
      const message = String(params.message ?? "SubAgent is working.").trim();
      const progress = clampProgress(params.progress ?? state.progress + 10);
      state.progress = Math.max(state.progress, progress);
      emitProgress(message, state.progress, undefined, "subagent.progress", emitEvent);
      return {
        content: [{ type: "text", text: "Progress reported to CyberVerse." }],
        details: { progress: state.progress },
      };
    },
  });

  const artifactTool = defineTool({
    name: "cyberverse_create_artifact",
    label: "CyberVerse Artifact",
    description: "Create a visible deliverable artifact for the current CyberVerse background task.",
    parameters: Type.Object({
      title: Type.String({ description: "Artifact title." }),
      type: Type.Optional(Type.String({ description: "Artifact type, for example markdown or text." })),
      mime_type: Type.Optional(Type.String({ description: "MIME type." })),
      content: Type.String({ description: "Artifact body content." }),
    }),
    async execute(_toolCallId, params) {
      const artifact = normalizeArtifact(params);
      if (!artifact) {
        return {
          content: [{ type: "text", text: "No artifact was created because content was empty." }],
          details: { created: false },
        };
      }
      state.artifacts.push(artifact);
      emitEvent({ type: "artifact", artifact });
      return {
        content: [{ type: "text", text: `Artifact created: ${artifact.title}` }],
        details: { created: true, title: artifact.title },
      };
    },
  });

  return (pi) => {
    pi.registerTool(progressTool);
    pi.registerTool(artifactTool);
  };
}

function buildFallbackArtifact(request, text) {
  const task = asObject(request.task);
  const title = String(task.title ?? "SubAgent 结果").trim() || "SubAgent 结果";
  const contentText = String(text ?? "").trim();
  if (!contentText) return null;
  const content = contentText.startsWith("#") ? contentText : `# ${title}\n\n${contentText}`;
  return normalizeArtifact({
    title,
    type: "markdown",
    mime_type: "text/markdown; charset=utf-8",
    content,
  });
}

function buildSystemPrompt(request) {
  const task = asObject(request.task);
  const context = asObject(request.context);
  return [
    "You are a CyberVerse role-scoped background SubAgent running through Pi SDK.",
    "Work only for the current role and current task.",
    "Report progress with cyberverse_progress when you begin meaningful work, after intermediate milestones, and before completion.",
    "Create a concise final markdown deliverable with cyberverse_create_artifact before completing whenever the task asks for research, analysis, planning, comparison, or a reusable result.",
    "The main CyberVerse agent continues talking to the user while you work; keep reports concise and factual.",
    `Task id: ${String(task.id ?? request.id ?? "")}`,
    `Character id: ${String(context.character_id ?? "")}`,
  ];
}

function registerConfiguredProvider(context, authStorage, modelRegistry) {
  const provider = asString(context.provider);
  const modelId = asString(context.model);
  const api = asString(context.provider_api || context.api);
  const baseUrl = resolveSimpleEnvValue(context.provider_base_url || context.base_url);
  if (!provider || !modelId || !api || !baseUrl || typeof modelRegistry.registerProvider !== "function") {
    return;
  }
  const apiKeyEnv = asString(context.provider_api_key_env || context.api_key_env);
  const apiKey = apiKeyEnv ? process.env[apiKeyEnv] : "";
  if (apiKey && !isPlaceholderCredential(apiKey) && typeof authStorage.setRuntimeApiKey === "function") {
    authStorage.setRuntimeApiKey(provider, apiKey);
  }
  modelRegistry.registerProvider(provider, {
    name: provider,
    baseUrl,
    api,
    ...(apiKeyEnv ? { apiKey: `$${apiKeyEnv}` } : {}),
    authHeader: true,
    models: [
      {
        id: modelId,
        name: modelId,
        api,
        baseUrl,
        reasoning: false,
        input: ["text"],
        cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
        contextWindow: 131072,
        maxTokens: 8192,
        compat: api === "openai-completions" ? { supportsDeveloperRole: false, supportsReasoningEffort: false } : undefined,
      },
    ],
  });
}

export function buildSessionOptions(request, state, deps = defaultDeps, emitEvent = emit) {
  const context = asObject(request.context);
  const cwd = String(context.workspace || process.cwd());
  const agentDir = String(context.agent_dir || process.env.CYBERVERSE_PI_AGENT_DIR || defaultAgentDir);
  const sessionDir = String(context.session_dir || process.env.CYBERVERSE_PI_SESSION_DIR || path.join(agentDir, "sessions"));
  const extensionSources = [
    ...asArray(context.extension_paths),
    ...asArray(context.extension_package_urls),
    ...asArray(context.allowed_packages),
  ];
  const settingsManager = deps.SettingsManager.inMemory(mergeSettings({
    defaultProjectTrust: "never",
    compaction: { enabled: true },
  }, context.settings));
  const authStorage = deps.AuthStorage.create(path.join(agentDir, "auth.json"));
  const modelRegistry = deps.ModelRegistry.create(authStorage, path.join(agentDir, "models.json"));
  registerConfiguredProvider(context, authStorage, modelRegistry);
  const resourceLoader = new deps.DefaultResourceLoader({
    cwd,
    agentDir,
    settingsManager,
    additionalExtensionPaths: extensionSources,
    additionalSkillPaths: asArray(context.allowed_skills),
    extensionFactories: [createCyberVerseExtension(state, emitEvent)],
    noExtensions: true,
    noSkills: true,
    noPromptTemplates: true,
    noThemes: true,
    noContextFiles: true,
    appendSystemPrompt: buildSystemPrompt(request),
  });
  const allowedTools = asArray(context.allowed_tools);
  const tools =
    allowedTools.length > 0
      ? [...new Set([...allowedTools, "cyberverse_progress", "cyberverse_create_artifact"])]
      : undefined;
  const provider = String(context.provider ?? "").trim();
  const modelId = String(context.model ?? "").trim();
  const model = provider && modelId ? modelRegistry.find(provider, modelId) ?? deps.getModel(provider, modelId) : undefined;

  return {
    cwd,
    agentDir,
    sessionDir,
    sessionId: safeSessionId(`${String(context.session_id || "role")}-${String(request.id || Date.now())}`),
    model,
    modelRegistry,
    authStorage,
    settingsManager,
    resourceLoader,
    tools,
    noTools: tools ? undefined : context.no_builtin_tools === false ? undefined : "builtin",
    extensionSources,
  };
}

export async function runTask(request, state = undefined, deps = defaultDeps, emitEvent = emit) {
  state ??= { progress: 5, artifacts: [], session: undefined, cancelRequested: false };
  if (state.cancelRequested) {
    throw new Error("cancelled");
  }
  const options = buildSessionOptions(request, state, deps, emitEvent);
  await mkdir(options.agentDir, { recursive: true });
  await mkdir(options.sessionDir, { recursive: true });
  emitProgress("SubAgent 正在加载 Pi SDK 角色运行时。", 10, {
    extension_count: options.extensionSources.length,
  }, "subagent.bridge_started", emitEvent);

  const extensionWarnings = [];
  options.resourceLoader.getExtensions?.();
  await options.resourceLoader.reload({
    resolveProjectTrust: async () => false,
  });
  const extensionResult = options.resourceLoader.getExtensions();
  for (const error of extensionResult.errors ?? []) {
    const message = error?.message ? String(error.message) : String(error ?? "");
    if (message) extensionWarnings.push(message);
  }
  if (extensionWarnings.length > 0) {
    emitProgress("部分角色扩展未能加载，SubAgent 将继续执行可用能力。", 15, {
      warnings: extensionWarnings.slice(0, 5),
    }, "subagent.extension_warning", emitEvent);
  }

  const { session, modelFallbackMessage } = await deps.createAgentSession({
    cwd: options.cwd,
    agentDir: options.agentDir,
    model: options.model,
    authStorage: options.authStorage,
    modelRegistry: options.modelRegistry,
    resourceLoader: options.resourceLoader,
    settingsManager: options.settingsManager,
    sessionManager: deps.SessionManager.create(options.cwd, options.sessionDir, { id: options.sessionId }),
    tools: options.tools,
    noTools: options.noTools,
    sessionStartEvent: {
      type: "session_start",
      reason: "new",
    },
  });
  state.session = session;
  if (state.cancelRequested) {
    await abortSession(session);
    throw new Error("cancelled");
  }

  let streamedAssistantText = "";
  let latestAssistantText = "";
  let latestAssistantError = "";
  let agentEndMessages = [];
  try {
    if (modelFallbackMessage) {
      emitProgress(modelFallbackMessage, 20, undefined, "subagent.model_fallback", emitEvent);
    }
    session.subscribe((event) => {
      if (event.type === "agent_start") {
        state.progress = Math.max(state.progress, 20);
        emitProgress("SubAgent 已开始执行任务。", state.progress, undefined, "subagent.started", emitEvent);
      } else if (event.type === "tool_execution_start") {
        state.progress = Math.max(state.progress, 45);
        emitProgress(`SubAgent 正在调用工具：${event.toolName}`, state.progress, {
          tool_name: event.toolName,
        }, "subagent.tool_started", emitEvent);
      } else if (event.type === "message_update") {
        const deltaText = assistantEventText(event.assistantMessageEvent);
        if (deltaText) streamedAssistantText += deltaText;
        const text = messageText(event.message).trim();
        if (text && event.message?.role === "assistant") latestAssistantText = text;
        const errorText = assistantErrorText(event.message);
        if (errorText) latestAssistantError = errorText;
      } else if (event.type === "message_end" && event.message?.role === "assistant") {
        const text = messageText(event.message).trim();
        if (text) latestAssistantText = text;
        const errorText = assistantErrorText(event.message);
        if (errorText) latestAssistantError = errorText;
      } else if (event.type === "turn_end" && event.message?.role === "assistant") {
        const text = messageText(event.message).trim();
        if (text) latestAssistantText = text;
        const errorText = assistantErrorText(event.message);
        if (errorText) latestAssistantError = errorText;
      } else if (event.type === "agent_end") {
        agentEndMessages = Array.isArray(event.messages) ? event.messages : [];
        for (let i = agentEndMessages.length - 1; i >= 0; i -= 1) {
          const errorText = assistantErrorText(agentEndMessages[i]);
          if (errorText) {
            latestAssistantError = errorText;
            break;
          }
        }
      }
    });

    await session.prompt(String(request.prompt ?? ""), {
      expandPromptTemplates: true,
      source: "rpc",
    });
    if (state.cancelRequested) {
      throw new Error("cancelled");
    }
  } finally {
    session.dispose();
    state.session = undefined;
  }

  const finalText = lastAssistantText(agentEndMessages, latestAssistantText || streamedAssistantText);
  const parsed = parseJsonObject(finalText);
  const parsedArtifacts = Array.isArray(parsed?.artifacts) ? parsed.artifacts : [];
  for (const item of parsedArtifacts) {
    const artifact = normalizeArtifact(item);
    if (!artifact) continue;
    if (state.artifacts.some((existing) => existing.title === artifact.title && existing.content === artifact.content)) {
      continue;
    }
    state.artifacts.push(artifact);
    emitEvent({ type: "artifact", artifact });
  }
  const summary = String(parsed?.summary ?? parsed?.result ?? parsed?.message ?? finalText ?? "").trim();
  if (state.artifacts.length === 0) {
    const fallbackArtifact = buildFallbackArtifact(request, summary || finalText);
    if (fallbackArtifact) {
      state.artifacts.push(fallbackArtifact);
      emitEvent({ type: "artifact", artifact: fallbackArtifact });
    }
  }
  if (state.artifacts.length === 0 && !summary) {
    if (latestAssistantError) {
      throw new Error(latestAssistantError);
    }
    throw new Error("Pi SDK subagent finished without assistant output or artifacts");
  }
  emitEvent({
    type: "completed",
    summary: summary || "后台任务已完成。",
    artifact_count: state.artifacts.length,
  });
}

async function abortSession(session) {
  if (!session || typeof session.abort !== "function") {
    return;
  }
  await session.abort();
}

export async function requestCancel(request, activeRuns, emitEvent = emit) {
  const id = String(request?.id ?? "").trim();
  const state = id ? activeRuns.get(id) : [...activeRuns.values()].at(-1);
  if (!state) {
    emitEvent({
      id,
      type: "failed",
      error: id ? `no active task for cancel id: ${id}` : "no active task to cancel",
    });
    return;
  }
  state.cancelRequested = true;
  await abortSession(state.session);
  emitProgress("SubAgent 取消请求已发送。", state.progress || 5, undefined, "subagent.cancel_requested", emitEvent);
}

export async function handleLine(line, activeRuns, deps = defaultDeps, emitEvent = emit) {
  if (!line.trim()) return;
  let request;
  try {
    request = JSON.parse(line);
  } catch (error) {
    throw new Error(`Invalid JSONL request: ${error instanceof Error ? error.message : String(error)}`);
  }
  if (request?.type === "cancel") {
    await requestCancel(request, activeRuns, emitEvent);
    return;
  }
  if (request?.type !== "run_task") {
    throw new Error(`Unsupported bridge request type: ${String(request?.type ?? "")}`);
  }
  const id = String(request.id ?? "").trim();
  if (!id) {
    throw new Error("run_task request requires id");
  }
  if (activeRuns.has(id)) {
    throw new Error(`duplicate run_task id: ${id}`);
  }
  const state = { id, progress: 5, artifacts: [], session: undefined, cancelRequested: false };
  const promise = runTask(request, state, deps, emitEvent)
    .catch((error) => {
      const message = state.cancelRequested
        ? "cancelled"
        : error instanceof Error
        ? error.message
        : String(error);
      emitEvent({ id, type: "failed", error: message });
      if (!state.cancelRequested) {
        process.stderr.write(`${message}\n`);
        process.exitCode = 1;
      }
    })
    .finally(() => {
      activeRuns.delete(id);
    });
  state.promise = promise;
  activeRuns.set(id, state);
}

async function* readJsonlLines(input) {
  input.setEncoding("utf8");
  let buffer = "";
  for await (const chunk of input) {
    buffer += chunk;
    let index = buffer.indexOf("\n");
    while (index >= 0) {
      const line = buffer.slice(0, index).replace(/\r$/, "");
      buffer = buffer.slice(index + 1);
      yield line;
      index = buffer.indexOf("\n");
    }
  }
  if (buffer.length > 0) {
    yield buffer.replace(/\r$/, "");
  }
}

export async function main(input = process.stdin, deps = defaultDeps, emitEvent = emit) {
  const activeRuns = new Map();
  try {
    for await (const line of readJsonlLines(input)) {
      await handleLine(line, activeRuns, deps, emitEvent);
    }
    await Promise.allSettled([...activeRuns.values()].map((state) => state.promise));
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    emitEvent({ type: "failed", error: message });
    process.stderr.write(`${message}\n`);
    process.exitCode = 1;
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await main();
}
