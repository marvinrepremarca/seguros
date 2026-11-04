// Campos de datos de riesgo a partir del esquema JSON del ramo (lo publica Catalog). El formulario
// maneja texto; toRiskData los convierte al tipo que exige el esquema.
export interface RiskField {
  name: string;
  label: string;
  type: "string" | "integer";
  required: boolean;
  pattern?: string;
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

export function riskFields(schema: Readonly<Record<string, unknown>> | undefined): RiskField[] {
  if (!schema || !isRecord(schema["properties"])) return [];
  const required = Array.isArray(schema["required"]) ? schema["required"] : [];
  const order = (v: unknown) =>
    isRecord(v) && typeof v["x-order"] === "number" ? v["x-order"] : 0;
  return Object.entries(schema["properties"])
    .sort(([, a], [, b]) => order(a) - order(b))
    .flatMap(([name, def]): RiskField[] => {
      if (!isRecord(def)) return [];
      const label = typeof def["x-label"] === "string" ? def["x-label"] : name;
      const pattern = typeof def["pattern"] === "string" ? def["pattern"] : undefined;
      return [
        {
          name,
          label,
          type: def["type"] === "integer" ? "integer" : "string",
          required: required.includes(name),
          ...(pattern === undefined ? {} : { pattern }),
        },
      ];
    });
}

/** Omite los vacíos y convierte enteros. Un entero mal escrito se envía como texto: lo rechaza el backend. */
export function toRiskData(
  fields: readonly RiskField[],
  values: Readonly<Record<string, string | undefined>>,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const f of fields) {
    const raw = values[f.name]?.trim();
    if (!raw) continue;
    out[f.name] = f.type === "integer" && /^-?\d+$/.test(raw) ? Number(raw) : raw;
  }
  return out;
}

/** Valores de formulario a partir de datos guardados. */
export function fromRiskData(data: Readonly<Record<string, unknown>>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(data)) {
    if (typeof v === "string" || typeof v === "number") out[k] = String(v);
  }
  return out;
}
