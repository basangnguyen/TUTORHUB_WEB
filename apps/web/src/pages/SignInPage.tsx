import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from "@tutorhub/ui";
import { Pause, Play } from "lucide-react";
import {
  Fragment,
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  useSyncExternalStore,
  type FocusEvent,
  type FormEvent,
} from "react";
import { useLocation } from "react-router";
import { useI18n, type Language } from "../app/i18n";
import { useSession } from "../app/session";
import "./SignInPage.css";

const REMEMBERED_EMAIL_KEY = "tutorhub.remembered_email";
const REMEMBERED_EMAIL_MAX_LENGTH = 320;
const SLIDE_INTERVAL_MS = 4000;
const OTP_COOLDOWN_SECONDS = 60;
const MIN_PASSWORD_LENGTH = 8;
const DEFAULT_RETURN_TO = "/app/home";
const REDUCED_MOTION_QUERY = "(prefers-reduced-motion: reduce)";
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const OTP_PATTERN = /^\d{6}$/;

const AUTH_IMAGE_ROOT = "/images/auth";
const LOGO_SRC = `${AUTH_IMAGE_ROOT}/logomoi.png`;
const FACEBOOK_ICON_SRC = `${AUTH_IMAGE_ROOT}/icon/facebook.svg`;
const GOOGLE_ICON_SRC = `${AUTH_IMAGE_ROOT}/icon/google.svg`;
const SLIDES = [
  { src: `${AUTH_IMAGE_ROOT}/slide_login/login1.png`, altKey: "slide1Alt" },
  { src: `${AUTH_IMAGE_ROOT}/slide_login/login2.png`, altKey: "slide2Alt" },
  { src: `${AUTH_IMAGE_ROOT}/slide_login/login3.png`, altKey: "slide3Alt" },
] as const;
const SLIDE_WIDTH = 1672;
const SLIDE_HEIGHT = 941;

const en = {
  badge: "TutorHub Enterprise",
  titleLead: "",
  titleAccent: "AI-Powered",
  titleTail: "Tutoring Platform",
  subtitle:
    "Manage classes, match tutors, and track schedules in one intelligent platform.",
  sliderLabel: "TutorHub highlights",
  slidePosition: "{current} of {total}",
  slide1Alt:
    "Modern classroom with TutorHub screens showing the course library and a live whiteboard lesson",
  slide2Alt:
    "Online class with students on video sharing an interactive whiteboard on a large display and a tablet",
  slide3Alt:
    "TutorHub dashboard with today's classes, AI class matching, parent chat, and homework progress",
  slideFailed: "Slide image failed to load",
  previousSlide: "Previous slide",
  nextSlide: "Next slide",
  goToSlide: "Go to slide {index}",
  pauseSlides: "Pause slideshow",
  playSlides: "Play slideshow",
  panelLabel: "Account access",
  brandName: "TutorHub",
  brandTagline: "Tutoring Platform",
  signUp: "Sign Up",
  logIn: "Log In",
  loginHeading: "Log in to TutorHub",
  identifierLabel: "Email or username",
  identifierPlaceholder: "Enter email or username",
  passwordLabel: "Password",
  passwordPlaceholder: "Enter the password",
  keepSignedIn: "Keep me signed in",
  keepSignedInHint:
    "Session length is managed securely by the TutorHub identity portal.",
  rememberMe: "Remember Me",
  rememberMeHint: "Only your email or username is remembered on this device.",
  or: "Or",
  facebook: "Log in with Facebook",
  google: "Log in with Google",
  forgotPassword: "Forget Password",
  signUpHeading: "Create an Account",
  fullNameLabel: "Full Name",
  emailLabel: "Email Address",
  getCode: "Get Code",
  resendCode: "Resend",
  codeCountdown: "{seconds}s",
  otpLabel: "OTP code",
  otpPlaceholder: "- Enter OTP Code -",
  signUpPasswordPlaceholder: "Password",
  termsLead: "I have read and agree to the ",
  userAgreement: "User Agreement",
  termsJoin: " and ",
  privacyPolicy: "Privacy Policy",
  createAccount: "Create Account",
  codeNotice:
    "Verification codes are issued by the TutorHub identity portal, so no code is sent from this page. Continue with secure sign-in to verify your email.",
  signUpNoticeTitle: "Finish on the TutorHub identity portal",
  signUpNotice:
    "TutorHub V2 creates accounts through its central identity portal. Nothing you entered here was sent or saved.",
  continueSecure: "Continue to secure sign-in",
  recoverTitle: "Recover Password",
  recoverSubtitle: "Enter your registered email to receive an OTP code",
  recoverEmailPlaceholder: "e.g. admin@tutorhub.com",
  recoverOtpLabel: "Authentication Code (OTP)",
  recoverOtpPlaceholder: "123456",
  newPasswordLabel: "New Password",
  newPasswordPlaceholder: "Enter new password",
  resetPassword: "Reset Password",
  getBack: "Get back",
  recoverNoticeTitle: "Finish on the TutorHub identity portal",
  recoverNotice:
    "Passwords are reset through the TutorHub identity portal. Your new password was not sent or saved by this page.",
  errorFullName: "Please enter your full name.",
  errorEmail: "Please enter a valid email address.",
  errorOtp: "Please enter the 6-digit OTP code.",
  errorPassword: "Password must be at least 8 characters.",
  errorTerms: "You must agree to the User Agreement and Privacy Policy.",
};

