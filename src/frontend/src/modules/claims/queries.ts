import { listClaims } from "./api/generated/claims";

/** Siniestros recientes para elegir en formularios de otros módulos. */
export const claimOptionsQuery = () => ({
  queryKey: ["claims", "options"],
  queryFn: async () => {
    const res = await listClaims({ limit: 200 });
    if (res.status !== 200) throw new Error("siniestros no disponibles");
    return res.data.items.map((c) => ({
      id: c.id,
      label: `${c.holderName} · ${c.claimType} · ${c.occurredOn}`,
    }));
  },
});
