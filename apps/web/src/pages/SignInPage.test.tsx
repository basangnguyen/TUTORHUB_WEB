import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { createMemoryRouter } from "react-router";
import { RouterProvider } from "react-router/dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, type Language } from "../app/i18n";
import { SignInPage } from "./SignInPage";

const { signInMock } = vi.hoisted(() => ({ signInMock: vi.fn() }));

vi.mock("../app/session", () => ({
  useSession: () => ({ signIn: signInMock }),
}));

const REMEMBERED_EMAIL_KEY = "tutorhub.remembered_email";
const SLIDE_ALTS = [
  "Modern classroom with TutorHub screens showing the course library and a live whiteboard lesson",
  "Online class with students on video sharing an interactive whiteboard on a large display and a tablet",
  "TutorHub dashboard with today's classes, AI class matching, parent chat, and homework progress",
] as const;

interface RenderOptions {
  from?: { pathname: string; search?: string };
  language?: Language;
}

function renderSignIn({ from, language = "en" }: RenderOptions = {}) {
  const router = createMemoryRouter(
    [{ path: "/sign-in", element: <SignInPage /> }],
    {
      initialEntries: [{ pathname: "/sign-in", state: from ? { from } : null }],
    },
  );
  return render(
    <I18nProvider initialLanguage={language}>
      <RouterProvider router={router} />
    </I18nProvider>,
  );
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
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  Reflect.deleteProperty(window, "matchMedia");
  Reflect.deleteProperty(document, "visibilityState");
});

describe("SignInPage", () => {
  it("renders the TutorHub V1 login layout with static auth assets", () => {
    renderSignIn();

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
      name: "Log in with Facebook",
    });
    expect(facebook.querySelector("img")).toHaveAttribute(
      "src",
      "/images/auth/icon/facebook.svg",
    );
    const google = screen.getByRole("button", { name: "Log in with Google" });
    expect(google.querySelector("img")).toHaveAttribute(
      "src",
      "/images/auth/icon/google.svg",
    );
    expect(
      screen.getByRole("button", { name: "Forget Password" }),
    ).toBeInTheDocument();
  });

  it("renders Vietnamese copy when the interface language is Vietnamese", () => {
    renderSignIn({ language: "vi" });

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

  it("exposes only the three real slides to assistive technology", () => {
    renderSignIn();
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

  it("changes slides with arrows and dots and auto-advances every 4 seconds", () => {
    vi.useFakeTimers();
    renderSignIn();

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

  it("auto-plays on load even with reduced motion and loops 3 -> 1 forward without rewinding", () => {
    stubReducedMotion(true);
    vi.useFakeTimers();
    renderSignIn();

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

  it("wraps backwards through the leading clone and settles on transitionend", () => {
    vi.useFakeTimers();
    renderSignIn();
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

  it("pauses with the pause control and resumes the remaining time on play", () => {
    vi.useFakeTimers();
    renderSignIn();

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

  it("pauses while hovered, keyboard-focused or when the browser tab is hidden", () => {
    vi.useFakeTimers();
    renderSignIn();
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

  it("fills a progress pill on the active dot that restarts per slide and pauses with the show", () => {
    vi.useFakeTimers();
    renderSignIn();
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

  it("changes slides by dragging or swiping the image area past 40px", () => {
    vi.useFakeTimers();
    renderSignIn();
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

  it("moves between slides with ArrowLeft and ArrowRight while the slider has focus", () => {
    vi.useFakeTimers();
    renderSignIn();
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

  it("remembers only the email when Remember Me is enabled and forgets it when cleared", () => {
    const { unmount } = renderSignIn();

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

    renderSignIn();
    expect(
      screen.getByRole("textbox", { name: "Email or username" }),
    ).toHaveValue("teacher@example.com");
    const remember = screen.getByRole("checkbox", { name: "Remember Me" });
    expect(remember).toBeChecked();

    fireEvent.click(remember);
    expect(window.localStorage.getItem(REMEMBERED_EMAIL_KEY)).toBeNull();
  });

  it("clears the remembered email when Remember Me is turned off before signing in", () => {
    window.localStorage.setItem(REMEMBERED_EMAIL_KEY, "old@example.com");
    renderSignIn();

    fireEvent.click(screen.getByRole("checkbox", { name: "Remember Me" }));
    fireEvent.click(screen.getByRole("button", { name: "Log In" }));

    expect(window.localStorage.length).toBe(0);
    expect(signInMock).toHaveBeenCalledWith("/app/home");
  });

  it("starts the BFF sign-in with the protected return path from Log In, Google and Facebook", () => {
    renderSignIn({ from: { pathname: "/app/classrooms", search: "?tab=1" } });

    fireEvent.click(screen.getByRole("button", { name: "Log In" }));
    fireEvent.click(screen.getByRole("button", { name: "Log in with Google" }));
    fireEvent.click(
      screen.getByRole("button", { name: "Log in with Facebook" }),
    );

    expect(signInMock).toHaveBeenCalledTimes(3);
    for (const call of signInMock.mock.calls) {
      expect(call).toEqual(["/app/classrooms?tab=1"]);
    }
  });

  it("defaults the return path to the home dashboard", () => {
    renderSignIn();

    fireEvent.click(screen.getByRole("button", { name: "Log In" }));

    expect(signInMock).toHaveBeenCalledWith("/app/home");
  });

  it("switches between Sign Up and Log In and validates the sign-up form", () => {
    vi.useFakeTimers();
    renderSignIn({ from: { pathname: "/app/calendar" } });

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
    renderSignIn();
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
    renderSignIn({ from: { pathname: "/app/messages" } });

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

function openSignUpForm(language: Language) {
  const copy = LEGAL_COPY[language];
  renderSignIn({ language });
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
      const field = openSignUpForm(language);
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
      const field = openSignUpForm(language);
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
      const field = openSignUpForm(language);
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
