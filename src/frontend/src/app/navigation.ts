// Menú principal: cada entrada de un módulo apagable declara su flag; las de administración, su
// permiso. El menú solo es UX: las rutas verifican el flag en beforeLoad y el backend autoriza.
import { features, type FeatureKey } from "@/shared/lib/features";
import { permissions } from "@/shared/lib/session";

/** Destinos tipados del router (TanStack Router valida rutas y parámetros). */
export type NavLink =
  | { to: "/" }
  | { to: "/clientes" }
  | { to: "/polizas" }
  | { to: "/configuracion/parametros" }
  | { to: "/cartera" }
  | { to: "/renovaciones" }
  | { to: "/prospectos" }
  | { to: "/cotizaciones" }
  | { to: "/siniestros" }
  | { to: "/comisiones" }
  | { to: "/cumplimiento" }
  | { to: "/documentos" }
  | { to: "/configuracion/catalogo" }
  | { to: "/configuracion/traspaso-cartera" }
  | { to: "/configuracion/funcionalidades" }
  | { to: "/notificaciones" }
  | { to: "/reportes" }
  | { to: "/integraciones" };

export interface NavItem {
  label: string;
  link: NavLink;
  flag?: FeatureKey;
  /** Basta uno de estos permisos para ver la entrada. */
  permissions?: readonly string[];
}

export interface NavSection {
  title: string;
  items: readonly NavItem[];
}

export const NAV_SECTIONS: readonly NavSection[] = [
  {
    title: "General",
    items: [
      { label: "Inicio", link: { to: "/" } },
      {
        label: "Clientes",
        link: { to: "/clientes" },
        permissions: [permissions.partyRead, permissions.partyReadOwn],
      },
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
      {
        label: "Prospectos (CRM)",
        link: { to: "/prospectos" },
        flag: features.crm.module,
        permissions: [permissions.leadRead, permissions.leadReadOwn],
      },
      {
        label: "Cotizaciones",
        link: { to: "/cotizaciones" },
        flag: features.quoting.module,
        permissions: [permissions.quoteRead, permissions.quoteReadOwn],
      },
    ],
  },
  {
    title: "Operación",
    items: [
      {
        label: "Cartera y cobro",
        link: { to: "/cartera" },
        flag: features.billing.module,
        permissions: [permissions.billingPlanRead],
      },
      {
        label: "Renovaciones",
        link: { to: "/renovaciones" },
        flag: features.renewals.module,
        permissions: [permissions.renewalRead],
      },
      {
        label: "Siniestros",
        link: { to: "/siniestros" },
        flag: features.claims.module,
        permissions: [permissions.claimRead, permissions.claimReadOwn],
      },
      {
        label: "Documentos",
        link: { to: "/documentos" },
        flag: features.documents.module,
        permissions: [permissions.documentRead],
      },
      {
        label: "Notificaciones",
        link: { to: "/notificaciones" },
        flag: features.notifications.module,
        permissions: [permissions.notificationRead],
      },
    ],
  },
  {
    title: "Finanzas y control",
    items: [
      {
        label: "Comisiones",
        link: { to: "/comisiones" },
        flag: features.commissions.module,
        permissions: [permissions.commissionRead],
      },
      {
        label: "Cumplimiento",
        link: { to: "/cumplimiento" },
        flag: features.compliance.module,
        permissions: [permissions.kycRead],
      },
      {
        label: "Reportes",
        link: { to: "/reportes" },
        flag: features.reporting.module,
        permissions: [permissions.reportingRead],
      },
      {
        label: "Integraciones",
        link: { to: "/integraciones" },
        flag: features.integrations.module,
        permissions: [permissions.integrationRead],
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
      {
        label: "Parámetros",
        link: { to: "/configuracion/parametros" },
        permissions: [
          permissions.policySettingsManage,
          permissions.billingSettingsManage,
          permissions.renewalSettingsManage,
          permissions.commissionRateManage,
          permissions.quoteSettingsManage,
        ],
      },
      {
        label: "Traspaso de cartera",
        link: { to: "/configuracion/traspaso-cartera" },
        permissions: [permissions.policyAssign],
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
