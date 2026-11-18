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
  type AuthProvidersResponse,
  type AuthSignupChallengeResponse,
} from "@tutorhub/api-client";
import { createMemoryRouter, useLocation } from "react-router";
import { RouterProvider } from "react-router/dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, type Language } from "../app/i18n";
import { SignInPage } from "./SignInPage";

const {
  api,
  completeNativeSignInMock,
  resetAfterPasswordRecoveryMock,
  signInMock,
} = vi.hoisted(() => ({
  api: {
    completeEmailSignup: vi.fn(),
    completePasswordRecovery: vi.fn(),
    getAuthProviders: vi.fn(),
    loginWithPassword: vi.fn(),
    resendAuthChallenge: vi.fn(),
    startEmailCodeLogin: vi.fn(),
    startEmailSignup: vi.fn(),
    startPasswordRecovery: vi.fn(),
    verifyEmailCodeLogin: vi.fn(),
  },
  completeNativeSignInMock: vi.fn(),
  resetAfterPasswordRecoveryMock: vi.fn(),
  signInMock: vi.fn(),
}));

vi.mock("@tutorhub/api-client", async (importOriginal) => ({
  ...(await importOriginal<typeof ApiClient>()),
  ...api,
}));

vi.mock("../app/session", () => ({
  useSession: () => ({
    completeNativeSignIn: completeNativeSignInMock,
    resetAfterPasswordRecovery: resetAfterPasswordRecoveryMock,
    signIn: signInMock,
  }),
}));

const REMEMBERED_EMAIL_KEY = "tutorhub.remembered_email";
const SLIDE_ALTS = [
  "Modern classroom with TutorHub screens showing the course library and a live whiteboard lesson",
  "Online class with students on video sharing an interactive whiteboard on a large display and a tablet",
  "TutorHub dashboard with today's classes, AI class matching, parent chat, and homework progress",
] as const;

/** Repository default: no native methods, sign-in goes through the OIDC portal. */
const OIDC_PROVIDERS: AuthProvidersResponse = {
  email_code_enabled: false,
  legal_terms_version: "2026-11-16-v2.0",
  mfa_totp_enabled: false,
  oauth_providers: [],
  oidc_default_enabled: true,
  passkeys_enabled: false,
  password_enabled: false,
  password_min_length: 8,
  phone_allowed_countries: ["VN"],
  phone_enabled: false,
  signup_enabled: false,
  sso_domains_enabled: false,
};
const NATIVE_PROVIDERS: AuthProvidersResponse = {
  ...OIDC_PROVIDERS,
  password_enabled: true,
  signup_enabled: true,
};

interface RenderOptions {
  from?: { pathname: string; search?: string };
  language?: Language;
  /** Query string on /sign-in itself (e.g. the OAuth callback's `?error=`). */
  search?: string;
}

function LandingProbe() {
  const location = useLocation();
  return (
    <p data-testid="landing">{`${location.pathname}${location.search}`}</p>
  );
}

/** Lets mocked API promises settle inside act (works with real and fake timers). */
async function settle() {
  await act(async () => {
    for (let tick = 0; tick < 10; tick += 1) {
      await Promise.resolve();
    }
  });
}

async function renderSignIn({
  from,
  language = "en",
  search = "",
}: RenderOptions = {}) {
  const router = createMemoryRouter(
    [
      { path: "/sign-in", element: <SignInPage /> },
      { path: "*", element: <LandingProbe /> },
    ],
    {
      initialEntries: [
        { pathname: "/sign-in", search, state: from ? { from } : null },
      ],
    },
  );
  const view = render(
    <I18nProvider initialLanguage={language}>
      <RouterProvider router={router} />
    </I18nProvider>,
  );
  await settle();
  return Object.assign(view, { router });
}

function apiError(status: number, code?: string, retryAfterSeconds?: number) {
  return new APIRequestError(
    status,
    { status, title: "Request failed", type: "about:blank", code },
    retryAfterSeconds,
  );
}

function challengeResponse(id: string): AuthSignupChallengeResponse {
  return { challenge_id: id, cooldown_seconds: 60, expires_in_seconds: 600 };
}

function tickSeconds(seconds: number) {
  for (let second = 0; second < seconds; second += 1) {
    act(() => {
      vi.advanceTimersByTime(1000);
    });
  }
}

function activeDotIndex() {
  const dots = screen.getAllByRole("button", { name: /^Go to slide \d$/ });
  return dots.findIndex((dot) => dot.getAttribute("aria-current") === "true");
}

function stubReducedMotion(matches: boolean) {
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: (query: string) => ({
      addEventListener: () => undefined,
      matches: matches && query === "(prefers-reduced-motion: reduce)",
      media: query,
      removeEventListener: () => undefined,
    }),
  });
}

function getSlider() {
  return screen.getByRole("region", { name: "TutorHub highlights" });
}

function getSliderPart(className: string) {
  const element = getSlider().querySelector<HTMLElement>(`.${className}`);
  if (!element) throw new Error(`Missing .${className}`);
  return element;
}

const getTrack = () => getSliderPart("sign-in-slider__track");
const getViewport = () => getSliderPart("sign-in-slider__viewport");

/** Track offset in % of one slide: -100 is the first real slide. */
function trackPercent() {
  const match = /calc\((-?\d+)%/.exec(getTrack().style.transform);
  return match ? Number(match[1]) : Number.NaN;
}

function dotProgress(index: number) {
  return screen
    .getByRole("button", { name: `Go to slide ${index}` })
    .querySelector<HTMLElement>(".sign-in-slider__dot-progress");
}

function advance(ms: number) {
  act(() => {
    vi.advanceTimersByTime(ms);
  });
}

function setPageVisibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, "visibilityState", {
    configurable: true,
    get: () => state,
  });
  act(() => {
    document.dispatchEvent(new Event("visibilitychange"));
  });
}

beforeEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
  signInMock.mockReset();
  completeNativeSignInMock.mockReset().mockResolvedValue(undefined);
  resetAfterPasswordRecoveryMock.mockReset().mockResolvedValue(undefined);
  for (const mock of Object.values(api)) mock.mockReset();
  api.getAuthProviders.mockResolvedValue(OIDC_PROVIDERS);
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  Reflect.deleteProperty(window, "matchMedia");
  Reflect.deleteProperty(document, "visibilityState");
});

