import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { useTenantCapabilities } from "../app/tenantCapabilities";
import { availableTenantCapabilities } from "../test/tenantCapabilities";
import { TenantFeatureControlsPanel } from "./TenantFeatureControlsPanel";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
    },
  });
}

function FeatureControlsHarness() {
  const capabilities = useTenantCapabilities(tenantID);
  return (
    <TenantFeatureControlsPanel
      capabilities={capabilities}
      tenantID={tenantID}
    />
  );
}

function renderPanel(fetchMock: ReturnType<typeof vi.fn>) {
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });
  vi.stubGlobal("fetch", fetchMock);
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="en">
        <FeatureControlsHarness />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("TenantFeatureControlsPanel", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("renders effective quotas with accessible meters and editable controls", async () => {
    renderPanel(
      vi
        .fn()
        .mockResolvedValue(jsonResponse(availableTenantCapabilities(tenantID))),
    );

    expect(
      await screen.findByRole("heading", { name: "Features and quotas" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("meter", { name: "Workspace members" }),
    ).toHaveAttribute("max", "100");
    expect(
      screen.getByRole("checkbox", { name: "Member invitations" }),
    ).toBeChecked();
  });

  it("edits configured values when deployment guardrails clamp effective values", async () => {
    const base = availableTenantCapabilities(tenantID);
    const capabilities = {
      ...base,
      features: {
        ...base.features,
        membership_invitations: {
          ...base.features.membership_invitations,
          enabled: false,
        },
      },
      quotas: {
        ...base.quotas,
        active_classes: {
          ...base.quotas.active_classes,
          limit: 10,
          remaining: 9,
        },
      },
    };

    renderPanel(vi.fn().mockResolvedValue(jsonResponse(capabilities)));

    expect(
      await screen.findByRole("meter", { name: "Active classes" }),
    ).toHaveAttribute("max", "10");
    expect(
      screen.getByRole("checkbox", { name: "Member invitations" }),
    ).toBeChecked();
    expect(
      screen.getByRole("spinbutton", { name: "Active classes" }),
    ).toHaveValue(25);
  });

  it("fails closed when the server returns another tenant projection", async () => {
    renderPanel(
      vi
        .fn()
        .mockResolvedValue(
          jsonResponse(
            availableTenantCapabilities("b5e07a4b-d8b2-4552-9f2f-e96b865cad97"),
          ),
        ),
    );

    expect(
      await screen.findByRole("heading", {
        name: "Features and quotas unavailable",
      }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("checkbox", { name: "Member invitations" }),
    ).not.toBeInTheDocument();
  });

  it("keeps the draft and offers reload after an optimistic conflict", async () => {
    const capabilities = availableTenantCapabilities(tenantID);
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith(`/api/v1/tenants/${tenantID}/capabilities`)) {
        return Promise.resolve(jsonResponse(capabilities));
      }
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "csrf-token" }));
      }
      if (
        request.url.endsWith(`/api/v1/tenants/${tenantID}/feature-controls`) &&
        request.method === "PUT"
      ) {
        return Promise.resolve(
          jsonResponse(
            {
              type: "urn:tutorhub:problem:http-409",
              code: "feature_control_conflict",
              title: "Feature controls changed",
              status: 409,
            },
            409,
          ),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderPanel(fetchMock);

    const checkbox = await screen.findByRole("checkbox", {
      name: "Member invitations",
    });
    fireEvent.click(checkbox);
    fireEvent.click(
      screen.getByRole("button", { name: "Save features and quotas" }),
    );

    expect(
      await screen.findByText(/configuration changed elsewhere/i),
    ).toBeInTheDocument();
    expect(checkbox).not.toBeChecked();
    expect(
      screen.getByRole("button", { name: "Load latest configuration" }),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          ([request]) => (request as Request).method === "PUT",
        ),
      ).toBe(true),
    );
  });
});
