// Stop: H7 verificación rápida antes de que Claude dé por terminado el turno.
import { spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import path from "node:path";
import { readInput, block, projectDir } from "./lib.mjs";

const input = readInput();
if (input.stop_hook_active) process.exit(0); // evita bucles

const root = projectDir();
const sh = { cwd: root, encoding: "utf8", shell: process.platform === "win32" };
const git = spawnSync("git", ["status", "--porcelain"], sh);
if (git.status !== 0) process.exit(0);
const changed = git.stdout.split("\n").map((l) => l.slice(3).trim()).filter(Boolean);

const has = (bin) =>
  spawnSync(process.platform === "win32" ? "where" : "which", [bin], { stdio: "ignore" }).status === 0;
const tail = (s) => (s || "").split("\n").slice(-40).join("\n");
const failures = [];

const backend = path.join(root, "src/backend");
if (changed.some((f) => f.startsWith("src/backend/")) && existsSync(path.join(backend, "go.mod")) && has("go")) {
  for (const args of [["build", "./..."], ["vet", "./..."]]) {
    const r = spawnSync("go", args, { ...sh, cwd: backend });
    if (r.status !== 0) failures.push(`go ${args.join(" ")}:\n${tail(r.stdout + r.stderr)}`);
  }
  if (has("go-arch-lint")) {
    const r = spawnSync("go-arch-lint", ["check"], { ...sh, cwd: backend });
    if (r.status !== 0) failures.push(`go-arch-lint check:\n${tail(r.stdout + r.stderr)}`);
  }
}

const frontend = path.join(root, "src/frontend");
if (changed.some((f) => f.startsWith("src/frontend/")) && existsSync(path.join(frontend, "node_modules")) && has("pnpm")) {
  for (const script of ["typecheck", "lint"]) {
    const r = spawnSync("pnpm", ["run", "--if-present", script], { ...sh, cwd: frontend });
    if (r.status !== 0) failures.push(`pnpm ${script}:\n${tail(r.stdout + r.stderr)}`);
  }
}

if (failures.length) {
  block(`H7: la verificación falló. Corrige antes de terminar:\n\n${failures.join("\n\n")}`);
}
process.exit(0);
