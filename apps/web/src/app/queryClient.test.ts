import { MutationObserver } from "@tanstack/react-query";
import { APIRequestError } from "@tutorhub/api-client";
import { describe, expect, it, vi } from "vitest";
import { createTutorHubQueryClient } from "./queryClient";

describe("TutorHub query client session boundary", () => {
  it("purges inactive private queries after a query 401 without retrying it", async () => {
    const queryClient = createTutorHubQueryClient();
    const attempts = vi.fn();
    queryClient.setQueryData(["classes", "private"], {
      title: "Private class",
    });

    await expect(
      queryClient.fetchQuery({
        queryKey: ["classes", "expired"],
        queryFn: async () => {
          attempts();
          throw new APIRequestError(401);
        },
      }),
    ).rejects.toMatchObject({ status: 401 });

    expect(attempts).toHaveBeenCalledTimes(1);
    expect(queryClient.getQueryData(["classes", "private"])).toBeUndefined();
  });

  it("purges private query and mutation caches after a mutation 401", async () => {
    const queryClient = createTutorHubQueryClient();
    queryClient.setQueryData(["tenants", "private"], {
      name: "Private workspace",
    });
    const mutation = new MutationObserver(queryClient, {
      mutationFn: async () => {
        throw new APIRequestError(401);
      },
    });

    await expect(mutation.mutate()).rejects.toMatchObject({ status: 401 });

    expect(queryClient.getQueryData(["tenants", "private"])).toBeUndefined();
    expect(queryClient.getMutationCache().getAll()).toHaveLength(0);
  });

  it("does not purge private data for a non-session 403", async () => {
    const queryClient = createTutorHubQueryClient();
    const attempts = vi.fn();
    const privateData = { title: "Still authorized elsewhere" };
    queryClient.setQueryData(["classes", "private"], privateData);

    await expect(
      queryClient.fetchQuery({
        queryKey: ["classes", "forbidden-action"],
        queryFn: async () => {
          attempts();
          throw new APIRequestError(403);
        },
      }),
    ).rejects.toMatchObject({ status: 403 });

    expect(attempts).toHaveBeenCalledTimes(1);
    expect(queryClient.getQueryData(["classes", "private"])).toEqual(
      privateData,
    );
  });
});
