// Consultas del catálogo.
import {
  listInsurers,
  listLinesOfBusiness,
  listProducts,
  listRiskSchemas,
} from "./api/generated/catalog";

// Los selectores necesitan el catálogo completo: recorre todas las páginas.
async function allPages<T>(
  page: (cursor?: string) => Promise<{ items: T[]; nextCursor?: string }>,
): Promise<T[]> {
  const all: T[] = [];
  let cursor: string | undefined;
  do {
    const { items, nextCursor } = await page(cursor);
    all.push(...items);
    cursor = nextCursor;
  } while (cursor);
  return all;
}

function fetchInsurers() {
  return allPages(async (cursor) => {
    const res = await listInsurers({ limit: 200, ...(cursor ? { cursor } : {}) });
    if (res.status !== 200) throw new Error("aseguradoras no disponibles");
    return res.data;
  });
}

function fetchLines() {
  return allPages(async (cursor) => {
    const res = await listLinesOfBusiness({ limit: 200, ...(cursor ? { cursor } : {}) });
    if (res.status !== 200) throw new Error("ramos no disponibles");
    return res.data;
  });
}

function fetchProducts() {
  return allPages(async (cursor) => {
    const res = await listProducts({ limit: 200, ...(cursor ? { cursor } : {}) });
    if (res.status !== 200) throw new Error("productos no disponibles");
    return res.data;
  });
}

async function fetchRiskSchemas() {
  const res = await listRiskSchemas();
  if (res.status !== 200) throw new Error("esquemas no disponibles");
  return res.data.items;
}

export const insurersQuery = () => ({ queryKey: ["catalog", "insurers"], queryFn: fetchInsurers });
export const linesQuery = () => ({ queryKey: ["catalog", "lines"], queryFn: fetchLines });
export const productsQuery = () => ({ queryKey: ["catalog", "products"], queryFn: fetchProducts });
export const riskSchemasQuery = () => ({
  queryKey: ["catalog", "risk-schemas"],
  queryFn: fetchRiskSchemas,
});
