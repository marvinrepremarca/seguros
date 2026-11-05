/** Convierte el texto "número;valor" por línea en las líneas de una liquidación. */
export function parseLines(text: string): { policyNumber: string; amount: string }[] {
  return text
    .split("\n")
    .map((l) => l.trim())
    .filter((l) => l !== "")
    .map((l) => {
      const [policyNumber = "", amount = ""] = l.split(";").map((x) => x.trim());
      return { policyNumber, amount };
    });
}
