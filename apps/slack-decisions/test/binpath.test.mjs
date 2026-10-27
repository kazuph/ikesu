// resolveIkesuBin / createRunAnswer: a missing IKESU_BIN_PATH (e.g. a
// versioned Homebrew Cellar path removed by `brew upgrade`) must fall back
// to PATH and the usual prefixes instead of failing every spawn.

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRunAnswer, resolveIkesuBin } from "../slack-decisions.mjs";

const GONE = "/opt/homebrew/Cellar/ikesu/0.2.35/bin/ikesu";

function fakeBinDir() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "ikesu-bin-"));
  const bin = path.join(dir, "ikesu");
  fs.writeFileSync(bin, "#!/bin/sh\necho fake-ikesu $*\n", { mode: 0o755 });
  return { dir, bin };
}

test("missing IKESU_BIN_PATH falls back to a ikesu on PATH", () => {
  const { dir, bin } = fakeBinDir();
  const originalPath = process.env.PATH;
  process.env.PATH = `${dir}${path.delimiter}${originalPath}`;
  try {
    assert.equal(
      resolveIkesuBin({ ikesuBin: GONE }),
      bin,
    );
  } finally {
    process.env.PATH = originalPath;
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("existing IKESU_BIN_PATH wins over the fallbacks", () => {
  const { dir, bin } = fakeBinDir();
  try {
    assert.equal(resolveIkesuBin({ ikesuBin: bin }), bin);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("runAnswer still sends the answer when IKESU_BIN_PATH is gone", async () => {
  const { dir } = fakeBinDir();
  const originalPath = process.env.PATH;
  process.env.PATH = `${dir}${path.delimiter}${originalPath}`;
  try {
    const runAnswer = createRunAnswer({ ikesuBin: GONE });
    const result = await runAnswer(["decision", "answer", "dec-x", "1"]);
    assert.equal(result.code, 0);
    assert.match(result.stdout, /fake-ikesu decision answer dec-x 1/);
  } finally {
    process.env.PATH = originalPath;
    fs.rmSync(dir, { recursive: true, force: true });
  }
});
