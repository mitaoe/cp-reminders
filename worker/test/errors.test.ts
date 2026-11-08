import { describe, expect, it } from "vitest";
import { FAILURE, UpstreamError, failureCode, httpFailure } from "../src/errors.ts";

describe("failureCode", () => {
  it("uses the code of an upstream error", () => {
    expect(failureCode(new UpstreamError(httpFailure(401), "dispatch HTTP 401: Bad credentials"))).toBe("HTTP 401");
    expect(failureCode(new UpstreamError(FAILURE.empty, "clist returned no contests"))).toBe(FAILURE.empty);
  });

  it("maps a fired timeout signal to timeout", () => {
    expect(failureCode(new DOMException("The operation timed out.", "TimeoutError"))).toBe(FAILURE.timeout);
    expect(failureCode(new DOMException("aborted", "AbortError"))).toBe(FAILURE.timeout);
  });

  it("maps an unparsable body to invalid response", () => {
    expect(failureCode(new SyntaxError("Unexpected token < in JSON"))).toBe(FAILURE.invalid);
  });

  it("maps anything else, such as a failed connection, to network error", () => {
    expect(failureCode(new TypeError("fetch failed"))).toBe(FAILURE.network);
    expect(failureCode(new Error("connection reset"))).toBe(FAILURE.network);
    expect(failureCode("string thrown")).toBe(FAILURE.network);
  });
});