type SignInCopy = Record<keyof typeof en, string>;
type CopyKey = keyof SignInCopy;

const vi: SignInCopy = {
  badge: "TutorHub Enterprise",
  titleLead: "Nền tảng gia sư",
  titleAccent: "ứng dụng AI",
  titleTail: "",
  subtitle:
    "Quản lý lớp học, kết nối gia sư và theo dõi lịch học trên một nền tảng thông minh.",
  sliderLabel: "Điểm nổi bật của TutorHub",
  slidePosition: "{current}/{total}",
  slide1Alt:
    "Lớp học hiện đại với màn hình TutorHub hiển thị thư viện khóa học và bảng trắng đang giảng bài",
  slide2Alt:
    "Lớp học trực tuyến với học sinh qua video cùng dùng bảng trắng tương tác trên màn hình lớn và máy tính bảng",
  slide3Alt:
    "Bảng điều khiển TutorHub với lớp học hôm nay, gợi ý lớp bằng AI, trò chuyện với phụ huynh và tiến độ bài tập",
  slideFailed: "Chưa tải được ảnh trình chiếu",
  previousSlide: "Ảnh trước",
  nextSlide: "Ảnh tiếp theo",
  goToSlide: "Chuyển tới ảnh {index}",
  pauseSlides: "Tạm dừng trình chiếu",
  playSlides: "Tiếp tục trình chiếu",
  panelLabel: "Truy cập tài khoản",
  brandName: "TutorHub",
  brandTagline: "Nền tảng gia sư",
  signUp: "Đăng ký",
  logIn: "Đăng nhập",
  loginHeading: "Đăng nhập vào TutorHub",
  identifierLabel: "Email hoặc tên đăng nhập",
  identifierPlaceholder: "Nhập email hoặc tên đăng nhập",
  passwordLabel: "Mật khẩu",
  passwordPlaceholder: "Nhập mật khẩu",
  keepSignedIn: "Duy trì đăng nhập",
  keepSignedInHint:
    "Thời hạn phiên đăng nhập do cổng định danh TutorHub quản lý an toàn.",
  rememberMe: "Ghi nhớ tôi",
  rememberMeHint: "Chỉ ghi nhớ email hoặc tên đăng nhập trên thiết bị này.",
  or: "Hoặc",
  facebook: "Đăng nhập bằng Facebook",
  google: "Đăng nhập bằng Google",
  forgotPassword: "Quên mật khẩu",
  signUpHeading: "Tạo tài khoản mới",
  fullNameLabel: "Họ và tên",
  emailLabel: "Địa chỉ email",
  getCode: "Lấy mã",
  resendCode: "Gửi lại",
  codeCountdown: "{seconds}s",
  otpLabel: "Mã OTP",
  otpPlaceholder: "- Nhập mã OTP -",
  signUpPasswordPlaceholder: "Mật khẩu",
  termsLead: "Tôi đã đọc và đồng ý với ",
  userAgreement: "Thỏa thuận người dùng",
  termsJoin: " và ",
  privacyPolicy: "Chính sách quyền riêng tư",
  createAccount: "Tạo tài khoản",
  codeNotice:
    "Mã xác minh do cổng định danh TutorHub cấp nên trang này không gửi mã. Hãy tiếp tục đăng nhập bảo mật để xác minh email.",
  signUpNoticeTitle: "Hoàn tất tại cổng định danh TutorHub",
  signUpNotice:
    "TutorHub V2 tạo tài khoản qua cổng định danh tập trung. Thông tin bạn vừa nhập chưa được gửi hay lưu lại.",
  continueSecure: "Tiếp tục đăng nhập bảo mật",
  recoverTitle: "Khôi phục mật khẩu",
  recoverSubtitle: "Nhập email đã đăng ký để nhận mã OTP",
  recoverEmailPlaceholder: "ví dụ: admin@tutorhub.com",
  recoverOtpLabel: "Mã xác thực (OTP)",
  recoverOtpPlaceholder: "123456",
  newPasswordLabel: "Mật khẩu mới",
  newPasswordPlaceholder: "Nhập mật khẩu mới",
  resetPassword: "Đặt lại mật khẩu",
  getBack: "Quay lại",
  recoverNoticeTitle: "Hoàn tất tại cổng định danh TutorHub",
  recoverNotice:
    "Mật khẩu được đặt lại qua cổng định danh TutorHub. Mật khẩu mới chưa được gửi hay lưu bởi trang này.",
  errorFullName: "Vui lòng nhập họ và tên.",
  errorEmail: "Vui lòng nhập địa chỉ email hợp lệ.",
  errorOtp: "Vui lòng nhập mã OTP gồm 6 chữ số.",
  errorPassword: "Mật khẩu cần có ít nhất 8 ký tự.",
  errorTerms:
    "Bạn cần đồng ý với Thỏa thuận người dùng và Chính sách quyền riêng tư.",
};

