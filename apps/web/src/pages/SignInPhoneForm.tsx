import {
  APIRequestError,
  resendAuthChallenge,
  startPhoneLogin,
  verifyPhoneLogin,
  type AuthSignupChallengeResponse,
  type NativeAuthenticationResponse,
} from "@tutorhub/api-client";
import {
  useEffect,
  useId,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
  type RefObject,
} from "react";
import { useI18n, type Language } from "../app/i18n";
import type { MFAMethod } from "../app/mfa";
import {
  DEFAULT_PHONE_CODE_TTL_SECONDS,
  DEFAULT_PHONE_RESEND_SECONDS,
  PHONE_CODE_LENGTH,
  PHONE_INPUT_MAX_LENGTH,
  boundedPhoneSeconds,
  compactPhoneNumber,
  formatPhoneClock,
  isCompletePhoneCode,
  isPlausiblePhoneNumber,
  phoneCountryLabel,
  phoneCountryOptions,
  sanitizePhoneCode,
  usePhoneCountdown,
} from "../app/phone";
import { getApiBaseUrl } from "./SignInPage.shared";

const en = {
  heading: "Log in with a code sent to your phone",
  countryLabel: "Country code",
  phoneLabel: "Phone number",
  phonePlaceholder: "e.g. 0912 345 678",
  phoneHint:
    "Enter your mobile number. Your account keeps its email address for sign-in and notices.",
  sendCode: "Send code",
  sending: "Sending…",
  waitLabel: "{label} ({seconds}s)",
  codeSent:
    "If this number can receive sign-in codes, a 6-digit code is on its way to {destination}.",
  codeResent: "A new 6-digit code is on its way to {destination}.",
  codeExpiresIn: "The code expires in {time}.",
  codeExpired: "This code has expired. Select Resend code to get a new one.",
  codeLabel: "6-digit code",
  codePlaceholder: "______",
  verify: "Verify and log in",
  verifying: "Verifying…",
  resend: "Resend code",
  changeNumber: "Change phone number",
  errorPhoneRequired: "Enter your phone number.",
  errorPhoneInvalid:
    "Enter a valid mobile number for a supported country. Landlines and hotline numbers can't receive sign-in codes.",
  errorCodeRequired: "Enter the 6-digit code.",
  errorCodeFormat: "The code has exactly 6 digits.",
  codeInvalid:
    "That verification code is incorrect or has expired. Check it or select Resend code.",
  phoneStale:
    "This phone number has to be verified again before it can sign you in. Log in with your email, password, a passkey or a linked provider, then verify the number again in Settings.",
  secondFactorUnavailable:
    "This account needs an extra verification step that isn't available right now. Log in with your email, password, a passkey or a linked provider instead.",
  deliveryUnavailable:
    "Phone codes are temporarily unavailable. Log in with your email, a passkey or Google, Microsoft or Facebook instead.",
  rateLimited: "Too many attempts. Please wait a moment and try again.",
  rateLimitedWait: "Too many attempts. Please try again in {seconds} seconds.",
  busy: "TutorHub is busy right now. Please try again shortly.",
  busyWait:
    "TutorHub is busy right now. Please try again in {seconds} seconds.",
  network: "Couldn't reach TutorHub. Check your connection and try again.",
  generic: "Something went wrong. Please try again.",
  session: "You're signed in, but we couldn't load your account yet.",
  retry: "Retry",
} as const;

type PhoneCopy = Record<keyof typeof en, string>;
type PhoneCopyKey = keyof PhoneCopy;

