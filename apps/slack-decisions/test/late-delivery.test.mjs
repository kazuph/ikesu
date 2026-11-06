import { test } from "node:test";
import assert from "node:assert/strict";
import { buildDecisionBlocks } from "../slack-decisions.mjs";

test("配達待ちと配達失敗は回答文を表示し、再回答を案内しない", () => {
  const text = "さっきの質問『色は？』への回答：青（メモ：濃い青）";
  const decision = {
    decision_id: "dec-test", kind: "agent_prompt", title: "色は？",
    options: [{ id: "1", label: "青", role: "other" }],
    status: "answered", answer: { option_id: "1", text: "濃い青" },
  };
  for (const [status, reason, expected] of [
    ["pending", "", "配達待ち：Codex が入力を待つ状態になったら、会話で届けます"],
    ["failed", "ペインが閉じました", "届けられなかった：ペインが閉じました"],
  ]) {
    const blocks = buildDecisionBlocks({
      decision: { ...decision, delivery: { status, text, reason } },
      machine: { id: "local" }, status: "answered",
    });
    const rendered = JSON.stringify(blocks);
    assert.ok(rendered.includes(expected), rendered);
    assert.ok(rendered.includes(text), rendered);
    assert.ok(!rendered.includes("数字リアクションで選択"), rendered);
  }
});