describe("SignInPage", () => {
  it("renders the TutorHub V1 login layout with static auth assets", async () => {
    await renderSignIn();

    expect(screen.getByText("TutorHub Enterprise")).toBeInTheDocument();
    const title = screen.getByRole("heading", {
      level: 1,
      name: "AI-Powered Tutoring Platform",
    });
    expect(within(title).getByText("AI-Powered")).toHaveClass(
      "sign-in__title-accent",
    );
    expect(
      screen.getByText(
        "Manage classes, match tutors, and track schedules in one intelligent platform.",
      ),
    ).toBeInTheDocument();

    const slider = screen.getByRole("region", { name: "TutorHub highlights" });
    expect(slider).toHaveAttribute("aria-roledescription", "carousel");
    const slideImages = slider.querySelectorAll('[role="group"] img');
    expect(
      Array.from(slideImages, (image) => image.getAttribute("src")),
    ).toEqual([
      "/images/auth/slide_login/login1.png",
      "/images/auth/slide_login/login2.png",
      "/images/auth/slide_login/login3.png",
    ]);
    expect(
      within(slider).getByRole("button", { name: "Previous slide" }),
    ).toBeInTheDocument();
    expect(
      within(slider).getByRole("button", { name: "Next slide" }),
    ).toBeInTheDocument();

    expect(screen.getByText("TutorHub")).toBeInTheDocument();
    expect(
      screen.getByText("Tutoring Platform", {
        selector: ".sign-in__brand-tagline",
      }),
    ).toBeInTheDocument();
    expect(
      document.querySelector('img[src="/images/auth/logomoi.png"]'),
    ).toHaveAttribute("width", "48");
    expect(screen.getByRole("button", { name: "Sign Up" })).toBeInTheDocument();

    const identifier = screen.getByRole("textbox", {
      name: "Email or username",
    });
    expect(identifier).toHaveAttribute(
      "placeholder",
      "Enter email or username",
    );
    const password = screen.getByLabelText("Password");
    expect(password).toHaveAttribute("type", "password");
    expect(password).toHaveAttribute("placeholder", "Enter the password");
    expect(screen.getByRole("button", { name: "Log In" })).toHaveAttribute(
      "type",
      "submit",
    );

    const keepSignedIn = screen.getByRole("checkbox", {
      name: "Keep me signed in",
    });
    expect(keepSignedIn).toBeDisabled();
    expect(keepSignedIn).toHaveAccessibleDescription(
      "Session length is managed securely by the TutorHub identity portal.",
    );
    expect(
      screen.getByRole("checkbox", { name: "Remember Me" }),
    ).not.toBeChecked();
    expect(screen.getByText("Or")).toBeInTheDocument();

    const facebook = screen.getByRole("button", {
      name: "Continue with Facebook",
    });
    // The approved "f" logo is inline, unmodified, and hidden from AT.
    const facebookMark = facebook.querySelector("svg");
    expect(facebookMark).toHaveAttribute("aria-hidden", "true");
    expect(facebookMark).toHaveClass("sign-in-facebook-mark");
    expect(facebook.querySelector("img")).toBeNull();
    const google = screen.getByRole("button", { name: "Sign in with Google" });
    // The standard "G" mark is inline (branding colours), hidden from AT.
    const mark = google.querySelector("svg");
    expect(mark).toHaveAttribute("aria-hidden", "true");
    expect(
      Array.from(mark?.querySelectorAll("path[fill]") ?? []).map((path) =>
        path.getAttribute("fill"),
      ),
    ).toEqual(["#EA4335", "#4285F4", "#FBBC05", "#34A853", "none"]);
    expect(google.querySelector("img")).toBeNull();
    expect(
      screen.getByRole("button", { name: "Forget Password" }),
    ).toBeInTheDocument();
  });

  it("renders Vietnamese copy when the interface language is Vietnamese", async () => {
    await renderSignIn({ language: "vi" });

    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Nền tảng gia sư ứng dụng AI",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { level: 2, name: "Đăng nhập vào TutorHub" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Đăng nhập" })).toHaveAttribute(
      "type",
      "submit",
    );
    expect(screen.getByRole("button", { name: "Đăng ký" })).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Tạm dừng trình chiếu" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("group", { name: "1/3" })).toHaveAttribute(
      "aria-roledescription",
      "slide",
    );
  });

  it("exposes only the three real slides to assistive technology", async () => {
    await renderSignIn();
    const slider = getSlider();
    expect(slider).toHaveAttribute("tabindex", "0");

    const groups = within(slider).getAllByRole("group", { hidden: true });
    expect(groups.map((group) => group.getAttribute("aria-label"))).toEqual([
      "1 of 3",
      "2 of 3",
      "3 of 3",
    ]);
    expect(
      within(slider)
        .getAllByRole("group")
        .map((group) => group.getAttribute("aria-label")),
    ).toEqual(["1 of 3"]);
    expect(within(slider).getAllByRole("img")).toHaveLength(1);
    expect(groups[1]).toHaveAttribute("aria-hidden", "true");
    expect(groups[2]).toHaveAttribute("aria-hidden", "true");

    // Track order is [clone of 3, 1, 2, 3, clone of 1].
    const clones = Array.from(
      slider.querySelectorAll(".sign-in-slider__slide.is-clone"),
    );
    expect(clones).toHaveLength(2);
    for (const clone of clones) {
      expect(clone).toHaveAttribute("aria-hidden", "true");
      expect(clone).not.toHaveAttribute("role");
      expect(clone).not.toHaveAttribute("aria-roledescription");
      expect(clone).not.toHaveAttribute("aria-label");
      expect(clone.querySelector("img")).toHaveAttribute("alt", "");
    }
    expect(
      clones.map((clone) => clone.querySelector("img")?.getAttribute("src")),
    ).toEqual([
      "/images/auth/slide_login/login3.png",
      "/images/auth/slide_login/login1.png",
    ]);
    expect(slider.querySelectorAll("img")).toHaveLength(5);
    expect(trackPercent()).toBe(-100);

    for (const name of ["Previous slide", "Next slide"]) {
      expect(
        within(slider).getByRole("button", { name }).querySelector("svg"),
      ).toHaveAttribute("aria-hidden", "true");
    }
  });

  it("changes slides with arrows and dots and auto-advances every 4 seconds", async () => {
    vi.useFakeTimers();
    await renderSignIn();

    expect(activeDotIndex()).toBe(0);
    expect(screen.getByRole("img", { name: SLIDE_ALTS[0] })).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "Next slide" }));
    expect(activeDotIndex()).toBe(1);
    expect(
      screen.getByRole("img", { name: SLIDE_ALTS[1] }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("img", { name: SLIDE_ALTS[0] })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Previous slide" }));
    fireEvent.click(screen.getByRole("button", { name: "Previous slide" }));
    expect(activeDotIndex()).toBe(2);

    fireEvent.click(screen.getByRole("button", { name: "Go to slide 1" }));
    expect(activeDotIndex()).toBe(0);

    act(() => {
      vi.advanceTimersByTime(3999);
    });
    expect(activeDotIndex()).toBe(0);
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(activeDotIndex()).toBe(1);

    act(() => {
      vi.advanceTimersByTime(2000);
    });
    fireEvent.click(screen.getByRole("button", { name: "Go to slide 3" }));
    act(() => {
      vi.advanceTimersByTime(3999);
    });
    expect(activeDotIndex()).toBe(2);
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(activeDotIndex()).toBe(0);
  });

  it("auto-plays on load even with reduced motion and loops 3 -> 1 forward without rewinding", async () => {
    stubReducedMotion(true);
    vi.useFakeTimers();
    await renderSignIn();

    expect(
      screen.getByRole("button", { name: "Pause slideshow" }),
    ).toBeInTheDocument();
    expect(getSlider()).not.toHaveClass("is-paused");
    expect(activeDotIndex()).toBe(0);
    expect(trackPercent()).toBe(-100);

    advance(3999);
    expect(activeDotIndex()).toBe(0);
    advance(1);
    expect(activeDotIndex()).toBe(1);
    expect(trackPercent()).toBe(-200);
    expect(screen.getByRole("group", { name: "2 of 3" })).toBeInTheDocument();

    advance(4000);
    expect(activeDotIndex()).toBe(2);
    expect(trackPercent()).toBe(-300);

    // Slide 3 -> 1 keeps sliding forward onto the trailing clone of slide 1...
    advance(4000);
    expect(activeDotIndex()).toBe(0);
    expect(trackPercent()).toBe(-400);
    expect(getTrack()).not.toHaveClass("is-instant");
    expect(screen.getByRole("group", { name: "1 of 3" })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: SLIDE_ALTS[0] })).toBeVisible();

    // ...then silently snaps back to the real slide 1 after the transition.
    advance(600);
    expect(trackPercent()).toBe(-100);
    expect(getTrack()).not.toHaveClass("is-instant");
    expect(activeDotIndex()).toBe(0);

    // The loop reset does not delay the next slide.
    advance(3399);
    expect(activeDotIndex()).toBe(0);
    advance(1);
    expect(activeDotIndex()).toBe(1);
  });

  it("wraps backwards through the leading clone and settles on transitionend", async () => {
    vi.useFakeTimers();
    await renderSignIn();
    const previous = screen.getByRole("button", { name: "Previous slide" });
    const next = screen.getByRole("button", { name: "Next slide" });

    fireEvent.click(previous);
    expect(activeDotIndex()).toBe(2);
    expect(trackPercent()).toBe(0);
    expect(screen.getByRole("group", { name: "3 of 3" })).toBeInTheDocument();

    fireEvent.transitionEnd(getTrack(), { propertyName: "opacity" });
    expect(trackPercent()).toBe(0);
    fireEvent.transitionEnd(getTrack(), { propertyName: "transform" });
    expect(trackPercent()).toBe(-300);
    expect(getTrack()).not.toHaveClass("is-instant");

    // Clicking again mid-loop settles first, so rapid clicks stay in order.
    fireEvent.click(next);
    expect(trackPercent()).toBe(-400);
    fireEvent.click(next);
    expect(activeDotIndex()).toBe(1);
    expect(trackPercent()).toBe(-200);
  });

  it("pauses with the pause control and resumes the remaining time on play", async () => {
    vi.useFakeTimers();
    await renderSignIn();

    advance(1500);
    fireEvent.click(screen.getByRole("button", { name: "Pause slideshow" }));
    expect(
      screen.getByRole("button", { name: "Play slideshow" }),
    ).toBeInTheDocument();
    expect(getSlider()).toHaveClass("is-paused");
    expect(getViewport()).toHaveAttribute("aria-live", "polite");
    advance(10000);
    expect(activeDotIndex()).toBe(0);

    fireEvent.click(screen.getByRole("button", { name: "Play slideshow" }));
    expect(
      screen.getByRole("button", { name: "Pause slideshow" }),
    ).toBeInTheDocument();
    expect(getSlider()).not.toHaveClass("is-paused");
    expect(getViewport()).toHaveAttribute("aria-live", "off");
    advance(2499);
    expect(activeDotIndex()).toBe(0);
    advance(1);
    expect(activeDotIndex()).toBe(1);
  });

  it("pauses while hovered, keyboard-focused or when the browser tab is hidden", async () => {
    vi.useFakeTimers();
    await renderSignIn();
    const slider = getSlider();

    fireEvent.mouseEnter(slider);
    expect(slider).toHaveClass("is-paused");
    advance(8000);
    expect(activeDotIndex()).toBe(0);
    fireEvent.mouseLeave(slider);
    expect(slider).not.toHaveClass("is-paused");
    advance(4000);
    expect(activeDotIndex()).toBe(1);

    const nextButton = screen.getByRole("button", { name: "Next slide" });
    act(() => nextButton.focus());
    expect(slider).toHaveClass("is-paused");
    advance(8000);
    expect(activeDotIndex()).toBe(1);
    act(() => nextButton.blur());
    advance(4000);
    expect(activeDotIndex()).toBe(2);

    setPageVisibility("hidden");
    expect(slider).toHaveClass("is-paused");
    advance(8000);
    expect(activeDotIndex()).toBe(2);
    setPageVisibility("visible");
    expect(slider).not.toHaveClass("is-paused");
    advance(4000);
    expect(activeDotIndex()).toBe(0);

    // A mouse press focuses the arrow but must not leave the show paused.
    fireEvent.pointerDown(nextButton);
    act(() => nextButton.focus());
    fireEvent.pointerUp(nextButton);
    fireEvent.click(nextButton);
    expect(nextButton).toHaveFocus();
    expect(slider).not.toHaveClass("is-paused");
    expect(
      screen.getByRole("button", { name: "Pause slideshow" }),
    ).toBeInTheDocument();
  });

  it("fills a progress pill on the active dot that restarts per slide and pauses with the show", async () => {
    vi.useFakeTimers();
    await renderSignIn();
    const firstDot = screen.getByRole("button", { name: "Go to slide 1" });

    const progress = dotProgress(1);
    expect(firstDot).toHaveClass("is-active");
    expect(progress).not.toBeNull();
    expect(progress?.style.animationDuration).toBe("4000ms");
    expect(progress?.parentElement).toHaveAttribute("aria-hidden", "true");
    expect(dotProgress(2)).toBeNull();
    expect(dotProgress(3)).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Pause slideshow" }));
    expect(getSlider()).toHaveClass("is-paused");
    expect(dotProgress(1)).toBe(progress);
    fireEvent.click(screen.getByRole("button", { name: "Play slideshow" }));
    expect(dotProgress(1)).toBe(progress);

    advance(4000);
    expect(dotProgress(1)).toBeNull();
    const secondProgress = dotProgress(2);
    expect(secondProgress).not.toBeNull();

    // Choosing the current slide again restarts its progress and timer.
    advance(3000);
    fireEvent.click(screen.getByRole("button", { name: "Go to slide 2" }));
    expect(dotProgress(2)).not.toBe(secondProgress);
    advance(3999);
    expect(activeDotIndex()).toBe(1);
    advance(1);
    expect(activeDotIndex()).toBe(2);
  });

  it("changes slides by dragging or swiping the image area past 40px", async () => {
    vi.useFakeTimers();
    await renderSignIn();
    const viewport = getViewport();

    fireEvent.pointerDown(viewport, { button: 0, clientX: 300, pointerId: 1 });
    expect(viewport).toHaveClass("is-dragging");
    expect(getSlider()).toHaveClass("is-paused");
    fireEvent.pointerMove(viewport, { clientX: 260, pointerId: 1 });
    expect(getTrack()).toHaveClass("is-instant");
    expect(getTrack().style.transform).toContain("-40px");
    advance(8000);
    expect(activeDotIndex()).toBe(0);
    fireEvent.pointerUp(viewport, { clientX: 250, pointerId: 1 });
    expect(activeDotIndex()).toBe(1);
    expect(viewport).not.toHaveClass("is-dragging");
    expect(getTrack()).not.toHaveClass("is-instant");
    expect(trackPercent()).toBe(-200);

    // Swiping right goes back.
    fireEvent.pointerDown(viewport, { button: 0, clientX: 100, pointerId: 2 });
    fireEvent.pointerUp(viewport, { clientX: 160, pointerId: 2 });
    expect(activeDotIndex()).toBe(0);

    // A short drag or a cancelled gesture snaps back to the same slide.
    fireEvent.pointerDown(viewport, { button: 0, clientX: 200, pointerId: 3 });
    fireEvent.pointerMove(viewport, { clientX: 230, pointerId: 3 });
    fireEvent.pointerUp(viewport, { clientX: 230, pointerId: 3 });
    expect(activeDotIndex()).toBe(0);
    expect(trackPercent()).toBe(-100);
    expect(getTrack().style.transform).toContain("+ 0px");
    fireEvent.pointerDown(viewport, { button: 0, clientX: 200, pointerId: 4 });
    fireEvent.pointerMove(viewport, { clientX: 80, pointerId: 4 });
    fireEvent.pointerCancel(viewport, { clientX: 80, pointerId: 4 });
    expect(activeDotIndex()).toBe(0);
    expect(viewport).not.toHaveClass("is-dragging");

    // Other pointers and secondary buttons are ignored.
    fireEvent.pointerDown(viewport, { button: 2, clientX: 300, pointerId: 5 });
    expect(viewport).not.toHaveClass("is-dragging");

    // Swiping right on slide 1 wraps to slide 3 through the leading clone.
    fireEvent.pointerDown(viewport, { button: 0, clientX: 100, pointerId: 6 });
    fireEvent.pointerUp(viewport, { clientX: 200, pointerId: 6 });
    expect(activeDotIndex()).toBe(2);
    expect(trackPercent()).toBe(0);
    // Grabbing mid-loop settles onto the real slide before dragging.
    fireEvent.pointerDown(viewport, { button: 0, clientX: 300, pointerId: 7 });
    expect(trackPercent()).toBe(-300);
    fireEvent.pointerUp(viewport, { clientX: 200, pointerId: 7 });
    expect(activeDotIndex()).toBe(0);
    expect(trackPercent()).toBe(-400);
  });

  it("moves between slides with ArrowLeft and ArrowRight while the slider has focus", async () => {
    vi.useFakeTimers();
    await renderSignIn();
    const slider = getSlider();

    act(() => slider.focus());
    expect(slider).toHaveFocus();
    expect(slider).toHaveClass("is-paused");

    fireEvent.keyDown(slider, { key: "ArrowRight" });
    expect(activeDotIndex()).toBe(1);
    fireEvent.keyDown(slider, { key: "ArrowLeft" });
    expect(activeDotIndex()).toBe(0);
    fireEvent.keyDown(slider, { key: "ArrowLeft" });
    expect(activeDotIndex()).toBe(2);
    expect(trackPercent()).toBe(0);

    fireEvent.keyDown(slider, { key: "ArrowRight", shiftKey: true });
    fireEvent.keyDown(slider, { key: "Enter" });
    expect(activeDotIndex()).toBe(2);

    // Arrow keys also work while a slider control holds focus.
    const dot = screen.getByRole("button", { name: "Go to slide 1" });
    act(() => dot.focus());
    fireEvent.keyDown(dot, { key: "ArrowRight" });
    expect(activeDotIndex()).toBe(0);
    expect(trackPercent()).toBe(-400);

    advance(8000);
    expect(activeDotIndex()).toBe(0);
    expect(
      screen.getByRole("button", { name: "Pause slideshow" }),
    ).toBeInTheDocument();
  });

  it("remembers only the email when Remember Me is enabled and forgets it when cleared", async () => {
    const { unmount } = await renderSignIn();

    fireEvent.change(
      screen.getByRole("textbox", { name: "Email or username" }),
      { target: { value: "  teacher@example.com  " } },
    );
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "super-secret-password" },
    });
    fireEvent.click(screen.getByRole("checkbox", { name: "Remember Me" }));
    fireEvent.click(screen.getByRole("button", { name: "Log In" }));

    expect(window.localStorage.length).toBe(1);
    expect(window.localStorage.getItem(REMEMBERED_EMAIL_KEY)).toBe(
      "teacher@example.com",
    );
    expect(window.sessionStorage.length).toBe(0);
    expect(screen.getByLabelText("Password")).toHaveValue("");
    unmount();

    await renderSignIn();
    expect(
      screen.getByRole("textbox", { name: "Email or username" }),
    ).toHaveValue("teacher@example.com");
    const remember = screen.getByRole("checkbox", { name: "Remember Me" });
    expect(remember).toBeChecked();

    fireEvent.click(remember);
    expect(window.localStorage.getItem(REMEMBERED_EMAIL_KEY)).toBeNull();
  });

  it("clears the remembered email when Remember Me is turned off before signing in", async () => {
    window.localStorage.setItem(REMEMBERED_EMAIL_KEY, "old@example.com");
    await renderSignIn();

    fireEvent.click(screen.getByRole("checkbox", { name: "Remember Me" }));
    fireEvent.click(screen.getByRole("button", { name: "Log In" }));

    expect(window.localStorage.length).toBe(0);
    expect(signInMock).toHaveBeenCalledWith("/app/home");
  });

  it("starts the BFF sign-in with the protected return path from Log In, Google and Facebook", async () => {
    await renderSignIn({
      from: { pathname: "/app/classrooms", search: "?tab=1" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Log In" }));
    fireEvent.click(
      screen.getByRole("button", { name: "Sign in with Google" }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Continue with Facebook" }),
    );

    expect(signInMock).toHaveBeenCalledTimes(3);
    for (const call of signInMock.mock.calls) {
      expect(call).toEqual(["/app/classrooms?tab=1"]);
    }
    expect(api.getAuthProviders).toHaveBeenCalledWith(
      expect.objectContaining({ baseUrl: "/api" }),
    );
    expect(api.loginWithPassword).not.toHaveBeenCalled();
    expect(completeNativeSignInMock).not.toHaveBeenCalled();
  });

  it("defaults the return path to the home dashboard", async () => {
    await renderSignIn();

    fireEvent.click(screen.getByRole("button", { name: "Log In" }));

    expect(signInMock).toHaveBeenCalledWith("/app/home");
  });

  it("switches between Sign Up and Log In and validates the sign-up form", async () => {
    vi.useFakeTimers();
    await renderSignIn({ from: { pathname: "/app/calendar" } });

    fireEvent.click(screen.getByRole("button", { name: "Sign Up" }));

    const form = screen.getByRole("form", { name: "Create an Account" });
    const fullName = within(form).getByRole("textbox", { name: "Full Name" });
    expect(fullName).toHaveFocus();
    expect(screen.queryByRole("button", { name: "Sign Up" })).toBeNull();
    expect(screen.getByRole("button", { name: "Log In" })).toHaveAttribute(
      "type",
      "button",
    );
    expect(
      within(form).getByRole("textbox", { name: "OTP code" }),
    ).toHaveAttribute("placeholder", "- Enter OTP Code -");
    for (const name of ["User Agreement", "Privacy Policy"]) {
      const legalButton = within(form).getByRole("button", { name });
      expect(legalButton).toHaveAttribute("type", "button");
      expect(legalButton).toHaveAttribute("aria-haspopup", "dialog");
      expect(legalButton).toHaveClass(
        "sign-in-check__accent",
        "sign-in-check__link",
      );
      expect(legalButton.closest("label")).toBeNull();
    }
    expect(
      within(form).getByRole("checkbox", {
        name: "I have read and agree to the User Agreement and Privacy Policy",
      }),
    ).not.toBeChecked();

    fireEvent.click(
      within(form).getByRole("button", { name: "Create Account" }),
    );
    expect(fullName).toHaveAttribute("aria-invalid", "true");
    expect(fullName).toHaveAccessibleDescription(
      "Please enter your full name.",
    );
    expect(fullName).toHaveFocus();
    expect(
      screen.getByText("Please enter a valid email address."),
    ).toBeVisible();
    expect(
      screen.getByText("Please enter the 6-digit OTP code."),
    ).toBeVisible();
    expect(
      screen.getByText("Password must be at least 8 characters."),
    ).toBeVisible();
    expect(
      screen.getByText(
        "You must agree to the User Agreement and Privacy Policy.",
      ),
    ).toBeVisible();

    const email = within(form).getByRole("textbox", { name: "Email Address" });
    const getCode = within(form).getByRole("button", { name: "Get Code" });
    fireEvent.change(email, { target: { value: "not-an-email" } });
    fireEvent.click(getCode);
    expect(getCode).toBeEnabled();
    expect(email).toHaveFocus();

    fireEvent.change(email, { target: { value: "new.student@example.com" } });
    fireEvent.click(getCode);
    expect(getCode).toBeDisabled();
    expect(getCode).toHaveTextContent("60s");
    expect(
      within(form).getByRole("textbox", { name: "OTP code" }),
    ).toHaveFocus();
    expect(
      screen.getByText(/no code is sent from this page/),
    ).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(getCode).toHaveTextContent("59s");
    for (let second = 0; second < 59; second += 1) {
      act(() => {
        vi.advanceTimersByTime(1000);
      });
    }
    expect(getCode).toBeEnabled();
    expect(getCode).toHaveTextContent("Get Code");

    fireEvent.change(fullName, { target: { value: "New Student" } });
    fireEvent.change(within(form).getByRole("textbox", { name: "OTP code" }), {
      target: { value: "123456" },
    });
    fireEvent.change(within(form).getByLabelText("Password"), {
      target: { value: "Str0ng-passphrase" },
    });
    fireEvent.click(within(form).getByRole("checkbox"));
    fireEvent.click(
      within(form).getByRole("button", { name: "Create Account" }),
    );

    expect(
      screen.getByText("Finish on the TutorHub identity portal"),
    ).toBeInTheDocument();
    expect(within(form).getByLabelText("Password")).toHaveValue("");
    expect(window.localStorage.length).toBe(0);
    expect(window.sessionStorage.length).toBe(0);
    expect(signInMock).not.toHaveBeenCalled();

    fireEvent.click(
      screen.getByRole("button", { name: "Continue to secure sign-in" }),
    );
    expect(signInMock).toHaveBeenCalledWith("/app/calendar");
    // Sign-up is disabled on the server, so nothing native may be called.
    expect(api.startEmailSignup).not.toHaveBeenCalled();
    expect(api.resendAuthChallenge).not.toHaveBeenCalled();
    expect(api.completeEmailSignup).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Log In" }));
    expect(
      screen.getByRole("textbox", { name: "Email or username" }),
    ).toHaveFocus();
    expect(screen.getByRole("button", { name: "Sign Up" })).toBeInTheDocument();
    expect(
      screen.queryByRole("form", { name: "Create an Account" }),
    ).toBeNull();
  });

  it("opens an accessible Recover Password dialog and restores focus when closed", async () => {
    await renderSignIn();
    const trigger = screen.getByRole("button", { name: "Forget Password" });

    fireEvent.click(trigger);
    const dialog = await screen.findByRole("dialog", {
      name: "Recover Password",
    });
    expect(dialog).toHaveAttribute("aria-modal", "true");
    expect(dialog).toHaveAccessibleDescription(
      "Enter your registered email to receive an OTP code",
    );
    const email = within(dialog).getByRole("textbox", {
      name: "Email Address",
    });
    await waitFor(() => expect(email).toHaveFocus());
    expect(email).toHaveAttribute("placeholder", "e.g. admin@tutorhub.com");
    expect(
      within(dialog).getByRole("textbox", {
        name: "Authentication Code (OTP)",
      }),
    ).toHaveAttribute("placeholder", "123456");
    expect(within(dialog).getByLabelText("New Password")).toHaveAttribute(
      "type",
      "password",
    );

    fireEvent.keyDown(dialog, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await waitFor(() => expect(trigger).toHaveFocus());

    fireEvent.click(trigger);
    const reopened = await screen.findByRole("dialog", {
      name: "Recover Password",
    });
    fireEvent.click(within(reopened).getByRole("button", { name: "Get back" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await waitFor(() => expect(trigger).toHaveFocus());
  });

  it("validates password recovery, runs the 60s code timer and hands off to secure sign-in", async () => {
    await renderSignIn({ from: { pathname: "/app/messages" } });

    fireEvent.click(screen.getByRole("button", { name: "Forget Password" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Recover Password",
    });
    const email = within(dialog).getByRole("textbox", {
      name: "Email Address",
    });
    const otp = within(dialog).getByRole("textbox", {
      name: "Authentication Code (OTP)",
    });
    const newPassword = within(dialog).getByLabelText("New Password");

    fireEvent.click(
      within(dialog).getByRole("button", { name: "Reset Password" }),
    );
    expect(email).toHaveAttribute("aria-invalid", "true");
    expect(email).toHaveFocus();
    expect(otp).toHaveAttribute("aria-invalid", "true");
    expect(newPassword).toHaveAttribute("aria-invalid", "true");

    vi.useFakeTimers();
    fireEvent.change(email, { target: { value: "teacher@example.com" } });
    const getCode = within(dialog).getByRole("button", { name: "Get Code" });
    fireEvent.click(getCode);
    expect(getCode).toBeDisabled();
    expect(getCode).toHaveTextContent("60s");
    expect(otp).toHaveFocus();
    for (let second = 0; second < 60; second += 1) {
      act(() => {
        vi.advanceTimersByTime(1000);
      });
    }
    expect(getCode).toBeEnabled();
    expect(getCode).toHaveTextContent("Resend");
    vi.useRealTimers();

    fireEvent.change(otp, { target: { value: "654321" } });
    fireEvent.change(newPassword, { target: { value: "N3w-passphrase" } });
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Reset Password" }),
    );

    expect(
      within(dialog).getByText(/Passwords are reset through the TutorHub/),
    ).toBeInTheDocument();
    expect(newPassword).toHaveValue("");
    expect(otp).toHaveValue("");
    expect(window.localStorage.length).toBe(0);
    expect(window.sessionStorage.length).toBe(0);

    fireEvent.click(
      within(dialog).getByRole("button", {
        name: "Continue to secure sign-in",
      }),
    );
    expect(signInMock).toHaveBeenCalledWith("/app/messages");
  });
});

const LEGAL_COPY = {
  en: {
    signUp: "Sign Up",
    form: "Create an Account",
    fullName: "Full Name",
    email: "Email Address",
    otp: "OTP code",
    password: "Password",
    createAccount: "Create Account",
    terms: "I have read and agree to the User Agreement and Privacy Policy",
    termsError: "You must agree to the User Agreement and Privacy Policy.",
    agreement: "User Agreement",
    privacy: "Privacy Policy",
    dialog: "Terms & Privacy Policy",
    description:
      "Please review the User Agreement and Privacy Policy before creating your TutorHub account.",
    meta: "Version 2.0 · Updated 16 Nov 2026",
    highlights: "Key highlights",
    agreementFirst: "1. Scope & User Roles",
    agreementLast: "7. Changes to These Terms & Termination",
    privacyFirst: "1. Personal & Learning Data We Collect",
    privacyLast: "7. Protecting Student Data & Contact",
    close: "Close",
    closeDialog: "Close terms dialog",
    accept: "Agree & Continue",
  },
  vi: {
    signUp: "Đăng ký",
    form: "Tạo tài khoản mới",
    fullName: "Họ và tên",
    email: "Địa chỉ email",
    otp: "Mã OTP",
    password: "Mật khẩu",
    createAccount: "Tạo tài khoản",
    terms:
      "Tôi đã đọc và đồng ý với Thỏa thuận người dùng và Chính sách quyền riêng tư",
    termsError:
      "Bạn cần đồng ý với Thỏa thuận người dùng và Chính sách quyền riêng tư.",
    agreement: "Thỏa thuận người dùng",
    privacy: "Chính sách quyền riêng tư",
    dialog: "Điều khoản & Chính sách TutorHub",
    description:
      "Vui lòng đọc kỹ Thỏa thuận người dùng và Chính sách quyền riêng tư trước khi tạo tài khoản TutorHub.",
    meta: "Phiên bản 2.0 · Cập nhật 16/11/2026",
    highlights: "Tóm tắt điểm chính",
    agreementFirst: "1. Phạm vi áp dụng & Vai trò sử dụng",
    agreementLast: "7. Thay đổi điều khoản & Chấm dứt sử dụng",
    privacyFirst: "1. Dữ liệu cá nhân & học tập được thu thập",
    privacyLast: "7. Bảo vệ dữ liệu học sinh & Liên hệ",
    close: "Đóng",
    closeDialog: "Đóng hộp thoại điều khoản",
    accept: "Tôi đồng ý & Tiếp tục",
  },
} as const;

type LegalCopy = (typeof LEGAL_COPY)[Language];

async function openSignUpForm(language: Language) {
  const copy = LEGAL_COPY[language];
  await renderSignIn({ language });
  fireEvent.click(screen.getByRole("button", { name: copy.signUp }));
  const form = screen.getByRole("form", { name: copy.form });
  return {
    copy,
    form,
    fullName: within(form).getByRole("textbox", { name: copy.fullName }),
    email: within(form).getByRole("textbox", { name: copy.email }),
    otp: within(form).getByRole("textbox", { name: copy.otp }),
    password: within(form).getByLabelText(copy.password),
    terms: within(form).getByRole("checkbox", { name: copy.terms }),
    agreementButton: within(form).getByRole("button", {
      name: copy.agreement,
    }),
    privacyButton: within(form).getByRole("button", { name: copy.privacy }),
  };
}

async function findLegalDialog(copy: LegalCopy) {
  const dialog = await screen.findByRole("dialog", { name: copy.dialog });
  await waitFor(() =>
    expect(dialog.contains(document.activeElement)).toBe(true),
  );
  return dialog;
}

function expectActiveLegalTab(
  dialog: HTMLElement,
  tabName: string,
  firstSection: string,
  lastSection: string,
) {
  expect(within(dialog).getByRole("tab", { name: tabName })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  // The reading area is a single keyboard-scrollable region around the panel.
  const body = within(dialog).getByRole("region", { name: tabName });
  expect(body).toHaveAttribute("tabindex", "0");
  expect(body).toHaveClass("sign-in-legal__body");
  const panel = within(body).getByRole("tabpanel", { name: tabName });
  expect(panel).toHaveAttribute("tabindex", "-1");
  expect(within(dialog).getAllByRole("tabpanel")).toHaveLength(1);
  // Key highlights + 7 numbered sections.
  expect(within(panel).getAllByRole("heading", { level: 3 })).toHaveLength(8);
  expect(
    within(panel).getByRole("heading", { level: 3, name: firstSection }),
  ).toBeInTheDocument();
  expect(
    within(panel).getByRole("heading", { level: 3, name: lastSection }),
  ).toBeInTheDocument();
  expect(within(panel).getAllByRole("listitem")).toHaveLength(4);
  return panel;
}

/** Radix tabs activate on mousedown; a real click also moves focus to the tab. */
function clickTab(tab: HTMLElement) {
  fireEvent.mouseDown(tab);
  act(() => tab.focus());
  fireEvent.mouseUp(tab);
  fireEvent.click(tab);
}

async function expectDialogClosed() {
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
}

describe.each(["en", "vi"] as const)(
  "SignInPage terms dialog (%s)",
  (language) => {
    it("opens the User Agreement tab, switches tabs and keeps the typed sign-up values", async () => {
      const field = await openSignUpForm(language);
      const { copy } = field;
      fireEvent.change(field.fullName, { target: { value: "Nguyễn Văn A" } });
      fireEvent.change(field.email, {
        target: { value: "new.student@example.com" },
      });
      fireEvent.change(field.otp, { target: { value: "123456" } });
      fireEvent.change(field.password, {
        target: { value: "Str0ng-passphrase" },
      });

      fireEvent.click(field.agreementButton);
      const dialog = await findLegalDialog(copy);
      expect(dialog).toHaveAttribute("aria-modal", "true");
      expect(dialog).toHaveAccessibleDescription(copy.description);
      expect(within(dialog).getByText(copy.meta)).toBeInTheDocument();
      expect(
        within(dialog).getByRole("heading", { level: 2, name: copy.dialog }),
      ).toBeInTheDocument();
      expect(
        within(dialog).getByRole("button", { name: copy.closeDialog }),
      ).toBeInTheDocument();
      const agreementPanel = expectActiveLegalTab(
        dialog,
        copy.agreement,
        copy.agreementFirst,
        copy.agreementLast,
      );
      expect(
        within(agreementPanel).getByRole("heading", {
          level: 3,
          name: copy.highlights,
        }),
      ).toBeInTheDocument();
      expect(
        within(dialog).getByRole("tab", { name: copy.privacy }),
      ).toHaveAttribute("aria-selected", "false");

      clickTab(within(dialog).getByRole("tab", { name: copy.privacy }));
      expectActiveLegalTab(
        dialog,
        copy.privacy,
        copy.privacyFirst,
        copy.privacyLast,
      );
      expect(
        within(dialog).queryByRole("tabpanel", { name: copy.agreement }),
      ).toBeNull();

      fireEvent.keyDown(
        within(dialog).getByRole("tab", { name: copy.privacy }),
        {
          key: "ArrowLeft",
        },
      );
      const agreementTab = within(dialog).getByRole("tab", {
        name: copy.agreement,
      });
      await waitFor(() =>
        expect(agreementTab).toHaveAttribute("aria-selected", "true"),
      );
      expect(agreementTab).toHaveFocus();
      expectActiveLegalTab(
        dialog,
        copy.agreement,
        copy.agreementFirst,
        copy.agreementLast,
      );

      fireEvent.click(within(dialog).getByRole("button", { name: copy.close }));
      await expectDialogClosed();
      await waitFor(() => expect(field.agreementButton).toHaveFocus());
      expect(field.fullName).toHaveValue("Nguyễn Văn A");
      expect(field.email).toHaveValue("new.student@example.com");
      expect(field.otp).toHaveValue("123456");
      expect(field.password).toHaveValue("Str0ng-passphrase");
      expect(field.terms).not.toBeChecked();
      expect(window.localStorage.length).toBe(0);
      expect(window.sessionStorage.length).toBe(0);
    });

    it("opens the Privacy Policy tab directly and restores focus on Escape or × without accepting", async () => {
      const field = await openSignUpForm(language);
      const { copy } = field;

      fireEvent.click(field.privacyButton);
      const dialog = await findLegalDialog(copy);
      expectActiveLegalTab(
        dialog,
        copy.privacy,
        copy.privacyFirst,
        copy.privacyLast,
      );
      expect(
        within(dialog).getByRole("tab", { name: copy.agreement }),
      ).toHaveAttribute("aria-selected", "false");

      fireEvent.keyDown(dialog, { key: "Escape" });
      await expectDialogClosed();
      await waitFor(() => expect(field.privacyButton).toHaveFocus());
      expect(field.terms).not.toBeChecked();

      fireEvent.click(field.agreementButton);
      const reopened = await findLegalDialog(copy);
      expectActiveLegalTab(
        reopened,
        copy.agreement,
        copy.agreementFirst,
        copy.agreementLast,
      );
      fireEvent.click(
        within(reopened).getByRole("button", { name: copy.closeDialog }),
      );
      await expectDialogClosed();
      await waitFor(() => expect(field.agreementButton).toHaveFocus());
      expect(field.terms).not.toBeChecked();
    });

    it("checks the terms box, clears the missing-consent error and closes on Agree & Continue", async () => {
      const field = await openSignUpForm(language);
      const { copy, form } = field;

      fireEvent.click(
        within(form).getByRole("button", { name: copy.createAccount }),
      );
      expect(screen.getByText(copy.termsError)).toBeVisible();
      expect(field.terms).toHaveAttribute("aria-invalid", "true");
      expect(field.terms).toHaveAccessibleDescription(copy.termsError);

      fireEvent.click(field.privacyButton);
      const dialog = await findLegalDialog(copy);
      fireEvent.click(
        within(dialog).getByRole("button", { name: copy.accept }),
      );
      await expectDialogClosed();

      expect(field.terms).toBeChecked();
      expect(field.terms).not.toHaveAttribute("aria-invalid");
      expect(screen.queryByText(copy.termsError)).toBeNull();
      await waitFor(() => expect(field.terms).toHaveFocus());

      fireEvent.click(field.agreementButton);
      const reopened = await findLegalDialog(copy);
      fireEvent.click(
        within(reopened).getByRole("button", { name: copy.close }),
      );
      await expectDialogClosed();
      await waitFor(() => expect(field.agreementButton).toHaveFocus());
      expect(field.terms).toBeChecked();
    });
  },
);

/* ---------- AUTH-02B: native email sign-up and password login ---------- */

const NATIVE_COPY = {
  en: {
    signUp: "Sign Up",
    logIn: "Log In",
    form: "Create an Account",
    fullName: "Full Name",
    email: "Email Address",
    otp: "OTP code",
    password: "Password",
    identifier: "Email or username",
    keepSignedIn: "Keep me signed in",
    keepSignedInHint:
      "Stay signed in on this device for up to 30 days (administrator accounts: up to 24 hours). The server decides the final session length.",
    rememberMe: "Remember Me",
    terms: "I have read and agree to the User Agreement and Privacy Policy",
    getCode: "Get Code",
    resend: "Resend",
    createAccount: "Create Account",
    accept: "Agree & Continue",
    dialog: "Terms & Privacy Policy",
    reviewTerms: "Review updated terms",
    codeSent:
      "If the email address is valid and eligible, a 6-digit verification code has been sent. Please check your inbox. The code is valid for 10 minutes.",
    codeRequired: "Select Get Code to receive a verification code first.",
    termsChanged:
      "The User Agreement or Privacy Policy has just been updated. Please review it and agree again.",
    passwordPolicy:
      "This password is too weak or appears on a list of common or leaked passwords. Please choose a stronger password.",
    signUpFailed:
      "We couldn't complete sign-up. Check the code or request a new one, then try again. If you already have an account, try logging in.",
    rateLimited: "Too many attempts. Please try again in 42 seconds.",
    busy: "TutorHub is busy right now. Please try again in 2 seconds.",
    loginInvalid: "Incorrect email or password.",
    identifierRequired: "Please enter your email.",
    mfa: "This account requires multi-factor authentication, which this page doesn't support yet. Please contact your administrator.",
  },
  vi: {
    signUp: "Đăng ký",
    logIn: "Đăng nhập",
    form: "Tạo tài khoản mới",
    fullName: "Họ và tên",
    email: "Địa chỉ email",
    otp: "Mã OTP",
    password: "Mật khẩu",
    identifier: "Email hoặc tên đăng nhập",
    keepSignedIn: "Duy trì đăng nhập",
    keepSignedInHint:
      "Duy trì đăng nhập trên thiết bị này tối đa 30 ngày (tài khoản quản trị: tối đa 24 giờ). Thời hạn phiên thực tế do máy chủ quyết định.",
    rememberMe: "Ghi nhớ tôi",
    terms:
      "Tôi đã đọc và đồng ý với Thỏa thuận người dùng và Chính sách quyền riêng tư",
    getCode: "Lấy mã",
    resend: "Gửi lại",
    createAccount: "Tạo tài khoản",
    accept: "Tôi đồng ý & Tiếp tục",
    dialog: "Điều khoản & Chính sách TutorHub",
    reviewTerms: "Xem điều khoản mới",
    codeSent:
      "Nếu email hợp lệ và đủ điều kiện, mã xác thực 6 chữ số đã được gửi. Vui lòng kiểm tra hộp thư. Mã có hiệu lực trong 10 phút.",
    codeRequired: "Vui lòng bấm Lấy mã để nhận mã xác thực trước.",
    termsChanged:
      "Thỏa thuận người dùng hoặc Chính sách quyền riêng tư vừa được cập nhật. Vui lòng xem lại và đồng ý lại.",
    passwordPolicy:
      "Mật khẩu chưa đạt chuẩn an toàn hoặc nằm trong danh sách mật khẩu phổ biến/bị lộ. Vui lòng chọn mật khẩu mạnh hơn.",
    signUpFailed:
      "Không thể hoàn tất đăng ký. Vui lòng kiểm tra lại mã hoặc yêu cầu mã mới rồi thử lại. Nếu bạn đã có tài khoản, hãy thử đăng nhập.",
    rateLimited: "Bạn đã thử quá nhiều lần. Vui lòng thử lại sau 42 giây.",
    busy: "Hệ thống đang bận. Vui lòng thử lại sau 2 giây.",
    loginInvalid: "Email hoặc mật khẩu không đúng.",
    identifierRequired: "Vui lòng nhập email.",
    mfa: "Tài khoản này yêu cầu xác thực đa yếu tố nhưng trang này chưa hỗ trợ bước xác thực bổ sung. Vui lòng liên hệ quản trị viên.",
  },
} as const;

async function openNativeSignUp(
  language: Language,
  from?: RenderOptions["from"],
) {
  const copy = NATIVE_COPY[language];
  await renderSignIn({ from, language });
  fireEvent.click(screen.getByRole("button", { name: copy.signUp }));
  const form = screen.getByRole("form", { name: copy.form });
  return {
    copy,
    form,
    fullName: within(form).getByRole("textbox", { name: copy.fullName }),
    email: within(form).getByRole("textbox", { name: copy.email }),
    otp: within(form).getByRole("textbox", { name: copy.otp }),
    password: within(form).getByLabelText(copy.password),
    terms: within(form).getByRole("checkbox", { name: copy.terms }),
    getCode: within(form).getByRole("button", { name: copy.getCode }),
    createAccount: within(form).getByRole("button", {
      name: copy.createAccount,
    }),
  };
}

type NativeSignUpFields = Awaited<ReturnType<typeof openNativeSignUp>>;

function fillIdentity(field: NativeSignUpFields, email: string) {
  fireEvent.change(field.fullName, { target: { value: "  New Student  " } });
  fireEvent.change(field.email, { target: { value: email } });
}

function fillCodePasswordAndTerms(field: NativeSignUpFields) {
  fireEvent.change(field.otp, { target: { value: "123456" } });
  fireEvent.change(field.password, {
    target: { value: "Str0ng-passphrase" },
  });
  if (!(field.terms as HTMLInputElement).checked) fireEvent.click(field.terms);
}

function expectNoSecretsInBrowserStorage() {
  expect(window.sessionStorage.length).toBe(0);
  for (let index = 0; index < window.localStorage.length; index += 1) {
    const key = window.localStorage.key(index);
    expect(key).toBe(REMEMBERED_EMAIL_KEY);
  }
  const stored = JSON.stringify({ ...window.localStorage });
  for (const secret of ["Str0ng-passphrase", "123456", "challenge-"]) {
    expect(stored).not.toContain(secret);
  }
}

describe.each(["en", "vi"] as const)(
  "SignInPage native email sign-up (%s)",
  (language) => {
    const copy = NATIVE_COPY[language];

    beforeEach(() => {
      api.getAuthProviders.mockResolvedValue(NATIVE_PROVIDERS);
    });

    it("requests a code, resends it after the 60s cooldown and completes the session hand-off", async () => {
      vi.useFakeTimers();
      api.startEmailSignup.mockResolvedValue(challengeResponse("challenge-1"));
      api.resendAuthChallenge.mockResolvedValue(
        challengeResponse("challenge-2"),
      );
      api.completeEmailSignup.mockResolvedValue({ status: "authenticated" });
      const field = await openNativeSignUp(language, {
        pathname: "/app/calendar",
        search: "?view=week",
      });
      fillIdentity(field, "new.student@example.com");

      fireEvent.click(field.getCode);
      await settle();
      expect(api.startEmailSignup).toHaveBeenCalledWith(
        { email: "new.student@example.com" },
        { baseUrl: "/api" },
      );
      // Same neutral wording for new, existing and throttled addresses.
      expect(screen.getByText(copy.codeSent)).toBeInTheDocument();
      expect(field.email).toHaveAccessibleDescription(copy.codeSent);
      expect(field.getCode).toBeDisabled();
      expect(field.getCode).toHaveTextContent("60s");
      expect(field.otp).toHaveFocus();

      tickSeconds(59);
      expect(field.getCode).toHaveTextContent("1s");
      tickSeconds(1);
      expect(field.getCode).toBeEnabled();
      expect(field.getCode).toHaveTextContent(copy.resend);

      fireEvent.click(field.getCode);
      await settle();
      expect(api.resendAuthChallenge).toHaveBeenCalledTimes(1);
      expect(api.resendAuthChallenge).toHaveBeenCalledWith({ baseUrl: "/api" });
      expect(api.startEmailSignup).toHaveBeenCalledTimes(1);
      expect(field.getCode).toHaveTextContent("60s");

      fillCodePasswordAndTerms(field);
      fireEvent.click(field.createAccount);
      await settle();

      expect(api.completeEmailSignup).toHaveBeenCalledWith(
        {
          challenge_id: "challenge-2",
          code: "123456",
          display_name: "New Student",
          keep_signed_in: false,
          password: "Str0ng-passphrase",
          terms_version: "2026-11-16-v2.0",
        },
        { baseUrl: "/api" },
      );
      expect(completeNativeSignInMock).toHaveBeenCalledTimes(1);
      expect(signInMock).not.toHaveBeenCalled();
      expect(screen.getByTestId("landing")).toHaveTextContent(
        "/app/calendar?view=week",
      );
      expectNoSecretsInBrowserStorage();
    });

    it("drops the requested code when the email changes and asks for a new one", async () => {
      api.startEmailSignup
        .mockResolvedValueOnce(challengeResponse("challenge-1"))
        .mockResolvedValueOnce(challengeResponse("challenge-3"));
      const field = await openNativeSignUp(language);
      fillIdentity(field, "first@example.com");
      fireEvent.click(field.getCode);
      await settle();
      expect(field.getCode).toHaveTextContent("60s");
      fireEvent.change(field.otp, { target: { value: "123456" } });

      fireEvent.change(field.email, {
        target: { value: "second@example.com" },
      });
      expect(field.getCode).toBeEnabled();
      expect(field.getCode).toHaveTextContent(copy.getCode);
      expect(field.otp).toHaveValue("");
      expect(screen.queryByText(copy.codeSent)).toBeNull();

      fillCodePasswordAndTerms(field);
      fireEvent.click(field.createAccount);
      expect(field.otp).toHaveAttribute("aria-invalid", "true");
      expect(field.otp).toHaveAccessibleDescription(copy.codeRequired);
      expect(field.otp).toHaveFocus();
      expect(api.completeEmailSignup).not.toHaveBeenCalled();

      fireEvent.click(field.getCode);
      await settle();
      expect(api.startEmailSignup).toHaveBeenLastCalledWith(
        { email: "second@example.com" },
        { baseUrl: "/api" },
      );
      expect(api.resendAuthChallenge).not.toHaveBeenCalled();

      api.completeEmailSignup.mockResolvedValue({ status: "authenticated" });
      fireEvent.change(field.otp, { target: { value: "654321" } });
      fireEvent.click(field.createAccount);
      await settle();
      expect(api.completeEmailSignup).toHaveBeenCalledWith(
        expect.objectContaining({
          challenge_id: "challenge-3",
          code: "654321",
        }),
        { baseUrl: "/api" },
      );
    });

    it("reloads the providers, unticks consent and asks for a new agreement when the terms change", async () => {
      api.getAuthProviders
        .mockResolvedValue({
          ...NATIVE_PROVIDERS,
          legal_terms_version: "2026-12-01-v2.1",
        })
        .mockResolvedValueOnce(NATIVE_PROVIDERS);
      api.startEmailSignup.mockResolvedValue(challengeResponse("challenge-1"));
      api.completeEmailSignup
        .mockRejectedValueOnce(apiError(400, "auth_terms_version_changed"))
        .mockResolvedValueOnce({ status: "authenticated" });
      const field = await openNativeSignUp(language);
      fillIdentity(field, "new.student@example.com");
      fireEvent.click(field.getCode);
      await settle();
      fillCodePasswordAndTerms(field);

      fireEvent.click(field.createAccount);
      await settle();
      expect(api.completeEmailSignup).toHaveBeenLastCalledWith(
        expect.objectContaining({ terms_version: "2026-11-16-v2.0" }),
        { baseUrl: "/api" },
      );
      expect(api.getAuthProviders).toHaveBeenCalledTimes(2);
      expect(field.terms).not.toBeChecked();
      expect(field.terms).toHaveAttribute("aria-invalid", "true");
      expect(field.terms).toHaveAccessibleDescription(copy.termsChanged);
      expect(field.terms).toHaveFocus();
      expect(completeNativeSignInMock).not.toHaveBeenCalled();
      // The typed values stay so only consent has to be given again.
      expect(field.fullName).toHaveValue("  New Student  ");
      expect(field.otp).toHaveValue("123456");

      fireEvent.click(
        within(field.form).getByRole("button", { name: copy.reviewTerms }),
      );
      const dialog = await screen.findByRole("dialog", { name: copy.dialog });
      fireEvent.click(
        within(dialog).getByRole("button", { name: copy.accept }),
      );
      await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
      expect(field.terms).toBeChecked();

      fireEvent.click(field.createAccount);
      await settle();
      expect(api.completeEmailSignup).toHaveBeenLastCalledWith(
        expect.objectContaining({ terms_version: "2026-12-01-v2.1" }),
        { baseUrl: "/api" },
      );
      expect(completeNativeSignInMock).toHaveBeenCalledTimes(1);
      expect(screen.getByTestId("landing")).toHaveTextContent("/app/home");
    });

    it("asks for a stronger password without clearing the name, email or code", async () => {
      api.startEmailSignup.mockResolvedValue(challengeResponse("challenge-1"));
      api.completeEmailSignup.mockRejectedValue(
        apiError(400, "auth_password_policy"),
      );
      const field = await openNativeSignUp(language);
      fillIdentity(field, "new.student@example.com");
      fireEvent.click(field.getCode);
      await settle();
      fillCodePasswordAndTerms(field);

      fireEvent.click(field.createAccount);
      await settle();
      expect(field.password).toHaveValue("");
      expect(field.password).toHaveAttribute("aria-invalid", "true");
      expect(field.password).toHaveAccessibleDescription(copy.passwordPolicy);
      expect(field.password).toHaveFocus();
      expect(field.fullName).toHaveValue("  New Student  ");
      expect(field.email).toHaveValue("new.student@example.com");
      expect(field.otp).toHaveValue("123456");
      expect(field.terms).toBeChecked();
      expect(completeNativeSignInMock).not.toHaveBeenCalled();
    });

    it("shows one account-neutral message when the code or sign-up is rejected", async () => {
      api.startEmailSignup.mockResolvedValue(challengeResponse("challenge-1"));
      api.completeEmailSignup.mockRejectedValue(apiError(400));
      const field = await openNativeSignUp(language);
      fillIdentity(field, "existing@example.com");
      fireEvent.click(field.getCode);
      await settle();
      fillCodePasswordAndTerms(field);

      fireEvent.click(field.createAccount);
      await settle();
      expect(within(field.form).getByRole("alert")).toHaveTextContent(
        copy.signUpFailed,
      );
      expect(field.otp).toHaveValue("");
      expect(field.otp).toHaveFocus();
      expect(completeNativeSignInMock).not.toHaveBeenCalled();
      expect(screen.queryByTestId("landing")).toBeNull();
    });

    it("honours Retry-After for 429 code requests and 503 account creation", async () => {
      vi.useFakeTimers();
      api.startEmailSignup
        .mockRejectedValueOnce(apiError(429, undefined, 42))
        .mockResolvedValueOnce(challengeResponse("challenge-1"));
      api.completeEmailSignup
        .mockRejectedValueOnce(apiError(503, undefined, 2))
        .mockResolvedValueOnce({ status: "authenticated" });
      const field = await openNativeSignUp(language);
      fillIdentity(field, "new.student@example.com");

      fireEvent.click(field.getCode);
      await settle();
      expect(screen.getByText(copy.rateLimited)).toBeInTheDocument();
      expect(field.email).toHaveAccessibleDescription(copy.rateLimited);
      expect(field.email).toHaveFocus();
      expect(field.getCode).toBeDisabled();
      expect(field.getCode).toHaveTextContent("42s");
      tickSeconds(42);
      expect(field.getCode).toBeEnabled();
      expect(field.getCode).toHaveTextContent(copy.getCode);

      fireEvent.click(field.getCode);
      await settle();
      expect(screen.getByText(copy.codeSent)).toBeInTheDocument();
      fillCodePasswordAndTerms(field);

      fireEvent.click(field.createAccount);
      await settle();
      expect(within(field.form).getByRole("alert")).toHaveTextContent(
        copy.busy,
      );
      expect(field.createAccount).toBeDisabled();
      expect(field.createAccount).toHaveTextContent(
        `${copy.createAccount} (2s)`,
      );
      tickSeconds(2);
      expect(field.createAccount).toBeEnabled();
      expect(field.createAccount).toHaveTextContent(copy.createAccount);

      fireEvent.click(field.createAccount);
      await settle();
      expect(api.completeEmailSignup).toHaveBeenCalledTimes(2);
      expect(screen.getByTestId("landing")).toHaveTextContent("/app/home");
    });
  },
);

describe("SignInPage native email sign-up code lifetime", () => {
  it("reminds the user to resend once the code has expired", async () => {
    vi.useFakeTimers();
    api.getAuthProviders.mockResolvedValue(NATIVE_PROVIDERS);
    api.startEmailSignup.mockResolvedValue(challengeResponse("challenge-1"));
    const field = await openNativeSignUp("en");
    fillIdentity(field, "new.student@example.com");
    fireEvent.click(field.getCode);
    await settle();

    act(() => {
      vi.advanceTimersByTime(600_000);
    });
    expect(
      screen.getByText(
        "Your verification code has expired. Select Resend to get a new code.",
      ),
    ).toBeInTheDocument();

    fillCodePasswordAndTerms(field);
    fireEvent.click(field.createAccount);
    expect(field.otp).toHaveAccessibleDescription(
      "This code has expired. Select Resend to get a new code.",
    );
    expect(api.completeEmailSignup).not.toHaveBeenCalled();
  });

  it("ignores a late code response after the email was edited mid-request", async () => {
    api.getAuthProviders.mockResolvedValue(NATIVE_PROVIDERS);
    let resolveFirst: (value: AuthSignupChallengeResponse) => void = () =>
      undefined;
    api.startEmailSignup
      .mockReturnValueOnce(
        new Promise<AuthSignupChallengeResponse>((resolve) => {
          resolveFirst = resolve;
        }),
      )
      .mockResolvedValueOnce(challengeResponse("challenge-new"));
    const field = await openNativeSignUp("en");
    fillIdentity(field, "old@example.com");
    fireEvent.click(field.getCode);
    expect(field.getCode).toHaveTextContent("Sending…");

    fireEvent.change(field.email, { target: { value: "new@example.com" } });
    await act(async () => {
      resolveFirst(challengeResponse("challenge-old"));
    });
    await settle();
    expect(field.getCode).toHaveTextContent("Get Code");
    expect(field.getCode).toBeEnabled();
    expect(screen.queryByText(NATIVE_COPY.en.codeSent)).not.toBeInTheDocument();

    fireEvent.click(field.getCode);
    await settle();
    expect(api.startEmailSignup).toHaveBeenLastCalledWith(
      { email: "new@example.com" },
      { baseUrl: "/api" },
    );
    api.completeEmailSignup.mockResolvedValue({ status: "authenticated" });
    fillCodePasswordAndTerms(field);
    fireEvent.click(field.createAccount);
    await settle();
    expect(api.completeEmailSignup).toHaveBeenCalledWith(
      expect.objectContaining({ challenge_id: "challenge-new" }),
      { baseUrl: "/api" },
    );
  });

  it("starts a new transaction when the resend transaction has already ended", async () => {
    vi.useFakeTimers();
    api.getAuthProviders.mockResolvedValue(NATIVE_PROVIDERS);
    api.startEmailSignup
      .mockResolvedValueOnce(challengeResponse("challenge-1"))
      .mockResolvedValueOnce(challengeResponse("challenge-4"));
    api.resendAuthChallenge.mockRejectedValue(apiError(400));
    const field = await openNativeSignUp("en");
    fillIdentity(field, "new.student@example.com");
    fireEvent.click(field.getCode);
    await settle();
    tickSeconds(60);

    fireEvent.click(field.getCode);
    await settle();
    expect(api.resendAuthChallenge).toHaveBeenCalledTimes(1);
    expect(api.startEmailSignup).toHaveBeenCalledTimes(2);
    expect(api.startEmailSignup).toHaveBeenLastCalledWith(
      { email: "new.student@example.com" },
      { baseUrl: "/api" },
    );
    expect(field.getCode).toHaveTextContent("60s");
  });
});

describe.each(["en", "vi"] as const)(
  "SignInPage native password login (%s)",
  (language) => {
    const copy = NATIVE_COPY[language];

    beforeEach(() => {
      api.getAuthProviders.mockResolvedValue(NATIVE_PROVIDERS);
    });

    function loginFields() {
      return {
        identifier: screen.getByRole("textbox", { name: copy.identifier }),
        password: screen.getByLabelText(copy.password),
        keepSignedIn: screen.getByRole("checkbox", { name: copy.keepSignedIn }),
        rememberMe: screen.getByRole("checkbox", { name: copy.rememberMe }),
        submit: screen.getByRole("button", { name: copy.logIn }),
      };
    }

    it("signs in with keep-signed-in, waits for the session hand-off and remembers only the email", async () => {
      api.loginWithPassword.mockResolvedValue({ status: "authenticated" });
      let finishHandOff: () => void = () => undefined;
      completeNativeSignInMock.mockImplementation(
        () =>
          new Promise<void>((resolve) => {
            finishHandOff = resolve;
          }),
      );
      await renderSignIn({
        from: { pathname: "/app/classrooms", search: "?tab=1" },
        language,
      });
      const field = loginFields();
      expect(field.keepSignedIn).toBeEnabled();
      expect(field.keepSignedIn).toHaveAccessibleDescription(
        copy.keepSignedInHint,
      );

      fireEvent.change(field.identifier, {
        target: { value: "  teacher@example.com  " },
      });
      fireEvent.change(field.password, {
        target: { value: "correct horse battery staple" },
      });
      fireEvent.click(field.keepSignedIn);
      fireEvent.click(field.rememberMe);
      fireEvent.click(field.submit);
      await settle();

      expect(api.loginWithPassword).toHaveBeenCalledWith(
        {
          identifier: "teacher@example.com",
          keep_signed_in: true,
          password: "correct horse battery staple",
        },
        { baseUrl: "/api" },
      );
      expect(signInMock).not.toHaveBeenCalled();
      expect(completeNativeSignInMock).toHaveBeenCalledTimes(1);
      expect(field.password).toHaveValue("");
      // Navigation waits until CSRF rotation and /me have finished.
      expect(screen.queryByTestId("landing")).toBeNull();

      await act(async () => {
        finishHandOff();
      });
      await settle();
      expect(screen.getByTestId("landing")).toHaveTextContent(
        "/app/classrooms?tab=1",
      );
      expect(window.localStorage.length).toBe(1);
      expect(window.localStorage.getItem(REMEMBERED_EMAIL_KEY)).toBe(
        "teacher@example.com",
      );
      expect(JSON.stringify({ ...window.localStorage })).not.toContain(
        "correct horse",
      );
      expect(window.sessionStorage.length).toBe(0);
    });

    it("validates the email and password before calling the server", async () => {
      await renderSignIn({ language });
      const field = loginFields();

      fireEvent.click(field.submit);
      expect(field.identifier).toHaveAttribute("aria-invalid", "true");
      expect(field.identifier).toHaveAccessibleDescription(
        copy.identifierRequired,
      );
      expect(field.identifier).toHaveFocus();
      expect(field.password).toHaveAttribute("aria-invalid", "true");
      expect(api.loginWithPassword).not.toHaveBeenCalled();
      expect(signInMock).not.toHaveBeenCalled();
    });

    it("shows one generic message for wrong credentials and clears only the password", async () => {
      api.loginWithPassword.mockRejectedValue(apiError(401));
      await renderSignIn({ language });
      const field = loginFields();
      fireEvent.change(field.identifier, {
        target: { value: "teacher@example.com" },
      });
      fireEvent.change(field.password, { target: { value: "wrong-password" } });
      fireEvent.click(field.rememberMe);

      fireEvent.click(field.submit);
      await settle();
      expect(screen.getByRole("alert")).toHaveTextContent(copy.loginInvalid);
      expect(field.password).toHaveValue("");
      expect(field.password).toHaveFocus();
      expect(field.identifier).toHaveValue("teacher@example.com");
      expect(field.submit).toBeEnabled();
      expect(completeNativeSignInMock).not.toHaveBeenCalled();
      // Remember Me only persists after a successful sign-in.
      expect(window.localStorage.length).toBe(0);
    });

    it("stops at the multi-factor step without treating the user as signed in", async () => {
      api.loginWithPassword.mockResolvedValue({
        methods: ["totp"],
        status: "mfa_required",
      });
      await renderSignIn({ language });
      const field = loginFields();
      fireEvent.change(field.identifier, {
        target: { value: "admin@example.com" },
      });
      fireEvent.change(field.password, { target: { value: "admin-password" } });
      fireEvent.click(field.rememberMe);

      fireEvent.click(field.submit);
      await settle();
      expect(screen.getByRole("alert")).toHaveTextContent(copy.mfa);
      expect(field.password).toHaveValue("");
      expect(completeNativeSignInMock).not.toHaveBeenCalled();
      expect(screen.queryByTestId("landing")).toBeNull();
      expect(window.localStorage.length).toBe(0);
    });
  },
);

describe("SignInPage auth providers and native edge cases", () => {
  it("keeps the panel in place while the sign-in options load", async () => {
    let resolveProviders: (value: AuthProvidersResponse) => void = () =>
      undefined;
    api.getAuthProviders.mockReturnValue(
      new Promise<AuthProvidersResponse>((resolve) => {
        resolveProviders = resolve;
      }),
    );
    await renderSignIn();

    expect(screen.getByRole("status")).toHaveTextContent(
      "Loading sign-in options…",
    );
    expect(
      screen.getByRole("region", { name: "Account access" }),
    ).toHaveAttribute("aria-busy", "true");
    expect(screen.getByRole("button", { name: "Log In" })).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Sign in with Google" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("textbox", { name: "Email or username" }),
    ).toBeEnabled();

    await act(async () => {
      resolveProviders(OIDC_PROVIDERS);
    });
    await settle();
    expect(screen.queryByRole("status")).toBeNull();
    expect(screen.getByRole("button", { name: "Log In" })).toBeEnabled();
  });

  it("offers Retry when the sign-in options cannot be loaded", async () => {
    api.getAuthProviders.mockRejectedValueOnce(
      new TypeError("Failed to fetch"),
    );
    await renderSignIn();

    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent(
      "We couldn't load the sign-in options. Check your connection and try again.",
    );
    expect(screen.getByRole("button", { name: "Log In" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Log In" }));
    expect(signInMock).not.toHaveBeenCalled();

    fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));
    await settle();
    expect(api.getAuthProviders).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("alert")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Log In" }));
    expect(signInMock).toHaveBeenCalledWith("/app/home");
  });

  it("keeps Google and Facebook as the OIDC fallback beside password sign-in", async () => {
    api.getAuthProviders.mockResolvedValue(NATIVE_PROVIDERS);
    await renderSignIn({ from: { pathname: "/app/messages" } });

    expect(screen.getByText("Or")).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "Sign in with Google" }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Continue with Facebook" }),
    );
    expect(signInMock).toHaveBeenCalledTimes(2);
    expect(signInMock).toHaveBeenCalledWith("/app/messages");
    expect(api.loginWithPassword).not.toHaveBeenCalled();
  });

  it("hides the social buttons when neither OIDC nor an OAuth provider is enabled", async () => {
    api.getAuthProviders.mockResolvedValue({
      ...NATIVE_PROVIDERS,
      oidc_default_enabled: false,
    });
    await renderSignIn();

    expect(screen.queryByText("Or")).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Sign in with Google" }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Continue with Facebook" }),
    ).toBeNull();
    expect(screen.getByRole("button", { name: "Log In" })).toBeEnabled();
  });

  it("explains when no sign-in or sign-up method is enabled", async () => {
    api.getAuthProviders.mockResolvedValue({
      ...OIDC_PROVIDERS,
      oidc_default_enabled: false,
    });
    await renderSignIn();

    expect(screen.getByRole("status")).toHaveTextContent(
      "Sign-in is temporarily unavailable. Please try again later or contact your administrator.",
    );
    expect(screen.getByRole("button", { name: "Log In" })).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "Sign Up" }));
    const form = screen.getByRole("form", { name: "Create an Account" });
    expect(within(form).getByRole("status")).toHaveTextContent(
      "New accounts can't be created right now. Please contact your administrator.",
    );
    expect(
      within(form).getByRole("button", { name: "Create Account" }),
    ).toBeDisabled();
    expect(
      within(form).getByRole("button", { name: "Get Code" }),
    ).toBeDisabled();
  });

  it("locks Log In for the Retry-After window and reports busy and network failures", async () => {
    vi.useFakeTimers();
    api.getAuthProviders.mockResolvedValue(NATIVE_PROVIDERS);
    api.loginWithPassword
      .mockRejectedValueOnce(apiError(429, undefined, 3))
      .mockRejectedValueOnce(apiError(503, undefined, 2))
      .mockRejectedValueOnce(new TypeError("Failed to fetch"));
    await renderSignIn();
    fireEvent.change(
      screen.getByRole("textbox", { name: "Email or username" }),
      {
        target: { value: "teacher@example.com" },
      },
    );
    const password = screen.getByLabelText("Password");
    const submit = screen.getByRole("button", { name: "Log In" });

    fireEvent.change(password, { target: { value: "secret-password" } });
    fireEvent.click(submit);
    await settle();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Too many attempts. Please try again in 3 seconds.",
    );
    expect(submit).toBeDisabled();
    expect(submit).toHaveTextContent("Log In (3s)");
    expect(password).toHaveValue("secret-password");
    tickSeconds(3);
    expect(submit).toBeEnabled();

    fireEvent.click(submit);
    await settle();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "TutorHub is busy right now. Please try again in 2 seconds.",
    );
    tickSeconds(2);

    fireEvent.click(submit);
    await settle();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Couldn't reach TutorHub. Check your connection and try again.",
    );
    expect(api.loginWithPassword).toHaveBeenCalledTimes(3);
    expect(completeNativeSignInMock).not.toHaveBeenCalled();
  });

  it("retries only the session hand-off when /me cannot load after sign-in", async () => {
    api.getAuthProviders.mockResolvedValue(NATIVE_PROVIDERS);
    api.loginWithPassword.mockResolvedValue({ status: "authenticated" });
    completeNativeSignInMock
      .mockRejectedValueOnce(new Error("me unavailable"))
      .mockResolvedValueOnce(undefined);
    await renderSignIn();
    fireEvent.change(
      screen.getByRole("textbox", { name: "Email or username" }),
      {
        target: { value: "teacher@example.com" },
      },
    );
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "secret-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Log In" }));
    await settle();

    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent(
      "You're signed in, but we couldn't load your account yet.",
    );
    fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));
    await settle();
    expect(completeNativeSignInMock).toHaveBeenCalledTimes(2);
    expect(api.loginWithPassword).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("landing")).toHaveTextContent("/app/home");
  });

  it("only returns to same-origin SPA paths after sign-in", async () => {
    api.getAuthProviders.mockResolvedValue(NATIVE_PROVIDERS);
    api.loginWithPassword.mockResolvedValue({ status: "authenticated" });
    const { unmount } = await renderSignIn({
      from: { pathname: "//evil.example/steal" },
    });
    fireEvent.change(
      screen.getByRole("textbox", { name: "Email or username" }),
      {
        target: { value: "teacher@example.com" },
      },
    );
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "secret-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Log In" }));
    await settle();
    expect(screen.getByTestId("landing")).toHaveTextContent("/app/home");
    unmount();

    api.getAuthProviders.mockResolvedValue(OIDC_PROVIDERS);
    for (const pathname of ["/\\evil.example", "/sign-in"]) {
      signInMock.mockReset();
      const view = await renderSignIn({ from: { pathname } });
      fireEvent.click(screen.getByRole("button", { name: "Log In" }));
      expect(signInMock).toHaveBeenCalledWith("/app/home");
      view.unmount();
    }
  });
});

