import {
  APIRequestError,
  startMFAPasskey,
  verifyMFA,
  type MFAVerifyRequest,
  type PasskeyAssertion,
  type PasskeyRequestOptions,
} from "@tutorhub/api-client";
import { DialogDescription, DialogTitle } from "@tutorhub/ui";
import { Fingerprint, LifeBuoy, Smartphone } from "lucide-react";
import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { useI18n, type Language } from "../app/i18n";
import {
  DEFAULT_MFA_EXPIRES_IN_SECONDS,
  normalizeRecoveryCode,
  usableMfaMethods,
  type MFAMethod,
} from "../app/mfa";
import {
  classifyCeremonyError,
  getPasskeyAssertion,
  isWebAuthnSupported,
} from "../app/passkeys";
import { getApiBaseUrl } from "./SignInPage.shared";

const MAX_MFA_EXPIRES_IN_SECONDS = 300;
/** Upper bound for client-side locks; the server stays authoritative. */
const MAX_LOCK_SECONDS = 3600;
const TOTP_PATTERN = /^\d{6}$/;
/** AUTH-13A: the school turned `enforce_sso` on while this step was pending. */
const SSO_REQUIRED_CODE = "auth_sso_required";

const en = {
  title: "Two-step verification",
  recoveryTitle: "Verify it's you",
  description:
    "Your account uses two-step verification. Confirm it's you with one of the methods below to finish signing in.",
  recoveryDescription:
    "Your account uses two-step verification. Confirm it's you before the new password is saved.",
  expiresIn: "This verification expires in {time}.",
  methodGroup: "Verification method",
  methodTotp: "Authenticator app",
  methodPasskey: "Passkey",
  methodRecovery: "Recovery code",
  totpLabel: "6-digit code from your authenticator app",
  totpHint:
    "Open Google Authenticator, Microsoft Authenticator, Apple Passwords or your usual app and enter the current TutorHub code.",
  totpRequired: "Enter the 6-digit code.",
  totpFormat: "The code has exactly 6 digits.",
  totpInvalid:
    "That code is wrong or was already used. Wait for the next code and try again.",
  recoveryLabel: "Enter a recovery code",
  recoveryHint:
    "Format XXXX-XXXX-XXXX-XXXX. Each recovery code works only once.",
  recoveryRequired: "Enter one of your recovery codes.",
  recoveryFormat:
    "A recovery code has 16 letters and digits, for example ABCD-EFGH-JKMN-PQRS.",
  recoveryInvalid: "That recovery code is wrong or was already used.",
  passkeyIntro:
    "Use your screen lock, fingerprint, face or security key to confirm it's you.",
  passkeyButton: "Verify with a passkey",
  passkeyWaiting: "Waiting for your passkey...",
  passkeyCancelled:
    "Passkey verification was cancelled. You can try again or choose another method.",
  passkeyInvalid:
    "That passkey could not be verified. Try again or choose another method.",
  passkeyClone:
    "This passkey was refused because it may have been copied. Choose another method.",
  passkeyUnsupported:
    "This browser cannot use passkeys. Choose another method.",
  passkeyFailed: "The passkey could not be used right now. Try again.",
  verify: "Verify",
  verifying: "Verifying...",
  submitWait: "{label} ({seconds}s)",
  back: "Back to sign in",
  backRecovery: "Back to password reset",
  rateLimited: "Too many attempts. Try again in {seconds} seconds.",
  rateLimitedNoWait: "Too many attempts. Wait a moment and try again.",
  busy: "TutorHub is busy. Try again in {seconds} seconds.",
  busyNoWait: "TutorHub is busy right now. Try again shortly.",
  conflict: "Your verification changed meanwhile. Go back and start again.",
  network: "Cannot reach TutorHub. Check your connection and try again.",
  generic: "Verification could not be completed. Try again.",
  session:
    "You are verified, but your session could not be loaded yet. Try again.",
  retry: "Try again",
  noMethod:
    "This browser cannot use your account's second step. Use another browser or device, or go back.",
} as const;

type MfaCopy = Record<keyof typeof en, string>;

