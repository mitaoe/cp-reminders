export const FAILURE = {
  timeout: "timeout",
  network: "network error",
  invalid: "invalid response",
  empty: "no contests",
} as const;

export const httpFailure = (status: number): string => `HTTP ${status}`;

export class UpstreamError extends Error {
  constructor(
    readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = "UpstreamError";
  }
}

export const errorMessage = (e: unknown): string => (e instanceof Error ? e.message : String(e));

export function failureCode(e: unknown): string {
  if (e instanceof UpstreamError) return e.code;
  if (e instanceof Error && (e.name === "TimeoutError" || e.name === "AbortError")) return FAILURE.timeout;
  if (e instanceof SyntaxError) return FAILURE.invalid;
  return FAILURE.network;
}