/* ---------- AUTH-03B: email-code login and native password recovery ---------- */

const EMAIL_CODE_PROVIDERS: AuthProvidersResponse = {
  ...NATIVE_PROVIDERS,
  email_code_enabled: true,
  password_recovery_enabled: true,
};

const AUTH03_COPY = {
  en: {
    useEmailCode: "Log in with an email code",
    usePassword: "Log in with a password",
    emailForm: "Log in with a code sent to your email",
    email: "Email Address",
    otp: "OTP code",
    getCode: "Get Code",
    resend: "Resend",
    logIn: "Log In",
    identifier: "Email or username",
    keepSignedIn: "Keep me signed in",
    rememberMe: "Remember Me",
    forgot: "Forget Password",
    recoverDialog: "Recover Password",
    recoverOtp: "Authentication Code (OTP)",
    newPassword: "New Password",
    resetPassword: "Reset Password",
    codeSent: NATIVE_COPY.en.codeSent,
    codeInvalid:
      "The code is incorrect or has expired. Check it or select Resend to get a new code.",
    resetDone:
      "Your password has been reset and all your sessions were signed out. Please log in again with your new password.",
    passwordPolicy: NATIVE_COPY.en.passwordPolicy,
    mfa: NATIVE_COPY.en.mfa,
  },
  vi: {
    useEmailCode: "Đăng nhập bằng mã email",
    usePassword: "Đăng nhập bằng mật khẩu",
    emailForm: "Đăng nhập bằng mã gửi tới email",
    email: "Địa chỉ email",
    otp: "Mã OTP",
    getCode: "Lấy mã",
    resend: "Gửi lại",
    logIn: "Đăng nhập",
    identifier: "Email hoặc tên đăng nhập",
    keepSignedIn: "Duy trì đăng nhập",
    rememberMe: "Ghi nhớ tôi",
    forgot: "Quên mật khẩu",
    recoverDialog: "Khôi phục mật khẩu",
    recoverOtp: "Mã xác thực (OTP)",
    newPassword: "Mật khẩu mới",
    resetPassword: "Đặt lại mật khẩu",
    codeSent: NATIVE_COPY.vi.codeSent,
    codeInvalid:
      "Mã không đúng hoặc đã hết hạn. Vui lòng kiểm tra lại hoặc bấm Gửi lại để nhận mã mới.",
    resetDone:
      "Mật khẩu đã được đặt lại và mọi phiên đăng nhập của bạn đã bị đăng xuất. Vui lòng đăng nhập lại bằng mật khẩu mới.",
    passwordPolicy: NATIVE_COPY.vi.passwordPolicy,
    mfa: NATIVE_COPY.vi.mfa,
  },
} as const;

