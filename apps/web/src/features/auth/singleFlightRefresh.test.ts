import { afterEach, describe, expect, it, vi } from "vitest";
import { ensureFreshToken } from "./singleFlightRefresh";
import { getAccessToken, setAccessToken } from "@/lib/api/client";

describe("ensureFreshToken", () => {
  afterEach(() => {
    setAccessToken(null);
    vi.restoreAllMocks();
  });

  it("coalesces concurrent calls into exactly one /auth/refresh request", async () => {
    let callCount = 0;
    const fetchMock = vi.fn().mockImplementation(async () => {
      callCount += 1;
      return new Response(JSON.stringify({ accessToken: "fresh-token", mustChangePassword: false }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    const [tokenA, tokenB, tokenC] = await Promise.all([
      ensureFreshToken(),
      ensureFreshToken(),
      ensureFreshToken(),
    ]);

    expect(callCount).toBe(1);
    expect(tokenA).toBe("fresh-token");
    expect(tokenB).toBe("fresh-token");
    expect(tokenC).toBe("fresh-token");
    expect(getAccessToken()).toBe("fresh-token");
  });

  it("clears the token holder and rejects all callers when refresh fails", async () => {
    setAccessToken("stale-token");
    const fetchMock = vi.fn().mockResolvedValue(new Response("{}", { status: 401 }));
    vi.stubGlobal("fetch", fetchMock);

    const results = await Promise.allSettled([ensureFreshToken(), ensureFreshToken()]);

    expect(results[0].status).toBe("rejected");
    expect(results[1].status).toBe("rejected");
    expect(getAccessToken()).toBeNull();
  });

  it("starts a new refresh after a prior one has settled", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ accessToken: "token-1", mustChangePassword: false }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        })
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ accessToken: "token-2", mustChangePassword: false }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        })
      );
    vi.stubGlobal("fetch", fetchMock);

    const first = await ensureFreshToken();
    const second = await ensureFreshToken();

    expect(first).toBe("token-1");
    expect(second).toBe("token-2");
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
