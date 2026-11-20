import {
  APIRequestError,
  buildOAuthStartURL,
  completeEmailSignup,
  completePasswordRecovery,
  getPendingMFA,
  loginWithPassword,
  resendAuthChallenge,
  startEmailCodeLogin,
  startEmailSignup,
  startPasskeyAssertion,
  startPasswordRecovery,
  verifyEmailCodeLogin,
  verifyPasskeyAssertion,
  type APIRequestOptions,
  type AuthSignupChallengeResponse,
  type NativeAuthenticationResponse,
  type PasskeyAssertion,
  type PasskeyRequestOptions,
  type PasswordRecoveryResponse,
} from "@tutorhub/api-client";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from "@tutorhub/ui";
import {
  Fragment,
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type FormEvent,
  type MouseEvent,
  type ReactNode,
  type RefObject,
} from "react";
import { KeyRound } from "lucide-react";
import { Navigate, useLocation, useNavigate } from "react-router";
import { useI18n, type Language } from "../app/i18n";
import { DEFAULT_MFA_EXPIRES_IN_SECONDS, type MFAMethod } from "../app/mfa";
import {
  INVALID_PASSKEY_CODE,
  classifyCeremonyError,
  getPasskeyAssertion,
  isConditionalMediationAvailable,
  isWebAuthnSupported,
  signalUnknownPasskey,
} from "../app/passkeys";
import { phoneCountryOptions } from "../app/phone";
import { useSession } from "../app/session";
import { SignInLegalDialog } from "./SignInLegalDialog";
import { SignInMfaStep } from "./SignInMfaStep";
import { SignInPhoneForm } from "./SignInPhoneForm";
import {
  LINK_EMAIL_UNVERIFIED_TARGET,
  MIN_PASSWORD_LENGTH,
  getApiBaseUrl,
  linkPendingTarget,
  loginModeFor,
  passwordMinLengthFor,
  readCallbackError,
  readReturnTo,
  registrationPendingTarget,
  sanitizeReturnTo,
  searchWithoutCallbackError,
  signUpModeFor,
  socialAvailabilityFor,
  useAuthProviders,
  type CallbackErrorCode,
  type LoginMode,
  type SignUpMode,
  type SocialAvailability,
} from "./SignInPage.shared";
import { SignInSlider } from "./SignInSlider";
import type { SignInLegalTab } from "./signInLegalContent";
import "./SignInPage.css";

const REMEMBERED_EMAIL_KEY = "tutorhub.remembered_email";
const REMEMBERED_EMAIL_MAX_LENGTH = 320;
const OTP_COOLDOWN_SECONDS = 60;
/** Upper bound for client-side Retry-After locks; the server stays authoritative. */
const MAX_RETRY_AFTER_SECONDS = 3600;
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const OTP_PATTERN = /^\d{6}$/;

const AUTH_IMAGE_ROOT = "/images/auth";
const LOGO_SRC = `${AUTH_IMAGE_ROOT}/logomoi.png`;
const SLIDES = [
  { src: `${AUTH_IMAGE_ROOT}/slide_login/login1.png`, altKey: "slide1Alt" },
  { src: `${AUTH_IMAGE_ROOT}/slide_login/login2.png`, altKey: "slide2Alt" },
  { src: `${AUTH_IMAGE_ROOT}/slide_login/login3.png`, altKey: "slide3Alt" },
] as const;

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
  keepSignedInNativeHint:
    "Stay signed in on this device for up to 30 days (administrator accounts: up to 24 hours). The server decides the final session length.",
  rememberMe: "Remember Me",
  rememberMeHint: "Only your email or username is remembered on this device.",
  or: "Or",
  facebook: "Continue with Facebook",
  google: "Sign in with Google",
  microsoft: "Sign in with Microsoft",
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
  errorPassword: "Password must be at least {min} characters.",
  errorTerms: "You must agree to the User Agreement and Privacy Policy.",
  providersLoading: "Loading sign-in options…",
  providersError:
    "We couldn't load the sign-in options. Check your connection and try again.",
  retry: "Retry",
  loginUnavailable:
    "Sign-in is temporarily unavailable. Please try again later or contact your administrator.",
  signUpUnavailable:
    "New accounts can't be created right now. Please contact your administrator.",
  loggingIn: "Logging in…",
  creatingAccount: "Creating account…",
  sendingCode: "Sending…",
  submitWait: "{label} ({seconds}s)",
  errorIdentifier: "Please enter your email.",
  errorPasswordRequired: "Please enter your password.",
  loginInvalid: "Incorrect email or password.",
  loginMfaRequired:
    "This account requires two-step verification, but no verification method is available. Please try again or contact your administrator.",
  mfaExpired: "Your verification session expired. Please sign in again.",
  mfaRecoveryExpired:
    "The verification session expired. Request a new email code to reset your password.",
  mfaLoading: "Loading two-step verification…",
  mfaLoadError:
    "Two-step verification could not be loaded. Try again, or go back and sign in again.",
  mfaBack: "Back to sign in",
  rateLimited: "Too many attempts. Please wait a moment and try again.",
  rateLimitedWait: "Too many attempts. Please try again in {seconds} seconds.",
  busy: "TutorHub is busy right now. Please try again shortly.",
  busyWait:
    "TutorHub is busy right now. Please try again in {seconds} seconds.",
  networkError: "Couldn't reach TutorHub. Check your connection and try again.",
  genericError: "Something went wrong. Please try again.",
  sessionError: "You're signed in, but we couldn't load your account yet.",
  codeSent:
    "If the email address is valid and eligible, a 6-digit verification code has been sent. Please check your inbox. The code is valid for {minutes} minutes.",
  codeExpired:
    "Your verification code has expired. Select Resend to get a new code.",
  errorCodeRequired: "Select Get Code to receive a verification code first.",
  errorCodeExpired: "This code has expired. Select Resend to get a new code.",
  errorPasswordPolicy:
    "This password is too weak or appears on a list of common or leaked passwords. Please choose a stronger password.",
  errorTermsChanged:
    "The User Agreement or Privacy Policy has just been updated. Please review it and agree again.",
  reviewTerms: "Review updated terms",
  signUpFailed:
    "We couldn't complete sign-up. Check the code or request a new one, then try again. If you already have an account, try logging in.",
  useEmailCode: "Log in with an email code",
  usePassword: "Log in with a password",
  usePhone: "Log in with a phone number",
  emailCodeHeading: "Log in with a code sent to your email",
  codeInvalid:
    "The code is incorrect or has expired. Check it or select Resend to get a new code.",
  resettingPassword: "Resetting password…",
  recoverSubtitleNative:
    "Enter your email to receive a 6-digit code, then choose a new password.",
  passwordResetDone:
    "Your password has been reset and all your sessions were signed out. Please log in again with your new password.",
  socialLegalLead: "By continuing, you agree to the ",
  socialLegalTail: ".",
  callbackSignupDisabled:
    "New accounts can't be created with this sign-in option right now. Log in with the method you used before or contact your administrator.",
  callbackEmailUnverified:
    "The email address of the account you chose isn't verified by its provider. Verify it there, then try again, or use another sign-in method.",
  callbackProviderUnavailable:
    "This sign-in option isn't available right now. Please use another sign-in method.",
  callbackAssuranceRequired:
    "This account requires a stronger sign-in method. Log in with the security method set up for it.",
  callbackAuthenticationFailed:
    "We couldn't complete that sign-in. Please try again.",
  callbackTemporarilyUnavailable:
    "Sign-in is temporarily interrupted. Please try again in a few minutes.",
  callbackRateLimited:
    "Too many sign-in attempts. Please wait a moment and try again.",
  callbackUnknown: "We couldn't complete that sign-in. Please try again.",
  passkeySignIn: "Sign in with a passkey",
  passkeyWaiting: "Waiting for your passkey…",
  passkeyCancelled: "Passkey sign-in was cancelled.",
  passkeyInvalid:
    "We couldn't verify this passkey. It may have been removed from your account. Try another passkey or sign-in method.",
  passkeyClone:
    "This passkey can't be accepted. Sign in another way and review your passkeys in Settings.",
  passkeyUnsupported:
    "This browser can't use passkeys. Please use another sign-in method.",
  passkeyUnavailable:
    "Passkey sign-in isn't available right now. Please use another sign-in method.",
  passkeyFailed: "The passkey couldn't be used. Please try again.",
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
  keepSignedInNativeHint:
    "Duy trì đăng nhập trên thiết bị này tối đa 30 ngày (tài khoản quản trị: tối đa 24 giờ). Thời hạn phiên thực tế do máy chủ quyết định.",
  rememberMe: "Ghi nhớ tôi",
  rememberMeHint: "Chỉ ghi nhớ email hoặc tên đăng nhập trên thiết bị này.",
  or: "Hoặc",
  facebook: "Tiếp tục với Facebook",
  google: "Đăng nhập bằng Google",
  microsoft: "Đăng nhập bằng Microsoft",
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
  errorPassword: "Mật khẩu cần có ít nhất {min} ký tự.",
  errorTerms:
    "Bạn cần đồng ý với Thỏa thuận người dùng và Chính sách quyền riêng tư.",
  providersLoading: "Đang tải phương thức đăng nhập…",
  providersError:
    "Chưa tải được các phương thức đăng nhập. Vui lòng kiểm tra kết nối và thử lại.",
  retry: "Thử lại",
  loginUnavailable:
    "Tính năng đăng nhập tạm thời chưa khả dụng. Vui lòng thử lại sau hoặc liên hệ quản trị viên.",
  signUpUnavailable:
    "Hiện chưa thể tạo tài khoản mới. Vui lòng liên hệ quản trị viên.",
  loggingIn: "Đang đăng nhập…",
  creatingAccount: "Đang tạo tài khoản…",
  sendingCode: "Đang gửi…",
  submitWait: "{label} ({seconds}s)",
  errorIdentifier: "Vui lòng nhập email.",
  errorPasswordRequired: "Vui lòng nhập mật khẩu.",
  loginInvalid: "Email hoặc mật khẩu không đúng.",
  loginMfaRequired:
    "Tài khoản này yêu cầu xác minh hai bước nhưng hiện không có phương thức xác minh khả dụng. Vui lòng thử lại hoặc liên hệ quản trị viên.",
  mfaExpired: "Phiên xác minh đã hết hạn. Vui lòng đăng nhập lại.",
  mfaRecoveryExpired:
    "Phiên xác minh đã hết hạn. Hãy yêu cầu mã email mới để đặt lại mật khẩu.",
  mfaLoading: "Đang tải bước xác minh hai bước…",
  mfaLoadError:
    "Chưa thể tải bước xác minh hai bước. Hãy thử lại hoặc quay lại đăng nhập.",
  mfaBack: "Quay lại đăng nhập",
  rateLimited: "Bạn đã thử quá nhiều lần. Vui lòng chờ một lát rồi thử lại.",
  rateLimitedWait:
    "Bạn đã thử quá nhiều lần. Vui lòng thử lại sau {seconds} giây.",
  busy: "Hệ thống đang bận. Vui lòng thử lại sau giây lát.",
  busyWait: "Hệ thống đang bận. Vui lòng thử lại sau {seconds} giây.",
  networkError:
    "Không kết nối được tới TutorHub. Vui lòng kiểm tra kết nối mạng và thử lại.",
  genericError: "Đã có lỗi xảy ra. Vui lòng thử lại.",
  sessionError: "Bạn đã đăng nhập nhưng chưa tải được thông tin tài khoản.",
  codeSent:
    "Nếu email hợp lệ và đủ điều kiện, mã xác thực 6 chữ số đã được gửi. Vui lòng kiểm tra hộp thư. Mã có hiệu lực trong {minutes} phút.",
  codeExpired: "Mã xác thực đã hết hạn. Vui lòng bấm Gửi lại để nhận mã mới.",
  errorCodeRequired: "Vui lòng bấm Lấy mã để nhận mã xác thực trước.",
  errorCodeExpired: "Mã đã hết hạn. Vui lòng bấm Gửi lại để nhận mã mới.",
  errorPasswordPolicy:
    "Mật khẩu chưa đạt chuẩn an toàn hoặc nằm trong danh sách mật khẩu phổ biến/bị lộ. Vui lòng chọn mật khẩu mạnh hơn.",
  errorTermsChanged:
    "Thỏa thuận người dùng hoặc Chính sách quyền riêng tư vừa được cập nhật. Vui lòng xem lại và đồng ý lại.",
  reviewTerms: "Xem điều khoản mới",
  signUpFailed:
    "Không thể hoàn tất đăng ký. Vui lòng kiểm tra lại mã hoặc yêu cầu mã mới rồi thử lại. Nếu bạn đã có tài khoản, hãy thử đăng nhập.",
  useEmailCode: "Đăng nhập bằng mã email",
  usePassword: "Đăng nhập bằng mật khẩu",
  usePhone: "Đăng nhập bằng số điện thoại",
  emailCodeHeading: "Đăng nhập bằng mã gửi tới email",
  codeInvalid:
    "Mã không đúng hoặc đã hết hạn. Vui lòng kiểm tra lại hoặc bấm Gửi lại để nhận mã mới.",
  resettingPassword: "Đang đặt lại mật khẩu…",
  recoverSubtitleNative:
    "Nhập email để nhận mã 6 chữ số, sau đó chọn mật khẩu mới.",
  passwordResetDone:
    "Mật khẩu đã được đặt lại và mọi phiên đăng nhập của bạn đã bị đăng xuất. Vui lòng đăng nhập lại bằng mật khẩu mới.",
  socialLegalLead: "Bằng việc tiếp tục, bạn đồng ý với ",
  socialLegalTail: ".",
  callbackSignupDisabled:
    "Hiện chưa thể tạo tài khoản mới bằng phương thức này. Hãy đăng nhập bằng phương thức bạn đã dùng trước đây hoặc liên hệ quản trị viên.",
  callbackEmailUnverified:
    "Email của tài khoản bạn chọn chưa được nhà cung cấp xác minh. Hãy xác minh email rồi thử lại, hoặc dùng phương thức đăng nhập khác.",
  callbackProviderUnavailable:
    "Phương thức đăng nhập này hiện không khả dụng. Vui lòng dùng phương thức khác.",
  callbackAssuranceRequired:
    "Tài khoản này yêu cầu phương thức xác thực mạnh hơn. Hãy đăng nhập bằng phương thức bảo mật đã thiết lập.",
  callbackAuthenticationFailed:
    "Không thể hoàn tất đăng nhập. Vui lòng thử lại.",
  callbackTemporarilyUnavailable:
    "Đăng nhập đang tạm gián đoạn. Vui lòng thử lại sau ít phút.",
  callbackRateLimited:
    "Bạn đã thử đăng nhập quá nhiều lần. Vui lòng chờ một lát rồi thử lại.",
  callbackUnknown: "Không thể hoàn tất đăng nhập. Vui lòng thử lại.",
  passkeySignIn: "Đăng nhập bằng Passkey",
  passkeyWaiting: "Đang chờ passkey…",
  passkeyCancelled: "Đã hủy xác thực passkey.",
  passkeyInvalid:
    "Không thể xác minh passkey này. Có thể passkey đã bị gỡ khỏi tài khoản. Hãy thử passkey khác hoặc cách đăng nhập khác.",
  passkeyClone:
    "Passkey này không được chấp nhận. Hãy đăng nhập bằng cách khác và kiểm tra danh sách passkey trong Cài đặt.",
  passkeyUnsupported:
    "Trình duyệt này không dùng được passkey. Vui lòng chọn cách đăng nhập khác.",
  passkeyUnavailable:
    "Đăng nhập bằng passkey hiện không khả dụng. Vui lòng chọn cách đăng nhập khác.",
  passkeyFailed: "Không thể dùng passkey. Vui lòng thử lại.",
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