function expectNoAuthSecretsInBrowserStorage(secrets: readonly string[]) {
  expect(window.sessionStorage.length).toBe(0);
  const stored = JSON.stringify({ ...window.localStorage });
  for (const secret of secrets) {
    expect(stored).not.toContain(secret);
  }
}

async function openEmailCodeLogin(
  language: Language = "en",
  from?: RenderOptions["from"],
) {
  const copy = AUTH03_COPY[language];
  await renderSignIn({ from, language });
  fireEvent.click(screen.getByRole("button", { name: copy.useEmailCode }));
  const form = screen.getByRole("form", { name: copy.emailForm });
  return {
    copy,
    form,
    email: within(form).getByRole("textbox", { name: copy.email }),
    otp: within(form).getByRole("textbox", { name: copy.otp }),
    getCode: within(form).getByRole("button", { name: copy.getCode }),
    submit: within(form).getByRole("button", { name: copy.logIn }),
    keepSignedIn: within(form).getByRole("checkbox", {
      name: copy.keepSignedIn,
    }),
    rememberMe: within(form).getByRole("checkbox", { name: copy.rememberMe }),
  };
}

describe.each(["en", "vi"] as const)(
  "SignInPage email-code login (%s)",
  (language) => {
    const copy = AUTH03_COPY[language];

    beforeEach(() => {
      api.getAuthProviders.mockResolvedValue(EMAIL_CODE_PROVIDERS);
    });

    it("sends a neutral code notice, resends after 60s and signs in with keep-signed-in", async () => {
      vi.useFakeTimers();
      api.startEmailCodeLogin.mockResolvedValue(challengeResponse("login-1"));
      api.resendAuthChallenge.mockResolvedValue(challengeResponse("login-2"));
      api.verifyEmailCodeLogin.mockResolvedValue({ status: "authenticated" });
      const field = await openEmailCodeLogin(language, {
        pathname: "/app/calendar",
        search: "?view=week",
      });
      expect(field.email).toHaveFocus();
      expect(field.keepSignedIn).toBeEnabled();

      fireEvent.change(field.email, {
        target: { value: "learner@example.com" },
      });
      fireEvent.click(field.getCode);
      await settle();
      expect(api.startEmailCodeLogin).toHaveBeenCalledWith(
        { email: "learner@example.com" },
        { baseUrl: "/api" },
      );
      // Same wording whether or not the address has an account.
      expect(screen.getByText(copy.codeSent)).toBeInTheDocument();
      expect(field.email).toHaveAccessibleDescription(copy.codeSent);
      expect(field.getCode).toBeDisabled();
      expect(field.getCode).toHaveTextContent("60s");
      expect(field.otp).toHaveFocus();

      tickSeconds(60);
      expect(field.getCode).toBeEnabled();
      expect(field.getCode).toHaveTextContent(copy.resend);
      fireEvent.click(field.getCode);
      await settle();
      expect(api.resendAuthChallenge).toHaveBeenCalledWith({ baseUrl: "/api" });
      expect(api.startEmailCodeLogin).toHaveBeenCalledTimes(1);

      fireEvent.change(field.otp, { target: { value: "123456" } });
      fireEvent.click(field.keepSignedIn);
      fireEvent.click(field.rememberMe);
      fireEvent.click(field.submit);
      await settle();

      expect(api.verifyEmailCodeLogin).toHaveBeenCalledWith(
        { challenge_id: "login-2", code: "123456", keep_signed_in: true },
        { baseUrl: "/api" },
      );
      expect(completeNativeSignInMock).toHaveBeenCalledTimes(1);
      expect(screen.getByTestId("landing")).toHaveTextContent(
        "/app/calendar?view=week",
      );
      expect(window.localStorage.getItem(REMEMBERED_EMAIL_KEY)).toBe(
        "learner@example.com",
      );
      expectNoAuthSecretsInBrowserStorage(["123456", "login-"]);
      expect(api.loginWithPassword).not.toHaveBeenCalled();
      expect(signInMock).not.toHaveBeenCalled();
    });

    it("answers a wrong or expired code with one generic message", async () => {
      api.startEmailCodeLogin.mockResolvedValue(challengeResponse("login-1"));
      api.verifyEmailCodeLogin
        .mockRejectedValueOnce(apiError(401))
        .mockRejectedValueOnce(apiError(400));
      const field = await openEmailCodeLogin(language);
      fireEvent.change(field.email, {
        target: { value: "learner@example.com" },
      });
      fireEvent.click(field.getCode);
      await settle();

      for (const attempt of ["111111", "222222"]) {
        fireEvent.change(field.otp, { target: { value: attempt } });
        fireEvent.click(field.submit);
        await settle();
        expect(within(field.form).getByRole("alert")).toHaveTextContent(
          copy.codeInvalid,
        );
        expect(field.otp).toHaveValue("");
        expect(field.otp).toHaveFocus();
      }
      expect(completeNativeSignInMock).not.toHaveBeenCalled();
      expect(screen.queryByTestId("landing")).toBeNull();
    });

    it("stops at the multi-factor contract without treating the user as signed in", async () => {
      api.startEmailCodeLogin.mockResolvedValue(challengeResponse("login-1"));
      api.verifyEmailCodeLogin.mockResolvedValue({
        methods: ["totp"],
        status: "mfa_required",
      });
      const field = await openEmailCodeLogin(language);
      fireEvent.change(field.email, {
        target: { value: "admin@example.com" },
      });
      fireEvent.click(field.getCode);
      await settle();
      fireEvent.change(field.otp, { target: { value: "123456" } });
      fireEvent.click(field.submit);
      await settle();

      expect(within(field.form).getByRole("alert")).toHaveTextContent(copy.mfa);
      expect(completeNativeSignInMock).not.toHaveBeenCalled();
      expect(screen.queryByTestId("landing")).toBeNull();
    });
  },
);

