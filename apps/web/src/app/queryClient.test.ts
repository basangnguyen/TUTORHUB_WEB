import { MutationObserver } from "@tanstack/react-query";
import { APIRequestError } from "@tutorhub/api-client";
import { describe, expect, it, vi } from "vitest";
import {
  advancePrincipalGeneration,
  createTutorHubQueryClient,
  currentPrincipalGeneration,
} from "./queryClient";

describe("TutorHub query client session boundary", () => {
  it("keeps a monotonic principal generation outside the query cache", () => {
    const queryClient = createTutorHubQueryClient();

    expect(currentPrincipalGeneration(queryClient)).toBe(0);
    expect(advancePrincipalGeneration(queryClient)).toBe(1);

    queryClient.clear();

    expect(currentPrincipalGeneration(queryClient)).toBe(1);
  });

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

  it.each(["auth_invalid_current_password", "auth_invalid_verification_code"])(
    "keeps the session caches when a mutation 401 only rejects a typed credential (%s)",
    async (code) => {
      const queryClient = createTutorHubQueryClient();
      const privateData = { name: "Private workspace" };
      const currentUser = { user: { id: "current-user" } };
      queryClient.setQueryData(["tenants", "private"], privateData);
      queryClient.setQueryData(["auth", "me"], currentUser);
      const generation = currentPrincipalGeneration(queryClient);
      const mutation = new MutationObserver(queryClient, {
        mutationFn: async () => {
          throw new APIRequestError(401, {
            type: "urn:tutorhub:problem:http-401",
            title: "Rejected credential",
            status: 401,
            code,
          });
        },
      });

      await expect(mutation.mutate()).rejects.toMatchObject({ status: 401 });

      expect(queryClient.getQueryData(["tenants", "private"])).toEqual(
        privateData,
      );
      expect(queryClient.getQueryData(["auth", "me"])).toEqual(currentUser);
      expect(currentPrincipalGeneration(queryClient)).toBe(generation);
    },
  );

  it.each(["auth_invalid_current_password", "auth_invalid_verification_code"])(
    "keeps the session caches when a query 401 only rejects a typed credential (%s)",
    async (code) => {
      const queryClient = createTutorHubQueryClient();
      const privateData = { title: "Private class" };
      queryClient.setQueryData(["classes", "private"], privateData);

      await expect(
        queryClient.fetchQuery({
          queryKey: ["security", "credential-check"],
          queryFn: async () => {
            throw new APIRequestError(401, {
              type: "urn:tutorhub:problem:http-401",
              title: "Rejected credential",
              status: 401,
              code,
            });
          },
        }),
      ).rejects.toMatchObject({ status: 401 });

      expect(queryClient.getQueryData(["classes", "private"])).toEqual(
        privateData,
      );
    },
  );

  it("still purges the session caches for a 401 problem without a credential code", async () => {
    const queryClient = createTutorHubQueryClient();
    queryClient.setQueryData(["tenants", "private"], {
      name: "Private workspace",
    });
    const generation = currentPrincipalGeneration(queryClient);
    const mutation = new MutationObserver(queryClient, {
      mutationFn: async () => {
        throw new APIRequestError(401, {
          type: "urn:tutorhub:problem:http-401",
          title: "Authentication required",
          status: 401,
        });
      },
    });

    await expect(mutation.mutate()).rejects.toMatchObject({ status: 401 });

    expect(queryClient.getQueryData(["tenants", "private"])).toBeUndefined();
    expect(queryClient.getMutationCache().getAll()).toHaveLength(0);
    expect(currentPrincipalGeneration(queryClient)).toBeGreaterThan(generation);
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

  it.each([
    "auth_link_intent_mismatch",
    "auth_assurance_required",
    "auth_recent_authentication_required",
  ])(
    "keeps the session and its caches after a link-intent mutation 403 (%s)",
    async (code) => {
      const queryClient = createTutorHubQueryClient();
      const privateData = { name: "Private workspace" };
      const currentUser = { user: { id: "current-user" } };
      queryClient.setQueryData(["tenants", "private"], privateData);
      queryClient.setQueryData(["auth", "me"], currentUser);
      const generation = currentPrincipalGeneration(queryClient);
      const mutation = new MutationObserver(queryClient, {
        mutationFn: async () => {
          throw new APIRequestError(403, {
            type: "about:blank",
            title: "Forbidden",
            status: 403,
            code,
          });
        },
      });

      await expect(mutation.mutate()).rejects.toMatchObject({ status: 403 });

      expect(queryClient.getQueryData(["auth", "me"])).toEqual(currentUser);
      expect(queryClient.getQueryData(["tenants", "private"])).toEqual(
        privateData,
      );
      expect(currentPrincipalGeneration(queryClient)).toBe(generation);
    },
  );
});
