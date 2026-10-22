#!/usr/bin/env node
// A single AppKit host presents independent windows for all pending decisions.
// Machine events retain drafts and answers; resolution closes only its own window.

import { execFile, execFileSync, spawn, spawnSync } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import readline from "node:readline";
import { fileURLToPath } from "node:url";

import { MacDialogDecisionsDaemon } from "./daemon.mjs";
import { NativeDialogHost } from "./native-host.mjs";
import { releaseVersion, readOwner, ownerAlive, ownerProven, requestSwitch, cancelSwitch, manageDaemon } from "./lifecycle.mjs";
import { buildDialogSpec, decisionKey, fmtDeadline, locationLabel, durationLabel } from "./spec.mjs";
import { DEFAULT_LATER_MS, log, sleep, daemonPidPath, removeDialogFiles } from "./common.mjs";
export { MacDialogDecisionsDaemon };

const PLUGIN_ID = "kazuph.mac-dialog-decisions";
const RUN_LABEL = "mac-dialog-decisions";
function homePath(...parts) {
  return path.join(os.homedir(), ...parts);
}

function loadConfig(env = process.env) {
  const configDir =
    env.IKESU_PLUGIN_CONFIG_DIR ||
    env.IKESU_PLUGIN_CONFIG_DIR ||
    homePath(".config", "ikesu", "plugins", "config", PLUGIN_ID);
  const configPath =
    env.MAC_DIALOG_CONFIG || path.join(configDir, "config.json");
  let file = {};
  try {
    file = JSON.parse(fs.readFileSync(configPath, "utf8"));
  } catch {
    file = {};
  }
  const stateDir =
    env.MAC_DIALOG_STATE_DIR ||
    env.IKESU_PLUGIN_STATE_DIR ||
    env.IKESU_PLUGIN_STATE_DIR ||
    homePath(".local", "state", "ikesu", "plugins", PLUGIN_ID);
  const pickNumber = (envValue, fileValue, fallback) => {
    const parsed = Number(envValue ?? fileValue);
    return Number.isFinite(parsed) && parsed >= 0 ? parsed : fallback;
  };
  let helperCmd = null;
  if (env.MAC_DIALOG_HELPER_CMD) {
    try {
      const parsed = JSON.parse(env.MAC_DIALOG_HELPER_CMD);
      if (Array.isArray(parsed) && parsed.length > 0) helperCmd = parsed;
    } catch {
      log(`MAC_DIALOG_HELPER_CMD の JSON が読めません: ${env.MAC_DIALOG_HELPER_CMD}`);
    }
  }
  if (!helperCmd && Array.isArray(file.helper_command) && file.helper_command.length > 0) {
    helperCmd = file.helper_command.map(String);
  }
  return {
    enabled:
      (env.MAC_DIALOG_ENABLED ?? file.enabled ?? true) !== false &&
      env.MAC_DIALOG_ENABLED !== "0",
    ikesuBin: env.IKESU_BIN_PATH || file.ikesu_bin || "ikesu",
    helperCmd, // null = built-in osascript dialog.js
    // Brief delay before a queued decision takes the screen, so a burst of
    // simultaneous decisions reports the true queue size on the first dialog.
    presentDelayMs: pickNumber(
      env.MAC_DIALOG_PRESENT_DELAY_MS,
      file.present_delay_ms,
      250,
    ),
    // あとで: how long a dismissed dialog stays hidden before it comes back.
    laterMs: pickNumber(
      env.MAC_DIALOG_LATER_MS,
      file.later_minutes !== undefined ? Number(file.later_minutes) * 60_000 : undefined,
      DEFAULT_LATER_MS,
    ),
    stateDir,
    configDir,
    configPath,
  };
}

