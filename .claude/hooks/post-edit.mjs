// PostToolUse (Edit|Write|MultiEdit): H5 formateo, H6 anti-patrones.
import { spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import path from "node:path";
import { readInput, relPath, block, newContent, projectDir } from "./lib.mjs";

const input = readInput();
const ti = input.tool_input || {};
const rel = relPath(ti.file_path);
if (!rel) process.exit(0);
const abs = path.join(projectDir(), rel);

const has = (bin) =>
  spawnSync(process.platform === "win32" ? "where" : "which", [bin], { stdio: "ignore" }).status === 0;
const run = (bin, args, cwd = projectDir()) =>
  spawnSync(bin, args, { cwd, stdio: "ignore", shell: process.platform === "win32" });

// H5 — formateo (no bloquea)
if (existsSync(abs)) {
  if (rel.endsWith(".go")) {
    if (has("goimports")) run("goimports", ["-w", abs]);
    else if (has("gofmt")) run("gofmt", ["-w", abs]);
  } else if (/^src\/frontend\/.+\.(ts|tsx|css|json)$/.test(rel) &&
             existsSync(path.join(projectDir(), "src/frontend/node_modules"))) {
    run("pnpm", ["exec", "prettier", "--write", abs], path.join(projectDir(), "src/frontend"));
  }
}

// H6 — anti-patrones en el contenido nuevo
const content = newContent(ti);
const issues = [];
const lines = content.split(/\r?\n/);

if (rel.endsWith(".go") && !rel.endsWith("_test.go")) {
  lines.forEach((l, i) => {
    const n = `línea ~${i + 1}`;
    if (/\bfloat(32|64)\b/.test(l) && /(premium|amount|commission|price|money|balance|prima|monto)/i.test(l))
      issues.push(`${n}: float para dinero → usa kernel.Money`);
    if (/\btime\.Now\(\)/.test(l) && !rel.includes("platform/kernel"))
      issues.push(`${n}: time.Now() directo → inyecta kernel.Clock`);
    if (/fmt\.Sprintf\([^)]*\b(SELECT|INSERT|UPDATE|DELETE)\b/i.test(l))
      issues.push(`${n}: SQL construido con fmt.Sprintf → usa sqlc/pgx con parámetros`);
    if (/^\s*_\s*=\s*[\w.]+\(/.test(l) || /,\s*_\s*:?=\s*[\w.]+\(/.test(l) && /err/i.test(l))
      issues.push(`${n}: posible error ignorado → manéjalo o justifica con comentario`);
    if (/\bpanic\(/.test(l) && /\/(domain|app)\//.test(rel))
      issues.push(`${n}: panic en código de negocio → devuelve error`);
    if (/InsecureSkipVerify:\s*true/.test(l))
      issues.push(`${n}: InsecureSkipVerify desactiva TLS`);
  });
  if (/"math\/rand(\/v2)?"/.test(content) && /(token|secret|key|nonce|password)/i.test(content))
    issues.push(`math/rand en código con secretos → usa crypto/rand`);
}

if (/^src\/frontend\/.+\.(ts|tsx)$/.test(rel) && !/\.(test|spec)\.tsx?$/.test(rel)) {
  if (/dangerouslySetInnerHTML/.test(content)) issues.push(`dangerouslySetInnerHTML prohibido (usa DOMPurify solo con justificación)`);
  if (/\b(localStorage|sessionStorage)\b/.test(content)) issues.push(`localStorage/sessionStorage: no guardes datos de negocio, PII ni tokens`);
  if (/\beval\(|new Function\(/.test(content)) issues.push(`eval/new Function prohibidos`);
}

if (/^src\/backend\/migrations\/.+\.sql$/.test(rel) && /CREATE\s+TABLE/i.test(content)) {
  if (/tenant_id/i.test(content) && !/ENABLE\s+ROW\s+LEVEL\s+SECURITY/i.test(content))
    issues.push(`tabla con tenant_id sin ENABLE/FORCE ROW LEVEL SECURITY + POLICY tenant_isolation`);
  if (/\b(real|double precision|money)\b/i.test(content))
    issues.push(`tipo flotante/money en SQL → usa numeric(19,4)`);
}

if (issues.length) {
  block(`H6: revisa "${rel}" (reglas no negociables de CLAUDE.md):\n- ${issues.join("\n- ")}`);
}
process.exit(0);
