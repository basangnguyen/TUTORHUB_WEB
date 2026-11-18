import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import type * as ApiClient from "@tutorhub/api-client";
import {
  APIRequestError,
  type FacebookDeletionStatus,
} from "@tutorhub/api-client";
import { createMemoryRouter } from "react-router";
import { RouterProvider } from "react-router/dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, type Language } from "../app/i18n";
import { DataDeletionStatusPage } from "./DataDeletionStatusPage";

const { api } = vi.hoisted(() => ({
  api: { getFacebookDeletionStatus: vi.fn() },
}));

vi.mock("@tutorhub/api-client", async (importOriginal) => ({
  ...(await importOriginal<typeof ApiClient>()),
  ...api,
}));

const CODE = "0123456789abcdef0123456789abcdef";

function apiError(status: number, code?: string, retryAfterSeconds?: number) {
  return new APIRequestError(
    status,
    { status, title: "Rejected", type: "about:blank", code },
    retryAfterSeconds,
  );
}

async function settle() {
  await act(async () => {
    for (let tick = 0; tick < 12; tick += 1) {
      await Promise.resolve();
    }
  });
}

async function renderPage({
  language = "en",
  search = `?code=${CODE}`,
}: { language?: Language; search?: string } = {}) {
  const router = createMemoryRouter(
    [{ path: "/legal/data-deletion", element: <DataDeletionStatusPage /> }],
    { initialEntries: [`/legal/data-deletion${search}`] },
  );
  const view = render(
    <I18nProvider initialLanguage={language}>
      <RouterProvider router={router} />
    </I18nProvider>,
  );
  await settle();
  return view;
}

beforeEach(() => {
  api.getFacebookDeletionStatus.mockReset();
  api.getFacebookDeletionStatus.mockResolvedValue({
    status: "completed",
  } satisfies FacebookDeletionStatus);
});

afterEach(() => {
  cleanup();
});

describe("DataDeletionStatusPage", () => {
  it("announces loading, then shows a completed receipt", async () => {
    let resolveStatus: (value: FacebookDeletionStatus) => void = () =>
      undefined;
    api.getFacebookDeletionStatus.mockReturnValue(
      new Promise<FacebookDeletionStatus>((resolve) => {
        resolveStatus = resolve;
      }),
    );
    await renderPage();

    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Facebook data deletion status",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("status", { name: "Checking the confirmation code" }),
    ).toBeInTheDocument();

    await act(async () => resolveStatus({ status: "completed" }));
    await settle();
    expect(screen.getByText("Completed")).toBeInTheDocument();
    expect(
      screen.getByText(
        "The data TutorHub received from Facebook for this request has been deleted.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByText(CODE)).toBeInTheDocument();
    expect(api.getFacebookDeletionStatus).toHaveBeenCalledWith(
      CODE,
      expect.objectContaining({ baseUrl: "/api" }),
    );
  });

  it("shows a received receipt", async () => {
    api.getFacebookDeletionStatus.mockResolvedValue({ status: "received" });
    await renderPage();

    expect(screen.getByText("Received")).toBeInTheDocument();
    expect(
      screen.getByText(
        "TutorHub has received the deletion request and is processing it.",
      ),
    ).toBeInTheDocument();
  });

  it("reports an unknown code (404)", async () => {
    api.getFacebookDeletionStatus.mockRejectedValue(
      apiError(404, "auth_deletion_request_unavailable"),
    );
    await renderPage();

    expect(
      screen.getByRole("heading", { name: "Code not found" }),
    ).toBeInTheDocument();
  });

  it.each([
    "",
    "?code=",
    "?code=ABCDEF0123456789ABCDEF0123456789",
    "?code=0123456789abcdef",
    `?code=${CODE}&code=${CODE}`,
    "?code=%3Cscript%3E",
  ])("does not call the API for a malformed code (%s)", async (search) => {
    const view = await renderPage({ search });

    expect(api.getFacebookDeletionStatus).not.toHaveBeenCalled();
    expect(
      screen.getByRole("heading", { name: "Code not found" }),
    ).toBeInTheDocument();
    // The raw query value is never rendered.
    expect(view.container.innerHTML).not.toContain("script");
  });

  it("locks out a 429 and offers Retry", async () => {
    api.getFacebookDeletionStatus
      .mockRejectedValueOnce(apiError(429, undefined, 20))
      .mockResolvedValueOnce({ status: "completed" });
    await renderPage();

    expect(
      screen.getByText("Too many checks. Please try again in 20 seconds."),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await settle();
    expect(api.getFacebookDeletionStatus).toHaveBeenCalledTimes(2);
    expect(screen.getByText("Completed")).toBeInTheDocument();
  });

  it.each([
    [apiError(503), "TutorHub is busy right now. Please try again shortly."],
    [new TypeError("offline"), "Check your connection and try again."],
  ])("offers Retry after %s", async (error, text) => {
    api.getFacebookDeletionStatus.mockRejectedValue(error);
    await renderPage();

    expect(
      screen.getByRole("heading", { name: "We couldn't check the status" }),
    ).toBeInTheDocument();
    expect(screen.getByText(text)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Try again" })).toBeVisible();
  });

  it("always explains how to request deletion, without personal data", async () => {
    await renderPage();

    expect(
      screen.getByRole("heading", {
        level: 2,
        name: "How to request deletion",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Delete your TutorHub account" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "This page only shows the request status. It never shows personal data.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Back to sign-in" }),
    ).toHaveAttribute("href", "/sign-in");
  });

  it("renders Vietnamese copy", async () => {
    await renderPage({ language: "vi" });

    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Trạng thái yêu cầu xóa dữ liệu Facebook",
      }),
    ).toBeInTheDocument();
    expect(screen.getByText("Đã hoàn tất")).toBeInTheDocument();
  });
});
