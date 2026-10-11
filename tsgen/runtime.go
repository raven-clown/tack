package tsgen

// clientMethods is the part of Client that every generated client shares:
// building the request, applying credentials, and reading the response.
const clientMethods = `  private async send<T, E, H = Record<string, never>>(op: Op, input: object, init?: RequestOptions): Promise<Result<T, E, H>> {
    const values = input as Record<string, unknown>;
    const path = op.path.replace(/\{([^}]+)\}/g, (_, name: string) => encodeURIComponent(String(values[name])));
    const query = new URLSearchParams();
    for (const name of op.query ?? []) appendQuery(query, name, values[name]);
    const headers = new Headers(this.options.headers);
    for (const name of op.headers ?? []) {
      const v = values[name];
      if (v === undefined || v === null) continue;
      headers.set(name, Array.isArray(v) ? v.map(String).join(", ") : String(v));
    }
    await this.applySecurity(op, headers, query);
    new Headers(init?.headers).forEach((v, k) => headers.set(k, v));
    let body: BodyInit | undefined;
    if (op.body && values.body !== undefined) {
      if (op.body === true) {
        body = JSON.stringify(values.body);
        if (!headers.has("Content-Type")) headers.set("Content-Type", "application/json");
      } else {
        body = formBody(values.body, op.body);
      }
    }
    const qs = query.toString();
    const url = (this.options.baseUrl ?? "").replace(/\/$/, "") + path + (qs ? "?" + qs : "");
    const fetchFn = this.options.fetch ?? globalThis.fetch;
    const response = await fetchFn(url, { ...init, method: op.method, headers, body });
    if (response.status === op.ok) {
      const empty = response.status === 204 || response.headers.get("Content-Length") === "0";
      let data: T;
      if (op.stream) data = events(response) as T;
      else if (op.json && !empty) data = (await response.json()) as T;
      else data = undefined as T;
      return { ok: true, status: response.status, data, headers: readHeaders(response.headers, op.resHeaders) as H, response };
    }
    if ((op.errors ?? []).includes(response.status) && isJSON(response)) {
      return { ok: false, status: response.status, error: (await response.json()) as E, response };
    }
    const text = await response.text().catch(() => "");
    let error: unknown = text;
    if (isJSON(response)) {
      try {
        error = JSON.parse(text);
      } catch {
        // Keep the text.
      }
    }
    throw new ApiError(response.status, error, response);
  }

  private async applySecurity(op: Op, headers: Headers, query: URLSearchParams): Promise<void> {
    const credentials = (this.options.credentials ?? {}) as Record<string, Credential | undefined>;
    const alternative = (op.security ?? []).find((names) => names.every((n) => credentials[n] !== undefined));
    if (!alternative) return;
    for (const name of alternative) {
      const scheme = securitySchemes[name];
      const credential = credentials[name];
      if (!scheme || credential === undefined) continue;
      const value = typeof credential === "function" ? await credential() : credential;
      if (scheme.type === "http" && scheme.scheme === "bearer") headers.set("Authorization", "Bearer " + value);
      else if (scheme.type === "http" && scheme.scheme === "basic") headers.set("Authorization", "Basic " + btoa(value));
      else if (scheme.type === "apiKey" && scheme.in === "header" && scheme.name) headers.set(scheme.name, value);
      else if (scheme.type === "apiKey" && scheme.in === "query" && scheme.name) query.set(scheme.name, value);
    }
  }
`