const copyByLanguage: Record<Language, SignInCopy> = { en, vi };

function useSignInCopy() {
  const { language } = useI18n();
  return copyByLanguage[language];
}

function formatCopy(template: string, values: Record<string, string | number>) {
  return template.replace(/\{(\w+)\}/g, (match, name: string) => {
    const value = values[name];
    return value === undefined ? match : String(value);
  });
}

function readReturnTo(state: unknown) {
  if (typeof state !== "object" || state === null || !("from" in state)) {
    return DEFAULT_RETURN_TO;
  }
  const from: unknown = state.from;
  if (typeof from !== "object" || from === null) {
    return DEFAULT_RETURN_TO;
  }
  const pathname =
    "pathname" in from && typeof from.pathname === "string"
      ? from.pathname
      : DEFAULT_RETURN_TO;
  const search =
    "search" in from && typeof from.search === "string" ? from.search : "";
  return `${pathname}${search}`;
}

function readRememberedEmail() {
  try {
    const value = window.localStorage.getItem(REMEMBERED_EMAIL_KEY);
    return value && value.length <= REMEMBERED_EMAIL_MAX_LENGTH ? value : "";
  } catch {
    return "";
  }
}

function writeRememberedEmail(value: string) {
  try {
    window.localStorage.setItem(REMEMBERED_EMAIL_KEY, value);
  } catch {
    // Storage may be unavailable (private mode or policy); remembering is best effort.
  }
}

function clearRememberedEmail() {
  try {
    window.localStorage.removeItem(REMEMBERED_EMAIL_KEY);
  } catch {
    // Storage may be unavailable (private mode or policy); nothing to clear.
  }
}

function subscribeReducedMotion(onChange: () => void) {
  if (typeof window.matchMedia !== "function") {
    return () => undefined;
  }
  const media = window.matchMedia(REDUCED_MOTION_QUERY);
  media.addEventListener("change", onChange);
  return () => media.removeEventListener("change", onChange);
}

function getReducedMotion() {
  return (
    typeof window.matchMedia === "function" &&
    window.matchMedia(REDUCED_MOTION_QUERY).matches
  );
}

function usePrefersReducedMotion() {
  return useSyncExternalStore(
    subscribeReducedMotion,
    getReducedMotion,
    () => false,
  );
}

function useCodeCooldown() {
  const [remaining, setRemaining] = useState(0);
  const [hasRequested, setHasRequested] = useState(false);

  useEffect(() => {
    if (remaining <= 0) {
      return;
    }
    const timer = window.setTimeout(() => {
      setRemaining((value) => Math.max(0, value - 1));
    }, 1000);
    return () => window.clearTimeout(timer);
  }, [remaining]);

  const start = useCallback(() => {
    setRemaining(OTP_COOLDOWN_SECONDS);
    setHasRequested(true);
  }, []);

  return { hasRequested, remaining, start };
}

function isFocusLeaving(event: FocusEvent<HTMLElement>) {
  const next = event.relatedTarget;
  return !(next instanceof Node && event.currentTarget.contains(next));
}

export function SignInPage() {
  const c = useSignInCopy();
  const { signIn } = useSession();
  const location = useLocation();
  const returnTo = readReturnTo(location.state);
  const [view, setView] = useState<"login" | "signup">("login");
  const [focusFormOnMount, setFocusFormOnMount] = useState(false);
  const heroTitleId = useId();

  const startSignIn = useCallback(() => signIn(returnTo), [returnTo, signIn]);

  function switchView() {
    setFocusFormOnMount(true);
    setView((current) => (current === "login" ? "signup" : "login"));
  }

  return (
    <main className="sign-in">
      <section className="sign-in__hero" aria-labelledby={heroTitleId}>
        <p className="sign-in__badge">{c.badge}</p>
        <h1 className="sign-in__title" id={heroTitleId}>
          {[
            { accent: false, text: c.titleLead },
            { accent: true, text: c.titleAccent },
            { accent: false, text: c.titleTail },
          ]
            .filter((part) => part.text)
            .map((part, index) => (
              <Fragment key={part.text}>
                {index > 0 ? " " : null}
                <span
                  className={
                    part.accent
                      ? "sign-in__title-part sign-in__title-accent"
                      : "sign-in__title-part"
                  }
                >
                  {part.text}
                </span>
              </Fragment>
            ))}
        </h1>
        <p className="sign-in__subtitle">{c.subtitle}</p>
        <LoginImageSlider />
      </section>

      <section className="sign-in__panel" aria-label={c.panelLabel}>
        <div className="sign-in__panel-inner">
          <div className="sign-in__brand-row">
            <div className="sign-in__brand">
              <img
                alt=""
                className="sign-in__brand-logo"
                decoding="async"
                height={48}
                src={LOGO_SRC}
                width={48}
              />
              <div className="sign-in__brand-text">
                <p className="sign-in__brand-name">{c.brandName}</p>
                <p className="sign-in__brand-tagline">{c.brandTagline}</p>
              </div>
            </div>
            <button
              className="sign-in__switch"
              onClick={switchView}
              type="button"
            >
              {view === "login" ? c.signUp : c.logIn}
            </button>
          </div>

          {view === "login" ? (
            <LoginForm autoFocus={focusFormOnMount} onSignIn={startSignIn} />
          ) : (
            <SignUpForm autoFocus={focusFormOnMount} onSignIn={startSignIn} />
          )}
        </div>
      </section>
    </main>
  );
}