describe("SignInPage email-code login edge cases", () => {
  beforeEach(() => {
    api.getAuthProviders.mockResolvedValue(EMAIL_CODE_PROVIDERS);
  });

  it("offers the email-code option only when the server enables it", async () => {
    api.getAuthProviders.mockResolvedValue(NATIVE_PROVIDERS);
    const { unmount } = await renderSignIn();
    expect(
      screen.queryByRole("button", { name: "Log in with an email code" }),
    ).toBeNull();
    unmount();

    api.getAuthProviders.mockResolvedValue(OIDC_PROVIDERS);
    await renderSignIn();
    expect(
      screen.queryByRole("button", { name: "Log in with an email code" }),
    ).toBeNull();
    expect(api.startEmailCodeLogin).not.toHaveBeenCalled();
  });

  it("switches back to the password form and prefills the code form from a typed email", async () => {
    await renderSignIn();
    fireEvent.change(
      screen.getByRole("textbox", { name: "Email or username" }),
      { target: { value: "teacher@example.com" } },
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Log in with an email code" }),
    );
    const form = screen.getByRole("form", {
      name: "Log in with a code sent to your email",
    });
    expect(
      within(form).getByRole("textbox", { name: "Email Address" }),
    ).toHaveValue("teacher@example.com");
    expect(screen.queryByLabelText("Password")).toBeNull();

    fireEvent.click(
      screen.getByRole("button", { name: "Log in with a password" }),
    );
    expect(
      screen.getByRole("textbox", { name: "Email or username" }),
    ).toHaveFocus();
    expect(screen.getByLabelText("Password")).toBeInTheDocument();
    expect(
      screen.queryByRole("form", {
        name: "Log in with a code sent to your email",
      }),
    ).toBeNull();
  });

  it("requires a requested code and drops it when the email changes mid-request", async () => {
    let resolveFirst: (value: AuthSignupChallengeResponse) => void = () =>
      undefined;
    api.startEmailCodeLogin
      .mockReturnValueOnce(
        new Promise<AuthSignupChallengeResponse>((resolve) => {
          resolveFirst = resolve;
        }),
      )
      .mockResolvedValueOnce(challengeResponse("login-new"));
    api.verifyEmailCodeLogin.mockResolvedValue({ status: "authenticated" });
    const field = await openEmailCodeLogin();

    fireEvent.change(field.otp, { target: { value: "123456" } });
    fireEvent.change(field.email, { target: { value: "old@example.com" } });
    fireEvent.click(field.submit);
    expect(field.otp).toHaveAccessibleDescription(
      "Select Get Code to receive a verification code first.",
    );
    expect(field.otp).toHaveFocus();
    expect(api.verifyEmailCodeLogin).not.toHaveBeenCalled();

    fireEvent.click(field.getCode);
    expect(field.getCode).toHaveTextContent("Sending…");
    fireEvent.change(field.email, { target: { value: "new@example.com" } });
    await act(async () => {
      resolveFirst(challengeResponse("login-old"));
    });
    await settle();
    expect(field.getCode).toHaveTextContent("Get Code");
    expect(screen.queryByText(AUTH03_COPY.en.codeSent)).toBeNull();

    fireEvent.click(field.getCode);
    await settle();
    expect(api.startEmailCodeLogin).toHaveBeenLastCalledWith(
      { email: "new@example.com" },
      { baseUrl: "/api" },
    );
    fireEvent.change(field.otp, { target: { value: "654321" } });
    fireEvent.click(field.submit);
    await settle();
    expect(api.verifyEmailCodeLogin).toHaveBeenCalledWith(
      expect.objectContaining({ challenge_id: "login-new", code: "654321" }),
      { baseUrl: "/api" },
    );
  });

  it("starts a new transaction when the resend transaction has ended and reminds about expiry", async () => {
    vi.useFakeTimers();
    api.startEmailCodeLogin
      .mockResolvedValueOnce(challengeResponse("login-1"))
      .mockResolvedValueOnce(challengeResponse("login-2"));
    api.resendAuthChallenge.mockRejectedValue(apiError(400));
    const field = await openEmailCodeLogin();
    fireEvent.change(field.email, {
      target: { value: "learner@example.com" },
    });
    fireEvent.click(field.getCode);
    await settle();
    tickSeconds(60);

    fireEvent.click(field.getCode);
    await settle();
    expect(api.resendAuthChallenge).toHaveBeenCalledTimes(1);
    expect(api.startEmailCodeLogin).toHaveBeenCalledTimes(2);
    expect(field.getCode).toHaveTextContent("60s");

    act(() => {
      vi.advanceTimersByTime(600_000);
    });
    expect(
      screen.getByText(
        "Your verification code has expired. Select Resend to get a new code.",
      ),
    ).toBeInTheDocument();
    fireEvent.change(field.otp, { target: { value: "123456" } });
    fireEvent.click(field.submit);
    expect(field.otp).toHaveAccessibleDescription(
      "This code has expired. Select Resend to get a new code.",
    );
    expect(api.verifyEmailCodeLogin).not.toHaveBeenCalled();
  });

  it("honours Retry-After for 429 code requests and 503 verification", async () => {
    vi.useFakeTimers();
    api.startEmailCodeLogin
      .mockRejectedValueOnce(apiError(429, undefined, 42))
      .mockResolvedValueOnce(challengeResponse("login-1"));
    api.verifyEmailCodeLogin
      .mockRejectedValueOnce(apiError(503, undefined, 2))
      .mockResolvedValueOnce({ status: "authenticated" });
    const field = await openEmailCodeLogin();
    fireEvent.change(field.email, {
      target: { value: "learner@example.com" },
    });

    fireEvent.click(field.getCode);
    await settle();
    expect(
      screen.getByText("Too many attempts. Please try again in 42 seconds."),
    ).toBeInTheDocument();
    expect(field.getCode).toBeDisabled();
    expect(field.getCode).toHaveTextContent("42s");
    tickSeconds(42);
    fireEvent.click(field.getCode);
    await settle();
    expect(screen.getByText(AUTH03_COPY.en.codeSent)).toBeInTheDocument();

    fireEvent.change(field.otp, { target: { value: "123456" } });
    fireEvent.click(field.submit);
    await settle();
    expect(within(field.form).getByRole("alert")).toHaveTextContent(
      "TutorHub is busy right now. Please try again in 2 seconds.",
    );
    expect(field.submit).toBeDisabled();
    expect(field.submit).toHaveTextContent("Log In (2s)");
    expect(field.otp).toHaveValue("123456");
    tickSeconds(2);
    fireEvent.click(field.submit);
    await settle();
    expect(api.verifyEmailCodeLogin).toHaveBeenCalledTimes(2);
    expect(screen.getByTestId("landing")).toHaveTextContent("/app/home");
  });
});