const CALLBACK_ERROR_COPY: Record<CallbackErrorCode | "unknown", CopyKey> = {
  assurance_required: "callbackAssuranceRequired",
  authentication_failed: "callbackAuthenticationFailed",
  email_unverified: "callbackEmailUnverified",
  provider_unavailable: "callbackProviderUnavailable",
  rate_limited: "callbackRateLimited",
  signup_disabled: "callbackSignupDisabled",
  temporarily_unavailable: "callbackTemporarilyUnavailable",
  unknown: "callbackUnknown",
};

/**
 * Standard multi-colour Google "G" (Sign in with Google branding guidelines):
 * fixed colours, square aspect ratio, always on the button's white fill.
 */
function GoogleMark() {
  return (
    <svg
      aria-hidden="true"
      className="sign-in-google-mark"
      focusable="false"
      height={20}
      viewBox="0 0 48 48"
      width={20}
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        d="M24 9.5c3.54 0 6.71 1.22 9.21 3.6l6.85-6.85C35.9 2.38 30.47 0 24 0 14.62 0 6.51 5.38 2.56 13.22l7.98 6.19C12.43 13.72 17.74 9.5 24 9.5z"
        fill="#EA4335"
      />
      <path
        d="M46.98 24.55c0-1.57-.15-3.09-.38-4.55H24v9.02h12.94c-.58 2.96-2.26 5.48-4.78 7.18l7.73 6c4.51-4.18 7.09-10.36 7.09-17.65z"
        fill="#4285F4"
      />
      <path
        d="M10.53 28.59c-.48-1.45-.76-2.99-.76-4.59s.27-3.14.76-4.59l-7.98-6.19C.92 16.46 0 20.12 0 24c0 3.88.92 7.54 2.56 10.78l7.97-6.19z"
        fill="#FBBC05"
      />
      <path
        d="M24 48c6.48 0 11.93-2.13 15.89-5.81l-7.73-6c-2.15 1.45-4.92 2.3-8.16 2.3-6.26 0-11.57-4.22-13.47-9.91l-7.98 6.19C6.51 42.62 14.62 48 24 48z"
        fill="#34A853"
      />
      <path d="M0 0h48v48H0z" fill="none" />
    </svg>
  );
}

/**
 * Facebook "f" logo (Meta Facebook Login guidelines): the approved mark in
 * its primary colours, unmodified, placed before the call to action.
 */
function FacebookMark() {
  return (
    <svg
      aria-hidden="true"
      className="sign-in-facebook-mark"
      focusable="false"
      height={20}
      viewBox="0 0 1024 1024"
      width={20}
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        d="M1024 512C1024 229.23 794.77 0 512 0S0 229.23 0 512c0 255.55 187.23 467.37 432 505.78V660H302V512h130V399.2C432 270.88 508.44 200 625.39 200 681.41 200 740 210 740 210v126h-64.56c-63.6 0-83.44 39.47-83.44 79.96V512h142l-22.7 148H592v357.78C836.77 979.37 1024 767.55 1024 512z"
        fill="#1877F2"
      />
      <path
        d="M711.3 660 734 512H592v-96.04c0-40.49 19.84-79.96 83.44-79.96H740V210s-58.59-10-114.61-10C508.44 200 432 270.88 432 399.2V512H302v148h130v357.78a517.58 517.58 0 0 0 160 0V660z"
        fill="#FFFFFF"
      />
    </svg>
  );
}

/**
 * Microsoft logo (Sign in with Microsoft branding guidelines): four 9×9
 * squares in a 21×21 frame, official colours, never altered or recoloured.
 */
function MicrosoftMark() {
  return (
    <svg
      aria-hidden="true"
      className="sign-in-microsoft-mark"
      focusable="false"
      height={20}
      viewBox="0 0 21 21"
      width={20}
      xmlns="http://www.w3.org/2000/svg"
    >
      <rect fill="#F25022" height={9} width={9} x={1} y={1} />
      <rect fill="#7FBA00" height={9} width={9} x={11} y={1} />
      <rect fill="#00A4EF" height={9} width={9} x={1} y={11} />
      <rect fill="#FFB900" height={9} width={9} x={11} y={11} />
    </svg>
  );
}

type AuthFailure =
  | { kind: "invalidCredentials" }
  | { kind: "mfaRequired" }
  | { kind: "mfaExpired" }
  | { kind: "mfaRecoveryExpired" }
  | { kind: "rateLimited"; retryAfterSeconds?: number }
  | { kind: "busy"; retryAfterSeconds?: number }
  | { kind: "badRequest" }
  | { kind: "termsChanged" }
  | { kind: "passwordPolicy" }
  | { kind: "signUpFailed" }
  | { kind: "codeInvalid" }
  | { kind: "session" }
  | { kind: "network" }
  | { kind: "passkeyInvalid" }
  | { kind: "passkeyClone" }
  | { kind: "passkeyUnsupported" }
  | { kind: "passkeyUnavailable" }
  | { kind: "passkeyFailed" }
  | { kind: "generic" };

function boundedRetryAfter(value: number | undefined) {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? Math.min(Math.ceil(value), MAX_RETRY_AFTER_SECONDS)
    : undefined;
}

function authFailureFrom(error: unknown): AuthFailure {
  if (!(error instanceof APIRequestError)) {
    // fetch() rejects with a TypeError (or AbortError) when the network fails.
    return { kind: "network" };
  }
  switch (error.status) {
    case 400:
      if (error.problem?.code === "auth_terms_version_changed") {
        return { kind: "termsChanged" };
      }
      if (error.problem?.code === "auth_password_policy") {
        return { kind: "passwordPolicy" };
      }
      return { kind: "badRequest" };
    case 401:
      return { kind: "invalidCredentials" };
    case 429:
      return {
        kind: "rateLimited",
        retryAfterSeconds: boundedRetryAfter(error.retryAfterSeconds),
      };
    case 503:
      return {
        kind: "busy",
        retryAfterSeconds: boundedRetryAfter(error.retryAfterSeconds),
      };
    default:
      return { kind: "generic" };
  }
}

/** AUTH-08A passkey problem contract for the discoverable sign-in. */
function passkeyFailureFrom(error: unknown): AuthFailure {
  if (!(error instanceof APIRequestError)) return { kind: "network" };
  switch (error.status) {
    case 401:
      return error.problem?.code === INVALID_PASSKEY_CODE
        ? { kind: "passkeyInvalid" }
        : { kind: "passkeyFailed" };
    case 403:
      return error.problem?.code === "auth_passkey_clone_detected"
        ? { kind: "passkeyClone" }
        : { kind: "passkeyFailed" };
    case 404:
      // The deployment turned passkeys off (or the RP is misconfigured).
      return { kind: "passkeyUnavailable" };
    case 429:
    case 503:
      return authFailureFrom(error);
    default:
      return { kind: "passkeyFailed" };
  }
}

function retryAfterOf(failure: AuthFailure) {
  return failure.kind === "rateLimited" || failure.kind === "busy"
    ? failure.retryAfterSeconds
    : undefined;
}

