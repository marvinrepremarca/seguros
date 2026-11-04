import { Field, Input } from "@/shared/ui/field";

import type { RiskField } from "../risk";

interface RiskFieldsProps {
  fields: readonly RiskField[];
  values: Readonly<Record<string, string>>;
  onChange: (name: string, value: string) => void;
  /** Distingue las etiquetas cuando hay varios bienes. */
  suffix?: string;
}

export function RiskFields({ fields, values, onChange, suffix = "" }: RiskFieldsProps) {
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      {fields.map((f) => (
        <Field key={f.name} label={`${f.label}${f.required ? " *" : ""}${suffix}`}>
          {(p) => (
            <Input
              {...p}
              {...(f.type === "integer" ? { inputMode: "numeric" as const } : {})}
              {...(f.required ? { required: true } : {})}
              value={values[f.name] ?? ""}
              onChange={(e) => {
                onChange(f.name, e.target.value);
              }}
            />
          )}
        </Field>
      ))}
    </div>
  );
}
