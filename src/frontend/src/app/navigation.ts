// Menú principal: cada entrada de un módulo apagable declara su flag; las de administración, su
// permiso. El menú solo es UX: las rutas verifican el flag en beforeLoad y el backend autoriza.
import { features, type FeatureKey } from "@/shared/lib/features";
import { permissions } from "@/shared/lib/session";

/** Destinos tipados del router (TanStack Router valida rutas y parámetros). */
export type NavLink =
  | { to: "/" }
  | { to: "/clientes" }
  | { to: "/polizas" }
  | { to: "/configuracion/catalogo" }
  | { to: "/configuracion/funcionalidades" }
  | { to: "/modulos/$modulo"; params: { modulo: string } };

export interface NavItem {
  label: string;
  link: NavLink;
  flag?: FeatureKey;
  /** Basta uno de estos permisos para ver la entrada. */
  permissions?: readonly string[];
}

const moduleLink = (modulo: string): NavLink => ({ to: "/modulos/$modulo", params: { modulo } });

export interface NavSection {
  title: string;
  items: readonly NavItem[];
}

export const NAV_SECTIONS: readonly NavSection[] = [
  {
    title: "General",
    items: [
      { label: "Inicio", link: { to: "/" } },
      { label: "Clientes", link: { to: "/clientes" }, permissions: [permissions.partyRead] },
      {
        label: "Pólizas",
        link: { to: "/polizas" },
        permissions: [permissions.policyRead, permissions.policyReadOwn],
      },
    ],
  },
  {
    title: "Venta",
    items: [
      { label: "Prospectos (CRM)", link: moduleLink("crm"), flag: features.crm.module },
      { label: "Cotizaciones", link: moduleLink("quoting"), flag: features.quoting.module },
    ],
  },
  {
    title: "Operación",
    items: [
      { label: "Cartera y cobro", link: moduleLink("billing"), flag: features.billing.module },
      { label: "Renovaciones", link: moduleLink("renewals"), flag: features.renewals.module },
      { label: "Siniestros", link: moduleLink("claims"), flag: features.claims.module },
      { label: "Documentos", link: moduleLink("documents"), flag: features.documents.module },
      {
        label: "Notificaciones",
        link: moduleLink("notifications"),
        flag: features.notifications.module,
      },
    ],
  },
  {
    title: "Finanzas y control",
    items: [
      { label: "Comisiones", link: moduleLink("commissions"), flag: features.commissions.module },
      { label: "Cumplimiento", link: moduleLink("compliance"), flag: features.compliance.module },
      { label: "Reportes", link: moduleLink("reporting"), flag: features.reporting.module },
      {
        label: "Integraciones",
        link: moduleLink("integrations"),
        flag: features.integrations.module,
      },
    ],
  },
  {
    title: "Configuración",
    items: [
      {
        label: "Funcionalidades",
        link: { to: "/configuracion/funcionalidades" },
        permissions: [permissions.featureFlagRead],
      },
      {
        label: "Catálogo",
        link: { to: "/configuracion/catalogo" },
        permissions: [permissions.catalogRead],
      },
    ],
  },
];

/** Filtra el menú por flags encendidos y permisos; omite las secciones que quedan vacías. */
export function visibleSections(
  sections: readonly NavSection[],
  isEnabled: (key: FeatureKey) => boolean,
  can: (permission: string) => boolean,
): NavSection[] {
  return sections
    .map((s) => ({
      ...s,
      items: s.items.filter(
        (i) =>
          (i.flag === undefined || isEnabled(i.flag)) &&
          (i.permissions === undefined || i.permissions.some(can)),
      ),
    }))
    .filter((s) => s.items.length > 0);
}

/** Nombre visible de cada módulo apagable (para la página del módulo). */
export const MODULE_LABELS: Readonly<Record<string, string>> = Object.fromEntries(
  NAV_SECTIONS.flatMap((s) => s.items).flatMap((i) =>
    i.link.to === "/modulos/$modulo" ? [[i.link.params.modulo, i.label] as const] : [],
  ),
);
