import type { Pet } from "./api";

export function label(p: Pet): string {
  return p.name.toUpperCase();
}
