// PreToolUse (Bash|PowerShell): H3 comandos destructivos.
import { readInput, block } from "./lib.mjs";

const cmd = String(readInput().tool_input?.command || "");

const rules = [
  [/\brm\s+-[a-z]*r[a-z]*f?[a-z]*\s+(\/|~|\*|\.\s*$|\.\/?\s*$)/i, "borrado recursivo de raíz, home o directorio actual"],
  [/Remove-Item\b[^\n]*-Recurse[^\n]*\s(\/|\\|C:\\|~|\.)\s*($|\s)/i, "borrado recursivo de raíz o directorio actual"],
  [/\b(docker|podman)\s+volume\s+(rm|prune)\b/i, "eliminación de volúmenes de contenedores (datos)"],
  [/\b(docker|podman)\s+system\s+prune\b[^\n]*--volumes/i, "limpieza de volúmenes de contenedores"],
  [/\bDROP\s+(DATABASE|SCHEMA)\b/i, "DROP DATABASE/SCHEMA"],
  [/\bTRUNCATE\b/i, "TRUNCATE"],
  [/\bgit\s+push\b[^\n]*(--force\b|-f\b|--force-with-lease)[^\n]*\b(main|master)\b/i, "push forzado a la rama principal"],
  [/\bgit\s+(reset\s+--hard|clean\s+-[a-z]*f)/i, "descarte irreversible de cambios locales"],
  [/\bgoose\b[^\n]*(prod|production)/i, "migración contra producción"],
  [/\b(migrate|cmd\/migrate)\b[^\n]*(prod|production)/i, "migración contra producción"],
];

for (const [re, why] of rules) {
  if (re.test(cmd)) {
    block(`H3: comando bloqueado (${why}). Si es realmente necesario, pide al usuario que lo ejecute manualmente.`);
  }
}
process.exit(0);