function failureText(c: SignInCopy, failure: AuthFailure) {
  switch (failure.kind) {
    case "invalidCredentials":
      return c.loginInvalid;
    case "mfaRequired":
      return c.loginMfaRequired;
    case "mfaExpired":
      return c.mfaExpired;
    case "mfaRecoveryExpired":
      return c.mfaRecoveryExpired;
    case "rateLimited":
      return failure.retryAfterSeconds
        ? formatCopy(c.rateLimitedWait, {
            seconds: failure.retryAfterSeconds,
          })
        : c.rateLimited;
    case "busy":
      return failure.retryAfterSeconds
        ? formatCopy(c.busyWait, { seconds: failure.retryAfterSeconds })
        : c.busy;
    case "signUpFailed":
      return c.signUpFailed;
    case "codeInvalid":
      return c.codeInvalid;
    case "session":
      return c.sessionError;
    case "network":
      return c.networkError;
    case "passkeyInvalid":
      return c.passkeyInvalid;
    case "passkeyClone":
      return c.passkeyClone;
    case "passkeyUnsupported":
      return c.passkeyUnsupported;
    case "passkeyUnavailable":
      return c.passkeyUnavailable;
    case "passkeyFailed":
      return c.passkeyFailed;
    default:
      return c.genericError;
  }
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

function useCountdown() {
  const [remaining, setRemaining] = useState(0);

  useEffect(() => {
    if (remaining <= 0) {
      return;
    }
    const timer = window.setTimeout(() => {
      setRemaining((value) => Math.max(0, value - 1));
    }, 1000);
    return () => window.clearTimeout(timer);
  }, [remaining]);

  const start = useCallback((seconds: number) => {
    setRemaining(
      Number.isFinite(seconds) && seconds > 0
        ? Math.min(Math.ceil(seconds), MAX_RETRY_AFTER_SECONDS)
        : 0,
    );
  }, []);
  const reset = useCallback(() => setRemaining(0), []);

  return { remaining, reset, start };
}

/** How long the passkey button waits for an aborted autofill request. */
const CONDITIONAL_ABORT_GRACE_MS = 1000;

interface BackgroundPasskeyRequest {
  controller: AbortController;
  settled: Promise<void>;
}

interface PasskeySignInOptions {
  /** Passkey autofill may run (the password form is showing). */
  conditional: boolean;
  /** `passkeys_enabled` and this browser supports WebAuthn. */
  enabled: boolean;
  keepSignedIn: boolean;
  onAuthenticated: () => Promise<void>;
  /** AUTH-09B: a passkey without user verification needs a second step. */
  onMfaRequired: (methods: readonly MFAMethod[]) => void;
}

type PasskeyVerifyOutcome =
  | { kind: "authenticated" }
  | { kind: "mfa"; methods: readonly MFAMethod[] }
  | { kind: "failed"; failure: AuthFailure };

/**
 * AUTH-08B discoverable passkey sign-in for LoginForm.
 *
 * - Autofill (conditional mediation) starts in the background while the
 *   password form shows and `isConditionalMediationAvailable()` is true. One
 *   AbortController covers the options fetch and the browser request; it is
 *   aborted on unmount, on leaving the password form, and before the button
 *   opens the modal prompt (waiting for the aborted request to settle, so the
 *   browser never sees two pending requests).
 * - Background failures before the user picks a passkey stay silent. Once a
 *   picked passkey reached the server, autofill is not re-armed (a stale
 *   passkey must not loop against the API); a cancelled modal re-arms it.
 * - A 401 `auth_invalid_passkey` with `unknown_credential: true` signals the
 *   authenticator (Signal API) with the options' rpId and the credential ID
 *   that was sent. Nothing is persisted in Web Storage.
 * - AUTH-09B: a passkey without user verification answers `mfa_required`;
 *   the form moves to the second step (`onMfaRequired`), and
 *   `stopBackground()` lets another first factor cancel autofill first.
 */
function usePasskeySignIn({
  conditional,
  enabled,
  keepSignedIn,
  onAuthenticated,
  onMfaRequired,
}: PasskeySignInOptions) {
  const [pending, setPending] = useState(false);
  const [cancelled, setCancelled] = useState(false);
  const [failure, setFailure] = useState<AuthFailure | null>(null);
  const [rearm, setRearm] = useState(0);
  const lock = useCountdown();
  const lockStart = lock.start;
  const keepSignedInRef = useRef(keepSignedIn);
  const onAuthenticatedRef = useRef(onAuthenticated);
  const onMfaRequiredRef = useRef(onMfaRequired);
  const backgroundRef = useRef<BackgroundPasskeyRequest | null>(null);
  const runningRef = useRef(false);

  useEffect(() => {
    keepSignedInRef.current = keepSignedIn;
    onAuthenticatedRef.current = onAuthenticated;
    onMfaRequiredRef.current = onMfaRequired;
  });

  const fail = useCallback(
    (next: AuthFailure) => {
      const retryAfter = retryAfterOf(next);
      if (retryAfter) lockStart(retryAfter);
      setFailure(next);
      setPending(false);
    },
    [lockStart],
  );

  /** POST /verify: signed in, a second step is needed, or a failure. */
  const verify = useCallback(
    async (
      options: PasskeyRequestOptions,
      assertion: PasskeyAssertion,
    ): Promise<PasskeyVerifyOutcome> => {
      try {
        const result = await verifyPasskeyAssertion(
          { assertion, keep_signed_in: keepSignedInRef.current },
          { baseUrl: getApiBaseUrl() },
        );
        return result.status === "mfa_required"
          ? { kind: "mfa", methods: result.methods }
          : { kind: "authenticated" };
      } catch (error) {
        void signalUnknownPasskey(error, {
          credentialId: assertion.id,
          rpId: options.rpId,
        });
        return { kind: "failed", failure: passkeyFailureFrom(error) };
      }
    },
    [],
  );

  /** Hands a verified first factor to the second step (never awaited). */
  const requireMfa = useCallback((methods: readonly MFAMethod[]) => {
    setPending(false);
    onMfaRequiredRef.current(methods);
  }, []);

  /** The session cookie exists: hand off CSRF + /me (retryable). */
  const finish = useCallback(async () => {
    setFailure(null);
    setPending(true);
    try {
      await onAuthenticatedRef.current();
    } catch {
      setFailure({ kind: "session" });
    } finally {
      setPending(false);
    }
  }, []);

  useEffect(() => {
    if (!enabled || !conditional) return;
    const controller = new AbortController();
    const request: BackgroundPasskeyRequest = {
      controller,
      settled: Promise.resolve(),
    };
    request.settled = (async () => {
      if (!(await isConditionalMediationAvailable())) return;
      if (controller.signal.aborted) return;
      let options: PasskeyRequestOptions;
      let assertion: PasskeyAssertion;
      try {
        options = await startPasskeyAssertion({
          baseUrl: getApiBaseUrl(),
          signal: controller.signal,
        });
        if (controller.signal.aborted) return;
        assertion = await getPasskeyAssertion(options, {
          mediation: "conditional",
          signal: controller.signal,
        });
      } catch {
        // Nothing was asked of the user yet: a cancel, an abort or a
        // background failure (offline, 429, 503) stays silent.
        return;
      }
      // The user picked a passkey from autofill (unless the button won).
      if (controller.signal.aborted || runningRef.current) return;
      runningRef.current = true;
      setCancelled(false);
      setFailure(null);
      setPending(true);
      try {
        const outcome = await verify(options, assertion);
        if (outcome.kind === "failed") {
          fail(outcome.failure);
          return;
        }
        if (outcome.kind === "mfa") {
          requireMfa(outcome.methods);
          return;
        }
        await finish();
      } finally {
        runningRef.current = false;
      }
    })();
    backgroundRef.current = request;
    return () => {
      controller.abort();
      if (backgroundRef.current === request) backgroundRef.current = null;
    };
  }, [conditional, enabled, fail, finish, rearm, requireMfa, verify]);

  /** Aborts passkey autofill and waits (briefly) for it to settle. */
  const stopBackground = useCallback(async () => {
    const background = backgroundRef.current;
    if (!background) return;
    background.controller.abort();
    await Promise.race([
      background.settled,
      new Promise((resolve) =>
        window.setTimeout(resolve, CONDITIONAL_ABORT_GRACE_MS),
      ),
    ]);
  }, []);

  const signIn = useCallback(async () => {
    if (runningRef.current) return;
    runningRef.current = true;
    setCancelled(false);
    setFailure(null);
    setPending(true);
    try {
      const background = backgroundRef.current;
      if (background) {
        background.controller.abort();
        await Promise.race([
          background.settled,
          new Promise((resolve) =>
            window.setTimeout(resolve, CONDITIONAL_ABORT_GRACE_MS),
          ),
        ]);
      }
      let options: PasskeyRequestOptions;
      try {
        options = await startPasskeyAssertion({ baseUrl: getApiBaseUrl() });
      } catch (error) {
        fail(passkeyFailureFrom(error));
        return;
      }
      let assertion: PasskeyAssertion;
      try {
        assertion = await getPasskeyAssertion(options, {
          mediation: "optional",
        });
      } catch (error) {
        const kind = classifyCeremonyError(error);
        if (kind === "cancelled") {
          // A gentle notice, never an error banner; autofill comes back.
          setPending(false);
          setCancelled(true);
          setRearm((value) => value + 1);
          return;
        }
        fail({
          kind: kind === "unsupported" ? "passkeyUnsupported" : "passkeyFailed",
        });
        return;
      }
      const outcome = await verify(options, assertion);
      if (outcome.kind === "failed") {
        fail(outcome.failure);
        return;
      }
      if (outcome.kind === "mfa") {
        requireMfa(outcome.methods);
        return;
      }
      await finish();
    } finally {
      runningRef.current = false;
    }
  }, [fail, finish, requireMfa, verify]);

  return {
    cancelled,
    failure,
    lockRemaining: lock.remaining,
    pending,
    retrySession: finish,
    signIn,
    stopBackground,
  };
}

function useCodeCooldown() {
  const { remaining, start: startCountdown } = useCountdown();
  const [hasRequested, setHasRequested] = useState(false);

  const start = useCallback(() => {
    startCountdown(OTP_COOLDOWN_SECONDS);
    setHasRequested(true);
  }, [startCountdown]);

  return { hasRequested, remaining, start };
}

type StartEmailChallenge = (
  input: { email: string },
  options: APIRequestOptions,
) => Promise<AuthSignupChallengeResponse>;

/**
 * The server binds one pending auth transaction to one browser cookie. This
 * ref records which mounted flow (login code or recovery) issued it last, so a
 * flow whose transaction was replaced starts a new one instead of resending
 * the other flow's code.
 */
type TransactionOwnerRef = RefObject<symbol | null>;

type ChallengeOutcome = "sent" | "invalidEmail" | "failed" | "stale";

/**
 * Shared start → resend → expiry state for the native email-code login and
 * password recovery flows. Notices stay account-neutral by construction.
 */
function useEmailChallenge(
  startChallenge: StartEmailChallenge,
  ownerRef: TransactionOwnerRef,
) {
  const [ownerToken] = useState(() => Symbol("auth-transaction"));
  /** Bumped whenever the email changes so a late response is discarded. */
  const generationRef = useRef(0);
  const [challenge, setChallenge] = useState<SignUpChallenge | null>(null);
  const [expired, setExpired] = useState(false);
  const [requesting, setRequesting] = useState(false);
  const [notice, setNotice] = useState<CodeNotice | null>(null);
  const cooldown = useCountdown();
  const { reset: resetCooldown, start: startCooldown } = cooldown;

  // Remind the user to resend once the server-side code lifetime has passed.
  useEffect(() => {
    if (!challenge) return;
    const timer = window.setTimeout(
      () => setExpired(true),
      Math.min(challenge.expiresInSeconds, MAX_RETRY_AFTER_SECONDS) * 1000,
    );
    return () => window.clearTimeout(timer);
  }, [challenge]);

  /** A code belongs to one email: editing the address forces a new request. */
  const invalidate = useCallback(() => {
    generationRef.current += 1;
    setChallenge(null);
    setExpired(false);
    setNotice(null);
    setRequesting(false);
    resetCooldown();
  }, [resetCooldown]);

  async function issue(email: string) {
    const options = { baseUrl: getApiBaseUrl() };
    if (!challenge || ownerRef.current !== ownerToken) {
      return startChallenge({ email }, options);
    }
    try {
      // The destination and purpose stay bound to the server transaction.
      return await resendAuthChallenge(options);
    } catch (error) {
      if (error instanceof APIRequestError && error.status === 400) {
        // The transaction ended (TTL, restart or recovery): start a new one.
        return startChallenge({ email }, options);
      }
      throw error;
    }
  }

  async function request(email: string): Promise<ChallengeOutcome> {
    const generation = ++generationRef.current;
    const isStale = () => generation !== generationRef.current;
    setRequesting(true);
    setNotice(null);
    try {
      const result = await issue(email);
      if (isStale()) return "stale";
      ownerRef.current = ownerToken;
      setChallenge({
        expiresInSeconds: result.expires_in_seconds,
        id: result.challenge_id,
      });
      setExpired(false);
      startCooldown(result.cooldown_seconds);
      // Uniform wording: never reveal whether the address has an account.
      setNotice({
        kind: "sent",
        minutes: Math.max(1, Math.ceil(result.expires_in_seconds / 60)),
      });
      return "sent";
    } catch (error) {
      if (isStale()) return "stale";
      const failure = authFailureFrom(error);
      if (failure.kind === "badRequest") return "invalidEmail";
      const retryAfter = retryAfterOf(failure);
      if (retryAfter) startCooldown(retryAfter);
      setNotice({ kind: "failure", failure });
      return "failed";
    } finally {
      if (!isStale()) setRequesting(false);
    }
  }

  return {
    challenge,
    cooldownRemaining: cooldown.remaining,
    expired,
    invalidate,
    notice,
    request,
    requesting,
  };
}

type EmailChallenge = ReturnType<typeof useEmailChallenge>;

function challengeNoticeView(
  c: SignInCopy,
  code: EmailChallenge,
): { error: boolean; text: string } | null {
  if (code.challenge && code.expired) {
    return { error: true, text: c.codeExpired };
  }
  switch (code.notice?.kind) {
    case "sent":
      return {
        error: false,
        text: formatCopy(c.codeSent, { minutes: code.notice.minutes }),
      };
    case "failure":
      return { error: true, text: failureText(c, code.notice.failure) };
    default:
      return null;
  }
}

function challengeButtonLabel(c: SignInCopy, code: EmailChallenge) {
  if (code.requesting) return c.sendingCode;
  if (code.cooldownRemaining > 0) {
    return formatCopy(c.codeCountdown, { seconds: code.cooldownRemaining });
  }
  return code.challenge ? c.resendCode : c.getCode;
}

/** Validates the code step shared by the email-code login and recovery. */
function codeErrorFor(code: EmailChallenge, otp: string): CopyKey | undefined {
  if (!code.challenge) return "errorCodeRequired";
  if (code.expired) return "errorCodeExpired";
  return OTP_PATTERN.test(otp.trim()) ? undefined : "errorOtp";
}

interface FormAlertProps {
  failure: AuthFailure;
  onRetrySession?: () => void;
}

function FormAlert({ failure, onRetrySession }: FormAlertProps) {
  const c = useSignInCopy();
  return (
    <div
      className={
        failure.kind === "mfaRequired" ||
        failure.kind === "mfaExpired" ||
        failure.kind === "mfaRecoveryExpired"
          ? "sign-in-form__alert sign-in-form__alert--info"
          : "sign-in-form__alert"
      }
      role="alert"
    >
      <p>{failureText(c, failure)}</p>
      {failure.kind === "session" && onRetrySession ? (
        <button
          className="sign-in-button sign-in-button--outline"
          onClick={onRetrySession}
          type="button"
        >
          {c.retry}
        </button>
      ) : null}
    </div>
  );
}

export function SignInPage() {
  const location = useLocation();
  // The OAuth callback parked a provider identity for explicit linking.
  const linkTarget = linkPendingTarget(location.search);
  if (linkTarget) {
    return <Navigate replace to={linkTarget} />;
  }
  // A new Facebook subject must verify an email before an account exists.
  const registrationTarget = registrationPendingTarget(location.search);
  if (registrationTarget) {
    return <Navigate replace to={registrationTarget} />;
  }
  return <SignInScreen />;
}

function SignInScreen() {
  const c = useSignInCopy();
  const {
    completeNativeSignIn,
    resetAfterPasswordRecovery,
    signIn,
    status: sessionStatus,
  } = useSession();
  const navigate = useNavigate();
  const location = useLocation();
  const returnTo = readReturnTo(location.state);
  const { reload: reloadProviders, state: providersState } = useAuthProviders();
  const [view, setView] = useState<"login" | "signup">("login");
  const [focusFormOnMount, setFocusFormOnMount] = useState(false);
  // Read once: the fixed code survives removing it from the address bar.
  const [callbackError] = useState(() => readCallbackError(location.search));
  // AUTH-09B: an OAuth callback parked a first factor for a second step.
  const [resumePendingMfa] = useState(
    () => new URLSearchParams(location.search).get("mfa") === "pending",
  );
  const heroTitleId = useId();

  // AUTH-07B: a provider link started from Settings (so a session exists)
  // whose provider withheld a trusted email ends here; the guidance on what to
  // do next belongs to Settings, where the link was started.
  const linkEmailUnverified =
    callbackError === "email_unverified" && sessionStatus === "authenticated";
  useEffect(() => {
    if (!linkEmailUnverified) return;
    void navigate(LINK_EMAIL_UNVERIFIED_TARGET, { replace: true });
  }, [linkEmailUnverified, navigate]);

  const searchParams = new URLSearchParams(location.search);
  const hasTransientParam =
    searchParams.has("error") || searchParams.has("mfa");
  useEffect(() => {
    if (!hasTransientParam || linkEmailUnverified) return;
    // Router-aware history.replaceState: keeps the protected `from` state.
    void navigate(
      {
        hash: location.hash,
        pathname: location.pathname,
        search: searchWithoutTransientParams(location.search),
      },
      { replace: true, state: location.state as unknown },
    );
  }, [
    hasTransientParam,
    linkEmailUnverified,
    location.hash,
    location.pathname,
    location.search,
    location.state,
    navigate,
  ]);

  const startSignIn = useCallback(() => signIn(returnTo), [returnTo, signIn]);
  const startProviderSignIn = useCallback(
    (provider: "facebook" | "google" | "microsoft") => {
      // A full-page navigation (never fetch): the server sets the flow cookie.
      window.location.assign(
        buildOAuthStartURL(
          provider,
          { intent: "login", returnTo },
          { baseUrl: getApiBaseUrl() },
        ),
      );
    },
    [returnTo],
  );
  const startGoogleSignIn = useCallback(
    () => startProviderSignIn("google"),
    [startProviderSignIn],
  );
  const startFacebookSignIn = useCallback(
    () => startProviderSignIn("facebook"),
    [startProviderSignIn],
  );
  const startMicrosoftSignIn = useCallback(
    () => startProviderSignIn("microsoft"),
    [startProviderSignIn],
  );
  const finishNativeSignIn = useCallback(
    async (options?: NativeSignInOptions) => {
      await completeNativeSignIn();
      // A second step after OAuth carries the server-validated destination.
      void navigate(
        options?.returnTo ? sanitizeReturnTo(options.returnTo) : returnTo,
        { replace: true },
      );
    },
    [completeNativeSignIn, navigate, returnTo],
  );

  const providers =
    providersState.status === "ready" ? providersState.providers : null;

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

      <section
        aria-busy={providersState.status === "loading" || undefined}
        aria-label={c.panelLabel}
        className="sign-in__panel"
      >
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

          {providersState.status === "loading" ? (
            <p className="visually-hidden" role="status">
              {c.providersLoading}
            </p>
          ) : null}
          {providersState.status === "error" ? (
            <div className="sign-in-status" role="alert">
              <p>{c.providersError}</p>
              <button
                className="sign-in-button sign-in-button--outline"
                onClick={reloadProviders}
                type="button"
              >
                {c.retry}
              </button>
            </div>
          ) : null}
          {callbackError ? (
            <div
              className="sign-in-status sign-in-status--callback"
              role="alert"
            >
              <p>{c[CALLBACK_ERROR_COPY[callbackError]]}</p>
            </div>
          ) : null}

          {view === "login" ? (
            <LoginForm
              autoFocus={focusFormOnMount}
              emailCodeEnabled={providers?.email_code_enabled === true}
              mode={loginModeFor(providers)}
              onAuthenticated={finishNativeSignIn}
              onFacebookSignIn={startFacebookSignIn}
              onGoogleSignIn={startGoogleSignIn}
              onMicrosoftSignIn={startMicrosoftSignIn}
              onPasswordReset={resetAfterPasswordRecovery}
              onSignIn={startSignIn}
              // A signed-in visitor gets no background passkey request.
              passkeyAutofill={sessionStatus === "unauthenticated"}
              passkeysEnabled={providers?.passkeys_enabled === true}
              passwordMinLength={passwordMinLengthFor(providers)}
              phoneAllowedCountries={providers?.phone_allowed_countries}
              phoneEnabled={providers?.phone_enabled === true}
              // Older servers omit the flag: absence means recovery is off.
              recoveryEnabled={providers?.password_recovery_enabled === true}
              resumePendingMfa={resumePendingMfa}
              social={socialAvailabilityFor(providers)}
            />
          ) : (
            <SignUpForm
              autoFocus={focusFormOnMount}
              mode={signUpModeFor(providers)}
              onAuthenticated={finishNativeSignIn}
              onSignIn={startSignIn}
              onTermsChanged={reloadProviders}
              passwordMinLength={passwordMinLengthFor(providers)}
              termsVersion={providers?.legal_terms_version ?? null}
            />
          )}
        </div>
      </section>
    </main>
  );
}