async function openNativeRecovery(language: Language = "en") {
  const copy = AUTH03_COPY[language];
  await renderSignIn({ language });
  const trigger = screen.getByRole("button", { name: copy.forgot });
  fireEvent.click(trigger);
  const dialog = screen.getByRole("dialog", { name: copy.recoverDialog });
  return {
    copy,
    dialog,
    trigger,
    email: within(dialog).getByRole("textbox", { name: copy.email }),
    otp: within(dialog).getByRole("textbox", { name: copy.recoverOtp }),
    newPassword: within(dialog).getByLabelText(copy.newPassword),
    getCode: within(dialog).getByRole("button", { name: copy.getCode }),
    submit: within(dialog).getByRole("button", { name: copy.resetPassword }),
  };
}

type NativeRecoveryFields = Awaited<ReturnType<typeof openNativeRecovery>>;

async function requestRecoveryCode(field: NativeRecoveryFields) {
  fireEvent.change(field.email, { target: { value: "teacher@example.com" } });
  fireEvent.click(field.getCode);
  await settle();
}

describe.each(["en", "vi"] as const)(
  "SignInPage native password recovery (%s)",
  (language) => {
    const copy = AUTH03_COPY[language];

    beforeEach(() => {
      api.getAuthProviders.mockResolvedValue(EMAIL_CODE_PROVIDERS);
      api.startPasswordRecovery.mockResolvedValue(
        challengeResponse("recovery-1"),
      );
    });

    it("resets the password with an emailed code, never signs in and asks to log in again", async () => {
      api.completePasswordRecovery.mockResolvedValue({
        status: "password_reset",
      });
      const field = await openNativeRecovery(language);
      await requestRecoveryCode(field);
      expect(api.startPasswordRecovery).toHaveBeenCalledWith(
        { email: "teacher@example.com" },
        { baseUrl: "/api" },
      );
      expect(within(field.dialog).getByText(copy.codeSent)).toBeInTheDocument();
      expect(field.email).toHaveAccessibleDescription(copy.codeSent);
      expect(field.getCode).toHaveTextContent("60s");
      expect(field.otp).toHaveFocus();

      fireEvent.change(field.otp, { target: { value: "123456" } });
      fireEvent.change(field.newPassword, {
        target: { value: "N3w-recovered-passphrase" },
      });
      fireEvent.click(field.submit);
      await settle();

      expect(api.completePasswordRecovery).toHaveBeenCalledWith(
        {
          challenge_id: "recovery-1",
          code: "123456",
          new_password: "N3w-recovered-passphrase",
        },
        { baseUrl: "/api" },
      );
      expect(resetAfterPasswordRecoveryMock).toHaveBeenCalledTimes(1);
      await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
      expect(screen.getByText(copy.resetDone)).toHaveAttribute(
        "role",
        "status",
      );
      await waitFor(() =>
        expect(
          screen.getByRole("textbox", { name: copy.identifier }),
        ).toHaveFocus(),
      );
      expect(completeNativeSignInMock).not.toHaveBeenCalled();
      expect(signInMock).not.toHaveBeenCalled();
      expect(api.loginWithPassword).not.toHaveBeenCalled();
      expect(screen.queryByTestId("landing")).toBeNull();
      expectNoAuthSecretsInBrowserStorage([
        "N3w-recovered-passphrase",
        "123456",
        "recovery-",
      ]);
    });

    it("clears only the new password when it fails the password policy", async () => {
      api.completePasswordRecovery.mockRejectedValue(
        apiError(400, "auth_password_policy"),
      );
      const field = await openNativeRecovery(language);
      await requestRecoveryCode(field);
      fireEvent.change(field.otp, { target: { value: "123456" } });
      fireEvent.change(field.newPassword, {
        target: { value: "password123" },
      });
      fireEvent.click(field.submit);
      await settle();

      expect(field.newPassword).toHaveValue("");
      expect(field.newPassword).toHaveAttribute("aria-invalid", "true");
      expect(field.newPassword).toHaveAccessibleDescription(
        copy.passwordPolicy,
      );
      expect(field.newPassword).toHaveFocus();
      expect(field.email).toHaveValue("teacher@example.com");
      expect(field.otp).toHaveValue("123456");
      expect(resetAfterPasswordRecoveryMock).not.toHaveBeenCalled();
      expect(screen.getByRole("dialog")).toBeInTheDocument();
    });

    it("answers a wrong or expired recovery code generically", async () => {
      api.completePasswordRecovery.mockRejectedValue(apiError(401));
      const field = await openNativeRecovery(language);
      await requestRecoveryCode(field);
      fireEvent.change(field.otp, { target: { value: "999999" } });
      fireEvent.change(field.newPassword, {
        target: { value: "N3w-recovered-passphrase" },
      });
      fireEvent.click(field.submit);
      await settle();

      expect(within(field.dialog).getByRole("alert")).toHaveTextContent(
        copy.codeInvalid,
      );
      expect(field.otp).toHaveValue("");
      expect(field.otp).toHaveFocus();
      expect(resetAfterPasswordRecoveryMock).not.toHaveBeenCalled();
    });
  },
);

