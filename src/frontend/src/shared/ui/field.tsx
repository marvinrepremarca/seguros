import type { ComponentProps, ReactNode } from "react";
import { useId } from "react";

import { cn } from "@/shared/lib/cn";

const CONTROL =
  "w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:opacity-50 aria-invalid:border-destructive";

interface FieldProps {
  label: string;
  error?: string | undefined;
  hint?: string;
  children: (props: {
    id: string;
    "aria-invalid"?: true;
    "aria-describedby"?: string;
  }) => ReactNode;
}

/** Etiqueta, control y mensaje de error enlazados para lectores de pantalla (WCAG 2.2 AA). */
export function Field({ label, error, hint, children }: FieldProps) {
  const id = useId();
  const messageId = `${id}-message`;
  return (
    <div className="space-y-1">
      <label htmlFor={id} className="block text-sm font-medium">
        {label}
      </label>
      {children({
        id,
        ...(error ? { "aria-invalid": true as const } : {}),
        ...(error || hint ? { "aria-describedby": messageId } : {}),
      })}
      {error ? (
        <p id={messageId} className="text-sm text-destructive">
          {error}
        </p>
      ) : (
        hint && (
          <p id={messageId} className="text-sm text-muted-foreground">
            {hint}
          </p>
        )
      )}
    </div>
  );
}

export function Input({ className, ...props }: ComponentProps<"input">) {
  return <input className={cn(CONTROL, className)} {...props} />;
}

export function Select({ className, ...props }: ComponentProps<"select">) {
  return <select className={cn(CONTROL, className)} {...props} />;
}

export function Textarea({ className, ...props }: ComponentProps<"textarea">) {
  return <textarea className={cn(CONTROL, className)} rows={3} {...props} />;
}

/** Estado de una vista: carga, error o vacío. */
export function QueryState({
  isPending,
  isError,
  isEmpty,
  emptyMessage,
}: {
  isPending: boolean;
  isError: boolean;
  isEmpty: boolean;
  emptyMessage: string;
}) {
  if (isPending) return <p aria-busy="true">Cargando…</p>;
  if (isError) return <p role="alert">No se pudo cargar la información. Inténtalo de nuevo.</p>;
  if (isEmpty) return <p className="text-muted-foreground">{emptyMessage}</p>;
  return null;
}

export function ErrorMessage({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <p role="alert" className="rounded border border-destructive/40 p-3 text-sm text-destructive">
      {message}
    </p>
  );
}
