import type { components } from "./api";

type Pet = components["schemas"]["Pet"];

export function label(p: Pet): string {
  return p.name.toUpperCase();
}