describe("SignInPage native password recovery edge cases", () => {
  it("keeps the identity-portal placeholder when password_recovery_enabled is missing", async () => {
    api.getAuthProviders.mockResolvedValue({
      ...NATIVE_PROVIDERS,
      email_code_enabled: true,
    });
    await renderSignIn();
    fireEvent.click(screen.getByRole("button", { name: "Forget Password" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Recover Password",
    });
    expect(dialog).toHaveAccessibleDescription(
      "Enter your registered email to receive an OTP code",
    );
    fireEvent.change(
      within(dialog).getByRole("textbox", { name: "Email Address" }),
      { target: { value: "teacher@example.com" } },
    );
    fireEvent.click(within(dialog).getByRole("button", { name: "Get Code" }));
    expect(
      within(dialog).getByText(/no code is sent from this page/),
    ).toBeInTheDocument();
    expect(api.startPasswordRecovery).not.toHaveBeenCalled();
  });

  it("validates the code step and the deployment minimum before calling the server", async () => {
    api.getAuthProviders.mockResolvedValue({
      ...EMAIL_CODE_PROVIDERS,
      password_min_length: 12,
    });
    api.startPasswordRecovery.mockResolvedValue(
      challengeResponse("recovery-1"),
    );
    const field = await openNativeRecovery();

    fireEvent.change(field.email, { target: { value: "teacher@example.com" } });
    fireEvent.change(field.otp, { target: { value: "123456" } });
    fireEvent.change(field.newPassword, {
      target: { value: "Long-enough-pass" },
    });
    fireEvent.click(field.submit);
    expect(field.otp).toHaveAccessibleDescription(
      "Select Get Code to receive a verification code first.",
    );
    expect(field.otp).toHaveFocus();

    fireEvent.click(field.getCode);
    await settle();
    fireEvent.change(field.otp, { target: { value: "123456" } });
    fireEvent.change(field.newPassword, { target: { value: "short-pass1" } });
    fireEvent.click(field.submit);
    expect(field.newPassword).toHaveAccessibleDescription(
      "Password must be at least 12 characters.",
    );
    expect(field.newPassword).toHaveFocus();
    expect(api.completePasswordRecovery).not.toHaveBeenCalled();
  });

  it("honours Retry-After when recovery is throttled or busy", async () => {
    vi.useFakeTimers();
    api.getAuthProviders.mockResolvedValue(EMAIL_CODE_PROVIDERS);
    api.startPasswordRecovery
      .mockRejectedValueOnce(apiError(429, undefined, 30))
      .mockResolvedValueOnce(challengeResponse("recovery-1"));
    api.completePasswordRecovery.mockRejectedValueOnce(
      apiError(503, undefined, 3),
    );
    const field = await openNativeRecovery();
    fireEvent.change(field.email, { target: { value: "teacher@example.com" } });

    fireEvent.click(field.getCode);
    await settle();
    expect(
      within(field.dialog).getByText(
        "Too many attempts. Please try again in 30 seconds.",
      ),
    ).toBeInTheDocument();
    expect(field.getCode).toBeDisabled();
    expect(field.getCode).toHaveTextContent("30s");
    tickSeconds(30);
    fireEvent.click(field.getCode);
    await settle();

    fireEvent.change(field.otp, { target: { value: "123456" } });
    fireEvent.change(field.newPassword, {
      target: { value: "N3w-recovered-passphrase" },
    });
    fireEvent.click(field.submit);
    await settle();
    expect(within(field.dialog).getByRole("alert")).toHaveTextContent(
      "TutorHub is busy right now. Please try again in 3 seconds.",
    );
    expect(field.submit).toBeDisabled();
    expect(field.submit).toHaveTextContent("Reset Password (3s)");
    tickSeconds(3);
    expect(field.submit).toBeEnabled();
  });

  it("starts a new login transaction after recovery replaced the browser-bound one", async () => {
    vi.useFakeTimers();
    api.getAuthProviders.mockResolvedValue(EMAIL_CODE_PROVIDERS);
    api.startEmailCodeLogin
      .mockResolvedValueOnce(challengeResponse("login-1"))
      .mockResolvedValueOnce(challengeResponse("login-2"));
    api.startPasswordRecovery.mockResolvedValue(
      challengeResponse("recovery-1"),
    );
    const field = await openEmailCodeLogin();
    fireEvent.change(field.email, {
      target: { value: "teacher@example.com" },
    });
    fireEvent.click(field.getCode);
    await settle();

    fireEvent.click(screen.getByRole("button", { name: "Forget Password" }));
    const dialog = screen.getByRole("dialog", { name: "Recover Password" });
    fireEvent.change(
      within(dialog).getByRole("textbox", { name: "Email Address" }),
      { target: { value: "teacher@example.com" } },
    );
    fireEvent.click(within(dialog).getByRole("button", { name: "Get Code" }));
    await settle();
    expect(api.startPasswordRecovery).toHaveBeenCalledTimes(1);
    fireEvent.click(within(dialog).getByRole("button", { name: "Get back" }));
    await settle();
    expect(screen.queryByRole("dialog")).toBeNull();

    tickSeconds(60);
    expect(field.getCode).toHaveTextContent("Resend");
    fireEvent.click(field.getCode);
    await settle();
    // Resending would re-issue the recovery code bound to the cookie.
    expect(api.resendAuthChallenge).not.toHaveBeenCalled();
    expect(api.startEmailCodeLogin).toHaveBeenCalledTimes(2);
  });
});

describe("SignInPage AUTH-06B Facebook federation", () => {
  const FACEBOOK_PROVIDERS: AuthProvidersResponse = {
    ...NATIVE_PROVIDERS,
    oauth_providers: ["facebook"],
    oidc_default_enabled: false,
  };

  function stubLocationAssign() {
    const assign = vi.fn();
    vi.stubGlobal("location", {
      ...window.location,
      assign,
      href: window.location.href,
      origin: window.location.origin,
    });
    return assign;
  }

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("starts the Facebook flow with a full-page navigation and the protected return path", async () => {
    api.getAuthProviders.mockResolvedValue(FACEBOOK_PROVIDERS);
    const assign = stubLocationAssign();
    await renderSignIn({
      from: { pathname: "/app/classrooms", search: "?tab=2" },
    });

    const facebook = screen.getByRole("button", {
      name: "Continue with Facebook",
    });
    expect(facebook).toBeEnabled();
    fireEvent.click(facebook);

    expect(assign).toHaveBeenCalledTimes(1);
    const target = new URL(String(assign.mock.calls[0]?.[0]));
    expect(target.pathname).toBe("/api/v1/auth/oauth/facebook/start");
    expect(target.searchParams.get("return_to")).toBe("/app/classrooms?tab=2");
    expect(target.searchParams.get("intent")).toBe("login");
    expect(signInMock).not.toHaveBeenCalled();
    // Google is not listed and there is no OIDC portal: no Google button.
    expect(
      screen.queryByRole("button", { name: "Sign in with Google" }),
    ).not.toBeInTheDocument();
  });

  it("keeps the OIDC fallback when the server does not list Facebook", async () => {
    api.getAuthProviders.mockResolvedValue({
      ...NATIVE_PROVIDERS,
      oidc_default_enabled: true,
    });
    const assign = stubLocationAssign();
    await renderSignIn();

    fireEvent.click(
      screen.getByRole("button", { name: "Continue with Facebook" }),
    );

    expect(assign).not.toHaveBeenCalled();
    expect(signInMock).toHaveBeenCalledWith("/app/home");
  });

  it("hides Facebook when neither the server nor the OIDC portal offers it", async () => {
    api.getAuthProviders.mockResolvedValue({
      ...NATIVE_PROVIDERS,
      oidc_default_enabled: false,
    });
    await renderSignIn();

    expect(
      screen.queryByRole("button", { name: "Continue with Facebook" }),
    ).not.toBeInTheDocument();
  });

  it("starts each provider's own flow when both are listed", async () => {
    api.getAuthProviders.mockResolvedValue({
      ...NATIVE_PROVIDERS,
      oauth_providers: ["google", "facebook"],
    });
    const assign = stubLocationAssign();
    await renderSignIn();

    fireEvent.click(
      screen.getByRole("button", { name: "Continue with Facebook" }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Sign in with Google" }),
    );

    const paths = assign.mock.calls.map(
      ([url]) => new URL(String(url)).pathname,
    );
    expect(paths).toEqual([
      "/api/v1/auth/oauth/facebook/start",
      "/api/v1/auth/oauth/google/start",
    ]);
  });

  it("labels the Facebook button in Vietnamese", async () => {
    api.getAuthProviders.mockResolvedValue(FACEBOOK_PROVIDERS);
    await renderSignIn({ language: "vi" });

    expect(
      screen.getByRole("button", { name: "Tiếp tục với Facebook" }),
    ).toBeInTheDocument();
  });
});

describe("SignInPage AUTH-05B Google federation", () => {
  const GOOGLE_PROVIDERS: AuthProvidersResponse = {
    ...NATIVE_PROVIDERS,
    oauth_providers: ["google"],
  };

  function stubLocationAssign() {
    const assign = vi.fn();
    vi.stubGlobal("location", {
      ...window.location,
      assign,
      href: window.location.href,
      origin: window.location.origin,
    });
    return assign;
  }

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("starts the Google flow with a full-page navigation and the protected return path", async () => {
    api.getAuthProviders.mockResolvedValue(GOOGLE_PROVIDERS);
    const assign = stubLocationAssign();
    await renderSignIn({
      from: { pathname: "/app/classrooms", search: "?tab=1" },
    });

    fireEvent.click(
      screen.getByRole("button", { name: "Sign in with Google" }),
    );

    expect(assign).toHaveBeenCalledTimes(1);
    const target = new URL(String(assign.mock.calls[0]?.[0]));
    expect(target.pathname).toBe("/api/v1/auth/oauth/google/start");
    expect(target.searchParams.get("return_to")).toBe("/app/classrooms?tab=1");
    expect(target.searchParams.get("intent")).toBe("login");
    expect(signInMock).not.toHaveBeenCalled();
    // Facebook is not listed: it stays the OIDC-portal fallback.
    fireEvent.click(
      screen.getByRole("button", { name: "Continue with Facebook" }),
    );
    expect(signInMock).toHaveBeenCalledWith("/app/classrooms?tab=1");
    expect(assign).toHaveBeenCalledTimes(1);
  });

  it("keeps the OIDC fallback when the server does not list Google", async () => {
    api.getAuthProviders.mockResolvedValue({
      ...NATIVE_PROVIDERS,
      oidc_default_enabled: true,
    });
    const assign = stubLocationAssign();
    await renderSignIn();

    fireEvent.click(
      screen.getByRole("button", { name: "Sign in with Google" }),
    );

    expect(assign).not.toHaveBeenCalled();
    expect(signInMock).toHaveBeenCalledWith("/app/home");
  });

  it("puts the consent line under the social buttons and opens the legal dialog", async () => {
    api.getAuthProviders.mockResolvedValue(GOOGLE_PROVIDERS);
    await renderSignIn();

    const consent = screen.getByText(/^By continuing, you agree to the/);
    const agreement = within(consent).getByRole("button", {
      name: "User Agreement",
    });
    expect(agreement).toHaveAttribute("aria-haspopup", "dialog");
    fireEvent.click(agreement);
    const dialog = await screen.findByRole("dialog", {
      name: "Terms & Privacy Policy",
    });
    expect(
      within(dialog).getByRole("tab", { name: "User Agreement" }),
    ).toHaveAttribute("aria-selected", "true");
    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(agreement).toHaveFocus();

    fireEvent.click(
      within(consent).getByRole("button", { name: "Privacy Policy" }),
    );
    const privacy = await screen.findByRole("dialog", {
      name: "Terms & Privacy Policy",
    });
    expect(
      within(privacy).getByRole("tab", { name: "Privacy Policy" }),
    ).toHaveAttribute("aria-selected", "true");
  });

  it("shows the Vietnamese Google label and consent line", async () => {
    api.getAuthProviders.mockResolvedValue(GOOGLE_PROVIDERS);
    await renderSignIn({ language: "vi" });

    expect(
      screen.getByRole("button", { name: "Đăng nhập bằng Google" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/^Bằng việc tiếp tục, bạn đồng ý với/),
    ).toBeInTheDocument();
  });

  it.each([
    [
      "signup_disabled",
      "New accounts can't be created with this sign-in option right now. Log in with the method you used before or contact your administrator.",
    ],
    [
      "email_unverified",
      "The email address of the account you chose isn't verified by its provider. Verify it there, then try again, or use another sign-in method.",
    ],
    [
      "provider_unavailable",
      "This sign-in option isn't available right now. Please use another sign-in method.",
    ],
    [
      "assurance_required",
      "This account requires a stronger sign-in method. Log in with the security method set up for it.",
    ],
    [
      "authentication_failed",
      "We couldn't complete that sign-in. Please try again.",
    ],
    [
      "temporarily_unavailable",
      "Sign-in is temporarily interrupted. Please try again in a few minutes.",
    ],
    [
      "rate_limited",
      "Too many sign-in attempts. Please wait a moment and try again.",
    ],
  ])(
    "maps ?error=%s to a fixed message and cleans the address",
    async (code, message) => {
      api.getAuthProviders.mockResolvedValue(GOOGLE_PROVIDERS);
      const { router } = await renderSignIn({
        from: { pathname: "/app/messages" },
        search: `?error=${code}`,
      });

      expect(screen.getByRole("alert")).toHaveTextContent(message);
      await waitFor(() => expect(router.state.location.search).toBe(""));
      expect(router.state.location.pathname).toBe("/sign-in");
      // The protected return path survives the replace.
      expect(router.state.location.state).toEqual({
        from: { pathname: "/app/messages" },
      });
      expect(screen.getByRole("alert")).toHaveTextContent(message);
    },
  );

  it("uses a generic message for unknown codes and never renders the query value", async () => {
    const { container, router } = await renderSignIn({
      search: "?error=%3Cb%3Einjected%3C%2Fb%3E",
    });

    expect(screen.getByRole("alert")).toHaveTextContent(
      "We couldn't complete that sign-in. Please try again.",
    );
    expect(container.innerHTML).not.toContain("injected");
    await waitFor(() => expect(router.state.location.search).toBe(""));
  });

  it("shows the Vietnamese callback message", async () => {
    await renderSignIn({ language: "vi", search: "?error=email_unverified" });

    expect(screen.getByRole("alert")).toHaveTextContent(
      "Email của tài khoản bạn chọn chưa được nhà cung cấp xác minh.",
    );
  });

  it("moves a pending link to /account-link without loading sign-in options", async () => {
    await renderSignIn({ search: "?link=pending&provider=google" });

    expect(screen.getByTestId("landing")).toHaveTextContent(
      "/account-link?provider=google",
    );
    expect(api.getAuthProviders).not.toHaveBeenCalled();
  });

  it("moves a pending Facebook registration to /complete-registration", async () => {
    await renderSignIn({
      search: "?registration=pending&provider=facebook",
    });

    expect(screen.getByTestId("landing")).toHaveTextContent(
      "/complete-registration?provider=facebook",
    );
    expect(api.getAuthProviders).not.toHaveBeenCalled();
  });

  it("fixes the provider of a pending registration to Facebook", async () => {
    await renderSignIn({
      search: "?registration=pending&provider=javascript%3Aalert(1)",
    });

    expect(screen.getByTestId("landing")).toHaveTextContent(
      "/complete-registration?provider=facebook",
    );
  });

  it("allowlists the provider of a pending link", async () => {
    await renderSignIn({
      search: "?link=pending&provider=javascript%3Aalert(1)",
    });

    expect(screen.getByTestId("landing")).toHaveTextContent(
      "/account-link?provider=google",
    );
  });
});