// runtime holds the exported types and the helpers outside Client.
const runtime = `export interface ClientOptions {
  /** Prefix for every path, such as "https://api.example.com". Empty means the current origin. */
  baseUrl?: string;
  /** Replaces the global fetch, for tests or a runtime without one. */
  fetch?: typeof fetch;
  /** Headers sent with every request. */
  headers?: HeadersInit;
  /** Credentials by scheme name. An operation that requires a scheme sends the first alternative that is set. */
  credentials?: Partial<Record<SecurityName, Credential>>;
}

/** A value for a security scheme, or a function that returns it: the token for bearer, "user:password" for basic, the key for apiKey. */
export type Credential = string | (() => string | Promise<string>);

/** Options for one request; method, body, and the headers the operation sets take precedence. */
export type RequestOptions = Omit<RequestInit, "method" | "body">;

export interface Ok<T, H> {
  ok: true;
  status: number;
  data: T;
  /** The response headers the API documents, parsed. The rest are on response.headers. */
  headers: H;
  response: Response;
}

export interface Err<E> {
  ok: false;
  status: number;
  /** The problem the API documents for this status; switch on error.code. */
  error: E;
  response: Response;
}

export type Result<T, E, H = Record<string, never>> = Ok<T, H> | Err<E>;

/** One server-sent event; data is the parsed JSON of its data line. */
export interface ServerEvent<T> {
  data: T;
  event?: string;
  id?: string;
}

/** Thrown for a response the API does not document, and by unwrap for an error result. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    /** The parsed body when it was JSON, otherwise the text. */
    readonly error: unknown,
    readonly response: Response,
  ) {
    super(problemMessage(status, error));
    this.name = "ApiError";
  }
}

/** Returns the data of an ok result and throws ApiError for an error result. */
export function unwrap<T, H>(result: Result<T, unknown, H>): T {
  if (result.ok) return result.data;
  throw new ApiError(result.status, result.error, result.response);
}

type HeaderKind = "string" | "number" | "boolean" | "array" | "numbers";

interface Op {
  method: string;
  path: string;
  query?: string[];
  headers?: string[];
  /** true sends JSON; "multipart" sends FormData; "urlencoded" sends URLSearchParams. */
  body?: boolean | "multipart" | "urlencoded";
  ok: number;
  json?: boolean;
  stream?: boolean;
  errors?: number[];
  resHeaders?: [string, HeaderKind][];
  security?: string[][];
}

function appendQuery(query: URLSearchParams, name: string, value: unknown): void {
  if (value === undefined || value === null) return;
  if (Array.isArray(value)) {
    for (const v of value) appendQuery(query, name, v);
    return;
  }
  query.append(name, typeof value === "object" ? JSON.stringify(value) : String(value));
}

function formBody(value: unknown, kind: "multipart" | "urlencoded"): BodyInit {
  const form = kind === "multipart" ? new FormData() : new URLSearchParams();
  const add = (name: string, v: unknown) => {
    if (v === undefined || v === null) return;
    if (Array.isArray(v)) {
      for (const item of v) add(name, item);
      return;
    }
    if (v instanceof Blob) {
      if (form instanceof FormData) form.append(name, v);
      return;
    }
    form.append(name, typeof v === "object" ? JSON.stringify(v) : String(v));
  };
  for (const [name, v] of Object.entries((value ?? {}) as Record<string, unknown>)) add(name, v);
  return form;
}

/** Reads a text/event-stream body as parsed events until the server closes it. */
async function* events<T>(response: Response): AsyncIterable<ServerEvent<T>> {
  if (!response.body) return;
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let data: string[] = [];
  let event: string | undefined;
  let id: string | undefined;
  const flush = (): ServerEvent<T> | undefined => {
    if (data.length === 0) {
      event = id = undefined;
      return undefined;
    }
    const text = data.join("\n");
    data = [];
    let parsed: unknown = text;
    try {
      parsed = JSON.parse(text);
    } catch {
      // A data line that is not JSON is delivered as text.
    }
    const out: ServerEvent<T> = { data: parsed as T };
    if (event !== undefined) out.event = event;
    if (id !== undefined) out.id = id;
    event = id = undefined;
    return out;
  };
  try {
    for (;;) {
      const { done, value } = await reader.read();
      buffer += done ? "\n\n" : decoder.decode(value, { stream: true });
      let index: number;
      while ((index = buffer.indexOf("\n")) >= 0) {
        const line = buffer.slice(0, index).replace(/\r$/, "");
        buffer = buffer.slice(index + 1);
        if (line === "") {
          const e = flush();
          if (e) yield e;
        } else if (line.startsWith("data:")) data.push(line.slice(5).replace(/^ /, ""));
        else if (line.startsWith("event:")) event = line.slice(6).replace(/^ /, "");
        else if (line.startsWith("id:")) id = line.slice(3).replace(/^ /, "");
      }
      if (done) return;
    }
  } finally {
    reader.releaseLock();
  }
}

function readHeaders(headers: Headers, spec?: [string, HeaderKind][]): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [name, kind] of spec ?? []) {
    const value = headers.get(name);
    if (value === null) continue;
    const list = () => value.split(",").map((s) => s.trim()).filter((s) => s !== "");
    out[name] =
      kind === "number" ? Number(value) :
      kind === "boolean" ? value === "true" :
      kind === "array" ? list() :
      kind === "numbers" ? list().map(Number) :
      value;
  }
  return out;
}

function isJSON(response: Response): boolean {
  return /^application\/([a-z0-9.+-]+\+)?json\b/i.test(response.headers.get("Content-Type") ?? "");
}

function problemMessage(status: number, error: unknown): string {
  if (typeof error === "object" && error !== null) {
    const p = error as { detail?: unknown; title?: unknown };
    if (typeof p.detail === "string" && p.detail !== "") return p.detail;
    if (typeof p.title === "string" && p.title !== "") return p.title;
  }
  return "HTTP " + status;
}

`