function LoginImageSlider() {
  const c = useSignInCopy();
  const reducedMotion = usePrefersReducedMotion();
  const [activeIndex, setActiveIndex] = useState(0);
  const [cycle, setCycle] = useState(0);
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [rotationPreference, setRotationPreference] = useState<boolean | null>(
    null,
  );
  const [failedSlides, setFailedSlides] = useState<readonly number[]>([]);
  const rotationEnabled = rotationPreference ?? !reducedMotion;
  const autoRotating = rotationEnabled && !hovered && !focused;
  const total = SLIDES.length;

  useEffect(() => {
    if (!autoRotating || total < 2) {
      return;
    }
    const timer = window.setTimeout(() => {
      setActiveIndex((index) => (index + 1) % total);
    }, SLIDE_INTERVAL_MS);
    return () => window.clearTimeout(timer);
  }, [activeIndex, autoRotating, cycle, total]);

  function showSlide(index: number) {
    setActiveIndex(((index % total) + total) % total);
    setCycle((value) => value + 1);
  }

  function markFailed(index: number) {
    setFailedSlides((current) =>
      current.includes(index) ? current : [...current, index],
    );
  }

  return (
    <div
      aria-label={c.sliderLabel}
      aria-roledescription="carousel"
      className="sign-in-slider"
      onBlur={(event) => {
        if (isFocusLeaving(event)) setFocused(false);
      }}
      onFocus={() => setFocused(true)}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      role="region"
    >
      <div
        aria-live={autoRotating ? "off" : "polite"}
        className="sign-in-slider__viewport"
      >
        {SLIDES.map((slide, index) => {
          const active = index === activeIndex;
          return (
            <div
              aria-hidden={active ? undefined : true}
              aria-label={formatCopy(c.slidePosition, {
                current: index + 1,
                total,
              })}
              aria-roledescription="slide"
              className={`sign-in-slider__slide${active ? " is-active" : ""}`}
              key={slide.src}
              role="group"
            >
              {failedSlides.includes(index) ? (
                <p className="sign-in-slider__fallback">{c.slideFailed}</p>
              ) : (
                <img
                  alt={c[slide.altKey]}
                  decoding="async"
                  draggable={false}
                  fetchPriority={index === 0 ? "high" : "auto"}
                  height={SLIDE_HEIGHT}
                  onError={() => markFailed(index)}
                  src={slide.src}
                  width={SLIDE_WIDTH}
                />
              )}
            </div>
          );
        })}
      </div>

      <button
        aria-label={c.previousSlide}
        className="sign-in-slider__arrow sign-in-slider__arrow--prev"
        onClick={() => showSlide(activeIndex - 1)}
        type="button"
      >
        <span aria-hidden="true">‹</span>
      </button>
      <button
        aria-label={c.nextSlide}
        className="sign-in-slider__arrow sign-in-slider__arrow--next"
        onClick={() => showSlide(activeIndex + 1)}
        type="button"
      >
        <span aria-hidden="true">›</span>
      </button>

      <div className="sign-in-slider__dots">
        {SLIDES.map((slide, index) => (
          <button
            aria-current={index === activeIndex ? "true" : undefined}
            aria-label={formatCopy(c.goToSlide, { index: index + 1 })}
            className={`sign-in-slider__dot${
              index === activeIndex ? " is-active" : ""
            }`}
            key={slide.src}
            onClick={() => showSlide(index)}
            type="button"
          />
        ))}
      </div>

      <button
        aria-label={rotationEnabled ? c.pauseSlides : c.playSlides}
        className="sign-in-slider__rotation"
        onClick={() => setRotationPreference(!rotationEnabled)}
        type="button"
      >
        {rotationEnabled ? (
          <Pause aria-hidden="true" />
        ) : (
          <Play aria-hidden="true" />
        )}
      </button>
    </div>
  );
}