function LoginImageSlider() {
  const c = useSignInCopy();
  return (
    <SignInSlider
      labels={{
        goToSlide: (index) => formatCopy(c.goToSlide, { index }),
        next: c.nextSlide,
        pause: c.pauseSlides,
        play: c.playSlides,
        previous: c.previousSlide,
        region: c.sliderLabel,
        slideFailed: c.slideFailed,
        slidePosition: (current, total) =>
          formatCopy(c.slidePosition, { current, total }),
      }}
      slides={SLIDES.map((slide) => ({ alt: c[slide.altKey], src: slide.src }))}
    />
  );
}

/** AUTH-09B: what a finished second step tells the page. */
export interface NativeSignInOptions {
  /** Server-validated local path from the first factor (e.g. OAuth). */
  returnTo?: string;
}

/** Same search string without `error` and `mfa` (other params are kept). */
function searchWithoutTransientParams(search: string) {
  const params = new URLSearchParams(searchWithoutCallbackError(search));
  params.delete("mfa");
  const next = params.toString();
  return next ? `?${next}` : "";
}

export interface LoginFormProps {
  autoFocus: boolean;
  /** `email_code_enabled`: offers "log in with an email code". */
  emailCodeEnabled: boolean;
  mode: LoginMode;
  /** Runs after the server issued a native session: CSRF, /me, navigation. */
  onAuthenticated: (options?: NativeSignInOptions) => Promise<void>;
  /** Starts the Facebook flow when `social.facebookFederation` is on. */
  onFacebookSignIn?: () => void;
  /** Starts the Google flow when `social.googleFederation` is on. */
  onGoogleSignIn?: () => void;
  /** Starts the Microsoft flow; rendered only when `social.microsoft`. */
  onMicrosoftSignIn?: () => void;
  /** Drops client session state after a recovery revoked every session. */
  onPasswordReset: () => Promise<void>;
  /** Redirects to the default OIDC portal (BFF `/api/v1/auth/login`). */
  onSignIn: () => void;
  /**
   * `passkeys_enabled` (AUTH-08B): offers "Sign in with a passkey" and
   * passkey autofill when this browser supports WebAuthn.
   */
  passkeysEnabled?: boolean;
  /**
   * Whether passkey autofill may start in the background (default true);
   * /sign-in turns it off while a session already exists.
   */
  passkeyAutofill?: boolean;
  passwordMinLength: number;
  /**
   * `phone_allowed_countries` (AUTH-10B); the first region is the default.
   * Phone sign-in needs at least one region.
   */
  phoneAllowedCountries?: readonly string[];
  /** `phone_enabled` (AUTH-10B): offers "Log in with a phone number". */
  phoneEnabled?: boolean;
  /** `password_recovery_enabled`: the dialog runs the native recovery. */
  recoveryEnabled: boolean;
  /**
   * AUTH-09B: `/sign-in?mfa=pending` — load the parked second step
   * (`GET /auth/mfa`) instead of asking for a first factor.
   */
  resumePendingMfa?: boolean;
  social: SocialAvailability;
}

type MfaView =
  | { kind: "loading" }
  | { kind: "loadError" }
  | {
      kind: "challenge";
      expiresInSeconds: number;
      id: number;
      methods: readonly MFAMethod[];
    };

type LoginField = "identifier" | "password";
type LoginErrors = Partial<Record<LoginField, CopyKey>>;
type LoginMethod = "password" | "emailCode" | "phone";

/**
 * Password / email-code / phone-code login with recovery. Exported for
 * /account-link, which signs in in place (no social buttons, no navigation).
 */
