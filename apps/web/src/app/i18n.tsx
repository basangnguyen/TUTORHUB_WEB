/* eslint-disable react-refresh/only-export-components -- This context module intentionally exports its hook and language contract. */

import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type PropsWithChildren,
} from "react";

export const supportedLanguages = ["vi", "en"] as const;
export type Language = (typeof supportedLanguages)[number];

const messages = {
  vi: {
    "app.loading": "Đang mở không gian học tập...",
    "brand.product": "TutorHub",
    "brand.version": "V2",
    "nav.home": "Tổng quan",
    "nav.classrooms": "Lớp học",
    "nav.calendar": "Lịch",
    "nav.messages": "Tin nhắn",
    "nav.tasks": "Nhiệm vụ",
    "nav.drive": "Tài liệu",
    "nav.settings": "Thiết lập",
    "shell.navigation": "Điều hướng chính",
    "shell.openNavigation": "Mở điều hướng",
    "shell.closeNavigation": "Đóng điều hướng",
    "shell.language": "Ngôn ngữ",
    "shell.profile": "Hồ sơ thử nghiệm",
    "shell.role.teacher": "Giáo viên",
    "shell.role.student": "Học viên",
    "shell.role.guest": "Khách",
    "shell.role.admin": "Quản trị tổ chức",
    "shell.online": "Có kết nối",
    "shell.offline": "Đang ngoại tuyến",
    "shell.offlineMessage": "Một số dữ liệu có thể chưa được cập nhật.",
    "shell.retryConnection": "Thử lại kết nối",
    "shell.skip": "Chuyển đến nội dung chính",
    "auth.signInTitle": "Đăng nhập vào TutorHub",
    "auth.signInDescription":
      "Tiếp tục qua nhà cung cấp danh tính được tổ chức phê duyệt. Mật khẩu và token nhà cung cấp không được lưu trong trình duyệt TutorHub.",
    "auth.signInAction": "Tiếp tục đăng nhập",
    "auth.signInAgain": "Đăng nhập lại",
    "auth.signOut": "Đăng xuất",
    "auth.signedOutTitle": "Bạn đã đăng xuất",
    "auth.signedOutDescription":
      "Phiên TutorHub trên thiết bị này đã được thu hồi.",
    "auth.unavailableTitle": "Chưa thể kiểm tra phiên đăng nhập",
    "auth.unavailableDescription":
      "Dịch vụ xác thực hiện không sẵn sàng. Hãy kiểm tra kết nối hoặc thử lại sau.",
    "workspace.kicker": "Thiết lập không gian học tập",
    "workspace.createTitle": "Tạo workspace đầu tiên",
    "workspace.createDescription":
      "Workspace là phạm vi riêng cho thành viên, lớp học và dữ liệu của tổ chức. Bạn sẽ được gán quyền quản trị sau khi tạo.",
    "workspace.stepIdentity": "Xác lập không gian và quyền quản trị tổ chức.",
    "workspace.stepClasses": "Tạo lớp học trong đúng phạm vi dữ liệu.",
    "workspace.stepInvite": "Mời giáo viên và học viên ở bước tiếp theo.",
    "workspace.detailsTitle": "Thông tin workspace",
    "workspace.detailsDescription":
      "Tên có thể thay đổi sau; địa chỉ ngắn dùng để nhận diện workspace.",
    "workspace.nameLabel": "Tên tổ chức hoặc nhóm học",
    "workspace.namePlaceholder": "Ví dụ: Khoa Công nghệ thông tin",
    "workspace.slugLabel": "Địa chỉ workspace",
    "workspace.slugHelp": "Dùng 3–63 chữ thường, chữ số hoặc dấu gạch ngang.",
    "workspace.createAction": "Tạo workspace",
    "workspace.creating": "Đang tạo workspace...",
    "workspace.createError":
      "Chưa thể tạo workspace. Hãy kiểm tra lại thông tin.",
    "workspace.selectTitle": "Chọn workspace để tiếp tục",
    "workspace.selectDescription":
      "Mọi lớp học và quyền thao tác sẽ được giới hạn trong workspace đang chọn.",
    "workspace.selectError":
      "Chưa thể chuyển workspace. Hãy thử lại hoặc kiểm tra membership.",
    "workspace.switching": "Đang chuyển workspace...",
    "workspace.activeLabel": "Workspace đang hoạt động",
    "home.kicker": "Không gian học tập",
    "home.title": "Tổng quan hôm nay",
    "home.description":
      "Khung nền đã sẵn sàng để ghép các luồng lớp học, lịch, trao đổi và tài liệu.",
    "home.workspace": "Phiên làm việc",
    "home.workspaceValue": "Bản xem trước nội bộ",
    "home.role": "Vai trò hiển thị",
    "home.roleValue": "Giáo viên",
    "home.language": "Ngôn ngữ giao diện",
    "home.serviceTitle": "Trạng thái Core API",
    "home.serviceDescription":
      "Kiểm tra endpoint health qua TanStack Query; dữ liệu được cache ngắn hạn và có retry giới hạn.",
    "home.serviceLoading": "Đang kiểm tra Core API...",
    "home.serviceReady": "TutorHub API đã sẵn sàng · {environment}",
    "home.serviceError": "Không thể kết nối Core API.",
    "home.retry": "Kiểm tra lại",
    "home.nextTitle": "Khu vực đang được chuẩn bị",
    "home.nextDescription":
      "Các route dưới đây là khung làm việc, chưa thay thế chức năng nghiệp vụ của từng module.",
    "home.openModule": "Mở {module}",
    "page.comingSoon": "Khu vực này đang được chuẩn bị",
    "page.moduleDescription":
      "P1-02 chỉ thiết lập route, trạng thái giao diện và điểm gắn vertical slice. Logic nghiệp vụ sẽ được bổ sung trong task riêng.",
    "page.backToHome": "Quay về tổng quan",
    "state.forbiddenTitle": "Bạn chưa có quyền truy cập khu vực này",
    "state.forbiddenDescription":
      "Route guard đang minh họa hợp đồng giao diện. Việc kiểm soát quyền thực tế vẫn phải do backend xác nhận.",
    "state.notFoundTitle": "Không tìm thấy trang bạn yêu cầu",
    "state.notFoundDescription":
      "Đường dẫn có thể đã thay đổi hoặc chưa được triển khai.",
    "state.errorTitle": "Hệ thống chưa thể xử lý yêu cầu",
    "state.errorDescription":
      "Hãy thử lại. Nếu sự cố tiếp diễn, nhóm vận hành cần kiểm tra nhật ký phía máy chủ.",
    "state.offlineTitle": "Bạn đang ngoại tuyến",
    "state.offlineDescription":
      "Kết nối mạng chưa sẵn sàng. Dữ liệu cần máy chủ sẽ không được tải cho đến khi kết nối được khôi phục.",
    "state.goHome": "Về tổng quan",
    "state.retry": "Thử lại",
  },
  en: {
    "app.loading": "Opening your learning workspace...",
    "brand.product": "TutorHub",
    "brand.version": "V2",
    "nav.home": "Overview",
    "nav.classrooms": "Classrooms",
    "nav.calendar": "Calendar",
    "nav.messages": "Messages",
    "nav.tasks": "Tasks",
    "nav.drive": "Resources",
    "nav.settings": "Settings",
    "shell.navigation": "Primary navigation",
    "shell.openNavigation": "Open navigation",
    "shell.closeNavigation": "Close navigation",
    "shell.language": "Language",
    "shell.profile": "Preview profile",
    "shell.role.teacher": "Instructor",
    "shell.role.student": "Learner",
    "shell.role.guest": "Guest",
    "shell.role.admin": "Organization admin",
    "shell.online": "Online",
    "shell.offline": "Offline",
    "shell.offlineMessage": "Some data may be out of date.",
    "shell.retryConnection": "Retry connection",
    "shell.skip": "Skip to main content",
    "auth.signInTitle": "Sign in to TutorHub",
    "auth.signInDescription":
      "Continue through your organization's approved identity provider. TutorHub does not store provider passwords or tokens in the browser.",
    "auth.signInAction": "Continue to sign in",
    "auth.signInAgain": "Sign in again",
    "auth.signOut": "Sign out",
    "auth.signedOutTitle": "You are signed out",
    "auth.signedOutDescription":
      "The TutorHub session on this device has been revoked.",
    "auth.unavailableTitle": "We could not verify your session",
    "auth.unavailableDescription":
      "Authentication is currently unavailable. Check your connection or try again later.",
    "workspace.kicker": "Learning workspace setup",
    "workspace.createTitle": "Create your first workspace",
    "workspace.createDescription":
      "A workspace is the private boundary for your members, classes and organization data. You will become its administrator after creation.",
    "workspace.stepIdentity":
      "Establish the organization boundary and admin access.",
    "workspace.stepClasses": "Create classes inside the correct data boundary.",
    "workspace.stepInvite": "Invite instructors and learners in the next step.",
    "workspace.detailsTitle": "Workspace details",
    "workspace.detailsDescription":
      "The name can change later; the short address identifies this workspace.",
    "workspace.nameLabel": "Organization or learning group name",
    "workspace.namePlaceholder": "Example: School of Information Technology",
    "workspace.slugLabel": "Workspace address",
    "workspace.slugHelp": "Use 3–63 lowercase letters, numbers or hyphens.",
    "workspace.createAction": "Create workspace",
    "workspace.creating": "Creating workspace...",
    "workspace.createError":
      "We could not create the workspace. Check the details and try again.",
    "workspace.selectTitle": "Choose a workspace to continue",
    "workspace.selectDescription":
      "Classes and permissions are always limited to the selected workspace.",
    "workspace.selectError":
      "We could not switch workspaces. Try again or check your membership.",
    "workspace.switching": "Switching workspace...",
    "workspace.activeLabel": "Active workspace",
    "home.kicker": "Learning workspace",
    "home.title": "Today at a glance",
    "home.description":
      "The foundation is ready for classrooms, scheduling, communication and learning resources.",
    "home.workspace": "Workspace",
    "home.workspaceValue": "Internal preview",
    "home.role": "Displayed role",
    "home.roleValue": "Instructor",
    "home.language": "Interface language",
    "home.serviceTitle": "Core API status",
    "home.serviceDescription":
      "The health endpoint is checked through TanStack Query with a short cache and bounded retry.",
    "home.serviceLoading": "Checking Core API...",
    "home.serviceReady": "TutorHub API is ready · {environment}",
    "home.serviceError": "We could not connect to the Core API.",
    "home.retry": "Check again",
    "home.nextTitle": "Workspace sections in preparation",
    "home.nextDescription":
      "These routes provide the workspace frame only; they do not replace module business logic.",
    "home.openModule": "Open {module}",
    "page.comingSoon": "This workspace section is being prepared",
    "page.moduleDescription":
      "P1-02 establishes the route, interface states and vertical-slice attachment point. Business logic belongs to a separate task.",
    "page.backToHome": "Back to overview",
    "state.forbiddenTitle": "You do not have access to this area",
    "state.forbiddenDescription":
      "This route guard demonstrates a UI contract only. The backend must still enforce authorization.",
    "state.notFoundTitle": "We could not find that page",
    "state.notFoundDescription":
      "The address may have changed or the page has not been implemented yet.",
    "state.errorTitle": "The system could not process that request",
    "state.errorDescription":
      "Try again. If the issue continues, the operations team should review server logs.",
    "state.offlineTitle": "You are offline",
    "state.offlineDescription":
      "A network connection is not available. Server-backed data will load when connectivity returns.",
    "state.goHome": "Go to overview",
    "state.retry": "Try again",
  },
} as const satisfies Record<Language, Record<string, string>>;

export type TranslationKey = keyof (typeof messages)["vi"];

type TranslationValues = Record<string, string | number>;

interface I18nContextValue {
  language: Language;
  setLanguage: (language: Language) => void;
  t: (key: TranslationKey, values?: TranslationValues) => string;
}

const I18nContext = createContext<I18nContextValue | null>(null);

function formatMessage(template: string, values?: TranslationValues) {
  if (!values) {
    return template;
  }

  return template.replace(/\{(\w+)\}/g, (match, name: string) => {
    const value = values[name];
    return value === undefined ? match : String(value);
  });
}

export function I18nProvider({
  children,
  initialLanguage = "vi",
}: PropsWithChildren<{ initialLanguage?: Language }>) {
  const [language, setLanguage] = useState<Language>(initialLanguage);

  useEffect(() => {
    document.documentElement.lang = language;
  }, [language]);

  const value = useMemo<I18nContextValue>(
    () => ({
      language,
      setLanguage,
      t: (key, values) => formatMessage(messages[language][key], values),
    }),
    [language],
  );

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n() {
  const context = useContext(I18nContext);

  if (!context) {
    throw new Error("useI18n must be used inside I18nProvider.");
  }

  return context;
}
