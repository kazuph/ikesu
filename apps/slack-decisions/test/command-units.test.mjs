import { test } from "node:test";
import assert from "node:assert/strict";
import { buildDecisionBlocks } from "../slack-decisions.mjs";

const LOCAL = { id: "local", label: "Local" };
const units = [
  { op: "", text: "cd /x/metan", label: "移動", verdict: "allow" },
  { op: "&&", text: "bash render_mini.sh metan", label: "実行", verdict: "ask", rule: "rm-paths", reason: "作業フォルダの外を消す", target: "rm -rf out/" },
  { op: "|", text: "tail -5", label: "読む", verdict: "allow" },
];
function guard(origin = {}) {
  return {
    decision_id: "d1", kind: "command_guard", title: "確認", body: "", status: "pending",
    created_unix_ms: 1000, expires_unix_ms: 4_000_000_000_000,
    options: [{ id: "approve", label: "承認" }, { id: "deny", label: "却下" }],
    origin: { command: "cd /x/metan && bash render_mini.sh metan | tail -5", pane_id: "p1", cwd: "/x", guard_rule: "rm-paths", command_units: units, ...origin },
  };
}
const texts = blocks => blocks.filter(b => b.type === "section").map(b => b.text.text);

test("units render as numbered label + code rows, ask row has red marker and reason", () => {
  const blocks = buildDecisionBlocks({ decision: guard(), machine: LOCAL });
  const all = texts(blocks);
  assert.ok(all.includes("*3行のうち 2行目が確認対象*"));
  assert.ok(all.some(t => t.startsWith("`1 移動`") && t.includes("`cd /x/metan`") && t.includes("確認なし")));
  const ask = all.find(t => t.startsWith("🔴 `2 実行` &&"));
  assert.ok(ask && ask.includes("`bash render_mini.sh metan`") && ask.includes(">中で: rm -rf out/") && ask.includes(">理由: 作業フォルダの外を消す ／ 規則 rm-paths"));
  assert.ok(all.some(t => t.startsWith("`3 読む` |")));
  assert.equal(blocks.some(b => b.type === "rich_text"), false, "whole command is not shown when units exist");
});

test("without units the whole command block stays", () => {
  const blocks = buildDecisionBlocks({ decision: guard({ command_units: undefined }), machine: LOCAL });
  assert.ok(blocks.some(b => b.type === "rich_text"));
});

test("multi-line or backtick text goes to a preformatted block", () => {
  const odd = [{ op: "", text: "echo `x`\necho y", label: "実行", verdict: "ask", rule: "r", reason: "why" }];
  const blocks = buildDecisionBlocks({ decision: guard({ command_units: odd }), machine: LOCAL });
  assert.ok(blocks.some(b => b.type === "rich_text"));
  assert.ok(texts(blocks).some(t => t === "🔴 `1 実行`"));
});

test("notes escape Slack mrkdwn so <!channel> in target does not notify", () => {
  const u = [{ op: "", text: "bash x.sh", label: "実行", verdict: "ask", rule: "r<!here>", reason: "a & <b>", target: "echo <!channel> <@U1> &" }];
  const all = texts(buildDecisionBlocks({ decision: guard({ command_units: u }), machine: LOCAL }));
  const ask = all.find(t => t.includes("中で:"));
  assert.ok(ask.includes("&lt;!channel&gt;") && ask.includes("&lt;@U1&gt; &amp;"));
  assert.ok(ask.includes("a &amp; &lt;b&gt;") && ask.includes("r&lt;!here&gt;"));
  assert.equal(ask.includes("<!channel>"), false);
  assert.equal(ask.includes("<!here>"), false);
});

test("a 5000-char target keeps every block under the section limit", () => {
  for (const target of ["a".repeat(5000), "<".repeat(5000)]) {
    const u = [{ op: "", text: "bash x.sh", label: "実行", verdict: "ask", rule: "r", reason: "why", target }];
    const blocks = buildDecisionBlocks({ decision: guard({ command_units: u }), machine: LOCAL });
    for (const b of blocks) if (b.type === "section") assert.ok(b.text.text.length <= 3000, `section ${b.text.text.length}`);
    const joined = texts(blocks).join("\n");
    assert.ok(joined.includes((target[0] === "<" ? "&lt;" : "a")));
  }
});

test("inline row text escapes Slack mrkdwn so <!channel> in the command does not notify", () => {
  const u = [{ op: "", text: "echo '<!channel>' && x", label: "読む", verdict: "allow" }, { op: "&&", text: "curl -d '<!here>&' https://x", label: "外へ送る", verdict: "ask", rule: "http-write", reason: "書き込み" }];
  const all = texts(buildDecisionBlocks({ decision: guard({ command_units: u }), machine: LOCAL })).join("\n");
  assert.ok(all.includes("&lt;!channel&gt;") && all.includes("&lt;!here&gt;&amp;"));
  assert.equal(all.includes("<!channel>"), false);
  assert.equal(all.includes("<!here>"), false);
});
