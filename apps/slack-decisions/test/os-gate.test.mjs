import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { gateCheck } from "../slack-decisions.mjs";
const run = promisify(execFile);

test("OS gate uses real user information and ignores configurable identities", async () => {
  for (const allowedOsUser of ["kazuph", "someoneelse"]) {
    assert.equal(await gateCheck({ enabled: true, osUser: "kazuph", allowedOsUser }, () => ({ username: "someoneelse" })), false);
  }
  assert.equal(await gateCheck({ enabled: true, osUser: "someoneelse", allowedOsUser: "someoneelse" }, () => ({ username: "kazuph" })), true);
});

for (const mode of ["run", "deliver", "deliver-context"]) {
  test(`${mode} rejects a different OS user before reading credentials`, async () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "slack-os-gate-"));
    const config = path.join(dir, "config.json");
    fs.writeFileSync(config, JSON.stringify({ allowed_os_user: "someoneelse", credentials_env_files: [path.join(dir, "credentials")] }));
    // A real FIFO cannot be read without a writer. Rejection must finish
    // before credential loading attempts to open it.
    await run("/usr/bin/mkfifo", [path.join(dir, "credentials")]);
    const source = `import { main } from ${JSON.stringify(new URL("../slack-decisions.mjs", import.meta.url).href)}; await main({argv:['node','daemon',${JSON.stringify(mode)},'/missing'], userInfo:()=>({username:'someoneelse'})});`;
    try {
      const result = await run(process.execPath, ["--input-type=module", "-e", source], {
        env: { ...process.env, SLACK_DECISIONS_CONFIG: config, SLACK_DECISIONS_OS_USER: "kazuph", SLACK_DECISIONS_ALLOWED_USER: "kazuph", SLACK_DECISIONS_STATE_DIR: dir }, timeout: 3000,
      });
      assert.match(result.stderr, /Slack 送信をしません.*whoami=someoneelse/);
    } finally { fs.rmSync(dir, { recursive: true, force: true }); }
  });
}