interface LoginFormProps {
  autoFocus: boolean;
  onSignIn: () => void;
}

function LoginForm({ autoFocus, onSignIn }: LoginFormProps) {
  const c = useSignInCopy();
  const headingId = useId();
  const keepSignedInHintId = useId();
  const rememberHintId = useId();
  const identifierRef = useRef<HTMLInputElement>(null);
  const [identifier, setIdentifier] = useState(readRememberedEmail);
  const [password, setPassword] = useState("");
  const [rememberMe, setRememberMe] = useState(() =>
    Boolean(readRememberedEmail()),
  );

  useEffect(() => {
    if (autoFocus) identifierRef.current?.focus();
  }, [autoFocus]);

  function handleRememberChange(checked: boolean) {
    setRememberMe(checked);
    if (!checked) clearRememberedEmail();
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const remembered = identifier.trim();
    if (
      rememberMe &&
      remembered &&
      remembered.length <= REMEMBERED_EMAIL_MAX_LENGTH
    ) {
      writeRememberedEmail(remembered);
    } else {
      clearRememberedEmail();
    }
    setPassword("");
    onSignIn();
  }

  return (
    <div className="sign-in-form">
      <form
        aria-labelledby={headingId}
        className="sign-in-form__stack"
        noValidate
        onSubmit={handleSubmit}
      >
        <h2 className="visually-hidden" id={headingId}>
          {c.loginHeading}
        </h2>
        <input
          aria-label={c.identifierLabel}
          autoComplete="username"
          className="sign-in-input"
          maxLength={REMEMBERED_EMAIL_MAX_LENGTH}
          name="identifier"
          onChange={(event) => setIdentifier(event.target.value)}
          placeholder={c.identifierPlaceholder}
          ref={identifierRef}
          spellCheck={false}
          type="text"
          value={identifier}
        />
        <input
          aria-label={c.passwordLabel}
          autoComplete="current-password"
          className="sign-in-input"
          name="password"
          onChange={(event) => setPassword(event.target.value)}
          placeholder={c.passwordPlaceholder}
          type="password"
          value={password}
        />
        <button
          className="sign-in-button sign-in-button--primary sign-in-form__submit"
          type="submit"
        >
          {c.logIn}
        </button>
        <div className="sign-in-form__options">
          <label className="sign-in-check" title={c.keepSignedInHint}>
            <input
              aria-describedby={keepSignedInHintId}
              disabled
              name="keep-signed-in"
              type="checkbox"
            />
            <span>{c.keepSignedIn}</span>
          </label>
          <label className="sign-in-check" title={c.rememberMeHint}>
            <input
              aria-describedby={rememberHintId}
              checked={rememberMe}
              name="remember-me"
              onChange={(event) => handleRememberChange(event.target.checked)}
              type="checkbox"
            />
            <span>{c.rememberMe}</span>
          </label>
          <span className="visually-hidden" id={keepSignedInHintId}>
            {c.keepSignedInHint}
          </span>
          <span className="visually-hidden" id={rememberHintId}>
            {c.rememberMeHint}
          </span>
        </div>
      </form>

      <div className="sign-in-divider">
        <span>{c.or}</span>
      </div>

      <div className="sign-in-form__social">
        <button
          className="sign-in-button sign-in-button--facebook"
          onClick={onSignIn}
          type="button"
        >
          <img alt="" height={22} src={FACEBOOK_ICON_SRC} width={22} />
          <span>{c.facebook}</span>
        </button>
        <button
          className="sign-in-button sign-in-button--google"
          onClick={onSignIn}
          type="button"
        >
          <img alt="" height={22} src={GOOGLE_ICON_SRC} width={22} />
          <span>{c.google}</span>
        </button>
      </div>

      <Dialog>
        <DialogTrigger asChild>
          <button className="sign-in-form__forgot" type="button">
            {c.forgotPassword}
          </button>
        </DialogTrigger>
        <DialogContent aria-modal="true" className="sign-in-recover" hideClose>
          <RecoverPasswordForm onSignIn={onSignIn} />
        </DialogContent>
      </Dialog>
    </div>
  );
}

type SignUpField = "fullName" | "email" | "otp" | "password" | "terms";
type SignUpErrors = Partial<Record<SignUpField, CopyKey>>;
const SIGN_UP_FIELD_ORDER: readonly SignUpField[] = [
  "fullName",
  "email",
  "otp",
  "password",
  "terms",
];

interface SignUpFormProps {
  autoFocus: boolean;
  onSignIn: () => void;
}