// The daemon outlives the server that launched it: a Homebrew install records
// IKESU_BIN_PATH as a versioned Cellar path that `brew upgrade` deletes, so
// every ikesu invocation resolves the binary afresh — the configured path
// while it exists, then PATH, then the usual prefixes.
function resolveIkesuBin(config, logFn = () => {}, env = process.env) {
  const configured = config.ikesuBin;
  if (configured && configured !== "ikesu" && fs.existsSync(configured)) return configured;
  for (const name of ["ikesu"]) {
    const candidates = [...(env.PATH || "").split(path.delimiter).filter(Boolean),
      "/opt/homebrew/bin", path.join(os.homedir(), ".local", "bin")].map(dir => path.join(dir, name));
    for (const candidate of candidates) {
      try { fs.accessSync(candidate, fs.constants.X_OK); return candidate; } catch {}
    }
  }
  logFn("ikesu 実行ファイルが見つかりません");
  return null;
}

function execFileText(file, args, options = {}) {
  return new Promise((resolve) => {
    execFile(
      file,
      args,
      { encoding: "utf8", timeout: options.timeoutMs ?? 30_000, ...options },
      (error, stdout, stderr) => {
        resolve({
          code:
            error && typeof error.code === "number" ? error.code : error ? 1 : 0,
          stdout: String(stdout || ""),
          stderr: String(stderr || (error ? error.message : "")),
        });
      },
    );
  });
}

// --- dialog spec (display text) ------------------------------------------------

// --- process plumbing -------------------------------------------------------------

function pluginRootDir() {
  return path.dirname(fileURLToPath(import.meta.url));
}

function spawnDecisionWatch(config, logFn, env = process.env) {
  const bin = resolveIkesuBin(config, logFn);
  if (!bin) {
    return {
      lines: (async function* () {
        throw new Error(
          `ikesu 実行ファイルが見つかりません (IKESU_BIN_PATH=${config.ikesuBin})。` +
            "PATH・/opt/homebrew/bin・~/.local/bin を確認してください",
        );
      })(),
      close() {},
    };
  }
  const child = spawn(bin, ["decision", "watch", "--all-machines"], {
    env,
    stdio: ["ignore", "pipe", "pipe"],
  });
  child.stderr.on("data", (chunk) => {
    for (const line of String(chunk).split("\n")) {
      if (line.trim()) logFn(`watch stderr: ${line.trim()}`);
    }
  });
  const rl = readline.createInterface({ input: child.stdout });
  let resolveExit;
  const exited = new Promise((resolve) => {
    resolveExit = resolve;
    child.on("exit", resolve);
  });
  return {
    lines: (async function* () {
      try {
        for await (const raw of rl) {
          if (!raw.trim()) continue;
          try {
            yield JSON.parse(raw);
          } catch {
            logFn(`watch の JSON でない行を捨てました: ${raw.slice(0, 120)}`);
          }
        }
        const code = await exited;
        throw new Error(`decision watch exited (code ${code})`);
      } finally {
        rl.close();
        try {
          child.kill();
        } catch {}
      }
    })(),
    close() {
      rl.close();
      try {
        child.kill();
      } catch {}
      resolveExit?.(0);
    },
  };
}

// Runs the dialog as a plain child process: the compiled native panel, or a
// MAC_DIALOG_HELPER_CMD stand-in in tests. It prints JSON lines on stdout; the
// first {"result": ...} line is the user's answer and {"event":"shown"} marks
// that the window is on screen.
function spawnHelperDialog(config, specPath, logFn = () => {}) {
  const argv = config.helperCmd;
  const child = spawn(argv[0], [...argv.slice(1), specPath], {
    stdio: ["pipe", "pipe", "pipe"],
  });
  child.stdin.on("error", error => logFn(`dialog stdin: ${error.message}`));
  let spawnError;
  child.on("error", error => {
    spawnError = error;
    logFn(`dialog spawn に失敗: ${error.message} (argv=${argv.join(" ")})`);
  });
  child.on("exit", (code, signal) =>
    logFn(`dialog が終了: code=${code} signal=${signal}`),
  );
  child.stderr.on("data", (chunk) => {
    for (const line of String(chunk).split("\n")) {
      if (line.trim()) logFn(`dialog stderr: ${line.trim()}`);
    }
  });
  const rl = readline.createInterface({ input: child.stdout });
  const exited = new Promise((resolve) => {
    child.once("close", (code, signal) => resolve({code, signal, error: spawnError}));
  });
  return {
    lines: (async function* () {
      try {
        for await (const raw of rl) {
          if (!raw.trim()) continue;
          yield raw;
        }
      } finally {
        rl.close();
      }
    })(),
    exited,
    pid: child.pid,
    respond(message) {
      if (!child.stdin.destroyed && child.exitCode === null) child.stdin.write(JSON.stringify(message) + "\n");
    },
    kill() {
      try {
        child.kill("SIGTERM");
      } catch {}

    },
  };
}

