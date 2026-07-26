import { QueryClient } from "@tanstack/react-query";
import type { CurrentUser } from "@tutorhub/api-client";
import { describe, expect, it } from "vitest";
import {
  advancePrincipalGeneration,
  currentPrincipalGeneration,
} from "../../app/queryClient";
import { isCurrentCalendarPrincipal } from "./queries";

const tenantA = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const tenantB = "fa84bf8c-8205-4162-8ee9-d86ca11ddf26";
const userID = "be85eb92-0f18-4163-85ba-50e4d343d632";

function setPrincipal(
  queryClient: QueryClient,
  tenantID: string,
  principalUserID = userID,
) {
  queryClient.setQueryData(["auth", "me"], {
    active_tenant: { id: tenantID },
    user: { id: principalUserID },
  } as CurrentUser);
}

describe("calendar principal mutation boundary", () => {
  it("rejects a mutation result after workspace generation advances", () => {
    const queryClient = new QueryClient();
    setPrincipal(queryClient, tenantA);
    const snapshot = {
      generation: currentPrincipalGeneration(queryClient),
      tenantID: tenantA,
      userID,
    };

    expect(isCurrentCalendarPrincipal(queryClient, snapshot)).toBe(true);

    advancePrincipalGeneration(queryClient);
    setPrincipal(queryClient, tenantB);

    expect(isCurrentCalendarPrincipal(queryClient, snapshot)).toBe(false);
    expect(
      isCurrentCalendarPrincipal(queryClient, {
        generation: currentPrincipalGeneration(queryClient),
        tenantID: tenantB,
        userID,
      }),
    ).toBe(true);
  });

  it("rejects a mutation result after logout clears the current user", () => {
    const queryClient = new QueryClient();
    setPrincipal(queryClient, tenantA);
    const snapshot = {
      generation: currentPrincipalGeneration(queryClient),
      tenantID: tenantA,
      userID,
    };

    advancePrincipalGeneration(queryClient);
    queryClient.removeQueries({ exact: true, queryKey: ["auth", "me"] });

    expect(isCurrentCalendarPrincipal(queryClient, snapshot)).toBe(false);
  });
});