const vi: PhoneCopy = {
  heading: "Đăng nhập bằng mã gửi tới số điện thoại",
  countryLabel: "Mã quốc gia",
  phoneLabel: "Số điện thoại",
  phonePlaceholder: "Ví dụ: 0912 345 678",
  phoneHint:
    "Nhập số di động của bạn. Tài khoản vẫn giữ địa chỉ email chính để đăng nhập và nhận thông báo.",
  sendCode: "Gửi mã",
  sending: "Đang gửi…",
  waitLabel: "{label} ({seconds}s)",
  codeSent:
    "Nếu số này nhận được mã đăng nhập, mã gồm 6 chữ số đang được gửi tới {destination}.",
  codeResent: "Mã mới gồm 6 chữ số đang được gửi tới {destination}.",
  codeExpiresIn: "Mã hết hạn sau {time}.",
  codeExpired: "Mã đã hết hạn. Hãy chọn Gửi lại mã để nhận mã mới.",
  codeLabel: "Mã gồm 6 chữ số",
  codePlaceholder: "______",
  verify: "Xác minh và đăng nhập",
  verifying: "Đang xác minh…",
  resend: "Gửi lại mã",
  changeNumber: "Đổi số điện thoại",
  errorPhoneRequired: "Hãy nhập số điện thoại.",
  errorPhoneInvalid:
    "Hãy nhập số di động hợp lệ của quốc gia được hỗ trợ. Số cố định và số tổng đài không nhận được mã đăng nhập.",
  errorCodeRequired: "Hãy nhập mã gồm 6 chữ số.",
  errorCodeFormat: "Mã gồm đúng 6 chữ số.",
  codeInvalid:
    "Mã xác minh không đúng hoặc đã hết hạn. Hãy kiểm tra lại hoặc chọn Gửi lại mã.",
  phoneStale:
    "Số điện thoại này cần được xác minh lại định kỳ trước khi dùng để đăng nhập. Hãy đăng nhập bằng email, mật khẩu, passkey hoặc nhà cung cấp đã liên kết, rồi vào Cài đặt để xác minh lại số.",
  secondFactorUnavailable:
    "Tài khoản này yêu cầu xác minh bổ sung nhưng phương thức thứ hai hiện chưa khả dụng. Hãy đăng nhập trực tiếp bằng email, mật khẩu, passkey hoặc nhà cung cấp đã liên kết.",
  deliveryUnavailable:
    "Kênh gửi mã qua điện thoại đang tạm gián đoạn. Hãy đăng nhập bằng email, passkey hoặc Google, Microsoft, Facebook.",
  rateLimited: "Bạn thao tác quá nhiều lần. Hãy chờ một lát rồi thử lại.",
  rateLimitedWait:
    "Bạn thao tác quá nhiều lần. Hãy thử lại sau {seconds} giây.",
  busy: "TutorHub đang bận. Hãy thử lại sau ít phút.",
  busyWait: "TutorHub đang bận. Hãy thử lại sau {seconds} giây.",
  network: "Không kết nối được tới TutorHub. Hãy kiểm tra mạng rồi thử lại.",
  generic: "Đã có lỗi xảy ra. Hãy thử lại.",
  session:
    "Bạn đã đăng nhập nhưng chưa tải được thông tin tài khoản. Hãy thử lại.",
  retry: "Thử lại",
};

const copyByLanguage: Record<Language, PhoneCopy> = { en, vi };

function format(template: string, values: Record<string, string | number>) {
  return template.replace(/\{(\w+)\}/g, (match, name: string) => {
    const value = values[name];
    return value === undefined ? match : String(value);
  });
}

/** AUTH-10A phone problem contract, reduced to what the form can say. */
type PhoneSignInFailure =
  | { kind: "invalidNumber" }
  | { kind: "codeInvalid" }
  | { kind: "stale" }
  | { kind: "secondFactor" }
  | { kind: "delivery" }
  /** AUTH-13A: the account's school requires SSO (handed to the caller). */
  | { kind: "ssoRequired" }
  | { kind: "rateLimited"; seconds: number }
  | { kind: "busy"; seconds: number }
  | { kind: "network" }
  | { kind: "session" }
  | { kind: "generic" };

function phoneFailureFrom(error: unknown): PhoneSignInFailure {
  if (!(error instanceof APIRequestError)) return { kind: "network" };
  const code = error.problem?.code;
  switch (error.status) {
    case 400:
      return code === "auth_invalid_phone_number"
        ? { kind: "invalidNumber" }
        : { kind: "codeInvalid" };
    case 401:
      return { kind: "codeInvalid" };
    case 403:
      if (code === "auth_sso_required") return { kind: "ssoRequired" };
      if (code === "auth_phone_stale") return { kind: "stale" };
      if (code === "auth_phone_second_factor_required") {
        return { kind: "secondFactor" };
      }
      return { kind: "generic" };
    case 404:
      // The deployment turned phone sign-in off meanwhile.
      return { kind: "delivery" };
    case 429:
      return {
        kind: "rateLimited",
        seconds: boundedPhoneSeconds(error.retryAfterSeconds),
      };
    case 503:
      return code === "auth_phone_delivery_unavailable"
        ? { kind: "delivery" }
        : {
            kind: "busy",
            seconds: boundedPhoneSeconds(error.retryAfterSeconds),
          };
    default:
      return { kind: "generic" };
  }
}