function SignUpForm({ autoFocus, onSignIn }: SignUpFormProps) {
  const c = useSignInCopy();
  const ids = {
    heading: useId(),
    fullName: useId(),
    email: useId(),
    otp: useId(),
    password: useId(),
    terms: useId(),
    codeNotice: useId(),
  };
  const fullNameRef = useRef<HTMLInputElement>(null);
  const emailRef = useRef<HTMLInputElement>(null);
  const otpRef = useRef<HTMLInputElement>(null);
  const passwordRef = useRef<HTMLInputElement>(null);
  const termsRef = useRef<HTMLInputElement>(null);
  const [fullName, setFullName] = useState("");
  const [email, setEmail] = useState("");
  const [otp, setOtp] = useState("");
  const [password, setPassword] = useState("");
  const [acceptedTerms, setAcceptedTerms] = useState(false);
  const [errors, setErrors] = useState<SignUpErrors>({});
  const [codeNoticeVisible, setCodeNoticeVisible] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const cooldown = useCodeCooldown();

  useEffect(() => {
    if (autoFocus) fullNameRef.current?.focus();
  }, [autoFocus]);

  function focusField(field: SignUpField) {
    const fieldRefs = {
      fullName: fullNameRef,
      email: emailRef,
      otp: otpRef,
      password: passwordRef,
      terms: termsRef,
    };
    fieldRefs[field].current?.focus();
  }

  function clearError(field: SignUpField) {
    setErrors((current) => {
      if (!current[field]) return current;
      const next = { ...current };
      delete next[field];
      return next;
    });
  }

  function handleGetCode() {
    if (!EMAIL_PATTERN.test(email.trim())) {
      setErrors((current) => ({ ...current, email: "errorEmail" }));
      focusField("email");
      return;
    }
    clearError("email");
    cooldown.start();
    setCodeNoticeVisible(true);
    focusField("otp");
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const nextErrors: SignUpErrors = {};
    if (!fullName.trim()) nextErrors.fullName = "errorFullName";
    if (!EMAIL_PATTERN.test(email.trim())) nextErrors.email = "errorEmail";
    if (!OTP_PATTERN.test(otp.trim())) nextErrors.otp = "errorOtp";
    if (password.length < MIN_PASSWORD_LENGTH) {
      nextErrors.password = "errorPassword";
    }
    if (!acceptedTerms) nextErrors.terms = "errorTerms";
    setErrors(nextErrors);

    const firstInvalid = SIGN_UP_FIELD_ORDER.find((field) => nextErrors[field]);
    if (firstInvalid) {
      setSubmitted(false);
      focusField(firstInvalid);
      return;
    }

    setPassword("");
    setOtp("");
    setSubmitted(true);
  }

  function errorFor(field: SignUpField) {
    const key = errors[field];
    return key ? (
      <p className="sign-in-form__error" id={`${ids[field]}-error`}>
        {c[key]}
      </p>
    ) : null;
  }

  function describedBy(field: SignUpField, extra?: string) {
    const parts = [errors[field] ? `${ids[field]}-error` : "", extra ?? ""];
    const value = parts.filter(Boolean).join(" ");
    return value || undefined;
  }

  const codeButtonLabel =
    cooldown.remaining > 0
      ? formatCopy(c.codeCountdown, { seconds: cooldown.remaining })
      : c.getCode;

  return (
    <div className="sign-in-form">
      <form
        aria-labelledby={ids.heading}
        className="sign-in-form__stack sign-in-form__stack--signup"
        noValidate
        onSubmit={handleSubmit}
      >
        <h2 className="sign-in-form__heading" id={ids.heading}>
          {c.signUpHeading}
        </h2>

        <div className="sign-in-form__field">
          <input
            aria-describedby={describedBy("fullName")}
            aria-invalid={errors.fullName ? true : undefined}
            aria-label={c.fullNameLabel}
            autoComplete="name"
            className="sign-in-input"
            id={ids.fullName}
            name="full-name"
            onChange={(event) => {
              setFullName(event.target.value);
              clearError("fullName");
            }}
            placeholder={c.fullNameLabel}
            ref={fullNameRef}
            type="text"
            value={fullName}
          />
          {errorFor("fullName")}
        </div>

        <div className="sign-in-form__field">
          <div className="sign-in-form__code-row">
            <input
              aria-describedby={describedBy(
                "email",
                codeNoticeVisible ? ids.codeNotice : undefined,
              )}
              aria-invalid={errors.email ? true : undefined}
              aria-label={c.emailLabel}
              autoComplete="email"
              className="sign-in-input"
              id={ids.email}
              inputMode="email"
              name="email"
              onChange={(event) => {
                setEmail(event.target.value);
                clearError("email");
              }}
              placeholder={c.emailLabel}
              ref={emailRef}
              spellCheck={false}
              type="email"
              value={email}
            />
            <button
              className="sign-in-button sign-in-button--code"
              disabled={cooldown.remaining > 0}
              onClick={handleGetCode}
              type="button"
            >
              {codeButtonLabel}
            </button>
          </div>
          {errorFor("email")}
          <div aria-live="polite">
            {codeNoticeVisible ? (
              <p className="sign-in-form__hint" id={ids.codeNotice}>
                {c.codeNotice}
              </p>
            ) : null}
          </div>
        </div>

        <div className="sign-in-form__field">
          <input
            aria-describedby={describedBy("otp")}
            aria-invalid={errors.otp ? true : undefined}
            aria-label={c.otpLabel}
            autoComplete="one-time-code"
            className="sign-in-input sign-in-input--otp"
            id={ids.otp}
            inputMode="numeric"
            maxLength={6}
            name="otp"
            onChange={(event) => {
              setOtp(event.target.value);
              clearError("otp");
            }}
            placeholder={c.otpPlaceholder}
            ref={otpRef}
            type="text"
            value={otp}
          />
          {errorFor("otp")}
        </div>

        <div className="sign-in-form__field">
          <input
            aria-describedby={describedBy("password")}
            aria-invalid={errors.password ? true : undefined}
            aria-label={c.passwordLabel}
            autoComplete="new-password"
            className="sign-in-input"
            id={ids.password}
            name="new-password"
            onChange={(event) => {
              setPassword(event.target.value);
              clearError("password");
            }}
            placeholder={c.signUpPasswordPlaceholder}
            ref={passwordRef}
            type="password"
            value={password}
          />
          {errorFor("password")}
        </div>

        <div className="sign-in-form__field">
          <label className="sign-in-check sign-in-check--terms">
            <input
              aria-describedby={describedBy("terms")}
              aria-invalid={errors.terms ? true : undefined}
              checked={acceptedTerms}
              id={ids.terms}
              name="terms"
              onChange={(event) => {
                setAcceptedTerms(event.target.checked);
                clearError("terms");
              }}
              ref={termsRef}
              type="checkbox"
            />
            <span>
              {c.termsLead}
              <strong className="sign-in-check__accent">
                {c.userAgreement}
              </strong>
              {c.termsJoin}
              <strong className="sign-in-check__accent">
                {c.privacyPolicy}
              </strong>
            </span>
          </label>
          {errorFor("terms")}
        </div>

        <button
          className="sign-in-button sign-in-button--primary"
          type="submit"
        >
          {c.createAccount}
        </button>
      </form>

      <div aria-live="polite" className="sign-in-form__notice-region">
        {submitted ? (
          <SecureSignInNotice
            description={c.signUpNotice}
            onSignIn={onSignIn}
            title={c.signUpNoticeTitle}
          />
        ) : null}
      </div>
    </div>
  );
}