// The native panel (dialog.swift) is compiled once per source version into
// the plugin state dir and run directly, so its stdout carries the result and
// a signal closes it. swiftc must use Xcode's default toolchain: a
// DEVELOPER_DIR pointing at CommandLineTools makes the SDK and compiler
// disagree and the build fails.
function nativeSources() {
  return ["dialog.swift", "main.swift", "host.swift", "preview.swift", "unicode-width.swift"].map(name => path.join(pluginRootDir(), name));
}

function nativeDialogPath(config) {
  const hash = nativeSources().reduce((hash, source) => hash.update(fs.readFileSync(source)), createHash("sha256")).digest("hex")
    .slice(0, 12);
  return path.join(config.stateDir, "bin", `ikesu-decision-dialog-${hash}`);
}

function ensureNativeDialog(config, logFn = () => {}) {
  const root = pluginRootDir();
  const app = path.join(root, "IkesuDecisionDialog.app");
  const installedRoot = homePath(".local", "share", "ikesu", "mac-dialog-decisions");
  if (fs.existsSync(path.join(root, "CHANNEL")) || path.dirname(root) === installedRoot || fs.existsSync(app)) {
    try {
      if (fs.readFileSync(path.join(root, "CHANNEL"), "utf8").trim() !== "release") throw Error("release CHANNEL is missing or invalid");
      const requirement = 'anchor apple generic and certificate leaf[subject.OU] = "67M8FDFZ28" and certificate leaf[subject.CN] = "Developer ID Application: KAZUHIRO HOMMA (67M8FDFZ28)"';
      execFileSync("codesign", ["--verify", "--deep", "--strict", "-R", requirement, app], {stdio: "pipe"});
      const executable = path.join(app, "Contents", "MacOS", "ikesu-decision-dialog");
      fs.accessSync(executable, fs.constants.X_OK);
      logFn(`署名済み配布パネルを起動します: ${executable}`);
      return executable;
    } catch (error) {
      logFn(`配布パネルの検証に失敗（swiftcへは戻りません）: ${error.message}`);
      return null;
    }
  }
  logFn("開発用パネルを使用します (swiftc)");
  const binary = nativeDialogPath(config);
  if (fs.existsSync(binary)) return binary;
  fs.mkdirSync(path.dirname(binary), { recursive: true });
  const tmp = `${binary}.tmp-${process.pid}`;
  const env = { ...process.env };
  delete env.DEVELOPER_DIR;
  logFn("ダイアログをビルドします (swiftc、初回は1〜2分かかります)");
  const res = spawnSync("swiftc", ["-O", ...nativeSources(), "-o", tmp], {
    encoding: "utf8",
    env,
    timeout: 600_000,
  });
  if (res.error || res.status !== 0) {
    logFn(
      `ダイアログのビルドに失敗: ${(res.stderr || res.error?.message || String(res.status)).slice(0, 2000)}`,
    );
    try {
      fs.rmSync(tmp, { force: true });
    } catch {}
    return null;
  }
  fs.renameSync(tmp, binary);
  logFn(`ダイアログをビルドしました: ${binary}`);
  return binary;
}