function failureText(c: PhoneCopy, failure: PhoneSignInFailure) {
  switch (failure.kind) {
    case "invalidNumber":
      return c.errorPhoneInvalid;
    case "codeInvalid":
      return c.codeInvalid;
    case "stale":
      return c.phoneStale;
    case "secondFactor":
      return c.secondFactorUnavailable;
    case "delivery":
      return c.deliveryUnavailable;
    case "rateLimited":
      return failure.seconds > 0
        ? format(c.rateLimitedWait, { seconds: failure.seconds })
        : c.rateLimited;
    case "busy":
      return failure.seconds > 0
        ? format(c.busyWait, { seconds: failure.seconds })
        : c.busy;
    case "network":
      return c.network;
    case "session":
      return c.session;
    default:
      return c.generic;
  }
}

/** Guidance (another sign-in method) rather than a plain error. */
function isGuidance(failure: PhoneSignInFailure) {
  return (
    failure.kind === "stale" ||
    failure.kind === "secondFactor" ||
    failure.kind === "delivery"
  );
}

interface PhoneChallenge {
  country: string;
  destination: string;
  id: string;
  /** Compacted number, kept in memory only for "Resend code". */
  number: string;
}

export interface SignInPhoneFormProps {
  /** `phone_allowed_countries`; the first entry is the default. */
  allowedCountries: readonly string[];
  autoFocus: boolean;
  /** Cancels background passkey autofill before any phone request. */
  beforeRequest: () => Promise<void>;
  keepSignedIn: boolean;
  /** Optional status shown above the fields (e.g. an expired second step). */
  notice: ReactNode;
  /** Runs after the server issued a session: CSRF, /me, navigation. */
  onAuthenticated: () => Promise<void>;
  /** The code was right but the account needs its second factor. */
  onMfaRequired: (methods: readonly MFAMethod[]) => Promise<void>;
  /**
   * AUTH-13B: 403 `auth_sso_required` — the code proved the number, but the
   * account's school requires SSO and the server consumed the transaction
   * (and its `auth_tx` cookie). The form has already dropped the code; the
   * caller leaves for the school SSO entry and explains why.
   */
  onSchoolSSORequired: () => void;
  /** Keep-signed-in checkbox shared with the other first factors. */
  options: ReactNode;
  /**
   * The server binds one pending auth transaction to one browser cookie
   * (shared with the email-code and recovery flows): a replaced transaction
   * starts again instead of resending another flow's code.
   */
  transactionOwnerRef: RefObject<symbol | null>;
}

type FieldErrors = Partial<Record<"phone" | "code", PhoneCopyKey>>;

/**
 * AUTH-10B: phone number → 6-digit SMS code → session (or the second step).
 * Every notice stays account-neutral; the number and code live only in this
 * component's state and the code is dropped as soon as it is submitted.
 */
