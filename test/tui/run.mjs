import { spawnSync } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { ShellUse } from "@microsoft/shell-use";
import { Resvg } from "@resvg/resvg-js";

const testRoot = dirname(fileURLToPath(import.meta.url));
const toolRoot = resolve(testRoot, "../..");
const mode = process.argv[2] ?? "semantic";
const updateGoldens = process.env.UPDEV_UPDATE_TUI_GOLDENS === "1";
const fixtureTmpRoot = process.platform === "win32" ? tmpdir() : "/tmp";
const workRoot = mkdtempSync(join(fixtureTmpRoot, "updev-tui-"));
const visualWorkRoot = process.platform === "win32" ? workRoot : "/tmp/updev-tui-xxxxxx";
const artifactRoot = join(testRoot, "artifacts", mode);
const terminalBaselineRoot = join(testRoot, "baselines", "terminal");
const visualBaselineRoot = join(testRoot, "baselines", "visual");
const odiff = join(testRoot, "node_modules", ".bin", process.platform === "win32" ? "odiff.cmd" : "odiff");
const inventoryFixtureExpectations = process.platform === "darwin"
  ? { rowCount: 5, readyText: "firefox", secondRowIdentity: "brew / brew / ripgrep" }
  : { rowCount: 2, readyText: "node", secondRowIdentity: "mise / tool / node" };

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    cwd: toolRoot,
    encoding: "utf8",
    stdio: options.stdio ?? "pipe",
    env: options.env ?? process.env,
  });
  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(" ")} failed (${result.status})\n${result.stdout ?? ""}${result.stderr ?? ""}`);
  }
  return result;
}

function writeExecutable(path, content) {
  writeFileSync(path, content, { mode: 0o755 });
}

function prepareFixture() {
  rmSync(workRoot, { recursive: true, force: true });
  rmSync(artifactRoot, { recursive: true, force: true });
  const home = join(workRoot, "home");
  const bin = join(workRoot, "bin");
  const source = join(workRoot, "source");
  const cache = join(workRoot, "cache");
  const state = join(workRoot, "state");
  const data = join(workRoot, "data");
  const config = join(workRoot, "config");
  const temp = join(workRoot, "tmp");
  for (const dir of [home, bin, source, cache, state, data, config, temp, artifactRoot, terminalBaselineRoot, visualBaselineRoot, join(source, "dot_config", "mise"), join(config, "updev")]) {
    mkdirSync(dir, { recursive: true });
  }

  writeExecutable(join(bin, "brew"), `#!/bin/sh
case "$*" in
  "list --formula -1") printf 'ripgrep\\njq\\nfd\\n' ;;
  "list --cask -1") printf 'firefox\\n' ;;
  "info --json=v2"*) printf '{"formulae":[],"casks":[]}\\n' ;;
  "outdated --json=v2"*) printf '{"formulae":[],"casks":[]}\\n' ;;
  "--version") printf 'Homebrew 9.9.9\\n' ;;
  *) exit 0 ;;
esac
`);
  writeExecutable(join(bin, "mise"), `#!/bin/sh
if [ "$1" = "--version" ]; then printf 'mise 2099.1.0\\n'; exit 0; fi
if [ "$1" = "settings" ] && [ "$2" = "ls" ]; then printf '{}\\n'; exit 0; fi
if [ "$1" = "ls" ] && [ "$2" = "--current" ] && [ "$3" = "--json" ]; then
  printf '{"ripgrep":[{"version":"14.1.1","requested_version":"14.1.1","installed":true,"active":true}],"node":[{"version":"24.16.0","requested_version":"lts","installed":true,"active":true}]}\\n'
  exit 0