async function acquireDaemonLock(binary, stateDir) {
  fs.mkdirSync(stateDir, {recursive: true});
  const child = spawn(binary, ["--daemon-lock", path.join(stateDir, "run.lock")], {stdio: ["pipe", "pipe", "pipe"]});
  const lines = readline.createInterface({input: child.stdout});
  const exited = new Promise(resolve => child.once("close", resolve));
  child.stdin.on("error", () => {});
  const acquired = await new Promise((resolve, reject) => {
    child.once("error", reject);
    lines.once("line", line => { try { resolve(JSON.parse(line).acquired === true); } catch (error) { reject(error); } });
    child.once("close", () => resolve(false));
  });
  lines.close();
  if (!acquired) { child.stdin.end(); await exited; return null; }
  return {exited, async close() { child.stdin.end(); await exited; }};
}

function createRunAnswer(config, logFn = () => {}, env = process.env) {
  return (args) => {
    const bin = resolveIkesuBin(config, logFn);
    if (!bin) {
      return Promise.resolve({
        code: 127,
        stdout: "",
        stderr:
          `ikesu 実行ファイルが見つかりません (IKESU_BIN_PATH=${config.ikesuBin})。` +
          "PATH・/opt/homebrew/bin・~/.local/bin を確認してください",
      });
    }
    return execFileText(bin, args, { timeoutMs: 60_000, env });
  };
}

function createGetDecision(config, logFn = () => {}, env = process.env) {
  const run = createRunAnswer(config, logFn, env);
  return async (machine, id) => {
    const response = await run(["decision", "get", id, "--machine", machine]);
    if (response.code !== 0) throw Error(response.stderr || response.stdout || "decision get failed");
    const decision = JSON.parse(response.stdout)?.result?.decision;
    if (!decision) throw Error("decision get returned no saved decision");
    return decision;
  };
}

// --- singleton ensure ----------------------------------------------------------------

