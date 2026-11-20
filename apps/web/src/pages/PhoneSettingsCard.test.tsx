import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type * as ApiClient from "@tutorhub/api-client";
import {
  APIRequestError,
  type PhoneChallengeResponse,
  type PhoneSummary,
} from "@tutorhub/api-client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, type Language } from "../app/i18n";
import type { StepUpRequestOptions, StepUpRetry } from "../app/security";
import { PhoneSettingsCard } from "./PhoneSettingsCard";

const { api } = vi.hoisted(() => ({
  api: {
    getMyPhone: vi.fn(),
    removeMyPhone: vi.fn(),
    resendMyPhoneChallenge: vi.fn(),
    rotateCSRFToken: vi.fn(),
    startMyPhone: vi.fn(),
    verifyMyPhone: vi.fn(),
  },
}));

vi.mock("@tutorhub/api-client", async (importOriginal) => ({
  ...(await importOriginal<typeof ApiClient>()),
  ...api,
}));

const ACTIVE: PhoneSummary = {
  calling_code: 84,
  country_code: "VN",
  id: "0d6f3c1e-7b0e-4f43-9b3f-1f0c3a0d9e21",
  last_used_at: "2026-11-19T08:00:00Z",
  last4: "5678",
  masked_number: "+84 ***5678",
  stale: false,
  verified_at: "2026-09-01T07:00:00Z",
};

const STALE: PhoneSummary = { ...ACTIVE, stale: true };

function challenge(id: string): PhoneChallengeResponse {
  return {
    challenge_id: id,
    cooldown_seconds: 60,
    expires_in_seconds: 300,
    masked_destination: "+84 ***5678",
    resend_after_seconds: 60,
  };
}

function apiError(status: number, code?: string, retryAfterSeconds?: number) {
  return new APIRequestError(
    status,
    { status, title: "Rejected", type: "about:blank", code },
    retryAfterSeconds,
  );
}

type StepUpCall = [StepUpRetry | undefined, StepUpRequestOptions | undefined];

const requestStepUp =
  vi.fn<(retry?: StepUpRetry, options?: StepUpRequestOptions) => void>();

async function settle() {
  await act(async () => {
    for (let tick = 0; tick < 12; tick += 1) await Promise.resolve();
  });
  await act(() => new Promise<void>((resolve) => setTimeout(resolve, 0)));
  await act(async () => {
    for (let tick = 0; tick < 12; tick += 1) await Promise.resolve();
  });
}

async function renderCard(language: Language = "en") {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage={language}>
        <PhoneSettingsCard
          allowedCountries={["VN"]}
          dateFormatter={
            new Intl.DateTimeFormat("en-US", {
              dateStyle: "medium",
              timeZone: "UTC",
            })
          }
          onRequestStepUp={requestStepUp}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  await settle();
  return queryClient;
}

function card() {
  return screen.getByRole("region", { name: "Phone number" });
}

const ADD_FORM = "Add a new phone number";

/** The number and code steps are different forms with the same name. */
function flowForm(name = ADD_FORM) {
  return within(card()).getByRole("form", { name });
}

async function openAdd() {
  fireEvent.click(
    within(card()).getByRole("button", { name: "Add phone number" }),
  );
  await settle();
  return ADD_FORM;
}

async function sendNumber(name: string, value = "0912 345 678") {
  fireEvent.change(
    within(flowForm(name)).getByRole("textbox", { name: "Mobile number" }),
    { target: { value } },
  );
  fireEvent.click(
    within(flowForm(name)).getByRole("button", {
      name: "Send verification code",
    }),
  );
  await settle();
}

async function sendCode(name: string, code = "123456") {
  fireEvent.change(
    within(flowForm(name)).getByRole("textbox", {
      name: "6-digit verification code",
    }),
    { target: { value: code } },
  );
  fireEvent.click(
    within(flowForm(name)).getByRole("button", { name: "Verify number" }),
  );
  await settle();
}