export function SignInPhoneForm({
  allowedCountries,
  autoFocus,
  beforeRequest,
  keepSignedIn,
  notice,
  onAuthenticated,
  onMfaRequired,
  onSchoolSSORequired,
  options,
  transactionOwnerRef,
}: SignInPhoneFormProps) {
  const { language } = useI18n();
  const c = copyByLanguage[language];
  const ids = {
    heading: useId(),
    phone: useId(),
    phoneHint: useId(),
    code: useId(),
    codeNotice: useId(),
  };
  const countries = phoneCountryOptions(allowedCountries);
  const phoneRef = useRef<HTMLInputElement>(null);
  const codeRef = useRef<HTMLInputElement>(null);
  const moveFocusRef = useRef(false);
  /** Bumped by every request or step change so a late answer is dropped. */
  const generationRef = useRef(0);
  const [ownerToken] = useState(() => Symbol("phone-auth-transaction"));
  const [country, setCountry] = useState(() => countries[0] ?? "VN");
  const [phone, setPhone] = useState("");
  const [code, setCode] = useState("");
  const [challenge, setChallenge] = useState<PhoneChallenge | null>(null);
  const [sentKind, setSentKind] = useState<"sent" | "resent">("sent");
  const [errors, setErrors] = useState<FieldErrors>({});
  const [failure, setFailure] = useState<PhoneSignInFailure | null>(null);
  const [sending, setSending] = useState(false);
  const [verifying, setVerifying] = useState(false);
  const expiry = usePhoneCountdown();
  const cooldown = usePhoneCountdown();
  const lock = usePhoneCountdown();
  const step = challenge ? "code" : "number";
  const expired = challenge !== null && expiry.remaining <= 0;
  const challengeId = challenge?.id ?? null;

  useEffect(() => {
    if (autoFocus) phoneRef.current?.focus();
  }, [autoFocus]);

  // A new step (or a new code) moves the caret where the user types next.
  useEffect(() => {
    if (!moveFocusRef.current) return;
    moveFocusRef.current = false;
    if (step === "code") codeRef.current?.focus();
    else phoneRef.current?.focus();
  }, [step, challengeId]);

  /**
   * AUTH-13B: the server consumed the transaction for an account whose
   * school requires SSO. Nothing typed outlives it: the code, the number,
   * the challenge and its clocks are dropped before the hand-over.
   */
  function leaveForSchoolSSO() {
    generationRef.current += 1;
    setChallenge(null);
    setCode("");
    setPhone("");
    setErrors({});
    setFailure(null);
    setSending(false);
    setVerifying(false);
    expiry.reset();
    cooldown.reset();
    onSchoolSSORequired();
  }

  function fail(next: PhoneSignInFailure) {
    if (next.kind === "ssoRequired") {
      leaveForSchoolSSO();
      return;
    }
    if (next.kind === "rateLimited" || next.kind === "busy") {
      if (next.seconds > 0) lock.start(next.seconds);
    }
    setFailure(next);
  }

  function applyChallenge(
    result: AuthSignupChallengeResponse,
    target: { country: string; number: string },
    kind: "sent" | "resent",
  ) {
    transactionOwnerRef.current = ownerToken;
    moveFocusRef.current = true;
    setChallenge((current) => ({
      country: target.country,
      destination: result.masked_destination ?? current?.destination ?? "",
      id: result.challenge_id,
      number: target.number,
    }));
    setSentKind(kind);
    setCode("");
    setErrors({});
    expiry.start(result.expires_in_seconds || DEFAULT_PHONE_CODE_TTL_SECONDS);
    cooldown.start(
      result.resend_after_seconds ??
        result.cooldown_seconds ??
        DEFAULT_PHONE_RESEND_SECONDS,
    );
  }

  function startChallenge(target: { country: string; number: string }) {
    return startPhoneLogin(
      {
        country_code: target.country,
        keep_signed_in: keepSignedIn,
        phone_number: target.number,
      },
      { baseUrl: getApiBaseUrl() },
    );
  }

  async function handleSend(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (sending || lock.remaining > 0) return;
    const number = compactPhoneNumber(phone);
    if (!number || !isPlausiblePhoneNumber(number)) {
      setErrors({ phone: number ? "errorPhoneInvalid" : "errorPhoneRequired" });
      phoneRef.current?.focus();
      return;
    }
    const target = { country, number };
    const generation = ++generationRef.current;
    setErrors({});
    setFailure(null);
    setSending(true);
    try {
      await beforeRequest();
      const result = await startChallenge(target);
      if (generation !== generationRef.current) return;
      applyChallenge(result, target, "sent");
    } catch (error) {
      if (generation !== generationRef.current) return;
      const next = phoneFailureFrom(error);
      if (next.kind === "invalidNumber" || next.kind === "codeInvalid") {
        setErrors({ phone: "errorPhoneInvalid" });
        phoneRef.current?.focus();
      } else {
        fail(next);
      }
    } finally {
      if (generation === generationRef.current) setSending(false);
    }
  }

  async function handleResend() {
    if (!challenge || sending || cooldown.remaining > 0 || lock.remaining > 0) {
      return;
    }
    const target = { country: challenge.country, number: challenge.number };
    const generation = ++generationRef.current;
    setErrors({});
    setFailure(null);
    setSending(true);
    try {
      await beforeRequest();
      let result: AuthSignupChallengeResponse;
      if (transactionOwnerRef.current !== ownerToken) {
        result = await startChallenge(target);
      } else {
        try {
          // The destination and purpose stay bound to the server transaction.
          result = await resendAuthChallenge({ baseUrl: getApiBaseUrl() });
        } catch (error) {
          if (
            error instanceof APIRequestError &&
            (error.status === 400 || error.status === 401)
          ) {
            // The transaction ended (TTL, attempts or restart): start again.
            result = await startChallenge(target);
          } else {
            throw error;
          }
        }
      }
      if (generation !== generationRef.current) return;
      applyChallenge(result, target, "resent");
    } catch (error) {
      if (generation !== generationRef.current) return;
      fail(phoneFailureFrom(error));
    } finally {
      if (generation === generationRef.current) setSending(false);
    }
  }

  function handleChangeNumber() {
    generationRef.current += 1;
    moveFocusRef.current = true;
    setChallenge(null);
    setCode("");
    setErrors({});
    setFailure(null);
    setSending(false);
    expiry.reset();
    cooldown.reset();
  }

  async function finishSession() {
    setFailure(null);
    setVerifying(true);
    try {
      await onAuthenticated();
    } catch {
      // The session cookie exists; retry only the CSRF + /me hand-off.
      setFailure({ kind: "session" });
      setVerifying(false);
    }
  }

  async function handleVerify(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!challenge || verifying || lock.remaining > 0) return;
    const value = sanitizePhoneCode(code);
    let codeError: PhoneCopyKey | undefined;
    if (expired) codeError = "codeExpired";
    else if (!value) codeError = "errorCodeRequired";
    else if (!isCompletePhoneCode(value)) codeError = "errorCodeFormat";
    if (codeError) {
      setErrors({ code: codeError });
      codeRef.current?.focus();
      return;
    }
    // The typed code never outlives its submission.
    setCode("");
    setErrors({});
    setFailure(null);
    setVerifying(true);
    let result: NativeAuthenticationResponse;
    try {
      result = await verifyPhoneLogin(
        {
          challenge_id: challenge.id,
          code: value,
          keep_signed_in: keepSignedIn,
        },
        { baseUrl: getApiBaseUrl() },
      );
    } catch (error) {
      const next = phoneFailureFrom(error);
      setVerifying(false);
      if (next.kind === "ssoRequired") {
        leaveForSchoolSSO();
        return;
      }
      if (next.kind === "stale" || next.kind === "secondFactor") {
        // The server consumed the transaction: only another method helps.
        generationRef.current += 1;
        setChallenge(null);
        expiry.reset();
        cooldown.reset();
        setFailure(next);
        return;
      }
      if (next.kind === "invalidNumber" || next.kind === "codeInvalid") {
        setFailure({ kind: "codeInvalid" });
        codeRef.current?.focus();
        return;
      }
      fail(next);
      return;
    }
    if (result.status === "mfa_required") {
      // The code is right; the session exists only after step two.
      setVerifying(false);
      await onMfaRequired(result.methods);
      return;
    }
    await finishSession();
  }

  function fieldError(field: "phone" | "code") {
    const key = errors[field];
    return key ? (
      <p className="sign-in-form__error" id={`${ids[field]}-error`}>
        {c[key]}
      </p>
    ) : null;
  }

  const alert = failure ? (
    <div
      className={
        isGuidance(failure)
          ? "sign-in-form__alert sign-in-form__alert--info"
          : "sign-in-form__alert"
      }
      role="alert"
    >
      <p>{failureText(c, failure)}</p>
      {failure.kind === "session" ? (
        <button
          className="sign-in-button sign-in-button--outline"
          onClick={() => void finishSession()}
          type="button"
        >
          {c.retry}
        </button>
      ) : null}
    </div>
  ) : null;

  const heading = (
    <h2 className="visually-hidden" id={ids.heading}>
      {c.heading}
    </h2>
  );

  if (step === "number") {
    const phoneDescribedBy = [
      ids.phoneHint,
      errors.phone ? `${ids.phone}-error` : "",
    ]
      .filter(Boolean)
      .join(" ");
    const sendLabel = sending
      ? c.sending
      : lock.remaining > 0
        ? format(c.waitLabel, { label: c.sendCode, seconds: lock.remaining })
        : c.sendCode;
    return (
      <form
        aria-labelledby={ids.heading}
        className="sign-in-form__stack"
        noValidate
        onSubmit={(event) => void handleSend(event)}
      >
        {heading}
        {notice}
        <div className="sign-in-form__field">
          <div className="sign-in-form__phone-row">
            <select
              aria-label={c.countryLabel}
              className="sign-in-input sign-in-select"
              name="phone-country"
              onChange={(event) => setCountry(event.target.value)}
              value={country}
            >
              {countries.map((option) => (
                <option key={option} value={option}>
                  {phoneCountryLabel(option)}
                </option>
              ))}
            </select>
            <input
              aria-describedby={phoneDescribedBy}
              aria-invalid={errors.phone ? true : undefined}
              aria-label={c.phoneLabel}
              autoComplete="tel"
              className="sign-in-input"
              id={ids.phone}
              inputMode="tel"
              maxLength={PHONE_INPUT_MAX_LENGTH}
              name="phone-number"
              onChange={(event) => {
                setPhone(event.target.value);
                if (errors.phone) setErrors({});
              }}
              placeholder={c.phonePlaceholder}
              ref={phoneRef}
              spellCheck={false}
              type="tel"
              value={phone}
            />
          </div>
          <p className="sign-in-form__phone-hint" id={ids.phoneHint}>
            {c.phoneHint}
          </p>
          {fieldError("phone")}
        </div>
        {alert}
        <button
          aria-disabled={sending || undefined}
          className="sign-in-button sign-in-button--primary sign-in-form__submit"
          disabled={lock.remaining > 0}
          type="submit"
        >
          {sendLabel}
        </button>
        {options}
      </form>
    );
  }

  const destination = challenge?.destination ?? "";
  const codeNotice = expired
    ? { error: true, text: c.codeExpired }
    : {
        error: false,
        text: format(sentKind === "resent" ? c.codeResent : c.codeSent, {
          destination,
        }),
      };
  const codeDescribedBy = [
    ids.codeNotice,
    errors.code ? `${ids.code}-error` : "",
  ]
    .filter(Boolean)
    .join(" ");
  const verifyLabel = verifying
    ? c.verifying
    : lock.remaining > 0
      ? format(c.waitLabel, { label: c.verify, seconds: lock.remaining })
      : c.verify;
  const resendLabel = sending
    ? c.sending
    : cooldown.remaining > 0
      ? format(c.waitLabel, { label: c.resend, seconds: cooldown.remaining })
      : c.resend;

  return (
    <form
      aria-labelledby={ids.heading}
      className="sign-in-form__stack"
      noValidate
      onSubmit={(event) => void handleVerify(event)}
    >
      {heading}
      {notice}
      <div aria-live="polite">
        <p
          className={
            codeNotice.error
              ? "sign-in-form__hint sign-in-form__hint--error"
              : "sign-in-form__hint"
          }
          id={ids.codeNotice}
        >
          {codeNotice.text}
        </p>
      </div>
      {!expired ? (
        // Static text: never a live region, so the clock is not re-read.
        <p className="sign-in-form__phone-hint">
          {format(c.codeExpiresIn, {
            time: formatPhoneClock(expiry.remaining),
          })}
        </p>
      ) : null}
      <div className="sign-in-form__field">
        <input
          aria-describedby={codeDescribedBy}
          aria-invalid={errors.code ? true : undefined}
          aria-label={c.codeLabel}
          autoComplete="one-time-code"
          className="sign-in-input sign-in-input--otp"
          id={ids.code}
          inputMode="numeric"
          maxLength={PHONE_CODE_LENGTH}
          name="phone-otp"
          onChange={(event) => {
            setCode(sanitizePhoneCode(event.target.value));
            if (errors.code) setErrors({});
          }}
          placeholder={c.codePlaceholder}
          ref={codeRef}
          type="text"
          value={code}
        />
        {fieldError("code")}
      </div>
      {alert}
      <button
        aria-disabled={verifying || undefined}
        className="sign-in-button sign-in-button--primary sign-in-form__submit"
        disabled={lock.remaining > 0}
        type="submit"
      >
        {verifyLabel}
      </button>
      <div className="sign-in-form__phone-actions">
        <button
          aria-disabled={sending || undefined}
          className="sign-in-form__method"
          disabled={cooldown.remaining > 0 || lock.remaining > 0}
          onClick={() => void handleResend()}
          type="button"
        >
          {resendLabel}
        </button>
        <button
          className="sign-in-form__method"
          onClick={handleChangeNumber}
          type="button"
        >
          {c.changeNumber}
        </button>
      </div>
      {options}
    </form>
  );
}
