// Utilidades compartidas por los hooks de Claude Code.
import { readFileSync } from "node:fs";
import path from "node:path";

export function readInput() {
  try {
    return JSON.parse(readFileSync(0, "utf8") || "{}");
  } catch {
    return {};
  }
}

export const projectDir = () =>
  path.resolve(process.env.CLAUDE_PROJECT_DIR || process.cwd());

// Ruta relativa al proyecto con "/" como separador; null si está fuera del proyecto.
export function relPath(filePath) {
  if (!filePath) return null;
  const rel = path.relative(projectDir(), path.resolve(projectDir(), filePath));
  if (rel.startsWith("..") || path.isAbsolute(rel)) return null;
  return rel.split(path.sep).join("/");
}

// Bloquea la acción y devuelve el motivo a Claude.
export function block(message) {
  process.stderr.write(message.trim() + "\n");
  process.exit(2);
}

export function newContent(toolInput) {
  if (!toolInput) return "";
  if (typeof toolInput.content === "string") return toolInput.content;
  if (typeof toolInput.new_string === "string") return toolInput.new_string;
  if (Array.isArray(toolInput.edits))
    return toolInput.edits.map((e) => e.new_string || "").join("\n");
  return "";
}
