// Hand-written client types for the Gin + swaggo API.

export type PetKind = "cat" | "dog" | "bird";

export interface Pet {
  id: number;
  name: string;
  kind: PetKind;
}

export interface PetInput {
  name: string;
  kind: PetKind;
}

export interface Problem {
  status: number;
  detail: string;
}

export async function listPets(baseUrl: string, query: { kind?: PetKind; limit?: number } = {}): Promise<Pet[]> {
  const params = new URLSearchParams();
  if (query.kind) params.set("kind", query.kind);
  if (query.limit !== undefined) params.set("limit", String(query.limit));
  const res = await fetch(`${baseUrl}/pets?${params}`);
  if (!res.ok) throw (await res.json()) as Problem;
  return (await res.json()) as Pet[];
}

export async function getPet(baseUrl: string, id: number): Promise<Pet> {
  const res = await fetch(`${baseUrl}/pets/${id}`);
  if (!res.ok) throw (await res.json()) as Problem;
  return (await res.json()) as Pet;
}

export async function createPet(baseUrl: string, body: PetInput): Promise<Pet> {
  const res = await fetch(`${baseUrl}/pets`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw (await res.json()) as Problem;
  return (await res.json()) as Pet;
}

export async function updatePet(baseUrl: string, id: number, body: PetInput): Promise<Pet> {
  const res = await fetch(`${baseUrl}/pets/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw (await res.json()) as Problem;
  return (await res.json()) as Pet;
}
