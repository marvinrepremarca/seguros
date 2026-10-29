// SessionStart: H8 contexto inicial (lo que se imprime en stdout se añade al contexto de Claude).
import { spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { projectDir } from "./lib.mjs";

const root = projectDir();
const out = [];
const sh = { cwd: root, encoding: "utf8", shell: process.platform === "win32" };

const branch = spawnSync("git", ["branch", "--show-current"], sh);
if (branch.status === 0) out.push(`Rama actual: ${branch.stdout.trim() || "(sin commits)"}`);

const status = spawnSync("git", ["status", "--porcelain"], sh);
if (status.status === 0) {
  const modules = new Set(
    status.stdout.split("\n").map((l) => l.slice(3)).map((f) => f.match(/internal\/modules\/([^/]+)/)?.[1]).filter(Boolean)
  );
  if (modules.size) out.push(`Módulos con cambios sin commit: ${[...modules].join(", ")}`);
}

const roadmap = path.join(root, "docs/ROADMAP.md");
if (existsSync(roadmap)) {
  const current = readFileSync(roadmap, "utf8").split("\n").filter((l) => /^\| Fase \d.*(🟨|⛔)/.test(l));
  if (current.length) out.push(`ROADMAP en curso/bloqueado:\n${current.join("\n")}`);
}

const registry = path.join(root, "src/backend/internal/platform/flags/registry.yaml");
if (existsSync(registry)) {
  const soon = Date.now() + 14 * 864e5;
  const txt = readFileSync(registry, "utf8");
  const expiring = [...txt.matchAll(/key:\s*(release\.[\w.-]+)[\s\S]*?removeBy:\s*["']?(\d{4}-\d{2}-\d{2})/g)]
    .filter(([, , d]) => Date.parse(d) <= soon).map(([, k, d]) => `${k} (${d})`);
  if (expiring.length) out.push(`Flags release por retirar en ≤14 días: ${expiring.join(", ")}`);
}

const decisions = path.join(root, "docs/domain/DECISIONS.md");
if (existsSync(decisions)) {
  const pending = (readFileSync(decisions, "utf8").match(/\[PENDIENTE\]/g) || []).length;
  if (pending) out.push(`Decisiones de negocio pendientes en docs/domain/DECISIONS.md: ${pending}`);
}

if (out.length) process.stdout.write(out.join("\n") + "\n");
process.exit(0);