/** Runs the action the card handed to the step-up dialog. */
async function completeStepUp(call = 0) {
  const retry = (requestStepUp.mock.calls[call] as StepUpCall)[0];
  await act(async () => {
    await retry?.();
  });
  await settle();
}

describe("PhoneSettingsCard (AUTH-10B)", () => {
  beforeEach(() => {
    for (const mock of Object.values(api)) mock.mockReset();
    requestStepUp.mockReset();
    api.rotateCSRFToken.mockResolvedValue({ csrf_token: "csrf" });
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("explains the empty state and offers to add a number", async () => {
    api.getMyPhone.mockResolvedValue({ phone: null });
    await renderCard();
    expect(within(card()).getByText("No phone number yet")).toBeInTheDocument();
    expect(
      within(card()).getByText(/Every account keeps its main email address/),
    ).toBeInTheDocument();
    expect(
      within(card()).getByRole("button", { name: "Add phone number" }),
    ).toBeEnabled();
  });

  it("shows a verified number with its country and dates", async () => {
    api.getMyPhone.mockResolvedValue({ phone: ACTIVE });
    await renderCard();
    const region = card();
    expect(within(region).getByText("+84 ***5678")).toBeInTheDocument();
    expect(within(region).getByText("Verified")).toBeInTheDocument();
    expect(within(region).getByText("Country: VN (+84)")).toBeInTheDocument();
    expect(region).toHaveTextContent("Added Sep 1, 2026");
    expect(region).toHaveTextContent("Last used or verified Nov 19, 2026");
    expect(
      within(region).queryByRole("button", { name: "Verify again" }),
    ).toBeNull();
    expect(
      within(region).getByRole("button", { name: "Change phone number" }),
    ).toBeInTheDocument();
    expect(
      within(region).getByRole("button", { name: "Remove phone number" }),
    ).toBeInTheDocument();
  });

  it("flags a stale number and verifies the same number again", async () => {
    const reverify = "Verify your phone number again";
    api.getMyPhone
      .mockResolvedValueOnce({ phone: STALE })
      .mockResolvedValue({ phone: ACTIVE });
    api.startMyPhone.mockResolvedValue(challenge("verify-1"));
    api.verifyMyPhone.mockResolvedValue({ phone: ACTIVE });
    await renderCard();
    expect(
      within(card()).getByText("Re-verification needed"),
    ).toBeInTheDocument();
    expect(
      within(card()).getByText(/hasn't been used for a while/),
    ).toBeInTheDocument();

    fireEvent.click(
      within(card()).getByRole("button", { name: "Verify again" }),
    );
    await settle();
    expect(
      within(flowForm(reverify)).getByText(
        "Enter +84 ***5678 again to receive a new verification code.",
      ),
    ).toBeInTheDocument();
    expect(
      within(flowForm(reverify)).getByRole("textbox", {
        name: "Mobile number",
      }),
    ).toHaveFocus();
    await sendNumber(reverify, "+84 912 345 678");
    expect(api.startMyPhone).toHaveBeenCalledWith(
      { country_code: "VN", phone_number: "+84912345678" },
      { baseUrl: "/api" },
    );
    await sendCode(reverify);
    expect(api.verifyMyPhone).toHaveBeenCalledWith(
      { challenge_id: "verify-1", code: "123456" },
      { baseUrl: "/api" },
    );
    expect(
      within(card()).getByText("Phone number +84 ***5678 verified again."),
    ).toBeInTheDocument();
    expect(within(card()).getByText("Verified")).toBeInTheDocument();
  });

  it("adds a number through the 6-digit code and drops the code at once", async () => {
    api.getMyPhone
      .mockResolvedValueOnce({ phone: null })
      .mockResolvedValue({ phone: ACTIVE });
    api.startMyPhone.mockResolvedValue(challenge("verify-1"));
    api.verifyMyPhone.mockResolvedValue({ phone: ACTIVE });
    await renderCard();
    const name = await openAdd();
    const number = within(flowForm(name)).getByRole("textbox", {
      name: "Mobile number",
    });
    expect(number).toHaveFocus();
    expect(number).toHaveAttribute("type", "tel");
    expect(number).toHaveAttribute("autocomplete", "tel");

    fireEvent.click(
      within(flowForm(name)).getByRole("button", {
        name: "Send verification code",
      }),
    );
    await settle();
    expect(
      within(flowForm(name)).getByText("Enter a mobile number."),
    ).toBeInTheDocument();
    expect(api.startMyPhone).not.toHaveBeenCalled();

    await sendNumber(name);
    expect(api.startMyPhone).toHaveBeenCalledWith(
      { country_code: "VN", phone_number: "0912345678" },
      { baseUrl: "/api" },
    );
    const form = flowForm(name);
    expect(
      within(form).getByText("A 6-digit code was sent to +84 ***5678."),
    ).toBeInTheDocument();
    expect(
      within(form).getByText("The code expires in 5:00."),
    ).toBeInTheDocument();
    const code = within(form).getByRole("textbox", {
      name: "6-digit verification code",
    });
    expect(code).toHaveFocus();
    expect(code).toHaveAttribute("autocomplete", "one-time-code");
    expect(
      within(form).getByRole("button", { name: "Resend code (60s)" }),
    ).toBeDisabled();

    fireEvent.change(code, { target: { value: "12x3456" } });
    expect(code).toHaveValue("123456");
    fireEvent.click(
      within(form).getByRole("button", { name: "Verify number" }),
    );
    expect(code).toHaveValue("");
    await settle();
    expect(api.verifyMyPhone).toHaveBeenCalledWith(
      { challenge_id: "verify-1", code: "123456" },
      { baseUrl: "/api" },
    );
    expect(
      within(card()).getByText("Phone number +84 ***5678 added."),
    ).toBeInTheDocument();
    expect(within(card()).getByText("+84 ***5678")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Phone number" })).toHaveFocus();
  });

  it("resends after the cooldown through the Settings transaction", async () => {
    // Only the countdown clock is faked; promises and timeouts stay real.
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval", "Date"] });
    api.getMyPhone.mockResolvedValue({ phone: null });
    api.startMyPhone.mockResolvedValue(challenge("verify-1"));
    api.resendMyPhoneChallenge.mockResolvedValue(challenge("verify-2"));
    api.verifyMyPhone.mockResolvedValue({ phone: ACTIVE });
    await renderCard();
    const name = await openAdd();
    await sendNumber(name);
    for (let second = 0; second < 60; second += 1) {
      act(() => {
        vi.advanceTimersByTime(1000);
      });
    }
    expect(
      within(flowForm(name)).getByText("The code expires in 4:00."),
    ).toBeInTheDocument();
    fireEvent.click(
      within(flowForm(name)).getByRole("button", { name: "Resend code" }),
    );
    await settle();
    expect(api.resendMyPhoneChallenge).toHaveBeenCalledWith({
      baseUrl: "/api",
    });
    await sendCode(name);
    expect(api.verifyMyPhone).toHaveBeenCalledWith(
      { challenge_id: "verify-2", code: "123456" },
      { baseUrl: "/api" },
    );
  });

  it("steps up for a stale session and sends the code once confirmed", async () => {
    api.getMyPhone.mockResolvedValue({ phone: null });
    api.startMyPhone
      .mockRejectedValueOnce(
        apiError(403, "auth_recent_authentication_required"),
      )
      .mockResolvedValueOnce(challenge("verify-1"));
    await renderCard();
    const name = await openAdd();
    await sendNumber(name);

    expect(requestStepUp).toHaveBeenCalledTimes(1);
    expect((requestStepUp.mock.calls[0] as StepUpCall)[1]).toEqual({
      requireAal2: false,
    });
    expect(within(card()).getByRole("alert")).toHaveTextContent(
      "Confirm it's you to keep changing your phone number.",
    );
    await completeStepUp();
    expect(api.startMyPhone).toHaveBeenCalledTimes(2);
    expect(
      within(flowForm(name)).getByText(
        "Identity confirmed. A new 6-digit code was sent to +84 ***5678.",
      ),
    ).toBeInTheDocument();
    expect(requestStepUp).toHaveBeenCalledTimes(1);
  });

  it("asks for an AAL2 factor and re-sends a fresh code when the code check is refused", async () => {
    const change = "Change to another phone number";
    api.getMyPhone.mockResolvedValue({ phone: ACTIVE });
    api.startMyPhone
      .mockResolvedValueOnce(challenge("verify-1"))
      .mockResolvedValueOnce(challenge("verify-2"));
    api.verifyMyPhone.mockRejectedValueOnce(
      apiError(403, "auth_assurance_required"),
    );
    await renderCard();
    fireEvent.click(
      within(card()).getByRole("button", { name: "Change phone number" }),
    );
    await settle();
    await sendNumber(change, "0987 654 321");
    await sendCode(change);

    expect((requestStepUp.mock.calls[0] as StepUpCall)[1]).toEqual({
      requireAal2: true,
    });
    expect(within(card()).getByRole("alert")).toHaveTextContent(
      /passkey or an authenticator app/,
    );
    await completeStepUp();
    // The step-up replaced the session: a new code, never the old one again.
    expect(api.verifyMyPhone).toHaveBeenCalledTimes(1);
    expect(api.startMyPhone).toHaveBeenNthCalledWith(
      2,
      { country_code: "VN", phone_number: "0987654321" },
      { baseUrl: "/api" },
    );
  });

  it("explains a number in use elsewhere and returns to the number step", async () => {
    api.getMyPhone.mockResolvedValue({ phone: null });
    api.startMyPhone.mockResolvedValue(challenge("verify-1"));
    api.verifyMyPhone.mockRejectedValueOnce(apiError(409, "auth_phone_in_use"));
    await renderCard();
    const name = await openAdd();
    await sendNumber(name);
    await sendCode(name);
    expect(within(card()).getByRole("alert")).toHaveTextContent(
      "This number can't be added to your account. Use another mobile number.",
    );
    expect(
      within(card()).getByRole("textbox", { name: "Mobile number" }),
    ).toHaveFocus();
  });

  it("shows field errors for an unsupported number and a wrong code", async () => {
    api.getMyPhone.mockResolvedValue({ phone: null });
    api.startMyPhone
      .mockRejectedValueOnce(apiError(400, "auth_invalid_phone_number"))
      .mockResolvedValueOnce(challenge("verify-1"));
    api.verifyMyPhone.mockRejectedValueOnce(
      apiError(401, "auth_invalid_verification_code"),
    );
    await renderCard();
    const name = await openAdd();
    await sendNumber(name, "024 3822 1234");
    const number = within(flowForm(name)).getByRole("textbox", {
      name: "Mobile number",
    });
    expect(number).toHaveAccessibleDescription(/isn't valid or supported/);
    expect(number).toHaveFocus();

    await sendNumber(name);
    await sendCode(name, "000000");
    const code = within(flowForm(name)).getByRole("textbox", {
      name: "6-digit verification code",
    });
    expect(code).toHaveAccessibleDescription(/incorrect or has expired/);
    expect(code).toHaveFocus();
    expect(requestStepUp).not.toHaveBeenCalled();
  });

  it("honours Retry-After on a throttled request", async () => {
    api.getMyPhone.mockResolvedValue({ phone: null });
    api.startMyPhone.mockRejectedValueOnce(apiError(429, undefined, 30));
    await renderCard();
    const name = await openAdd();
    await sendNumber(name);
    expect(within(card()).getByRole("alert")).toHaveTextContent(
      "Too many attempts. Try again in 30 seconds.",
    );
    expect(
      within(flowForm(name)).getByRole("button", {
        name: "Send verification code (30s)",
      }),
    ).toBeDisabled();
  });

  it("explains a delivery outage", async () => {
    api.getMyPhone.mockResolvedValue({ phone: null });
    api.startMyPhone.mockRejectedValueOnce(
      apiError(503, "auth_phone_delivery_unavailable"),
    );
    await renderCard();
    const name = await openAdd();
    await sendNumber(name);
    expect(within(card()).getByRole("alert")).toHaveTextContent(
      "Phone codes are temporarily unavailable.",
    );
  });

  it("removes the number after confirmation and resyncs the rotated session", async () => {
    api.getMyPhone
      .mockResolvedValueOnce({ phone: ACTIVE })
      .mockResolvedValue({ phone: null });
    api.removeMyPhone.mockResolvedValue(undefined);
    await renderCard();
    fireEvent.click(
      within(card()).getByRole("button", { name: "Remove phone number" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Remove this phone number?",
    });
    expect(dialog).toHaveAccessibleDescription(
      /codes sent to \+84 \*\*\*5678.*other devices will be signed out/,
    );
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Remove number" }),
    );
    await settle();
    expect(api.removeMyPhone).toHaveBeenCalledTimes(1);
    // DELETE /me/phone rotates the session and CSRF: resync like a step-up.
    expect(api.rotateCSRFToken).toHaveBeenCalled();
    await waitFor(() =>
      expect(
        within(card()).getByText(
          "Phone number removed. Your other devices were signed out.",
        ),
      ).toBeInTheDocument(),
    );
    expect(within(card()).getByText("No phone number yet")).toBeInTheDocument();
  });

  it("steps up to AAL2 before removing, then refuses the last sign-in method", async () => {
    api.getMyPhone.mockResolvedValue({ phone: ACTIVE });
    api.removeMyPhone
      .mockRejectedValueOnce(apiError(403, "auth_assurance_required"))
      .mockRejectedValueOnce(apiError(409, "auth_last_identity"));
    await renderCard();
    fireEvent.click(
      within(card()).getByRole("button", { name: "Remove phone number" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Remove this phone number?",
    });
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Remove number" }),
    );
    await settle();
    expect((requestStepUp.mock.calls[0] as StepUpCall)[1]).toEqual({
      requireAal2: true,
    });
    await completeStepUp();
    expect(api.removeMyPhone).toHaveBeenCalledTimes(2);
    expect(within(card()).getByRole("alert")).toHaveTextContent(
      "This number can't be removed because it's your last available sign-in method.",
    );
    expect(within(card()).getByText("+84 ***5678")).toBeInTheDocument();
    // A refusal after the step-up does not open the dialog again.
    expect(requestStepUp).toHaveBeenCalledTimes(1);
  });

  it("speaks Vietnamese", async () => {
    api.getMyPhone.mockResolvedValue({ phone: STALE });
    await renderCard("vi");
    const region = screen.getByRole("region", { name: "Số điện thoại" });
    expect(within(region).getByText("Cần xác minh lại")).toBeInTheDocument();
    expect(
      within(region).getByRole("button", { name: "Xác minh lại" }),
    ).toBeInTheDocument();
    expect(
      within(region).getByRole("button", { name: "Gỡ số điện thoại" }),
    ).toBeInTheDocument();
  });

  it("offers a retry when the number cannot be loaded", async () => {
    api.getMyPhone
      .mockRejectedValueOnce(apiError(503, undefined, 2))
      .mockResolvedValue({ phone: null });
    await renderCard();
    expect(within(card()).getByRole("alert")).toHaveTextContent(
      "TutorHub is busy. Try again in 2 seconds.",
    );
    fireEvent.click(within(card()).getByRole("button", { name: "Try again" }));
    await settle();
    expect(within(card()).getByText("No phone number yet")).toBeInTheDocument();
  });
});