fi
if [ "$1" = "ls" ] && [ "$2" = "--json" ]; then printf '[]\\n'; exit 0; fi
exit 0
`);
  writeExecutable(join(bin, "gh"), "#!/bin/sh\nexit 1\n");

  writeFileSync(join(source, "Brewfile.tmpl"), 'brew "ripgrep"\nbrew "jq"\ncask "firefox"\n');
  writeFileSync(join(source, "dot_config", "mise", "config.toml"), '[tools]\nripgrep = "14.1.1"\nnode = "lts"\n');
  writeFileSync(join(config, "updev", "config.toml"), `[sources]\nroot = "${join(workRoot, "default-source")}"\n\n[brewfile]\ndesired = "root"\nwrite_mode = "template"\n\n[update]\nsecurity = "off"\n`);

  const binary = join(workRoot, process.platform === "win32" ? "updev.exe" : "updev");
  run("go", ["build", "-o", binary, "."], { env: { ...process.env, GOCACHE: process.env.GOCACHE || join(workRoot, "gocache") } });

  const baseEnv = {
    HOME: home,
    PATH: `${bin}${process.platform === "win32" ? ";" : ":"}${process.env.PATH ?? ""}`,
    XDG_CONFIG_HOME: config,
    XDG_DATA_HOME: data,
    XDG_CACHE_HOME: cache,
    XDG_STATE_HOME: state,
    XDG_RUNTIME_DIR: join(workRoot, "runtime"),
    TMPDIR: temp,
    TERM: "xterm-256color",
    LC_ALL: "C",
    TZ: "UTC",
    UPDEV_CONFIG: join(config, "updev", "config.toml"),
    UPDEV_LANG: "ja",
    UPDEV_PROGRESS: "0",
  };
  mkdirSync(baseEnv.XDG_RUNTIME_DIR, { recursive: true });
  return { binary, source, cache, baseEnv };
}

function normalizeTerminal(text) {
  return text
    .replaceAll(workRoot, "<WORK>")
    .replace(/\d{4}-\d{2}-\d{2}( に期限切れになる local security policy rule)/g, "<DATE>$1")
    .replace(/^(期限: )\d{4}-\d{2}-\d{2}$/gm, "$1<DATE>")
    .replaceAll("\r\n", "\n")
    .replace(/[ \t]+$/gm, "")
    .replace(/\n+$/, "\n");
}

function assertSnapshot(name, actual) {
  const path = join(terminalBaselineRoot, `${name}.snap`);
  const normalized = normalizeTerminal(actual);
  if (updateGoldens) {
    writeFileSync(path, normalized);
    return;
  }
  const expected = normalizeTerminal(readFileSync(path, "utf8"));
  if (normalized !== expected) {
    writeFileSync(join(artifactRoot, `${name}.actual.snap`), normalized);
    throw new Error(`terminal snapshot changed: ${name}`);
  }
}

async function assertSemanticState(shell, name, requiredTexts) {
  const screen = await shell.text();
  for (const requiredText of requiredTexts) {
    if (!screen.includes(requiredText)) {
      throw new Error(`expected semantic state ${name} to include ${JSON.stringify(requiredText)}\n${screen}`);
    }
  }
  if (process.platform === "darwin") {
    assertSnapshot(name, screen);
  }
}

async function assertVisible(shell, text) {
  const screen = await shell.text();
  if (!screen.includes(text)) {
    throw new Error(`expected visible text ${JSON.stringify(text)}\n${screen}`);
  }
}

async function captureVisual(shell, name) {
  const svgPath = join(artifactRoot, `${name}.actual.svg`);
  const actualPng = join(artifactRoot, `${name}.actual.png`);
  const expectedPng = join(visualBaselineRoot, `${name}.png`);
  const diffPng = join(artifactRoot, `${name}.diff.png`);
  await shell.screenshot(svgPath);
  const normalizedSvg = readFileSync(svgPath, "utf8")
    .replaceAll(workRoot, visualWorkRoot)
    .replace(/\d{4}-\d{2}-\d{2}/g, "2099-01-01");
  writeFileSync(svgPath, normalizedSvg);
  const renderer = new Resvg(normalizedSvg, { fitTo: { mode: "original" } });
  writeFileSync(actualPng, renderer.render().asPng());
  if (updateGoldens) {
    copyFileSync(actualPng, expectedPng);
    return;
  }
  const result = spawnSync(odiff, [expectedPng, actualPng, diffPng, "--threshold", "0", "--fail-on-layout", "--parsable-stdout"], { encoding: "utf8" });
  if (result.status !== 0) {
    throw new Error(`visual baseline changed: ${name}\n${result.stdout ?? ""}${result.stderr ?? ""}`);
  }
}

async function withShell(fixture, name, args, size, color, body) {
  const env = { ...fixture.baseEnv };
  if (!color) env.NO_COLOR = "1";
  else env.COLORTERM = "truecolor";
  const shell = ShellUse.ephemeral(`updev-${name}`, { artifacts: { dir: artifactRoot, onFailure: "svg" }, timeouts: { text: 30000, exit: 10000, command: 30000, idle: 10000, ready: 10000 } });
  try {
    await shell.run(fixture.binary, args, { cols: size.cols, rows: size.rows, cwd: toolRoot, env, waitReady: false });
    await body(shell);
  } finally {
    await shell.closeQuiet();
  }
}

function seedSecurityReport(fixture) {
  const reportDir = join(fixture.cache, "updev", "reports");
  mkdirSync(reportDir, { recursive: true });
  writeFileSync(join(reportDir, "last-update.json"), JSON.stringify({
    version: 1,
    type: "update",
    created_at: "2026-01-01T00:00:00Z",
    report: {
      status: "held",
      root: fixture.source,
      dry_run: false,
      security: "strict",
      steps: [{ name: "brew", command: ["brew", "upgrade", "jq"], status: "held", skipped: true, reason: "security review required" }],
      safety: [{
        provider: "brew",
        status: "held",
        summary: { findings: 2, review: 1, hold: 1 },
        findings: [
          { provider: "brew", kind: "brew", name: "jq", installed_versions: ["1.8.1"], version: "1.8.2", decision: "hold", reason: "ローカル policy により保留中です", reason_code: "policy_hold" },
          { provider: "brew", kind: "cask", name: "firefox", version: "151.0", decision: "review", reason: "配布元の確認が必要です", reason_code: "homebrew_cask_host_review", homepage_host: "mozilla.org", url_host: "download.mozilla.net" },
        ],
      }],
      inventory: { status: "ok", items: [] },
    },
  }));
}

async function dashboardScenario(fixture, color) {
  const name = "dashboard-ready-ja";
  await withShell(fixture, name, ["--root", fixture.source, "--dry-run", "--interactive", "--security", "off"], { cols: 120, rows: 36 }, color, async (shell) => {
    await shell.waitText("updev update", { timeout: 30000 });
    await assertVisible(shell, "更新サマリー:");
    await shell.waitText("backend", { timeout: 30000 });
    await shell.waitIdle();
    if (color) {
      await captureVisual(shell, name);
    } else {
      await assertSemanticState(shell, name, ["updev update", "更新サマリー:", "確認アクション"]);
      await shell.resize(160, 48);
      await shell.waitIdle();
      await assertVisible(shell, "更新サマリー:");
      await assertVisible(shell, "確認アクション");
    }
    await shell.press("q");
    await shell.waitExit();
  });
}

async function inventoryScenario(fixture, color, expanded) {
  const name = expanded ? "inventory-expanded-last-ja" : "inventory-grouped-ja";
  rmSync(join(fixture.cache, "updev", "reports", "last-update.json"), { force: true });
  await withShell(fixture, name, ["list", "--root", fixture.source, "--interactive", "--refresh"], { cols: 120, rows: 36 }, color, async (shell) => {
    await shell.waitText("updev installed inventory", { timeout: 30000 });
    await shell.waitText(inventoryFixtureExpectations.readyText, { timeout: 30000 });
    await shell.waitIdle();
    if (expanded) {
      for (let index = 0; index < inventoryFixtureExpectations.rowCount - 1; index += 1) {
        await shell.write("j");
        await shell.waitText(`${index + 2}/${inventoryFixtureExpectations.rowCount} 行`, { timeout: 10000 });
      }
      await shell.press("Enter");
      await shell.waitText("mise / tool / node", { timeout: 10000 });
      await assertVisible(shell, "管理: mise / tool / node");
    }
    if (color) await captureVisual(shell, name);
    else if (expanded) await assertSemanticState(shell, name, ["updev installed inventory", "管理: mise / tool / node", "詳細"]);
    else await assertSemanticState(shell, name, ["updev installed inventory", inventoryFixtureExpectations.readyText, "mise / tool / runtime"]);
    await shell.press("q");
    await shell.waitExit();
  });
}

async function securityScenario(fixture, color, confirmation) {
  seedSecurityReport(fixture);
  const name = confirmation ? "mutation-confirm-ja" : "security-detail-ja";
  const size = confirmation ? { cols: 80, rows: 24 } : { cols: 120, rows: 36 };
  await withShell(fixture, name, ["last", "--interactive", "--section", "security"], size, color, async (shell) => {
    await shell.waitText("updev security details", { timeout: 30000 });
    await shell.press("Enter");
    await shell.waitIdle();
    await assertVisible(shell, "根拠");
    if (confirmation) {
      await shell.type("4");
      await shell.waitText("security policy", { timeout: 10000 });
      await assertVisible(shell, "security policy 操作");
    }
    if (color) await captureVisual(shell, name);
    else if (confirmation) await assertSemanticState(shell, name, ["security policy 操作", "brew/brew jq", "期限:"]);
    else await assertSemanticState(shell, name, ["updev security details", "根拠", "decision: hold"]);
    await shell.press("q");
    await shell.waitExit();
  });
}

async function routeRegression(fixture) {
  rmSync(join(fixture.cache, "updev", "reports", "last-update.json"), { force: true });
  await withShell(fixture, "route-regression", ["list", "--root", fixture.source, "--interactive", "--refresh"], { cols: 80, rows: 24 }, false, async (shell) => {
    await shell.waitText("updev installed inventory", { timeout: 30000 });
    await shell.type("/");
    await shell.type("ripgrep");
    await shell.press("Enter");
    await shell.waitText('filter="ripgrep"');
    await shell.press("Enter");
    await shell.write("a");
    await shell.waitText("updev backend", { timeout: 10000 });
    await shell.write("b");
    await shell.waitText('filter="ripgrep"', { timeout: 10000 });
    await shell.write("x");
    await shell.waitText(`1/${inventoryFixtureExpectations.rowCount} 行`, { timeout: 10000 });
    await shell.write("j");
    await shell.waitText(`2/${inventoryFixtureExpectations.rowCount} 行`, { timeout: 10000 });
    await shell.press("Enter");
    await shell.waitText(inventoryFixtureExpectations.secondRowIdentity, { timeout: 10000 });
    await assertVisible(shell, "詳細");
    await shell.press("Ctrl+C");
    await shell.waitExit();
  });
}

async function main() {
  try {
    const fixture = prepareFixture();
    if (mode === "semantic" || mode === "all") {
      await dashboardScenario(fixture, false);
      await inventoryScenario(fixture, false, false);
      await inventoryScenario(fixture, false, true);
      await securityScenario(fixture, false, false);
      await securityScenario(fixture, false, true);
      await routeRegression(fixture);
    }
    if (mode === "visual" || mode === "all") {
      await dashboardScenario(fixture, true);
      await inventoryScenario(fixture, true, false);
      await inventoryScenario(fixture, true, true);
      await securityScenario(fixture, true, false);
      await securityScenario(fixture, true, true);
    }
    console.log(`tui-test: ${mode} ok`);
  } finally {
    rmSync(workRoot, { recursive: true, force: true });
  }
}

main().catch((error) => {
  console.error(error instanceof Error ? error.stack : error);
  process.exitCode = 1;
});
