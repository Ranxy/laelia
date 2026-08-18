package pi

import (
	"os"
	"path/filepath"
	"runtime"
)

// windowsPowerShellExtensionTemplate overrides pi's built-in bash tool on
// Windows with a native PowerShell 5.1 backend, so pi agents work without
// requiring Git Bash or another POSIX shell. The command is passed over stdin
// as Base64(UTF-16LE) to avoid cmd.exe command-line length/escaping issues, and
// output is forced to UTF-8 no BOM.
const windowsPowerShellExtensionTemplate = `import { createBashTool } from "@earendil-works/pi-coding-agent";
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { join } from "node:path";

function resolvePowerShellPath() {
  const root = process.env.SystemRoot || process.env.windir || "C:\\Windows";
  const candidates = [
    join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"),
    "C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe",
    "powershell.exe",
  ];
  for (const candidate of candidates) {
    if (candidate === "powershell.exe") return candidate;
    try {
      if (existsSync(candidate)) return candidate;
    } catch {
      // fall through
    }
  }
  return "powershell.exe";
}

const POWERSHELL_STDIN_LOADER = [
  "$laeliaReader = [System.IO.StreamReader]::new([Console]::OpenStandardInput(), [System.Text.Encoding]::ASCII, $false)",
  "try { $laeliaEncodedScript = $laeliaReader.ReadToEnd() } finally { $laeliaReader.Dispose() }",
  "$laeliaScript = [System.Text.Encoding]::Unicode.GetString([System.Convert]::FromBase64String($laeliaEncodedScript))",
  "& ([ScriptBlock]::Create($laeliaScript))",
].join("\r\n");

const POWERSHELL_ARGS = [
  "-NoLogo",
  "-NoProfile",
  "-NonInteractive",
  "-ExecutionPolicy",
  "Bypass",
  "-Command",
  POWERSHELL_STDIN_LOADER,
];

function buildPowerShellScript(command: string): string {
  return [
    "$ErrorActionPreference = 'Stop'",
    "$utf8NoBom = New-Object System.Text.UTF8Encoding($false)",
    "[Console]::OutputEncoding = $utf8NoBom",
    "$OutputEncoding = $utf8NoBom",
    "$global:LASTEXITCODE = 0",
    "& {",
    command,
    "}",
    "$laeliaCommandSucceeded = $?",
    "$laeliaNativeExitCode = $global:LASTEXITCODE",
    "if ($laeliaNativeExitCode -ne 0) { exit $laeliaNativeExitCode }",
    "if (-not $laeliaCommandSucceeded) { exit 1 }",
    "exit 0",
    "",
  ].join("\r\n");
}

function resolveTaskkillPath() {
  const root = process.env.SystemRoot || process.env.windir || "C:\\Windows";
  const candidates = [
    join(root, "System32", "taskkill.exe"),
    "C:\\Windows\\System32\\taskkill.exe",
    "taskkill.exe",
  ];
  for (const candidate of candidates) {
    if (candidate === "taskkill.exe") return candidate;
    try {
      if (existsSync(candidate)) return candidate;
    } catch {
      // fall through
    }
  }
  return "taskkill.exe";
}

function killWindowsProcessTree(pid: number) {
  const killer = spawn(resolveTaskkillPath(), ["/F", "/T", "/PID", String(pid)], {
    detached: true,
    stdio: "ignore",
    windowsHide: true,
  });
  killer.unref();
}

function createWindowsPowerShellChildEnv(env: any) {
  const childEnv = { ...(env ?? process.env) };
  for (const key of Object.keys(childEnv)) {
    if (key.toLowerCase() === "psmodulepath") delete childEnv[key];
  }
  return childEnv;
}

function createPowerShellOperations() {
  return {
    exec: async (command: string, cwd: string, opts: any) => {
      const { onData, signal, timeout, env } = opts;
      if (signal?.aborted) throw new Error("aborted");
      const child = spawn(resolvePowerShellPath(), POWERSHELL_ARGS, {
        cwd,
        env: createWindowsPowerShellChildEnv(env),
        stdio: ["pipe", "pipe", "pipe"],
        windowsHide: true,
      });
      const observeData = (data: any) => onData(data);
      child.stdout.on("data", observeData);
      child.stderr.on("data", observeData);
      child.stdin.on("error", () => {});
      child.stdin.end(Buffer.from(buildPowerShellScript(command), "utf16le").toString("base64"));
      let timedOut = false;
      const terminate = () => {
        if (child.pid) killWindowsProcessTree(child.pid);
      };
      const timeoutHandle = timeout === undefined ? undefined : setTimeout(() => {
        timedOut = true;
        terminate();
      }, timeout * 1000);
      const onAbort = () => terminate();
      signal?.addEventListener("abort", onAbort, { once: true });
      if (signal?.aborted) terminate();
      try {
        const exitCode = await new Promise((resolve, reject) => {
          child.once("error", reject);
          child.once("close", (code) => resolve(code));
        });
        if (signal?.aborted) throw new Error("aborted");
        if (timedOut) throw new Error("timeout:" + timeout);
        return { exitCode };
      } finally {
        if (timeoutHandle) clearTimeout(timeoutHandle);
        signal?.removeEventListener("abort", onAbort);
      }
    },
  };
}

function ensureWindowsPath(env: any) {
  env = env || {};
  const root = process.env.SystemRoot || process.env.windir || "C:\\Windows";
  const system32 = join(root, "System32");
  const psDir = join(system32, "WindowsPowerShell", "v1.0");
  const pathKey = Object.keys(env).find((k) => k.toLowerCase() === "path") || "Path";
  const current = env[pathKey] || "";
  const seen = new Set<string>();
  const merged: string[] = [];
  for (const raw of [psDir, system32, current].join(";").split(";")) {
    const seg = raw.trim();
    if (!seg) continue;
    const key = seg.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    merged.push(seg);
  }
  return { ...env, [pathKey]: merged.join(";") };
}

export default function (pi: any) {
  if (process.platform !== "win32") return;
  const cwd = process.cwd();
  const bashTool = createBashTool(cwd, {
    operations: createPowerShellOperations(),
    spawnHook: ({ command, cwd, env }) => ({
      command,
      cwd,
      env: ensureWindowsPath(env),
    }),
  });
  pi.registerTool({
    ...bashTool,
    name: "bash",
    label: "PowerShell",
    description: "Execute a Windows PowerShell 5.1 command in the current working directory. Use PowerShell syntax, not Bash syntax. Standard output and standard error are returned.",
    promptGuidelines: [
      "- On Windows the bash tool is a compatibility name: it executes native Windows PowerShell 5.1. Use PowerShell syntax and never Bash heredocs or Unix-only commands.",
    ],
  });
}
`

// writeWindowsShellExtension materializes the Windows PowerShell bash override
// into the agent's project-local .pi/extensions directory so pi auto-discovers
// it at session start. No-op on non-Windows platforms.
func writeWindowsShellExtension(cfg *PiConfig) error {
	if cfg == nil || runtime.GOOS != "windows" {
		return nil
	}
	dir := filepath.Join(cfg.WorkingDir, ".pi", "extensions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "laelia-windows-powershell.ts"), []byte(windowsPowerShellExtensionTemplate), 0o600)
}