const vi: MfaCopy = {
  title: "Xác minh hai bước",
  recoveryTitle: "Xác minh đó là bạn",
  description:
    "Tài khoản của bạn đã bật xác minh hai bước. Hãy xác nhận bằng một trong các cách dưới đây để hoàn tất đăng nhập.",
  recoveryDescription:
    "Tài khoản của bạn đã bật xác minh hai bước. Hãy xác minh trước khi mật khẩu mới được lưu.",
  expiresIn: "Phiên xác minh hết hạn sau {time}.",
  methodGroup: "Cách xác minh",
  methodTotp: "Ứng dụng xác thực",
  methodPasskey: "Passkey",
  methodRecovery: "Mã khôi phục",
  totpLabel: "Mã 6 chữ số từ ứng dụng xác thực",
  totpHint:
    "Mở Google Authenticator, Microsoft Authenticator, Apple Passwords hoặc ứng dụng bạn đang dùng và nhập mã TutorHub đang hiển thị.",
  totpRequired: "Hãy nhập mã gồm 6 chữ số.",
  totpFormat: "Mã gồm đúng 6 chữ số.",
  totpInvalid:
    "Mã không đúng hoặc đã được dùng. Hãy chờ mã tiếp theo rồi thử lại.",
  recoveryLabel: "Nhập một mã khôi phục",
  recoveryHint:
    "Dạng XXXX-XXXX-XXXX-XXXX. Mỗi mã khôi phục chỉ dùng được một lần.",
  recoveryRequired: "Hãy nhập một mã khôi phục của bạn.",
  recoveryFormat: "Mã khôi phục gồm 16 chữ và số, ví dụ ABCD-EFGH-JKMN-PQRS.",
  recoveryInvalid: "Mã khôi phục không đúng hoặc đã được dùng.",
  passkeyIntro:
    "Dùng khóa màn hình, vân tay, khuôn mặt hoặc khóa bảo mật trên thiết bị để xác nhận.",
  passkeyButton: "Xác minh bằng passkey",
  passkeyWaiting: "Đang chờ passkey...",
  passkeyCancelled:
    "Đã hủy xác minh bằng passkey. Bạn có thể thử lại hoặc chọn cách khác.",
  passkeyInvalid:
    "Không thể xác minh passkey này. Hãy thử lại hoặc chọn cách khác.",
  passkeyClone:
    "Passkey này bị từ chối vì có dấu hiệu bị sao chép. Hãy chọn cách khác.",
  passkeyUnsupported:
    "Trình duyệt này không hỗ trợ passkey. Hãy chọn cách khác.",
  passkeyFailed: "Không thể dùng passkey lúc này. Hãy thử lại.",
  verify: "Xác minh",
  verifying: "Đang xác minh...",
  submitWait: "{label} ({seconds}s)",
  back: "Quay lại đăng nhập",
  backRecovery: "Quay lại đặt lại mật khẩu",
  rateLimited: "Bạn đã thử quá nhiều lần. Hãy thử lại sau {seconds} giây.",
  rateLimitedNoWait: "Bạn đã thử quá nhiều lần. Hãy chờ một lát rồi thử lại.",
  busy: "TutorHub đang bận. Hãy thử lại sau {seconds} giây.",
  busyNoWait: "TutorHub đang bận. Hãy thử lại sau ít phút.",
  conflict: "Trạng thái xác minh vừa thay đổi. Hãy quay lại và bắt đầu lại.",
  network: "Không kết nối được tới TutorHub. Hãy kiểm tra mạng rồi thử lại.",
  generic: "Chưa thể hoàn tất xác minh. Hãy thử lại.",
  session:
    "Bạn đã xác minh thành công nhưng chưa tải được phiên đăng nhập. Hãy thử lại.",
  retry: "Thử lại",
  noMethod:
    "Trình duyệt này không dùng được phương thức xác minh hai bước của tài khoản. Hãy dùng trình duyệt hoặc thiết bị khác, hoặc quay lại.",
};

const copyByLanguage: Record<Language, MfaCopy> = { en, vi };

function format(template: string, values: Record<string, string | number>) {
  return template.replace(/\{(\w+)\}/g, (match, key: string) =>
    key in values ? String(values[key]) : match,
  );
}

function boundedSeconds(value: number | undefined, max = MAX_LOCK_SECONDS) {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? Math.min(Math.ceil(value), max)
    : 0;
}

