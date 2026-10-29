// PreToolUse (Edit|Write|MultiEdit): H1 secretos, H2 migraciones/código generado, H4 escaneo de secretos.
import { existsSync } from "node:fs";
import path from "node:path";
import { readInput, relPath, block, newContent, projectDir } from "./lib.mjs";

const input = readInput();
const tool = input.tool_name;
const ti = input.tool_input || {};
const rel = relPath(ti.file_path);
if (!rel) process.exit(0);

// H1 — archivos de secretos
const secretFile =
  /(^|\/)\.env(\..+)?$/.test(rel) && !/\.env\.example$/.test(rel) ||
  /(^|\/)secrets\//.test(rel) ||
  /\.(pem|key|pfx|p12)$/.test(rel) ||
  /(^|\/)config\.production\./.test(rel);
if (secretFile) {
  block(`H1: "${rel}" es un archivo de secretos protegido. Los secretos van en variables de entorno o en OpenBao; usa .env.example sin valores.`);
}

// H2 — migraciones aplicadas y código generado
if (/^src\/backend\/migrations\/.+\.sql$/.test(rel)) {
  const exists = existsSync(path.join(projectDir(), rel));
  if (tool !== "Write" || exists) {
    block(`H2: no se editan migraciones existentes ("${rel}"). Crea una migración nueva con goose (skill /db-migration).`);
  }
}
if (/\.gen\.go$/.test(rel) || /\/sqlc\//.test(rel) || /^src\/frontend\/.*\/api\/generated\//.test(rel)) {
  block(`H2: "${rel}" es código generado. Edita la fuente (OpenAPI, queries SQL o registry.yaml) y ejecuta "make generate".`);
}

// H4 — secretos dentro del contenido (se omiten docs y ejemplos)
if (!/\.(md|example)$/.test(rel)) {
  const content = newContent(ti);
  const patterns = [
    [/AKIA[0-9A-Z]{16}/, "llave de acceso AWS"],
    [/-----BEGIN [A-Z ]*PRIVATE KEY-----/, "llave privada"],
    [/eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}/, "JWT"],
    [/postgres(ql)?:\/\/[^:\s"'@]+:(?!\$\{|changeme|password@|postgres@)[^@\s"']{6,}@/i, "connection string con contraseña"],
    [/(password|passwd|secret|api[_-]?key|client[_-]?secret)\s*[:=]\s*["'][^"'\s$]{12,}["']/i, "credencial literal"],
  ];
  for (const [re, name] of patterns) {
    if (re.test(content)) {
      block(`H4: posible ${name} en "${rel}". No escribas secretos en el repositorio; léelos de variables de entorno.`);
    }
  }
}

process.exit(0);