type RecoverField = "email" | "otp" | "newPassword";
type RecoverErrors = Partial<Record<RecoverField, CopyKey>>;
const RECOVER_FIELD_ORDER: readonly RecoverField[] = [
  "email",
  "otp",
  "newPassword",
];

function RecoverPasswordForm({ onSignIn }: { onSignIn: () => void }) {
  const c = useSignInCopy();
  const ids = {
    email: useId(),
    otp: useId(),
    newPassword: useId(),
    codeNotice: useId(),
  };
  const emailRef = useRef<HTMLInputElement>(null);
  const otpRef = useRef<HTMLInputElement>(null);
  const newPasswordRef = useRef<HTMLInputElement>(null);
  const [email, setEmail] = useState("");
  const [otp, setOtp] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [errors, setErrors] = useState<RecoverErrors>({});
  const [codeNoticeVisible, setCodeNoticeVisible] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const cooldown = useCodeCooldown();

  function focusField(field: RecoverField) {
    const fieldRefs = {
      email: emailRef,
      otp: otpRef,
      newPassword: newPasswordRef,
    };
    fieldRefs[field].current?.focus();
  }

  function clearError(field: RecoverField) {
    setErrors((current) => {
      if (!current[field]) return current;
      const next = { ...current };
      delete next[field];
      return next;
    });
  }

  function handleGetCode() {
    if (!EMAIL_PATTERN.test(email.trim())) {
      setErrors((current) => ({ ...current, email: "errorEmail" }));
      focusField("email");
      return;
    }
    clearError("email");
    cooldown.start();
    setCodeNoticeVisible(true);
    focusField("otp");
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const nextErrors: RecoverErrors = {};
    if (!EMAIL_PATTERN.test(email.trim())) nextErrors.email = "errorEmail";
    if (!OTP_PATTERN.test(otp.trim())) nextErrors.otp = "errorOtp";
    if (newPassword.length < MIN_PASSWORD_LENGTH) {
      nextErrors.newPassword = "errorPassword";
    }
    setErrors(nextErrors);

    const firstInvalid = RECOVER_FIELD_ORDER.find((field) => nextErrors[field]);
    if (firstInvalid) {
      setSubmitted(false);
      focusField(firstInvalid);
      return;
    }

    setNewPassword("");
    setOtp("");
    setSubmitted(true);
  }

  function errorFor(field: RecoverField) {
    const key = errors[field];
    return key ? (
      <p className="sign-in-form__error" id={`${ids[field]}-error`}>
        {c[key]}
      </p>
    ) : null;
  }

  function describedBy(field: RecoverField, extra?: string) {
    const parts = [errors[field] ? `${ids[field]}-error` : "", extra ?? ""];
    const value = parts.filter(Boolean).join(" ");
    return value || undefined;
  }

  const codeButtonLabel =
    cooldown.remaining > 0
      ? formatCopy(c.codeCountdown, { seconds: cooldown.remaining })
      : cooldown.hasRequested
        ? c.resendCode
        : c.getCode;

  return (
    <>
      <DialogTitle className="sign-in-recover__title">
        {c.recoverTitle}
      </DialogTitle>
      <DialogDescription className="sign-in-recover__subtitle">
        {c.recoverSubtitle}
      </DialogDescription>

      <form
        className="sign-in-recover__form"
        noValidate
        onSubmit={handleSubmit}
      >
        <div className="sign-in-form__field">
          <label className="sign-in-recover__label" htmlFor={ids.email}>
            {c.emailLabel}
          </label>
          <input
            aria-describedby={describedBy(
              "email",
              codeNoticeVisible ? ids.codeNotice : undefined,
            )}
            aria-invalid={errors.email ? true : undefined}
            autoComplete="email"
            className="sign-in-input sign-in-input--dialog"
            id={ids.email}
            inputMode="email"
            name="recovery-email"
            onChange={(event) => {
              setEmail(event.target.value);
              clearError("email");
            }}
            placeholder={c.recoverEmailPlaceholder}
            ref={emailRef}
            spellCheck={false}
            type="email"
            value={email}
          />
          {errorFor("email")}
        </div>

        <div className="sign-in-form__field">
          <label className="sign-in-recover__label" htmlFor={ids.otp}>
            {c.recoverOtpLabel}
          </label>
          <div className="sign-in-recover__code-row">
            <input
              aria-describedby={describedBy("otp")}
              aria-invalid={errors.otp ? true : undefined}
              autoComplete="one-time-code"
              className="sign-in-input sign-in-input--dialog sign-in-input--otp"
              id={ids.otp}
              inputMode="numeric"
              maxLength={6}
              name="recovery-otp"
              onChange={(event) => {
                setOtp(event.target.value);
                clearError("otp");
              }}
              placeholder={c.recoverOtpPlaceholder}
              ref={otpRef}
              type="text"
              value={otp}
            />
            <button
              className="sign-in-button sign-in-button--dialog-code"
              disabled={cooldown.remaining > 0}
              onClick={handleGetCode}
              type="button"
            >
              {codeButtonLabel}
            </button>
          </div>
          {errorFor("otp")}
          <div aria-live="polite">
            {codeNoticeVisible ? (
              <p className="sign-in-form__hint" id={ids.codeNotice}>
                {c.codeNotice}
              </p>
            ) : null}
          </div>
        </div>

        <div className="sign-in-form__field">
          <label className="sign-in-recover__label" htmlFor={ids.newPassword}>
            {c.newPasswordLabel}
          </label>
          <input
            aria-describedby={describedBy("newPassword")}
            aria-invalid={errors.newPassword ? true : undefined}
            autoComplete="new-password"
            className="sign-in-input sign-in-input--dialog"
            id={ids.newPassword}
            name="recovery-new-password"
            onChange={(event) => {
              setNewPassword(event.target.value);
              clearError("newPassword");
            }}
            placeholder={c.newPasswordPlaceholder}
            ref={newPasswordRef}
            type="password"
            value={newPassword}
          />
          {errorFor("newPassword")}
        </div>

        <button
          className="sign-in-button sign-in-button--primary sign-in-recover__submit"
          type="submit"
        >
          {c.resetPassword}
        </button>
      </form>

      <div aria-live="polite" className="sign-in-form__notice-region">
        {submitted ? (
          <SecureSignInNotice
            description={c.recoverNotice}
            onSignIn={onSignIn}
            title={c.recoverNoticeTitle}
          />
        ) : null}
      </div>

      <DialogClose asChild>
        <button className="sign-in-recover__back" type="button">
          {c.getBack}
        </button>
      </DialogClose>
    </>
  );
}

interface SecureSignInNoticeProps {
  description: string;
  onSignIn: () => void;
  title: string;
}

function SecureSignInNotice({
  description,
  onSignIn,
  title,
}: SecureSignInNoticeProps) {
  const c = useSignInCopy();
  return (
    <div className="sign-in-notice">
      <p className="sign-in-notice__title">{title}</p>
      <p className="sign-in-notice__description">{description}</p>
      <button
        className="sign-in-button sign-in-button--outline"
        onClick={onSignIn}
        type="button"
      >
        {c.continueSecure}
      </button>
    </div>
  );
}
