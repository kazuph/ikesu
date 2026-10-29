import { discoverRichContent } from "./rich.mjs";
import path from "node:path";
import { DEFAULT_LATER_MS } from "./common.mjs";

export function fmtDeadline(decision, now = Date.now()) {
  if (!decision.expires_unix_ms) return "期限なし";
  const at = new Date(decision.expires_unix_ms);
  const remainSec = Math.max(
    0,
    Math.round((decision.expires_unix_ms - now) / 1000),
  );
  const hh = String(at.getHours()).padStart(2, "0");
  const mm = String(at.getMinutes()).padStart(2, "0");
  const remain =
    remainSec >= 120 ? `${Math.round(remainSec / 60)}分` : `${remainSec}秒`;
  return `${hh}:${mm}（残り約${remain}）`;
}

export function optionDescription(option) {
  return typeof option?.description === "string" ? option.description.trim() : "";
}

export function optionPreview(option) {
  return typeof option?.preview === "string" ? option.preview.trimEnd() : "";
}

export function durationLabel(ms) {
  if (ms >= 60_000) return `${Math.round(ms / 60_000)}分`;
  return `${Math.max(1, Math.round(ms / 1000))}秒`;
}

export function sourceLabel(decision) {
  const origin = decision.origin || {};
  if (origin.agent) return `🤖 ${origin.agent}`;
  const kinds = {
    command_guard: "🛡 危険コマンドの承認",
    tool_permission: "🔐 ツールの許可",
    agent_prompt: "💬 エージェントの質問",
    ask: "❓ 確認",
  };
  return kinds[decision.kind] || "";
}

// All four fields are explicit, even when origin metadata is missing.
export function locationLabel(decision, machine) {
  const origin = decision.origin || {};
  const id = machine?.id || origin.machine;
  const host = id === "local" ? "ローカル" : machine?.label || id || "不明";
  const project = origin.cwd ? path.basename(origin.cwd) : "不明";
  const space = origin.workspace_label || "不明";
  const pane = [origin.pane_label || origin.pane_id, origin.agent].filter(Boolean).join(" / ") || "不明";
  return [host, project, space, pane].join("｜");
}

export function approvalCommand(decision) {
  return decision.kind === "command_guard" ? String(decision.origin?.target_command || decision.origin?.command || "") : "";
}

// Screen prompts can store the whole question in title, with either a copy
// or the question-progress label in body. Keep that question once and use a
// separate, non-repeated header only when the source actually supplies one.
export function decisionText(decision) {
  const title = String(decision.title || "");
  const body = String(decision.body || "");
  const questionLabel = String(decision.question_label || "");
  if (decision.kind !== "agent_prompt") {
    return {
      title: title || String(decision.decision_id || ""),
      body: body.trim() !== questionLabel.trim() ? body : "",
      question_label: questionLabel,
    };
  }
  const normalize = text => text.replace(/\s+/gu, " ").trim();
  const separateBody = body.trim() && normalize(body) !== normalize(questionLabel);
  let question = separateBody ? body : title || body;
  // A question number is a heading, not the question itself. A separate
  // supplement (for example Codex's countdown) must follow the full question.
  if (questionLabel.trim() && title.trim() && separateBody && !normalize(body).includes(normalize(title))) {
    question = title + "\n\n" + body;
  }
  const heading = questionLabel.trim() ? questionLabel : title;
  const repeated = normalize(question).startsWith(normalize(heading));
  return {title: heading.trim() && !repeated ? heading : "", body: question, question_label: ""};
}

export function projectName(decision) {
  const origin = decision.origin || {};
  if (origin.workspace_label) return origin.workspace_label;
  if (origin.cwd) return path.basename(origin.cwd);
  return "";
}

export function buildDialogSpec(decision, machine, queueRemaining = 0, options = {}) {
  const origin = decision.origin || {};
  const details = [];
  if (origin.command) {
    details.push({ label: "コマンド", value: String(origin.command), monospace: true });
  }
  if (origin.target_command && origin.target_command !== origin.command) {
    details.push({ label: "対象コマンド", value: String(origin.target_command), monospace: true });
  }
  if (origin.cwd) details.push({ label: "場所", value: String(origin.cwd), monospace: true });
  if (origin.branch) details.push({ label: "ブランチ", value: String(origin.branch), monospace: true });
  if (origin.reason && origin.reason !== "screen_prompt") {
    details.push({ label: "理由", value: String(origin.reason) });
  }
  if (origin.pane_id) details.push({ label: "ペイン", value: String(origin.pane_id), monospace: true });

  const machineId = machine?.id || origin.machine || "local";
  const remote = machineId !== "local";
  const project = projectName(decision);
  return {
    media_sources: (() => { const media = discoverRichContent(decision); return [...new Set([...media.links, ...media.images, ...(media.videos || [])])]; })(),
    kind: decision.kind || "",
    machine_id: machineId,
    window_title: project ? `ikesu 判断待ち — ${project}` : "ikesu 判断待ち",
    source: sourceLabel(decision),
    location: locationLabel(decision, machine),
    approval_command: approvalCommand(decision),
    machine: remote ? String(machine?.label || machineId) : "",
    ...decisionText(decision),
    details,
    options: (Array.isArray(decision.options) ? decision.options : []).map(
      (option) => ({
        id: String(option.id),
        label: String(option.label || option.id),
        description: optionDescription(option),
        preview: optionPreview(option),
        preview_unavailable: option?.preview_unavailable === true,
      }),
    ),
    multi_select: decision.multi_select === true,
    allow_text: decision.allow_text === true,
    deadline: decision.expires_unix_ms ? `期限 ${fmtDeadline(decision)}` : "",
    queue_remaining: queueRemaining,
    later_label: durationLabel(options.laterMs ?? DEFAULT_LATER_MS),
    daemon_pid: options.daemonPid ?? process.pid,
    decision_id: String(decision.decision_id || ""),
  };
}

// --- daemon ---------------------------------------------------------------------

export function decisionKey(machineId, decisionId) {
  return `${machineId} ${decisionId}`;
}

export function decisionIdOf(decision) {
  return String(decision?.decision_id || "");
}