function clockText(totalSeconds: number) {
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

type MfaFailure =
  | { kind: "rateLimited"; seconds: number }
  | { kind: "busy"; seconds: number }
  | { kind: "conflict" }
  | { kind: "network" }
  | { kind: "session" }
  | { kind: "passkeyInvalid" }
  | { kind: "passkeyClone" }
  | { kind: "passkeyUnsupported" }
  | { kind: "passkeyFailed" }
  | { kind: "generic" };

type FieldErrorKey =
  | "totpRequired"
  | "totpFormat"
  | "totpInvalid"
  | "recoveryRequired"
  | "recoveryFormat"
  | "recoveryInvalid";

function failureText(c: MfaCopy, failure: MfaFailure) {
  switch (failure.kind) {
    case "rateLimited":
      return failure.seconds > 0
        ? format(c.rateLimited, { seconds: failure.seconds })
        : c.rateLimitedNoWait;
    case "busy":
      return failure.seconds > 0
        ? format(c.busy, { seconds: failure.seconds })
        : c.busyNoWait;
    case "conflict":
      return c.conflict;
    case "network":
      return c.network;
    case "session":
      return c.session;
    case "passkeyInvalid":
      return c.passkeyInvalid;
    case "passkeyClone":
      return c.passkeyClone;
    case "passkeyUnsupported":
      return c.passkeyUnsupported;
    case "passkeyFailed":
      return c.passkeyFailed;
    default:
      return c.generic;
  }
}

function useCountdown() {
  const [remaining, setRemaining] = useState(0);
  useEffect(() => {
    if (remaining <= 0) return;
    const timer = window.setTimeout(
      () => setRemaining((value) => Math.max(0, value - 1)),
      1000,
    );
    return () => window.clearTimeout(timer);
  }, [remaining]);
  const start = useCallback((seconds: number) => {
    setRemaining(boundedSeconds(seconds));
  }, []);
  return { remaining, start };
}

/** Seconds left until a deadline fixed at mount; `onExpire` fires once. */
function useDeadline(expiresInSeconds: number, onExpire: () => void) {
  const [deadline] = useState(
    () =>
      Date.now() +
      (boundedSeconds(expiresInSeconds, MAX_MFA_EXPIRES_IN_SECONDS) ||
        DEFAULT_MFA_EXPIRES_IN_SECONDS) *
        1000,
  );
  const [left, setLeft] = useState(() =>
    Math.max(0, Math.ceil((deadline - Date.now()) / 1000)),
  );
  const onExpireRef = useRef(onExpire);
  useEffect(() => {
    onExpireRef.current = onExpire;
  });
  useEffect(() => {
    if (left <= 0) {
      onExpireRef.current();
      return;
    }
    const timer = window.setTimeout(
      () => setLeft(Math.max(0, Math.ceil((deadline - Date.now()) / 1000))),
      1000,
    );
    return () => window.clearTimeout(timer);
  }, [deadline, left]);
  return left;
}

export interface SignInMfaStepProps {
  /** Seconds left in the server transaction (`GET /auth/mfa`), else 300. */
  expiresInSeconds?: number;
  methods: readonly MFAMethod[];
  /** Runs once the server issued the session (`authenticated`). */
  onAuthenticated: (options: { returnTo?: string }) => Promise<void>;
  /** Leaves the second step (the server transaction simply expires). */
  onCancel: () => void;
  /** The transaction is gone (404 `auth_mfa_unavailable`) or timed out. */
  onExpired: () => void;
  /**
   * Runs after a recovery's second step (`password_reset`): every session was
   * revoked and the user is NOT signed in.
   */
  onPasswordReset: () => Promise<void>;
  /**
   * AUTH-13B: 403 `auth_sso_required` — the account's school now requires
   * SSO, so this transaction can never finish. The caller leaves the step
   * and explains it; no timer or device prompt outlives the call.
   */
  onSsoRequired: () => void;
  /** `passkeys_enabled` from `/auth/providers`. */
  passkeysEnabled: boolean;
  purpose: "signIn" | "recovery";
  /** Inside a dialog the heading is the dialog's title. */
  variant?: "page" | "dialog";
}

/**
 * AUTH-09B second step after a first factor answered `mfa_required`. Codes
 * and assertions live only in this component's state and leave memory as
 * soon as the request settles; nothing reaches Web Storage or the URL.
 */
export function SignInMfaStep({
  expiresInSeconds = DEFAULT_MFA_EXPIRES_IN_SECONDS,
  methods,
  onAuthenticated,
  onCancel,
  onExpired,
  onPasswordReset,
  onSsoRequired,
  passkeysEnabled,
  purpose,
  variant = "page",
}: SignInMfaStepProps) {
  const { language } = useI18n();
  const c = copyByLanguage[language];
  const ids = {
    heading: useId(),
    description: useId(),
    group: useId(),
    field: useId(),
    hint: useId(),
    error: useId(),
  };
  const [webAuthn] = useState(isWebAuthnSupported);
  const available = usableMfaMethods(methods, passkeysEnabled && webAuthn);
  const [method, setMethod] = useState<MFAMethod | null>(
    () => available[0] ?? null,
  );
  const [value, setValue] = useState("");
  const [fieldError, setFieldError] = useState<FieldErrorKey | null>(null);
  const [failure, setFailure] = useState<MfaFailure | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [passkeyCancelled, setPasskeyCancelled] = useState(false);
  const [methodSwitched, setMethodSwitched] = useState(false);
  const lock = useCountdown();
  const headingRef = useRef<HTMLHeadingElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const passkeyButtonRef = useRef<HTMLButtonElement>(null);
  const ceremonyRef = useRef<AbortController | null>(null);
  /** Set once the step was handed back (expired or school SSO required). */
  const closedRef = useRef(false);
  /** Set once the server issued the session; a retry only reloads it. */
  const [verified, setVerified] = useState<{ returnTo?: string } | null>(null);

  const expire = useCallback(() => {
    if (closedRef.current) return;
    closedRef.current = true;
    ceremonyRef.current?.abort();
    onExpired();
  }, [onExpired]);

  /** AUTH-13B: the expiry timer must not fire after this hand-over. */
  const leaveForSchoolSSO = useCallback(() => {
    if (closedRef.current) return;
    closedRef.current = true;
    ceremonyRef.current?.abort();
    onSsoRequired();
  }, [onSsoRequired]);
  const secondsLeft = useDeadline(expiresInSeconds, () => {
    // A verified user keeps the session even if the clock runs out.
    if (!verified) expire();
  });

  // Moving from the first factor: announce the new step by focusing it.
  useEffect(() => {
    headingRef.current?.focus();
  }, []);

  useEffect(() => {
    if (!methodSwitched) return;
    if (method === "passkey") passkeyButtonRef.current?.focus();
    else inputRef.current?.focus();
  }, [method, methodSwitched]);

  // Leaving the step cancels a pending device prompt.
  useEffect(
    () => () => {
      ceremonyRef.current?.abort();
      ceremonyRef.current = null;
    },
    [],
  );

  function switchMethod(next: MFAMethod) {
    if (submitting || next === method) return;
    setMethod(next);
    setValue("");
    setFieldError(null);
    setFailure(null);
    setPasskeyCancelled(false);
    setMethodSwitched(true);
  }

  function showFieldError(key: FieldErrorKey) {
    setFieldError(key);
    inputRef.current?.focus();
  }

  async function finishSignIn(options: { returnTo?: string }) {
    setFailure(null);
    setSubmitting(true);
    try {
      await onAuthenticated(options);
    } catch {
      // The session cookie exists; only the CSRF + /me hand-off is retried.
      setFailure({ kind: "session" });
      setSubmitting(false);
    }
  }

  function applyError(error: unknown, submitted: MFAMethod) {
    if (!(error instanceof APIRequestError)) {
      setFailure({ kind: "network" });
      return;
    }
    const code = error.problem?.code;
    switch (error.status) {
      case 400:
        if (submitted === "totp") showFieldError("totpFormat");
        else if (submitted === "recovery_code")
          showFieldError("recoveryFormat");
        else setFailure({ kind: "generic" });
        return;
      case 401:
        if (submitted === "totp") showFieldError("totpInvalid");
        else if (submitted === "recovery_code") {
          showFieldError("recoveryInvalid");
        } else setFailure({ kind: "passkeyInvalid" });
        return;
      case 403:
        if (code === SSO_REQUIRED_CODE) {
          leaveForSchoolSSO();
          return;
        }
        setFailure(
          code === "auth_passkey_clone_detected"
            ? { kind: "passkeyClone" }
            : { kind: "generic" },
        );
        return;
      case 404:
        // auth_mfa_unavailable: the transaction expired or was used.
        expire();
        return;
      case 409:
        setFailure({ kind: "conflict" });
        return;
      case 429: {
        const seconds = boundedSeconds(error.retryAfterSeconds);
        lock.start(seconds);
        setFailure({ kind: "rateLimited", seconds });
        return;
      }
      case 503: {
        const seconds = boundedSeconds(error.retryAfterSeconds);
        lock.start(seconds);
        setFailure({ kind: "busy", seconds });
        return;
      }
      default:
        setFailure({ kind: "generic" });
    }
  }

  async function submit(input: MFAVerifyRequest) {
    setFailure(null);
    setFieldError(null);
    setSubmitting(true);
    let result;
    try {
      result = await verifyMFA(input, { baseUrl: getApiBaseUrl() });
    } catch (error) {
      setValue("");
      applyError(error, input.method);
      setSubmitting(false);
      return;
    }
    setValue("");
    if (result.status === "password_reset") {
      // Every session was revoked; the user signs in with the new password.
      await onPasswordReset();
      return;
    }
    const options = result.return_to ? { returnTo: result.return_to } : {};
    setVerified(options);
    await finishSignIn(options);
  }

  function handleCodeSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting || lock.remaining > 0 || !method) return;
    if (method === "totp") {
      const code = value.replace(/\s+/g, "");
      if (TOTP_PATTERN.test(code)) {
        void submit({ code, method: "totp" });
      } else {
        showFieldError(code ? "totpFormat" : "totpRequired");
      }
      return;
    }
    const canonical = normalizeRecoveryCode(value);
    if (canonical) {
      void submit({ code: canonical, method: "recovery_code" });
    } else {
      showFieldError(value.trim() ? "recoveryFormat" : "recoveryRequired");
    }
  }

  /** Options bound to the `auth_tx` cookie → device prompt → verify. */
  async function handlePasskey() {
    if (submitting || lock.remaining > 0) return;
    setFailure(null);
    setPasskeyCancelled(false);
    setSubmitting(true);
    const controller = new AbortController();
    ceremonyRef.current = controller;
    try {
      let options: PasskeyRequestOptions;
      try {
        options = await startMFAPasskey({
          baseUrl: getApiBaseUrl(),
          signal: controller.signal,
        });
      } catch (error) {
        if (controller.signal.aborted) return;
        applyError(error, "passkey");
        setSubmitting(false);
        return;
      }
      let assertion: PasskeyAssertion;
      try {
        assertion = await getPasskeyAssertion(options, {
          signal: controller.signal,
        });
      } catch (error) {
        if (controller.signal.aborted) return;
        const kind = classifyCeremonyError(error);
        if (kind === "cancelled") {
          // A quiet notice, never an error banner.
          setPasskeyCancelled(true);
        } else {
          setFailure({
            kind:
              kind === "unsupported" ? "passkeyUnsupported" : "passkeyFailed",
          });
        }
        setSubmitting(false);
        passkeyButtonRef.current?.focus();
        return;
      }
      await submit({ assertion, method: "passkey" });
    } finally {
      if (ceremonyRef.current === controller) ceremonyRef.current = null;
    }
  }

  const title = purpose === "recovery" ? c.recoveryTitle : c.title;
  const description =
    purpose === "recovery" ? c.recoveryDescription : c.description;
  const verifyLabel =
    lock.remaining > 0
      ? format(c.submitWait, { label: c.verify, seconds: lock.remaining })
      : c.verify;
  const methodLabels: Record<MFAMethod, string> = {
    passkey: c.methodPasskey,
    recovery_code: c.methodRecovery,
    totp: c.methodTotp,
  };
  const methodIcons: Record<MFAMethod, typeof Smartphone> = {
    passkey: Fingerprint,
    recovery_code: LifeBuoy,
    totp: Smartphone,
  };

  const body = (
    <>
      {variant === "dialog" ? (
        // Radix names the dialog after its own title and description ids.
        <>
          <DialogTitle
            className="sign-in-recover__title"
            ref={headingRef}
            tabIndex={-1}
          >
            {title}
          </DialogTitle>
          <DialogDescription className="sign-in-recover__subtitle">
            {description}
          </DialogDescription>
        </>
      ) : (
        <>
          <h2
            className="sign-in-mfa__title"
            id={ids.heading}
            ref={headingRef}
            tabIndex={-1}
          >
            {title}
          </h2>
          <p className="sign-in-mfa__description" id={ids.description}>
            {description}
          </p>
        </>
      )}

      {/* Static text: never a live region, so the clock is not re-read. */}
      <p className="sign-in-mfa__expiry">
        {format(c.expiresIn, { time: clockText(secondsLeft) })}
      </p>

      {available.length > 1 ? (
        <div
          aria-labelledby={ids.group}
          className="sign-in-mfa__methods"
          role="group"
        >
          <span className="sign-in-mfa__methods-label" id={ids.group}>
            {c.methodGroup}
          </span>
          <div className="sign-in-mfa__methods-options">
            {available.map((option) => {
              const Icon = methodIcons[option];
              return (
                <button
                  aria-pressed={method === option}
                  className="sign-in-mfa__method"
                  disabled={submitting}
                  key={option}
                  onClick={() => switchMethod(option)}
                  type="button"
                >
                  <Icon aria-hidden="true" className="sign-in-mfa__icon" />
                  <span>{methodLabels[option]}</span>
                </button>
              );
            })}
          </div>
        </div>
      ) : null}

      {method === null ? (
        <p className="sign-in-form__alert" role="alert">
          {c.noMethod}
        </p>
      ) : method === "passkey" ? (
        <div className="sign-in-mfa__form">
          <p className="sign-in-mfa__intro">{c.passkeyIntro}</p>
          <button
            aria-disabled={submitting || undefined}
            className="sign-in-button sign-in-button--primary"
            disabled={lock.remaining > 0}
            onClick={() => void handlePasskey()}
            ref={passkeyButtonRef}
            type="button"
          >
            <Fingerprint aria-hidden="true" className="sign-in-mfa__icon" />
            <span>
              {submitting
                ? c.passkeyWaiting
                : lock.remaining > 0
                  ? format(c.submitWait, {
                      label: c.passkeyButton,
                      seconds: lock.remaining,
                    })
                  : c.passkeyButton}
            </span>
          </button>
        </div>
      ) : (
        <form
          aria-label={methodLabels[method]}
          className="sign-in-mfa__form"
          noValidate
          onSubmit={handleCodeSubmit}
        >
          <div className="sign-in-form__field">
            <label className="sign-in-mfa__label" htmlFor={ids.field}>
              {method === "totp" ? c.totpLabel : c.recoveryLabel}
            </label>
            <input
              aria-describedby={
                fieldError ? `${ids.error} ${ids.hint}` : ids.hint
              }
              aria-invalid={fieldError ? true : undefined}
              autoCapitalize={method === "totp" ? undefined : "characters"}
              autoComplete={method === "totp" ? "one-time-code" : "off"}
              className={
                method === "totp"
                  ? "sign-in-input sign-in-input--otp"
                  : "sign-in-input sign-in-input--recovery"
              }
              id={ids.field}
              inputMode={method === "totp" ? "numeric" : "text"}
              key={method}
              maxLength={method === "totp" ? 6 : 24}
              name={method === "totp" ? "mfa-totp" : "mfa-recovery-code"}
              onChange={(event) => {
                setValue(event.target.value);
                if (fieldError) setFieldError(null);
              }}
              placeholder={method === "totp" ? "000000" : "XXXX-XXXX-XXXX-XXXX"}
              ref={inputRef}
              spellCheck={false}
              type="text"
              value={value}
            />
            {fieldError ? (
              <p className="sign-in-form__error" id={ids.error}>
                {c[fieldError]}
              </p>
            ) : null}
            <p className="sign-in-mfa__hint" id={ids.hint}>
              {method === "totp" ? c.totpHint : c.recoveryHint}
            </p>
          </div>
          <button
            aria-disabled={submitting || undefined}
            className="sign-in-button sign-in-button--primary"
            disabled={lock.remaining > 0}
            type="submit"
          >
            {submitting ? c.verifying : verifyLabel}
          </button>
        </form>
      )}

      {/* Always mounted so assistive tech observes the cancel notice. */}
      <p aria-live="polite" className="sign-in-mfa__notice" role="status">
        {passkeyCancelled ? c.passkeyCancelled : null}
      </p>

      {failure ? (
        <div className="sign-in-form__alert" role="alert">
          <p>{failureText(c, failure)}</p>
          {failure.kind === "session" && verified ? (
            <button
              className="sign-in-button sign-in-button--outline"
              onClick={() => void finishSignIn(verified)}
              type="button"
            >
              {c.retry}
            </button>
          ) : null}
        </div>
      ) : null}

      <button
        className="sign-in-mfa__back"
        disabled={submitting}
        onClick={onCancel}
        type="button"
      >
        {purpose === "recovery" ? c.backRecovery : c.back}
      </button>
    </>
  );

  return variant === "dialog" ? (
    <div className="sign-in-mfa sign-in-mfa--dialog">{body}</div>
  ) : (
    <section
      aria-describedby={ids.description}
      aria-labelledby={ids.heading}
      className="sign-in-mfa sign-in-mfa--page"
    >
      {body}
    </section>
  );
}
