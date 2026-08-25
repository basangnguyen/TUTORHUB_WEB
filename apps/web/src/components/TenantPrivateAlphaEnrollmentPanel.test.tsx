import { APIRequestError } from "@tutorhub/api-client";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { I18nProvider } from "../app/i18n";
import { TenantPrivateAlphaEnrollmentPanel } from "./TenantPrivateAlphaEnrollmentPanel";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";

const hookMocks = vi.hoisted(() => ({
  usePrivateAlphaEnrollment: vi.fn(),
  useUpdatePrivateAlphaEnrollment: vi.fn(),
}));

vi.mock("../app/privateAlphaEnrollment", () => hookMocks);

const refetch = vi.fn();
const mutate = vi.fn();
const reset = vi.fn();

function enrollment(status: "active" | "not_enrolled" | "withdrawn") {
  return {
    accepted_at: status === "active" ? "2026-08-25T08:00:00Z" : null,
    allowed_actions: { manage_enrollment: true },
    notice_version: "p5-collab-19-v1",
    program: "classroom_whiteboards",
    revision: 3,
    status,
    tenant_id: tenantID,
    withdrawn_at: status === "withdrawn" ? "2026-08-25T09:00:00Z" : null,
  };
}

function renderPanel() {
  return render(
    <MemoryRouter>
      <I18nProvider initialLanguage="en">
        <TenantPrivateAlphaEnrollmentPanel tenantID={tenantID} />
      </I18nProvider>
    </MemoryRouter>,
  );
}

describe("TenantPrivateAlphaEnrollmentPanel", () => {
  beforeEach(() => {
    refetch.mockReset().mockResolvedValue(undefined);
    mutate.mockReset();
    reset.mockReset();
    hookMocks.usePrivateAlphaEnrollment.mockReturnValue({
      data: enrollment("not_enrolled"),
      error: null,
      isError: false,
      isPending: false,
      refetch,
    });
    hookMocks.useUpdatePrivateAlphaEnrollment.mockReturnValue({
      error: null,
      isError: false,
      isPending: false,
      mutate,
      reset,
    });
  });

  afterEach(() => {
    cleanup();
  });

  it("requires acknowledgment before enrollment and exposes support", () => {
    renderPanel();

    const enrollButton = screen.getByRole("button", {
      name: "Join Private Alpha",
    });
    expect(enrollButton).toBeDisabled();
    expect(
      screen.getByRole("link", { name: "Open Private Alpha support" }),
    ).toHaveAttribute("href", "/app/settings?source=whiteboard-private-alpha");

    fireEvent.click(
      screen.getByRole("checkbox", {
        name: "I have read and accept the Private Alpha terms.",
      }),
    );
    expect(enrollButton).toBeEnabled();
    fireEvent.click(enrollButton);

    expect(reset).toHaveBeenCalledTimes(1);
    expect(mutate).toHaveBeenCalledWith(
      {
        enrolled: true,
        expected_revision: 3,
        notice_version: "p5-collab-19-v1",
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("withdraws an active enrollment with optimistic revision protection", () => {
    hookMocks.usePrivateAlphaEnrollment.mockReturnValue({
      data: enrollment("active"),
      error: null,
      isError: false,
      isPending: false,
      refetch,
    });
    renderPanel();

    fireEvent.click(
      screen.getByRole("button", { name: "Leave Private Alpha" }),
    );

    expect(mutate).toHaveBeenCalledWith(
      {
        enrolled: false,
        expected_revision: 3,
        notice_version: "p5-collab-19-v1",
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("surfaces a 409 conflict and reloads the latest enrollment", async () => {
    hookMocks.useUpdatePrivateAlphaEnrollment.mockReturnValue({
      error: new APIRequestError(409),
      isError: true,
      isPending: false,
      mutate,
      reset,
    });
    renderPanel();

    expect(screen.getByRole("alert")).toHaveTextContent(
      "The enrollment changed elsewhere.",
    );
    fireEvent.click(screen.getByRole("button", { name: "Load latest" }));

    await waitFor(() => expect(refetch).toHaveBeenCalledTimes(1));
    expect(reset).toHaveBeenCalledTimes(1);
  });
});
