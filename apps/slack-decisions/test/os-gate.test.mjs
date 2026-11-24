import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { gateCheck } from "../slack-decisions.mjs";
const run = promisify(execFile);

test("OS gate allows any user when allowedUser is unset", async () => {
  assert.equal(await gateCheck({ enabled: true }, () => ({ username: "alice" })), true);
  assert.equal(await gateCheck({ enabled: true, allowedUser: "" }, () => ({ username: "bob" })), true);
  assert.equal(await gateCheck({ enabled: true, allowedUser: "   " }, () => ({ username: "bob" })), true);
});

test("OS gate compares the real user with the configured allowed user and ignores spoofed identities", async () => {
  assert.equal(await gateCheck({ enabled: true, allowedUser: "dealforest", osUser: "dealforest" }, () => ({ username: "someoneelse" })), false);
  assert.equal(await gateCheck({ enabled: true, allowedUser: " dealforest " }, () => ({ username: "dealforest" })), true);
  assert.equal(await gateCheck({ enabled: true, allowedUser: "dealforest", osUser: "someoneelse", allowedOsUser: "someoneelse" }, () => ({ username: "dealforest" })), true);
  assert.equal(await gateCheck({ enabled: true, allowedUser: "dealforest" }, () => ({ username: "alice" })), false);
  assert.equal(await gateCheck({ enabled: true, allowedUser: 12345 }, () => ({ username: "alice" })), true);
});

for (const mode of ["run", "deliver", "deliver-context"]) {
  test(`${mode} rejects a different OS user configured via config.json before reading credentials`, async () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "slack-os-gate-"));
    const config = path.join(dir, "config.json");
    fs.writeFileSync(config, JSON.stringify({ allowed_user: "alice", credentials_env_files: [path.join(dir, "credentials")] }));
    // A real FIFO cannot be read without a writer. Rejection must finish
    // before credential loading attempts to open it.
    await run("/usr/bin/mkfifo", [path.join(dir, "credentials")]);
    const source = `import { main } from ${JSON.stringify(new URL("../slack-decisions.mjs", import.meta.url).href)}; await main({argv:['node','daemon',${JSON.stringify(mode)},'/missing'], userInfo:()=>({username:'someoneelse'})});`;
    try {
      const result = await run(process.execPath, ["--input-type=module", "-e", source], {
        env: { ...process.env, SLACK_DECISIONS_CONFIG: config, SLACK_DECISIONS_OS_USER: "alice", SLACK_DECISIONS_ALLOWED_USER: "", SLACK_DECISIONS_STATE_DIR: dir }, timeout: 3000,
      });
      assert.match(result.stderr, /Slack 送信をしません.*whoami=someoneelse; 許可: alice/);
    } finally { fs.rmSync(dir, { recursive: true, force: true }); }
  });
}

test("SLACK_DECISIONS_ALLOWED_USER overrides config.json allowed_user", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "slack-os-gate-env-"));
  const config = path.join(dir, "config.json");
  fs.writeFileSync(config, JSON.stringify({ allowed_user: "someoneelse", credentials_env_files: [path.join(dir, "credentials")] }));
  await run("/usr/bin/mkfifo", [path.join(dir, "credentials")]);
  const source = `import { main } from ${JSON.stringify(new URL("../slack-decisions.mjs", import.meta.url).href)}; await main({argv:['node','daemon','run'], userInfo:()=>({username:'someoneelse'})});`;
  try {
    const result = await run(process.execPath, ["--input-type=module", "-e", source], {
      env: { ...process.env, SLACK_DECISIONS_CONFIG: config, SLACK_DECISIONS_ALLOWED_USER: "alice", SLACK_DECISIONS_STATE_DIR: dir }, timeout: 3000,
    });
    assert.match(result.stderr, /Slack 送信をしません.*whoami=someoneelse; 許可: alice/);
  } finally { fs.rmSync(dir, { recursive: true, force: true }); }
});
