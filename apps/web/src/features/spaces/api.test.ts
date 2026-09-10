import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { getSpace, createSpace, updateSpace } from "./api";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const space = {
  id: "s1",
  projectId: "p1",
  name: "Kitchen",
  type: "Kitchen",
  description: "Main kitchen",
  createdAt: "2026-01-15T00:00:00Z",
};

describe("spaces api", () => {
  it("getSpace fetches a single space by id", async () => {
    server.use(http.get(`${baseUrl}/spaces/:id`, () => HttpResponse.json(space)));
    const result = await getSpace("s1");
    expect(result).toEqual(space);
  });

  it("createSpace posts the full create body", async () => {
    let capturedBody: unknown = null;
    server.use(
      http.post(`${baseUrl}/spaces`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json(space);
      })
    );
    await createSpace({ projectId: "p1", name: "Kitchen", type: "Kitchen", description: "Main kitchen" });
    expect(capturedBody).toEqual({
      projectId: "p1",
      name: "Kitchen",
      type: "Kitchen",
      description: "Main kitchen",
    });
  });

  it("updateSpace always resubmits name/type/description in full, never a partial patch", async () => {
    let capturedBody: unknown = null;
    server.use(
      http.patch(`${baseUrl}/spaces/:id`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json(space);
      })
    );
    await updateSpace("s1", { name: "Kitchen", type: "Kitchen", description: "" });
    expect(capturedBody).toEqual({ name: "Kitchen", type: "Kitchen", description: "" });
  });
});