function pidAlive(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

function readDaemonPid(stateDir) {
  try {
    const parsed = JSON.parse(fs.readFileSync(daemonPidPath(stateDir), "utf8"));
    return Number(parsed.pid) || 0;
  } catch {
    return 0;
  }
}

function daemonRunning(stateDir) {
  try {
    const owner = readOwner(stateDir);
    return ownerProven(owner,stateDir) && owner.script === fileURLToPath(import.meta.url);
  } catch { return false; }
}

function writeDaemonPid(stateDir, owner) {
  fs.mkdirSync(stateDir, { recursive: true });
  fs.writeFileSync(
    daemonPidPath(stateDir),
    JSON.stringify({ pid: process.pid, started: Date.now(), owner, stateDir: fs.realpathSync(stateDir), script: fileURLToPath(import.meta.url), node: process.execPath, root: pluginRootDir(), version: releaseVersion(pluginRootDir()) }),
    { mode: 0o600 },
  );
}

function shellQuote(value) {
  return `'${String(value).replace(/'/g, "'\\''")}'`;
}

async function registeredPluginRoot(config, env = process.env) {
  const bin = resolveIkesuBin(config, log, env);
  if (!bin) throw Error("cannot verify plugin registration: ikesu executable unavailable");
  const response = await execFileText(bin,["plugin","list","--json"],{env});
  if (response.code!==0) throw Error(`cannot verify plugin registration: ${response.stderr || response.stdout}`);
  const plugins=JSON.parse(response.stdout)?.result?.plugins;
  if (!Array.isArray(plugins)) throw Error("invalid plugin registry response");
  const entry=plugins.find(p=>p.plugin_id===PLUGIN_ID && p.enabled);
  return entry ? path.dirname(entry.manifest_path) : null;
}

async function ensureDaemon(config, env) {
  fs.mkdirSync(config.stateDir, { recursive: true });
  const lockDir = path.join(config.stateDir, "ensure.lock");
  // mkdir is atomic: it serializes concurrent event-hook invocations.
  for (;;) {
    try {
      fs.mkdirSync(lockDir);
      break;
    } catch {
      let age = Number.MAX_SAFE_INTEGER;
      try {
        age = Date.now() - fs.statSync(lockDir).mtimeMs;
      } catch {}
      if (age > 60_000) {
        fs.rmSync(lockDir, { recursive: true, force: true });
        continue;
      }
      await sleep(200);
      if (age > 10_000) return; // another ensure is mid-flight
    }
  }
  try {
    const registered = await registeredPluginRoot(config,env);
    if (!registered) throw Error("Mac dialog plugin is not registered and enabled");
    if (registered !== pluginRootDir()) { log("登録先が変わったため、この版は起動しません"); return; }
    if (daemonRunning(config.stateDir)) {
      cancelSwitch(config.stateDir);
      log(`デーモンは起動済み (pid ${readDaemonPid(config.stateDir)})`);
      return;
    }
    const owner = readOwner(config.stateDir);
    if (ownerAlive(owner)) {
      requestSwitch(config.stateDir,pluginRootDir());
      log(ownerProven(owner,config.stateDir) ? "旧版に安全な切替を要求しました" : "稼働版不明・切替待ち（強制停止しません）");
      return;
    }
    cancelSwitch(config.stateDir);
    let startedAt = 0;
    try {
      startedAt = JSON.parse(
        fs.readFileSync(path.join(config.stateDir, "starting.json"), "utf8"),
      ).started;
    } catch {}
    if (Date.now() - startedAt < 30_000) {
      log("起動中のデーモンがあるため何もしません");
      return;
    }

    const pluginRoot = pluginRootDir();
    const exportsList = [
      ["IKESU_PLUGIN_CONFIG_DIR", config.configDir],
      ["IKESU_PLUGIN_STATE_DIR", config.stateDir],
      ["IKESU_SOCKET_PATH", env.IKESU_SOCKET_PATH || ""],
      ["IKESU_SOCKET_EXPLICIT", (env.IKESU_SOCKET_PATH) ? "1" : ""],
      ["IKESU_SOCKET_PATH_EXPLICIT", (env.IKESU_SOCKET_PATH) ? "1" : ""],
      ["IKESU_CONFIG_PATH", env.IKESU_CONFIG_PATH || ""],
      ["IKESU_BIN_PATH", resolveIkesuBin(config, log) || config.ikesuBin],
      ["PATH", env.PATH || ""],
    ]
      .filter(([, value]) => value)
      .map(([key, value]) => `${key}=${shellQuote(value)}`)
      .join(" ");
    const inner = `export ${exportsList}; exec node ${shellQuote(path.join(pluginRoot, "mac-dialog-decisions.mjs"))} run`;
    const daemonBin = resolveIkesuBin(config, log);
    if (!daemonBin) {
      throw Error(
        `ikesu 実行ファイルが見つかりません (IKESU_BIN_PATH=${config.ikesuBin})。` +
          "デーモンを起動できません",
      );
    }
    const result = await execFileText(
      daemonBin,
      [
        "run",
        ...(env.IKESU_PANE_ID ? ["--caller", env.IKESU_PANE_ID] : []),
        "--label",
        RUN_LABEL,
        "--cwd",
        pluginRoot,
        "--completion",
        "none",
        "--",
        "sh",
        "-c",
        inner,
      ],
      { timeoutMs: 15_000 },
    );
    if (result.code !== 0) {
      throw Error(`ikesu run に失敗: ${(result.stderr || result.stdout).trim()}`);
    }
    fs.writeFileSync(
      path.join(config.stateDir, "starting.json"),
      JSON.stringify({ started: Date.now() }),
      { mode: 0o600 },
    );
    log(`デーモンを ikesu run ジョブとして起動しました (label=${RUN_LABEL})`);
  } finally {
    fs.rmSync(lockDir, { recursive: true, force: true });
  }
}

// --- entry point ----------------------------------------------------------------------

async function gateCheck(config) {
  if (process.platform !== "darwin") {
    log("macOS 以外では動きません (platforms=[\"macos\"])");
    return false;
  }
  if (!config.enabled) {
    log("設定で無効化されています (enabled=false)");
    return false;
  }
  return true;
}

async function main() {
  const mode = process.argv[2] || "run";
  const config = loadConfig(process.env);

  if (!(await gateCheck(config))) return;

  if (mode === "ensure") {
    await ensureDaemon(config, process.env);
    return;
  }

  if (mode !== "run") {
    process.stderr.write("usage: mac-dialog-decisions.mjs [run|ensure]\n");
    process.exit(2);
  }

  const nativeDialog = ensureNativeDialog(config, log);
  if (!nativeDialog) throw new Error("native dialog build failed");
  const lock = await acquireDaemonLock(nativeDialog, config.stateDir);
  if (!lock) { log("同じstateのデーモンは起動済みです"); return; }
  config.daemonOwner = randomUUID();
  writeDaemonPid(config.stateDir, config.daemonOwner);
  // Only the lifetime lock owner may replace this state's specs. Old panels
  // detect the changed owner token; no process-name matching or killing.
  const specDir = path.join(config.stateDir, "dialogs");
  fs.rmSync(specDir, {recursive: true, force: true});
  const host = config.helperCmd ? null : new NativeDialogHost(nativeDialog, specDir, log);
  let lifecycle;
  const daemon = new MacDialogDecisionsDaemon({
    config,
    onFatal: () => shutdown(1),
    spawnWatch: () => Promise.resolve(spawnDecisionWatch(config, log)),
    runAnswer: createRunAnswer(config, log),
    getDecision: createGetDecision(config, log),
    spawnDialog: (specPath) => host ? host.open(specPath) : spawnHelperDialog(
        { ...config, helperCmd: config.helperCmd || [nativeDialog] },
        specPath,
        log,
      ),
    specFilePath: (key) =>
      path.join(specDir, `${createHash("sha256").update(key).digest("hex")}.json`),
    log,
  });
  let shuttingDown = false;
  const shutdown = (exitCode = 0, nextRoot = null) => {
    if (shuttingDown) return;
    shuttingDown = true;
    void (async () => {
      try {
        lifecycle?.stop();
        await daemon.stop();
        await host?.stop();
        try {
          const owner = JSON.parse(fs.readFileSync(daemonPidPath(config.stateDir), "utf8"));
          if (owner.owner === config.daemonOwner) fs.rmSync(daemonPidPath(config.stateDir));
        } catch (error) { if (error.code !== "ENOENT") log(`所有情報の削除に失敗: ${error.message}`); }
      } finally {
        await lock.close();
        if (nextRoot) {
          try {
            if (await registeredPluginRoot(config) === nextRoot) {
              const result = await execFileText(process.execPath,[path.join(nextRoot,"mac-dialog-decisions.mjs"),"ensure"],{env:process.env});
              if (result.code!==0) log(`次の版の起動に失敗: ${result.stderr || result.stdout}`);
            } else log("切替先が再変更されたため、古い切替先は起動しません");
          } catch (error) { log(`次の版の起動確認に失敗: ${error.message}`); }
        }
        process.exit(exitCode);
      }
    })();
  };
  void lock.exited.then(() => {
    if (!shuttingDown) { log("所有ロックのプロセスが終了したためデーモンを停止します"); shutdown(); }
  });
  process.on("SIGINT", () => shutdown());
  process.on("SIGTERM", () => shutdown());
  lifecycle = manageDaemon(daemon,config,{root:pluginRootDir(),registeredRoot:()=>registeredPluginRoot(config),onSwitch:root=>shutdown(0,root),log});
  await daemon.start();
  log("watch 開始 (ikesu decision watch --all-machines)");
}

const invokedAsScript = (() => {
  const entry = process.argv[1];
  if (!entry) return false;
  try {
    return (
      fs.realpathSync(entry) === fs.realpathSync(fileURLToPath(import.meta.url))
    );
  } catch {
    return false;
  }
})();

if (invokedAsScript) {
  main().catch((error) => {
    log(`fatal: ${error?.stack || error}`);
    process.exit(1);
  });
}

export {
  removeDialogFiles,
  acquireDaemonLock,
  spawnDecisionWatch,
  buildDialogSpec,
  createRunAnswer,
  createGetDecision,
  decisionKey,
  fmtDeadline,
  loadConfig,
  locationLabel,
  resolveIkesuBin,
  spawnHelperDialog,
  ensureNativeDialog,
  ensureDaemon,
};