export function LoginForm({
  autoFocus,
  emailCodeEnabled,
  mode,
  onAuthenticated,
  onFacebookSignIn,
  onGoogleSignIn,
  onMicrosoftSignIn,
  onPasswordReset,
  onSignIn,
  passkeysEnabled = false,
  passkeyAutofill = true,
  passwordMinLength,
  phoneAllowedCountries,
  phoneEnabled = false,
  recoveryEnabled,
  resumePendingMfa = false,
  social,
}: LoginFormProps) {
  const c = useSignInCopy();
  const ids = {
    heading: useId(),
    identifier: useId(),
    password: useId(),
    keepSignedInHint: useId(),
    rememberHint: useId(),
  };
  const identifierRef = useRef<HTMLInputElement>(null);
  const passwordRef = useRef<HTMLInputElement>(null);
  const transactionOwnerRef = useRef<symbol | null>(null);
  const focusIdentifierOnCloseRef = useRef(false);
  const [identifier, setIdentifier] = useState(readRememberedEmail);
  const [password, setPassword] = useState("");
  const [rememberMe, setRememberMe] = useState(() =>
    Boolean(readRememberedEmail()),
  );
  const [keepSignedIn, setKeepSignedIn] = useState(false);
  const [errors, setErrors] = useState<LoginErrors>({});
  const [failure, setFailure] = useState<AuthFailure | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [method, setMethod] = useState<LoginMethod>("password");
  const [methodSwitched, setMethodSwitched] = useState(false);
  const [recoverOpen, setRecoverOpen] = useState(false);
  const [passwordResetDone, setPasswordResetDone] = useState(false);
  const [legalOpen, setLegalOpen] = useState(false);
  const [legalTab, setLegalTab] = useState<SignInLegalTab>("agreement");
  const legalReturnFocusRef = useRef<HTMLElement | null>(null);
  const lock = useCountdown();
  const [webAuthnSupported] = useState(isWebAuthnSupported);
  const [mfa, setMfa] = useState<MfaView | null>(() =>
    resumePendingMfa ? { kind: "loading" } : null,
  );
  const [pendingMfaAttempt, setPendingMfaAttempt] = useState(
    resumePendingMfa ? 1 : 0,
  );
  const [refocusAfterMfa, setRefocusAfterMfa] = useState(0);
  /** Remembered (if asked) only once the second step issued a session. */
  const [mfaIdentifier, setMfaIdentifier] = useState("");
  const isPassword = mode === "password";
  const phoneCountries = phoneCountryOptions(phoneAllowedCountries);
  const phoneAvailable = phoneEnabled && phoneCountries.length > 0;
  const usesEmailCode = emailCodeEnabled && method === "emailCode";
  const usesPhone = phoneAvailable && method === "phone";
  const passkeysActive = passkeysEnabled && webAuthnSupported;
  /** Native methods (and passkeys) issue a first-party session that honours keep-signed-in. */
  const nativeSession =
    usesEmailCode || usesPhone || isPassword || passkeysActive;

  /** Shows the second step; every typed first-factor secret is dropped. */
  const showMfaChallenge = useCallback(
    (
      methods: readonly MFAMethod[],
      expiresInSeconds = DEFAULT_MFA_EXPIRES_IN_SECONDS,
    ) => {
      setPassword("");
      setErrors({});
      setSubmitting(false);
      setPasswordResetDone(false);
      if (methods.length === 0) {
        setMfa(null);
        setFailure({ kind: "mfaRequired" });
        return;
      }
      setFailure(null);
      setMfa({ kind: "challenge", expiresInSeconds, id: Date.now(), methods });
    },
    [],
  );
  const passkey = usePasskeySignIn({
    // Autofill belongs to the password form: never under the email-code or
    // phone form, the recovery dialog or the second step.
    conditional:
      passkeyAutofill &&
      !usesEmailCode &&
      !usesPhone &&
      !recoverOpen &&
      mfa === null,
    enabled: passkeysActive,
    keepSignedIn: nativeSession && keepSignedIn,
    onAuthenticated,
    onMfaRequired: showMfaChallenge,
  });
  const stopPasskeyAutofill = passkey.stopBackground;

  /** A password or emailed code passed: cancel autofill, then step two. */
  const enterMfa = useCallback(
    async (methods: readonly MFAMethod[], verifiedIdentifier: string) => {
      setMfaIdentifier(verifiedIdentifier);
      await stopPasskeyAutofill();
      showMfaChallenge(methods);
    },
    [showMfaChallenge, stopPasskeyAutofill],
  );

  // `/sign-in?mfa=pending`: read the parked transaction (methods + lifetime).
  useEffect(() => {
    if (pendingMfaAttempt === 0) return;
    const controller = new AbortController();
    void (async () => {
      try {
        const state = await getPendingMFA({
          baseUrl: getApiBaseUrl(),
          signal: controller.signal,
        });
        if (controller.signal.aborted) return;
        if (state.methods.length === 0) {
          setMfa(null);
          setFailure({ kind: "mfaExpired" });
          return;
        }
        setMfa({
          kind: "challenge",
          expiresInSeconds: state.expires_in_seconds,
          id: Date.now(),
          methods: state.methods,
        });
      } catch (error) {
        if (controller.signal.aborted) return;
        if (error instanceof APIRequestError && error.status === 404) {
          // auth_mfa_unavailable: expired, used, or the feature is off.
          setMfa(null);
          setFailure({ kind: "mfaExpired" });
          return;
        }
        setMfa({ kind: "loadError" });
      }
    })();
    return () => controller.abort();
  }, [pendingMfaAttempt]);

  // Leaving the second step returns the caret to the first factor.
  useEffect(() => {
    if (refocusAfterMfa > 0) identifierRef.current?.focus();
  }, [refocusAfterMfa]);

  function leaveMfa(notice: AuthFailure | null) {
    setMfa(null);
    setFailure(notice);
    setRefocusAfterMfa((value) => value + 1);
  }

  useEffect(() => {
    if (autoFocus) identifierRef.current?.focus();
  }, [autoFocus]);

  useEffect(() => {
    if (methodSwitched && !usesEmailCode && !usesPhone) {
      identifierRef.current?.focus();
    }
  }, [methodSwitched, usesEmailCode, usesPhone]);

  function switchMethod(next: LoginMethod) {
    setFailure(null);
    setErrors({});
    setPassword("");
    setMethodSwitched(true);
    // AUTH-10B: passkey autofill is cancelled before the phone form can
    // send a code (leaving the password form also aborts it).
    if (next === "phone") void stopPasskeyAutofill();
    setMethod(next);
  }

  async function handlePasswordReset() {
    try {
      await onPasswordReset();
    } catch {
      // The server already revoked every session; never sign the user in here.
    }
    setPassword("");
    setFailure(null);
    setErrors({});
    setMethod("password");
    setPasswordResetDone(true);
    focusIdentifierOnCloseRef.current = true;
    setRecoverOpen(false);
  }

  function openLegal(tab: SignInLegalTab, event: MouseEvent<HTMLElement>) {
    legalReturnFocusRef.current = event.currentTarget;
    setLegalTab(tab);
    setLegalOpen(true);
  }

  function handleRememberChange(checked: boolean) {
    setRememberMe(checked);
    if (!checked) clearRememberedEmail();
  }

  /** Only the identifier is ever persisted, never the password or a token. */
  function rememberIdentifier(value: string) {
    if (rememberMe && value && value.length <= REMEMBERED_EMAIL_MAX_LENGTH) {
      writeRememberedEmail(value);
    } else {
      clearRememberedEmail();
    }
  }

  function clearError(field: LoginField) {
    setErrors((current) => {
      if (!current[field]) return current;
      const next = { ...current };
      delete next[field];
      return next;
    });
  }

  async function finishSession() {
    setFailure(null);
    setSubmitting(true);
    try {
      await onAuthenticated();
    } catch {
      // The session cookie exists; retry only the CSRF + /me hand-off.
      setFailure({ kind: "session" });
      setSubmitting(false);
    }
  }

  async function signInWithPassword() {
    const trimmed = identifier.trim();
    const nextErrors: LoginErrors = {};
    if (!trimmed) nextErrors.identifier = "errorIdentifier";
    if (!password) nextErrors.password = "errorPasswordRequired";
    setErrors(nextErrors);
    if (nextErrors.identifier) {
      identifierRef.current?.focus();
      return;
    }
    if (nextErrors.password) {
      passwordRef.current?.focus();
      return;
    }

    setFailure(null);
    setPasswordResetDone(false);
    setSubmitting(true);
    let result: NativeAuthenticationResponse;
    try {
      result = await loginWithPassword(
        { identifier: trimmed, password, keep_signed_in: keepSignedIn },
        { baseUrl: getApiBaseUrl() },
      );
    } catch (error) {
      const next = authFailureFrom(error);
      // A malformed identifier gets the same generic answer as a wrong password.
      const shown: AuthFailure =
        next.kind === "badRequest" ? { kind: "invalidCredentials" } : next;
      if (shown.kind === "invalidCredentials") {
        setPassword("");
        passwordRef.current?.focus();
      }
      const retryAfter = retryAfterOf(shown);
      if (retryAfter) lock.start(retryAfter);
      setFailure(shown);
      setSubmitting(false);
      return;
    }

    setPassword("");
    if (result.status === "mfa_required") {
      // The password is right; the session exists only after step two.
      await enterMfa(result.methods, trimmed);
      return;
    }
    rememberIdentifier(trimmed);
    await finishSession();
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (mode === "oidc") {
      rememberIdentifier(identifier.trim());
      setPassword("");
      onSignIn();
      return;
    }
    if (!isPassword || submitting || lock.remaining > 0) return;
    void signInWithPassword();
  }

  function errorFor(field: LoginField) {
    const key = errors[field];
    return key ? (
      <p className="sign-in-form__error" id={`${ids[field]}-error`}>
        {c[key]}
      </p>
    ) : null;
  }

  const submitDisabled =
    mode === "pending" || mode === "unavailable" || lock.remaining > 0;
  const submitLabel = submitting
    ? c.loggingIn
    : lock.remaining > 0
      ? formatCopy(c.submitWait, { label: c.logIn, seconds: lock.remaining })
      : c.logIn;
  const keepSignedInHint = nativeSession
    ? c.keepSignedInNativeHint
    : c.keepSignedInHint;
  const showMicrosoft = social.microsoft && onMicrosoftSignIn !== undefined;
  const showSocial = social.facebook || social.google || showMicrosoft;

  const keepSignedInOption = (
    <>
      <label className="sign-in-check" title={keepSignedInHint}>
        <input
          aria-describedby={ids.keepSignedInHint}
          checked={nativeSession && keepSignedIn}
          disabled={!nativeSession}
          name="keep-signed-in"
          onChange={(event) => setKeepSignedIn(event.target.checked)}
          type="checkbox"
        />
        <span>{c.keepSignedIn}</span>
      </label>
      <span className="visually-hidden" id={ids.keepSignedInHint}>
        {keepSignedInHint}
      </span>
    </>
  );
  const options = (
    <div className="sign-in-form__options">
      {keepSignedInOption}
      <label className="sign-in-check" title={c.rememberMeHint}>
        <input
          aria-describedby={ids.rememberHint}
          checked={rememberMe}
          name="remember-me"
          onChange={(event) => handleRememberChange(event.target.checked)}
          type="checkbox"
        />
        <span>{c.rememberMe}</span>
      </label>
      <span className="visually-hidden" id={ids.rememberHint}>
        {c.rememberMeHint}
      </span>
    </div>
  );
  // AUTH-10B: only an email or username is ever remembered, so the phone
  // form offers keep-signed-in alone.
  const phoneOptions = (
    <div className="sign-in-form__options">{keepSignedInOption}</div>
  );
  if (mfa) {
    return (
      <div className="sign-in-form">
        {mfa.kind === "loading" ? (
          <p
            className="sign-in-form__alert sign-in-form__alert--info"
            role="status"
          >
            {c.mfaLoading}
          </p>
        ) : mfa.kind === "loadError" ? (
          <div className="sign-in-form__alert" role="alert">
            <p>{c.mfaLoadError}</p>
            <button
              className="sign-in-button sign-in-button--outline"
              onClick={() => {
                setMfa({ kind: "loading" });
                setPendingMfaAttempt((value) => value + 1);
              }}
              type="button"
            >
              {c.retry}
            </button>
            <button
              className="sign-in-mfa__back"
              onClick={() => leaveMfa(null)}
              type="button"
            >
              {c.mfaBack}
            </button>
          </div>
        ) : (
          <SignInMfaStep
            expiresInSeconds={mfa.expiresInSeconds}
            key={mfa.id}
            methods={mfa.methods}
            onAuthenticated={async (options) => {
              if (mfaIdentifier) rememberIdentifier(mfaIdentifier);
              await onAuthenticated(options);
            }}
            onCancel={() => leaveMfa(null)}
            onExpired={() => leaveMfa({ kind: "mfaExpired" })}
            onPasswordReset={async () => {
              setMfa(null);
              await handlePasswordReset();
            }}
            passkeysEnabled={passkeysEnabled}
            purpose="signIn"
          />
        )}
      </div>
    );
  }

  const resetBanner = passwordResetDone ? (
    <p
      className="sign-in-form__alert sign-in-form__alert--success"
      role="status"
    >
      {c.passwordResetDone}
    </p>
  ) : null;
  const trimmedIdentifier = identifier.trim();

  return (
    <div className="sign-in-form">
      {usesEmailCode ? (
        <EmailCodeLoginForm
          autoFocus={methodSwitched || refocusAfterMfa > 0}
          initialEmail={
            EMAIL_PATTERN.test(trimmedIdentifier) ? trimmedIdentifier : ""
          }
          keepSignedIn={keepSignedIn}
          notice={
            resetBanner ?? (failure ? <FormAlert failure={failure} /> : null)
          }
          onAuthenticated={onAuthenticated}
          onMfaRequired={async (methods, email) => {
            setIdentifier(email);
            await enterMfa(methods, email);
          }}
          onVerified={(email) => {
            setPasswordResetDone(false);
            setIdentifier(email);
            rememberIdentifier(email);
          }}
          options={options}
          transactionOwnerRef={transactionOwnerRef}
        />
      ) : usesPhone ? (
        <SignInPhoneForm
          allowedCountries={phoneCountries}
          autoFocus={methodSwitched || refocusAfterMfa > 0}
          beforeRequest={stopPasskeyAutofill}
          keepSignedIn={keepSignedIn}
          notice={
            resetBanner ?? (failure ? <FormAlert failure={failure} /> : null)
          }
          onAuthenticated={async () => {
            setPasswordResetDone(false);
            await onAuthenticated();
          }}
          onMfaRequired={async (methods) => {
            setPasswordResetDone(false);
            // Nothing is remembered for a phone sign-in.
            await enterMfa(methods, "");
          }}
          options={phoneOptions}
          transactionOwnerRef={transactionOwnerRef}
        />
      ) : (
        <form
          aria-labelledby={ids.heading}
          className="sign-in-form__stack"
          noValidate
          onSubmit={handleSubmit}
        >
          <h2 className="visually-hidden" id={ids.heading}>
            {c.loginHeading}
          </h2>
          {resetBanner}
          {mode === "unavailable" && !emailCodeEnabled && !phoneAvailable ? (
            <p className="sign-in-form__alert" role="status">
              {c.loginUnavailable}
            </p>
          ) : null}
          <div className="sign-in-form__field">
            <input
              aria-describedby={
                errors.identifier ? `${ids.identifier}-error` : undefined
              }
              aria-invalid={errors.identifier ? true : undefined}
              aria-label={c.identifierLabel}
              // `webauthn` lets the browser offer passkeys in this field.
              autoComplete={passkeysEnabled ? "username webauthn" : "username"}
              className="sign-in-input"
              id={ids.identifier}
              maxLength={REMEMBERED_EMAIL_MAX_LENGTH}
              name="identifier"
              onChange={(event) => {
                setIdentifier(event.target.value);
                clearError("identifier");
              }}
              placeholder={c.identifierPlaceholder}
              ref={identifierRef}
              spellCheck={false}
              type="text"
              value={identifier}
            />
            {errorFor("identifier")}
          </div>
          <div className="sign-in-form__field">
            <input
              aria-describedby={
                errors.password ? `${ids.password}-error` : undefined
              }
              aria-invalid={errors.password ? true : undefined}
              aria-label={c.passwordLabel}
              autoComplete="current-password"
              className="sign-in-input"
              id={ids.password}
              name="password"
              onChange={(event) => {
                setPassword(event.target.value);
                clearError("password");
              }}
              placeholder={c.passwordPlaceholder}
              ref={passwordRef}
              type="password"
              value={password}
            />
            {errorFor("password")}
          </div>
          {failure ? (
            <FormAlert
              failure={failure}
              onRetrySession={() => void finishSession()}
            />
          ) : null}
          <button
            aria-disabled={submitting || undefined}
            className="sign-in-button sign-in-button--primary sign-in-form__submit"
            disabled={submitDisabled}
            type="submit"
          >
            {submitLabel}
          </button>
          {options}
        </form>
      )}

      {emailCodeEnabled || phoneAvailable ? (
        <div className="sign-in-form__methods">
          {usesEmailCode || usesPhone ? (
            <button
              className="sign-in-form__method"
              onClick={() => switchMethod("password")}
              type="button"
            >
              {c.usePassword}
            </button>
          ) : null}
          {emailCodeEnabled && !usesEmailCode ? (
            <button
              className="sign-in-form__method"
              onClick={() => switchMethod("emailCode")}
              type="button"
            >
              {c.useEmailCode}
            </button>
          ) : null}
          {phoneAvailable && !usesPhone ? (
            <button
              className="sign-in-form__method"
              onClick={() => switchMethod("phone")}
              type="button"
            >
              {c.usePhone}
            </button>
          ) : null}
        </div>
      ) : null}

      {passkeysActive ? (
        <div className="sign-in-form__passkey">
          <button
            // aria-disabled (not disabled) while waiting keeps keyboard focus
            // on the button when the device prompt is closed.
            aria-disabled={passkey.pending || undefined}
            className="sign-in-button sign-in-button--outline sign-in-button--passkey"
            disabled={passkey.lockRemaining > 0}
            onClick={() => {
              if (!passkey.pending) void passkey.signIn();
            }}
            type="button"
          >
            <KeyRound aria-hidden="true" className="sign-in-passkey-mark" />
            <span>
              {passkey.pending
                ? c.passkeyWaiting
                : passkey.lockRemaining > 0
                  ? formatCopy(c.submitWait, {
                      label: c.passkeySignIn,
                      seconds: passkey.lockRemaining,
                    })
                  : c.passkeySignIn}
            </span>
          </button>
          {/* Always mounted so assistive tech observes the cancel notice. */}
          <p
            aria-live="polite"
            className="sign-in-form__passkey-notice"
            role="status"
          >
            {passkey.cancelled ? c.passkeyCancelled : null}
          </p>
          {passkey.failure ? (
            <FormAlert
              failure={passkey.failure}
              onRetrySession={() => void passkey.retrySession()}
            />
          ) : null}
        </div>
      ) : null}

      {showSocial ? (
        <>
          <div className="sign-in-divider">
            <span>{c.or}</span>
          </div>

          <div className="sign-in-form__social">
            {social.facebook ? (
              <button
                className="sign-in-button sign-in-button--facebook"
                disabled={social.disabled}
                onClick={
                  social.facebookFederation && onFacebookSignIn
                    ? onFacebookSignIn
                    : onSignIn
                }
                type="button"
              >
                <FacebookMark />
                <span>{c.facebook}</span>
              </button>
            ) : null}
            {social.google ? (
              <button
                className="sign-in-button sign-in-button--google"
                disabled={social.disabled}
                onClick={
                  social.googleFederation && onGoogleSignIn
                    ? onGoogleSignIn
                    : onSignIn
                }
                type="button"
              >
                <GoogleMark />
                <span>{c.google}</span>
              </button>
            ) : null}
            {showMicrosoft ? (
              <button
                className="sign-in-button sign-in-button--microsoft"
                disabled={social.disabled}
                onClick={onMicrosoftSignIn}
                type="button"
              >
                <MicrosoftMark />
                <span>{c.microsoft}</span>
              </button>
            ) : null}
          </div>
          <p className="sign-in-form__social-legal">
            {c.socialLegalLead}
            <button
              aria-haspopup="dialog"
              className="sign-in-check__link"
              onClick={(event) => openLegal("agreement", event)}
              type="button"
            >
              {c.userAgreement}
            </button>
            {c.termsJoin}
            <button
              aria-haspopup="dialog"
              className="sign-in-check__link"
              onClick={(event) => openLegal("privacy", event)}
              type="button"
            >
              {c.privacyPolicy}
            </button>
            {c.socialLegalTail}
          </p>
          <SignInLegalDialog
            activeTab={legalTab}
            // Reading the terms here is informational; nothing is recorded.
            onAccept={() => undefined}
            onOpenChange={setLegalOpen}
            onTabChange={setLegalTab}
            open={legalOpen}
            returnFocusRef={legalReturnFocusRef}
          />
        </>
      ) : null}

      <Dialog onOpenChange={setRecoverOpen} open={recoverOpen}>
        <DialogTrigger asChild>
          <button className="sign-in-form__forgot" type="button">
            {c.forgotPassword}
          </button>
        </DialogTrigger>
        <DialogContent
          aria-modal="true"
          className="sign-in-recover"
          hideClose
          onCloseAutoFocus={(event) => {
            // After a reset the next step is logging in, not reopening recovery.
            if (!focusIdentifierOnCloseRef.current) return;
            focusIdentifierOnCloseRef.current = false;
            event.preventDefault();
            identifierRef.current?.focus();
          }}
        >
          {recoveryEnabled ? (
            <NativeRecoverPasswordForm
              onAuthenticated={onAuthenticated}
              onPasswordReset={handlePasswordReset}
              passkeysEnabled={passkeysEnabled}
              passwordMinLength={passwordMinLength}
              transactionOwnerRef={transactionOwnerRef}
            />
          ) : (
            <RecoverPasswordForm onSignIn={onSignIn} />
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}

interface EmailCodeLoginFormProps {
  autoFocus: boolean;
  initialEmail: string;
  keepSignedIn: boolean;
  /** Optional status shown above the fields (e.g. after a password reset). */
  notice: ReactNode;
  /** Runs after the server issued a native session: CSRF, /me, navigation. */
  onAuthenticated: () => Promise<void>;
  /** AUTH-09B: the code was right but the account has a second factor. */
  onMfaRequired: (
    methods: readonly MFAMethod[],
    email: string,
  ) => Promise<void>;
  /** Called once the code was accepted, before the session hand-off. */
  onVerified: (email: string) => void;
  /** Keep-signed-in / Remember Me checkboxes shared with the password form. */
  options: ReactNode;
  transactionOwnerRef: TransactionOwnerRef;
}

type EmailCodeField = "email" | "otp";
type EmailCodeErrors = Partial<Record<EmailCodeField, CopyKey>>;

/** AUTH-03: start → 6-digit code → verify, with the same neutral notices as sign-up. */
function EmailCodeLoginForm({
  autoFocus,
  initialEmail,
  keepSignedIn,
  notice,
  onAuthenticated,
  onMfaRequired,
  onVerified,
  options,
  transactionOwnerRef,
}: EmailCodeLoginFormProps) {
  const c = useSignInCopy();
  const ids = {
    heading: useId(),
    email: useId(),
    otp: useId(),
    codeNotice: useId(),
  };
  const emailRef = useRef<HTMLInputElement>(null);
  const otpRef = useRef<HTMLInputElement>(null);
  const [email, setEmail] = useState(initialEmail);
  const [otp, setOtp] = useState("");
  const [errors, setErrors] = useState<EmailCodeErrors>({});
  const [failure, setFailure] = useState<AuthFailure | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const code = useEmailChallenge(startEmailCodeLogin, transactionOwnerRef);
  const lock = useCountdown();

  useEffect(() => {
    if (autoFocus) emailRef.current?.focus();
  }, [autoFocus]);

  function clearError(field: EmailCodeField) {
    setErrors((current) => {
      if (!current[field]) return current;
      const next = { ...current };
      delete next[field];
      return next;
    });
  }

  async function sendCode(trimmedEmail: string) {
    const outcome = await code.request(trimmedEmail);
    if (outcome === "sent") {
      clearError("otp");
      otpRef.current?.focus();
    } else if (outcome === "invalidEmail") {
      setErrors((current) => ({ ...current, email: "errorEmail" }));
      emailRef.current?.focus();
    } else if (outcome === "failed") {
      emailRef.current?.focus();
    }
  }

  function handleGetCode() {
    const trimmedEmail = email.trim();
    if (!EMAIL_PATTERN.test(trimmedEmail)) {
      setErrors((current) => ({ ...current, email: "errorEmail" }));
      emailRef.current?.focus();
      return;
    }
    clearError("email");
    if (code.requesting || code.cooldownRemaining > 0) return;
    void sendCode(trimmedEmail);
  }

  async function finishSession() {
    setFailure(null);
    setSubmitting(true);
    try {
      await onAuthenticated();
    } catch {
      // The session cookie exists; retry only the CSRF + /me hand-off.
      setFailure({ kind: "session" });
      setSubmitting(false);
    }
  }

  async function verify(challengeId: string, trimmedEmail: string) {
    setFailure(null);
    setSubmitting(true);
    let result: NativeAuthenticationResponse;
    try {
      result = await verifyEmailCodeLogin(
        {
          challenge_id: challengeId,
          code: otp.trim(),
          keep_signed_in: keepSignedIn,
        },
        { baseUrl: getApiBaseUrl() },
      );
    } catch (error) {
      const next = authFailureFrom(error);
      if (next.kind === "badRequest" || next.kind === "invalidCredentials") {
        // One answer for a wrong, used or expired code (no account oracle).
        setOtp("");
        setFailure({ kind: "codeInvalid" });
        otpRef.current?.focus();
      } else {
        const retryAfter = retryAfterOf(next);
        if (retryAfter) lock.start(retryAfter);
        setFailure(next);
      }
      setSubmitting(false);
      return;
    }

    setOtp("");
    if (result.status === "mfa_required") {
      // The code is right; the session exists only after step two.
      setSubmitting(false);
      await onMfaRequired(result.methods, trimmedEmail);
      return;
    }
    onVerified(trimmedEmail);
    await finishSession();
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting || lock.remaining > 0) return;
    const trimmedEmail = email.trim();
    const nextErrors: EmailCodeErrors = {};
    if (!EMAIL_PATTERN.test(trimmedEmail)) nextErrors.email = "errorEmail";
    const otpError = codeErrorFor(code, otp);
    if (otpError) nextErrors.otp = otpError;
    setErrors(nextErrors);
    if (nextErrors.email) {
      emailRef.current?.focus();
      return;
    }
    if (nextErrors.otp || !code.challenge) {
      otpRef.current?.focus();
      return;
    }
    void verify(code.challenge.id, trimmedEmail);
  }

  function errorFor(field: EmailCodeField) {
    const key = errors[field];
    return key ? (
      <p className="sign-in-form__error" id={`${ids[field]}-error`}>
        {c[key]}
      </p>
    ) : null;
  }

  const codeNotice = challengeNoticeView(c, code);
  const emailDescribedBy =
    [errors.email ? `${ids.email}-error` : "", codeNotice ? ids.codeNotice : ""]
      .filter(Boolean)
      .join(" ") || undefined;
  const submitLabel = submitting
    ? c.loggingIn
    : lock.remaining > 0
      ? formatCopy(c.submitWait, { label: c.logIn, seconds: lock.remaining })
      : c.logIn;

  return (
    <form
      aria-labelledby={ids.heading}
      className="sign-in-form__stack"
      noValidate
      onSubmit={handleSubmit}
    >
      <h2 className="visually-hidden" id={ids.heading}>
        {c.emailCodeHeading}
      </h2>
      {notice}
      <div className="sign-in-form__field">
        <div className="sign-in-form__code-row">
          <input
            aria-describedby={emailDescribedBy}
            aria-invalid={errors.email ? true : undefined}
            aria-label={c.emailLabel}
            autoComplete="email"
            className="sign-in-input"
            id={ids.email}
            inputMode="email"
            maxLength={REMEMBERED_EMAIL_MAX_LENGTH}
            name="login-email"
            onChange={(event) => {
              setEmail(event.target.value);
              clearError("email");
              if (code.challenge || code.requesting) {
                code.invalidate();
                setOtp("");
              }
            }}
            placeholder={c.emailLabel}
            ref={emailRef}
            spellCheck={false}
            type="email"
            value={email}
          />
          <button
            aria-disabled={code.requesting || undefined}
            className="sign-in-button sign-in-button--code"
            disabled={code.cooldownRemaining > 0}
            onClick={handleGetCode}
            type="button"
          >
            {challengeButtonLabel(c, code)}
          </button>
        </div>
        {errorFor("email")}
        <div aria-live="polite">
          {codeNotice ? (
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
          ) : null}
        </div>
      </div>

      <div className="sign-in-form__field">
        <input
          aria-describedby={errors.otp ? `${ids.otp}-error` : undefined}
          aria-invalid={errors.otp ? true : undefined}
          aria-label={c.otpLabel}
          autoComplete="one-time-code"
          className="sign-in-input sign-in-input--otp"
          id={ids.otp}
          inputMode="numeric"
          maxLength={6}
          name="login-otp"
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

      {failure ? (
        <FormAlert
          failure={failure}
          onRetrySession={() => void finishSession()}
        />
      ) : null}
      <button
        aria-disabled={submitting || undefined}
        className="sign-in-button sign-in-button--primary sign-in-form__submit"
        disabled={lock.remaining > 0}
        type="submit"
      >
        {submitLabel}
      </button>
      {options}
    </form>
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
  mode: SignUpMode;
  /** Runs after the server issued a native session: CSRF, /me, navigation. */
  onAuthenticated: () => Promise<void>;
  /** Redirects to the default OIDC portal (BFF `/api/v1/auth/login`). */
  onSignIn: () => void;
  /** Reloads `/auth/providers` after the server reports new legal terms. */
  onTermsChanged: () => void;
  passwordMinLength: number;
  termsVersion: string | null;
}

interface SignUpChallenge {
  expiresInSeconds: number;
  id: string;
}

type CodeNotice =
  | { kind: "legacy" }
  | { kind: "sent"; minutes: number }
  | { kind: "failure"; failure: AuthFailure };

function SignUpForm({
  autoFocus,
  mode,
  onAuthenticated,
  onSignIn,
  onTermsChanged,
  passwordMinLength,
  termsVersion,
}: SignUpFormProps) {
  const c = useSignInCopy();
  const ids = {
    heading: useId(),
    fullName: useId(),
    email: useId(),
    otp: useId(),
    password: useId(),
    terms: useId(),
    termsText: useId(),
    codeNotice: useId(),
  };
  const fullNameRef = useRef<HTMLInputElement>(null);
  const emailRef = useRef<HTMLInputElement>(null);
  const otpRef = useRef<HTMLInputElement>(null);
  const passwordRef = useRef<HTMLInputElement>(null);
  const termsRef = useRef<HTMLInputElement>(null);
  const legalReturnFocusRef = useRef<HTMLElement | null>(null);
  /** Bumped whenever the email changes so a late code response is discarded. */
  const codeRequestGenerationRef = useRef(0);
  const [fullName, setFullName] = useState("");
  const [email, setEmail] = useState("");
  const [otp, setOtp] = useState("");
  const [password, setPassword] = useState("");
  const [acceptedTerms, setAcceptedTerms] = useState(false);
  const [errors, setErrors] = useState<SignUpErrors>({});
  const [codeNotice, setCodeNotice] = useState<CodeNotice | null>(null);
  const [challenge, setChallenge] = useState<SignUpChallenge | null>(null);
  const [challengeExpired, setChallengeExpired] = useState(false);
  const [requestingCode, setRequestingCode] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [failure, setFailure] = useState<AuthFailure | null>(null);
  const [submitted, setSubmitted] = useState(false);
  const [legalOpen, setLegalOpen] = useState(false);
  const [legalTab, setLegalTab] = useState<SignInLegalTab>("agreement");
  const cooldown = useCountdown();
  const submitLock = useCountdown();
  const isNative = mode === "native";
  const inactive = mode === "pending" || mode === "unavailable";

  useEffect(() => {
    if (autoFocus) fullNameRef.current?.focus();
  }, [autoFocus]);

  // Remind the user to resend once the server-side code lifetime has passed.
  useEffect(() => {
    if (!challenge) return;
    const timer = window.setTimeout(
      () => setChallengeExpired(true),
      Math.min(challenge.expiresInSeconds, MAX_RETRY_AFTER_SECONDS) * 1000,
    );
    return () => window.clearTimeout(timer);
  }, [challenge]);

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

  /** A code belongs to one email: editing the address forces a new request. */
  function invalidateChallenge() {
    codeRequestGenerationRef.current += 1;
    setChallenge(null);
    setChallengeExpired(false);
    setCodeNotice(null);
    setRequestingCode(false);
    setOtp("");
    cooldown.reset();
  }

  async function requestChallenge(
    trimmedEmail: string,
  ): Promise<AuthSignupChallengeResponse> {
    const options = { baseUrl: getApiBaseUrl() };
    if (!challenge) {
      return startEmailSignup({ email: trimmedEmail }, options);
    }
    try {
      // The destination stays bound to the encrypted server transaction.
      return await resendAuthChallenge(options);
    } catch (error) {
      if (error instanceof APIRequestError && error.status === 400) {
        // The sign-up transaction ended (TTL or restart): start a new one.
        return startEmailSignup({ email: trimmedEmail }, options);
      }
      throw error;
    }
  }

  async function sendCode(trimmedEmail: string) {
    const generation = ++codeRequestGenerationRef.current;
    const isStale = () => generation !== codeRequestGenerationRef.current;
    setRequestingCode(true);
    setCodeNotice(null);
    try {
      const result = await requestChallenge(trimmedEmail);
      // The email was edited meanwhile: this code belongs to the old address.
      if (isStale()) return;
      setChallenge({
        expiresInSeconds: result.expires_in_seconds,
        id: result.challenge_id,
      });
      setChallengeExpired(false);
      cooldown.start(result.cooldown_seconds);
      // Uniform wording: never reveal whether the address already has an account.
      setCodeNotice({
        kind: "sent",
        minutes: Math.max(1, Math.ceil(result.expires_in_seconds / 60)),
      });
      clearError("otp");
      focusField("otp");
    } catch (error) {
      if (isStale()) return;
      const next = authFailureFrom(error);
      if (next.kind === "badRequest") {
        setErrors((current) => ({ ...current, email: "errorEmail" }));
      } else {
        const retryAfter = retryAfterOf(next);
        if (retryAfter) cooldown.start(retryAfter);
        setCodeNotice({ kind: "failure", failure: next });
      }
      focusField("email");
    } finally {
      if (!isStale()) setRequestingCode(false);
    }
  }

  function handleGetCode() {
    const trimmedEmail = email.trim();
    if (!EMAIL_PATTERN.test(trimmedEmail)) {
      setErrors((current) => ({ ...current, email: "errorEmail" }));
      focusField("email");
      return;
    }
    clearError("email");
    if (mode === "oidc") {
      cooldown.start(OTP_COOLDOWN_SECONDS);
      setCodeNotice({ kind: "legacy" });
      focusField("otp");
      return;
    }
    if (!isNative || requestingCode || cooldown.remaining > 0) return;
    void sendCode(trimmedEmail);
  }

  function openLegal(tab: SignInLegalTab, event: MouseEvent<HTMLElement>) {
    legalReturnFocusRef.current = event.currentTarget;
    setLegalTab(tab);
    setLegalOpen(true);
  }

  function acceptLegal() {
    setAcceptedTerms(true);
    clearError("terms");
    legalReturnFocusRef.current = termsRef.current;
  }

  function validate() {
    const nextErrors: SignUpErrors = {};
    if (!fullName.trim()) nextErrors.fullName = "errorFullName";
    if (!EMAIL_PATTERN.test(email.trim())) nextErrors.email = "errorEmail";
    if (isNative && !challenge) {
      nextErrors.otp = "errorCodeRequired";
    } else if (isNative && challengeExpired) {
      nextErrors.otp = "errorCodeExpired";
    } else if (!OTP_PATTERN.test(otp.trim())) {
      nextErrors.otp = "errorOtp";
    }
    // Count code points like the server (which counts runes after NFKC).
    if (Array.from(password).length < passwordMinLength) {
      nextErrors.password = "errorPassword";
    }
    if (!acceptedTerms) nextErrors.terms = "errorTerms";
    return nextErrors;
  }

  async function finishSession() {
    setFailure(null);
    setSubmitting(true);
    try {
      await onAuthenticated();
    } catch {
      // The account and session exist; retry only the CSRF + /me hand-off.
      setFailure({ kind: "session" });
      setSubmitting(false);
    }
  }

  function handleCreateAccountFailure(next: AuthFailure) {
    switch (next.kind) {
      case "termsChanged":
        setAcceptedTerms(false);
        setErrors({ terms: "errorTermsChanged" });
        onTermsChanged();
        focusField("terms");
        return;
      case "passwordPolicy":
        // Keep name, email and code; only the rejected password is cleared.
        setPassword("");
        setErrors({ password: "errorPasswordPolicy" });
        focusField("password");
        return;
      case "badRequest":
      case "invalidCredentials":
        setOtp("");
        setFailure({ kind: "signUpFailed" });
        focusField("otp");
        return;
      default: {
        const retryAfter = retryAfterOf(next);
        if (retryAfter) submitLock.start(retryAfter);
        setFailure(next);
      }
    }
  }

  async function createAccount(challengeId: string, version: string) {
    setFailure(null);
    setSubmitting(true);
    try {
      await completeEmailSignup(
        {
          challenge_id: challengeId,
          code: otp.trim(),
          display_name: fullName.trim(),
          keep_signed_in: false,
          password,
          terms_version: version,
        },
        { baseUrl: getApiBaseUrl() },
      );
    } catch (error) {
      handleCreateAccountFailure(authFailureFrom(error));
      setSubmitting(false);
      return;
    }
    setPassword("");
    setOtp("");
    await finishSession();
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inactive || submitting || submitLock.remaining > 0) return;
    const nextErrors = validate();
    setErrors(nextErrors);

    const firstInvalid = SIGN_UP_FIELD_ORDER.find((field) => nextErrors[field]);
    if (firstInvalid) {
      setSubmitted(false);
      focusField(firstInvalid);
      return;
    }

    if (mode === "oidc") {
      setPassword("");
      setOtp("");
      setSubmitted(true);
      return;
    }
    if (challenge && termsVersion) {
      void createAccount(challenge.id, termsVersion);
    }
  }

  function errorFor(field: SignUpField) {
    const key = errors[field];
    return key ? (
      <p className="sign-in-form__error" id={`${ids[field]}-error`}>
        {formatCopy(c[key], { min: passwordMinLength })}
      </p>
    ) : null;
  }

  function describedBy(field: SignUpField, extra?: string) {
    const parts = [errors[field] ? `${ids[field]}-error` : "", extra ?? ""];
    const value = parts.filter(Boolean).join(" ");
    return value || undefined;
  }

  function codeNoticeView(): { error: boolean; text: string } | null {
    if (isNative && challenge && challengeExpired) {
      return { error: true, text: c.codeExpired };
    }
    if (!codeNotice) return null;
    switch (codeNotice.kind) {
      case "legacy":
        return { error: false, text: c.codeNotice };
      case "sent":
        return {
          error: false,
          text: formatCopy(c.codeSent, { minutes: codeNotice.minutes }),
        };
      case "failure":
        return { error: true, text: failureText(c, codeNotice.failure) };
    }
  }

  const notice = codeNoticeView();
  const codeButtonLabel = requestingCode
    ? c.sendingCode
    : cooldown.remaining > 0
      ? formatCopy(c.codeCountdown, { seconds: cooldown.remaining })
      : isNative && challenge
        ? c.resendCode
        : c.getCode;
  const createLabel = submitting
    ? c.creatingAccount
    : submitLock.remaining > 0
      ? formatCopy(c.submitWait, {
          label: c.createAccount,
          seconds: submitLock.remaining,
        })
      : c.createAccount;

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
        {mode === "unavailable" ? (
          <p className="sign-in-form__alert" role="status">
            {c.signUpUnavailable}
          </p>
        ) : null}

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
                notice ? ids.codeNotice : undefined,
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
                if (challenge || requestingCode) invalidateChallenge();
              }}
              placeholder={c.emailLabel}
              ref={emailRef}
              spellCheck={false}
              type="email"
              value={email}
            />
            <button
              aria-disabled={requestingCode || undefined}
              className="sign-in-button sign-in-button--code"
              disabled={cooldown.remaining > 0 || inactive}
              onClick={handleGetCode}
              type="button"
            >
              {codeButtonLabel}
            </button>
          </div>
          {errorFor("email")}
          <div aria-live="polite">
            {notice ? (
              <p
                className={
                  notice.error
                    ? "sign-in-form__hint sign-in-form__hint--error"
                    : "sign-in-form__hint"
                }
                id={ids.codeNotice}
              >
                {notice.text}
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
          <div className="sign-in-check sign-in-check--terms">
            <input
              aria-describedby={describedBy("terms")}
              aria-invalid={errors.terms ? true : undefined}
              aria-labelledby={ids.termsText}
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
            <p className="sign-in-check__text" id={ids.termsText}>
              <label htmlFor={ids.terms}>{c.termsLead}</label>
              <button
                aria-haspopup="dialog"
                className="sign-in-check__accent sign-in-check__link"
                onClick={(event) => openLegal("agreement", event)}
                type="button"
              >
                {c.userAgreement}
              </button>
              {c.termsJoin}
              <button
                aria-haspopup="dialog"
                className="sign-in-check__accent sign-in-check__link"
                onClick={(event) => openLegal("privacy", event)}
                type="button"
              >
                {c.privacyPolicy}
              </button>
            </p>
          </div>
          {errorFor("terms")}
          {errors.terms === "errorTermsChanged" ? (
            <button
              aria-haspopup="dialog"
              className="sign-in-check__link sign-in-form__review-terms"
              onClick={(event) => openLegal("agreement", event)}
              type="button"
            >
              {c.reviewTerms}
            </button>
          ) : null}
        </div>

        {failure ? (
          <FormAlert
            failure={failure}
            onRetrySession={() => void finishSession()}
          />
        ) : null}
        <button
          aria-disabled={submitting || undefined}
          className="sign-in-button sign-in-button--primary"
          disabled={inactive || submitLock.remaining > 0}
          type="submit"
        >
          {createLabel}
        </button>
      </form>

      <SignInLegalDialog
        activeTab={legalTab}
        onAccept={acceptLegal}
        onOpenChange={setLegalOpen}
        onTabChange={setLegalTab}
        open={legalOpen}
        returnFocusRef={legalReturnFocusRef}
      />

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
        {formatCopy(c[key], { min: MIN_PASSWORD_LENGTH })}
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

interface NativeRecoverPasswordFormProps {
  /** Only if the server unexpectedly issues a session after step two. */
  onAuthenticated: (options?: NativeSignInOptions) => Promise<void>;
  /** Clears client state, closes the dialog and asks the user to log in. */
  onPasswordReset: () => Promise<void>;
  passkeysEnabled: boolean;
  passwordMinLength: number;
  transactionOwnerRef: TransactionOwnerRef;
}

/**
 * AUTH-03 recovery: start → 6-digit code → new password. The server revokes
 * every session on success and this flow never signs the user in. AUTH-09B:
 * an account with TOTP or a passkey must pass a second step before the new
 * password is saved (`mfa_required` → `password_reset`).
 */
function NativeRecoverPasswordForm({
  onAuthenticated,
  onPasswordReset,
  passkeysEnabled,
  passwordMinLength,
  transactionOwnerRef,
}: NativeRecoverPasswordFormProps) {
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
  const [failure, setFailure] = useState<AuthFailure | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const code = useEmailChallenge(startPasswordRecovery, transactionOwnerRef);
  const lock = useCountdown();
  const [mfa, setMfa] = useState<{
    id: number;
    methods: readonly MFAMethod[];
  } | null>(null);

  /** The emailed code was used by the server: request a fresh one. */
  function leaveMfa(notice: AuthFailure | null) {
    setMfa(null);
    code.invalidate();
    setOtp("");
    setFailure(notice);
  }

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

  async function sendCode(trimmedEmail: string) {
    const outcome = await code.request(trimmedEmail);
    if (outcome === "sent") {
      clearError("otp");
      focusField("otp");
    } else if (outcome === "invalidEmail") {
      setErrors((current) => ({ ...current, email: "errorEmail" }));
      focusField("email");
    } else if (outcome === "failed") {
      focusField("email");
    }
  }

  function handleGetCode() {
    const trimmedEmail = email.trim();
    if (!EMAIL_PATTERN.test(trimmedEmail)) {
      setErrors((current) => ({ ...current, email: "errorEmail" }));
      focusField("email");
      return;
    }
    clearError("email");
    if (code.requesting || code.cooldownRemaining > 0) return;
    void sendCode(trimmedEmail);
  }

  async function resetPassword(challengeId: string) {
    setFailure(null);
    setSubmitting(true);
    let result: PasswordRecoveryResponse;
    try {
      result = await completePasswordRecovery(
        {
          challenge_id: challengeId,
          code: otp.trim(),
          new_password: newPassword,
        },
        { baseUrl: getApiBaseUrl() },
      );
    } catch (error) {
      const next = authFailureFrom(error);
      switch (next.kind) {
        case "passwordPolicy":
          // Keep the email and code; only the rejected password is cleared.
          setNewPassword("");
          setErrors({ newPassword: "errorPasswordPolicy" });
          focusField("newPassword");
          break;
        case "badRequest":
        case "invalidCredentials":
          setOtp("");
          setFailure({ kind: "codeInvalid" });
          focusField("otp");
          break;
        default: {
          const retryAfter = retryAfterOf(next);
          if (retryAfter) lock.start(retryAfter);
          setFailure(next);
        }
      }
      setSubmitting(false);
      return;
    }

    setOtp("");
    setNewPassword("");
    setSubmitting(false);
    if (result.status === "mfa_required") {
      // Nothing changed yet: the new password waits for step two.
      if (result.methods.length === 0) {
        setFailure({ kind: "mfaRequired" });
        return;
      }
      setMfa({ id: Date.now(), methods: result.methods });
      return;
    }
    await onPasswordReset();
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting || lock.remaining > 0) return;
    const nextErrors: RecoverErrors = {};
    if (!EMAIL_PATTERN.test(email.trim())) nextErrors.email = "errorEmail";
    const otpError = codeErrorFor(code, otp);
    if (otpError) nextErrors.otp = otpError;
    // Count code points like the server (which counts runes after NFKC).
    if (Array.from(newPassword).length < passwordMinLength) {
      nextErrors.newPassword = "errorPassword";
    }
    setErrors(nextErrors);

    const firstInvalid = RECOVER_FIELD_ORDER.find((field) => nextErrors[field]);
    if (firstInvalid) {
      focusField(firstInvalid);
      return;
    }
    if (code.challenge) void resetPassword(code.challenge.id);
  }

  function errorFor(field: RecoverField) {
    const key = errors[field];
    return key ? (
      <p className="sign-in-form__error" id={`${ids[field]}-error`}>
        {formatCopy(c[key], { min: passwordMinLength })}
      </p>
    ) : null;
  }

  function describedBy(field: RecoverField, extra?: string) {
    const parts = [errors[field] ? `${ids[field]}-error` : "", extra ?? ""];
    const value = parts.filter(Boolean).join(" ");
    return value || undefined;
  }

  if (mfa) {
    return (
      <SignInMfaStep
        key={mfa.id}
        methods={mfa.methods}
        onAuthenticated={onAuthenticated}
        onCancel={() => leaveMfa(null)}
        onExpired={() => leaveMfa({ kind: "mfaRecoveryExpired" })}
        onPasswordReset={onPasswordReset}
        passkeysEnabled={passkeysEnabled}
        purpose="recovery"
        variant="dialog"
      />
    );
  }

  const codeNotice = challengeNoticeView(c, code);
  const submitLabel = submitting
    ? c.resettingPassword
    : lock.remaining > 0
      ? formatCopy(c.submitWait, {
          label: c.resetPassword,
          seconds: lock.remaining,
        })
      : c.resetPassword;

  return (
    <>
      <DialogTitle className="sign-in-recover__title">
        {c.recoverTitle}
      </DialogTitle>
      <DialogDescription className="sign-in-recover__subtitle">
        {c.recoverSubtitleNative}
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
              codeNotice ? ids.codeNotice : undefined,
            )}
            aria-invalid={errors.email ? true : undefined}
            autoComplete="email"
            className="sign-in-input sign-in-input--dialog"
            id={ids.email}
            inputMode="email"
            maxLength={REMEMBERED_EMAIL_MAX_LENGTH}
            name="recovery-email"
            onChange={(event) => {
              setEmail(event.target.value);
              clearError("email");
              if (code.challenge || code.requesting) {
                code.invalidate();
                setOtp("");
              }
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
              aria-disabled={code.requesting || undefined}
              className="sign-in-button sign-in-button--dialog-code"
              disabled={code.cooldownRemaining > 0}
              onClick={handleGetCode}
              type="button"
            >
              {challengeButtonLabel(c, code)}
            </button>
          </div>
          {errorFor("otp")}
          <div aria-live="polite">
            {codeNotice ? (
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

        {failure ? <FormAlert failure={failure} /> : null}
        <button
          aria-disabled={submitting || undefined}
          className="sign-in-button sign-in-button--primary sign-in-recover__submit"
          disabled={lock.remaining > 0}
          type="submit"
        >
          {submitLabel}
        </button>
      </form>

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
