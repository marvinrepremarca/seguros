// Consultas del catálogo.
import {
  listInsurers,
  listLinesOfBusiness,
  listProducts,
  listRiskSchemas,
} from "./api/generated/catalog";

async function fetchInsurers() {
  const res = await listInsurers({ limit: 200 });
  if (res.status !== 200) throw new Error("aseguradoras no disponibles");
  return res.data.items;
}

async function fetchLines() {
  const res = await listLinesOfBusiness({ limit: 200 });
  if (res.status !== 200) throw new Error("ramos no disponibles");
  return res.data.items;
}

async function fetchProducts() {
  const res = await listProducts({ limit: 200 });
  if (res.status !== 200) throw new Error("productos no disponibles");
  return res.data.items;
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
