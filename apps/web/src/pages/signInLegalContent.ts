import type { Language } from "../app/i18n";

/* UI-AUTH-02: bilingual User Agreement and Privacy Policy shown from the sign-up form. */

export const SIGN_IN_LEGAL_TABS = ["agreement", "privacy"] as const;
export type SignInLegalTab = (typeof SIGN_IN_LEGAL_TABS)[number];

export function isSignInLegalTab(value: string): value is SignInLegalTab {
  return (SIGN_IN_LEGAL_TABS as readonly string[]).includes(value);
}

export interface SignInLegalSection {
  readonly title: string;
  readonly paragraphs: readonly string[];
}

export interface SignInLegalDocument {
  readonly tabLabel: string;
  readonly highlights: readonly string[];
  readonly sections: readonly SignInLegalSection[];
}

export interface SignInLegalCopy {
  readonly meta: string;
  readonly title: string;
  readonly description: string;
  readonly tabListLabel: string;
  readonly highlightsTitle: string;
  readonly closeDialog: string;
  readonly close: string;
  readonly accept: string;
  readonly documents: Readonly<Record<SignInLegalTab, SignInLegalDocument>>;
}

const vi: SignInLegalCopy = {
  meta: "Phiên bản 2.0 · Cập nhật 16/11/2026",
  title: "Điều khoản & Chính sách TutorHub",
  description:
    "Vui lòng đọc kỹ Thỏa thuận người dùng và Chính sách quyền riêng tư trước khi tạo tài khoản TutorHub.",
  tabListLabel: "Chọn văn bản cần đọc",
  highlightsTitle: "Tóm tắt điểm chính",
  closeDialog: "Đóng hộp thoại điều khoản",
  close: "Đóng",
  accept: "Tôi đồng ý & Tiếp tục",
  documents: {
    agreement: {
      tabLabel: "Thỏa thuận người dùng",
      highlights: [
        "Tài khoản được tạo và xác thực tập trung qua cổng định danh OIDC + PKCE của TutorHub.",
        "Bạn chịu trách nhiệm bảo vệ thông tin truy cập và mọi hoạt động phát sinh từ phiên làm việc của mình.",
        "Hãy tôn trọng giảng viên và người học trong mọi kênh trao đổi của lớp học và phòng học trực tuyến.",
        "Thi có khóa thiết bị chỉ diễn ra qua ứng dụng Secure Exam chuyên biệt, không khóa máy trên trình duyệt web.",
      ],
      sections: [
        {
          title: "Phạm vi áp dụng & Vai trò sử dụng",
          paragraphs: [
            "TutorHub là nền tảng học trực tuyến web-first vận hành theo mô hình nhiều tổ chức: mỗi tổ chức có một Workspace (tenant) riêng với lớp học, lịch học và học liệu của mình.",
            "Thỏa thuận này áp dụng cho mọi người dùng TutorHub theo vai trò được cấp trong từng Workspace và lớp học: Quản trị viên, Giảng viên, Trợ giảng, Học sinh và Khách. Quyền thao tác của bạn phụ thuộc vào vai trò đó.",
          ],
        },
        {
          title: "Tài khoản & Xác thực tập trung",
          paragraphs: [
            "Việc đăng ký và đăng nhập được thực hiện qua cổng định danh tập trung của TutorHub theo chuẩn OIDC Authorization Code + PKCE. Trang đăng nhập này không tự gửi mã xác minh hay lưu mật khẩu của bạn.",
            "Bạn cần cung cấp thông tin chính xác, tự bảo vệ thông tin truy cập và chịu trách nhiệm về hoạt động phát sinh từ phiên làm việc của mình. Nếu nghi ngờ tài khoản bị truy cập trái phép, hãy đăng xuất và báo ngay cho Quản trị viên Workspace.",
          ],
        },
        {
          title: "Quy tắc ứng xử trong Lớp học & Phòng học trực tuyến",
          paragraphs: [
            "Hãy tôn trọng giảng viên và người học. Bạn không được quấy rối, mạo danh, phát tán mã độc, thư rác hoặc nội dung vi phạm bản quyền hay pháp luật qua chat, tệp đính kèm, micro, camera, chia sẻ màn hình hoặc bảng trắng.",
            "Chỉ chia sẻ nội dung phục vụ mục đích học tập và tôn trọng quyền riêng tư của những người cùng tham gia lớp học.",
          ],
        },
        {
          title: "Quản trị Workspace & Lớp học",
          paragraphs: [
            "Quản trị viên tổ chức và Giảng viên chủ lớp có quyền điều phối thành viên, mã mời, lịch học và kiểm duyệt phòng học trực tuyến trong phạm vi được cấp.",
            "Khi có vi phạm, họ có thể tạm ngưng quyền tham gia lớp học theo quy định của tổ chức. Mọi quyết định về quyền truy cập đều được máy chủ kiểm tra lại, không dựa vào thông tin do trình duyệt tự khai.",
          ],
        },
        {
          title: "Quyền sở hữu trí tuệ & Học liệu",
          paragraphs: [
            "Giảng viên và tổ chức giữ quyền đối với giáo trình, bài giảng và tài liệu do mình tải lên. Bạn chỉ sử dụng các học liệu này trong phạm vi lớp học được cấp quyền.",
            "Mã nguồn, giao diện và thương hiệu TutorHub thuộc quyền sở hữu của dự án TutorHub.",
          ],
        },
        {
          title: "Trợ lý học tập & Kỳ thi an toàn (Secure Exam)",
          paragraphs: [
            "Khi được bật cho Workspace của bạn, các tính năng hỗ trợ thông minh chỉ vận hành trong phạm vi quyền đã được cấp cho bạn và lớp học tương ứng.",
            "Các bài thi yêu cầu khóa thiết bị hoặc hệ điều hành được thực hiện qua ứng dụng Secure Exam chuyên biệt. TutorHub không khóa máy trực tiếp trên trình duyệt web.",
          ],
        },
        {
          title: "Thay đổi điều khoản & Chấm dứt sử dụng",
          paragraphs: [
            "TutorHub có thể cập nhật thỏa thuận này theo từng giai đoạn phát triển; phiên bản và ngày cập nhật mới nhất luôn được hiển thị ở đầu hộp thoại.",
            "Tài khoản có hành vi vi phạm nghiêm trọng có thể bị tạm ngưng hoặc chấm dứt quyền sử dụng.",
          ],
        },
      ],
    },
    privacy: {
      tabLabel: "Chính sách quyền riêng tư",
      highlights: [
        "Chỉ thu thập dữ liệu cần thiết cho tài khoản, lớp học và lịch học của bạn.",
        "Dữ liệu được cô lập theo từng tổ chức (tenant) và kiểm tra quyền phía máy chủ theo nguyên tắc mặc định từ chối.",
        "Phiên đăng nhập dùng cookie HttpOnly qua BFF; không lưu access token hay refresh token trong localStorage.",
        "TutorHub không bán hoặc chia sẻ dữ liệu người học cho bên thứ ba để quảng cáo.",
      ],
      sections: [
        {
          title: "Dữ liệu cá nhân & học tập được thu thập",
          paragraphs: [
            "Chúng tôi xử lý họ tên, địa chỉ email đã xác thực, tùy chọn ngôn ngữ và múi giờ, tư cách thành viên tổ chức và lớp học, lịch học, nội dung trao đổi học tập và các tệp tài liệu bạn chủ động gửi.",
          ],
        },
        {
          title: "Mục đích sử dụng dữ liệu",
          paragraphs: [
            "Dữ liệu được dùng để duy trì xác thực phiên làm việc, hiển thị đúng lịch học theo múi giờ của bạn, cung cấp phòng học trực tuyến, bảo đảm an toàn hệ thống và ghi nhật ký kiểm toán cho các thao tác quản trị.",
          ],
        },
        {
          title:
            "Cô lập dữ liệu theo Tổ chức (Tenant Isolation) & Phân quyền máy chủ",
          paragraphs: [
            "Mọi truy vấn dữ liệu đều được giới hạn chặt chẽ theo tenant_id ở phía máy chủ theo nguyên tắc mặc định từ chối (deny-by-default).",
            "Người dùng ngoài lớp học hoặc ngoài tổ chức không thể truy cập dữ liệu của bạn; vai trò và quyền luôn do máy chủ xác định, không dựa vào thông tin trình duyệt gửi lên.",
          ],
        },
        {
          title: "Cookie phiên & Bộ nhớ trình duyệt",
          paragraphs: [
            "Phiên đăng nhập sử dụng cookie bảo mật HttpOnly qua lớp BFF. TutorHub không lưu access token hay refresh token trong localStorage hoặc sessionStorage.",
            "Trang đăng nhập không lưu email, tên đăng nhập, số điện thoại hay mã OTP vào bộ nhớ cục bộ của trình duyệt (localStorage, sessionStorage). Việc điền sẵn thông tin, nếu có, do trình quản lý mật khẩu của trình duyệt đảm nhận theo lựa chọn của bạn.",
            "Phiên đăng nhập trên trang đăng nhập được duy trì tối đa 30 ngày trên thiết bị (tài khoản quản trị tổ chức: tối đa 24 giờ); thời hạn thực tế do máy chủ quyết định. Hãy đăng xuất sau khi dùng máy dùng chung.",
          ],
        },
        {
          title: "Hạ tầng vận hành & Giới hạn chia sẻ",
          paragraphs: [
            "Dữ liệu chỉ được xử lý ở mức tối thiểu qua các hạ tầng kỹ thuật lõi: cổng xác thực OIDC, cơ sở dữ liệu PostgreSQL, truyền thông thời gian thực LiveKit và lưu trữ tệp Backblaze B2 thông qua URL ký ngắn hạn do máy chủ cấp.",
            "TutorHub không bán hoặc chia sẻ dữ liệu người học cho bên thứ ba phục vụ quảng cáo.",
          ],
        },
        {
          title: "Lưu trữ, Nhật ký kiểm toán & Quyền của người dùng",
          paragraphs: [
            "Các thao tác quản trị quan trọng được ghi vào nhật ký kiểm toán bảo mật không cho phép chỉnh sửa hay xóa.",
            "Bạn có quyền xem và chỉnh sửa hồ sơ, quản lý các phương thức đăng nhập được liên kết hoặc rời khỏi lớp học ngay trong ứng dụng.",
          ],
        },
        {
          title: "Bảo vệ dữ liệu học sinh & Liên hệ",
          paragraphs: [
            "Chúng tôi giảm thiểu dữ liệu nhạy cảm trong nhật ký hệ thống và không ghi lại nội dung riêng tư của học sinh khi không cần thiết.",
            "Mọi thắc mắc về quyền riêng tư, vui lòng liên hệ Quản trị viên Workspace của bạn hoặc bộ phận hỗ trợ qua email support@tutorhub.local.",
          ],
        },
      ],
    },
  },
};

const en: SignInLegalCopy = {
  meta: "Version 2.0 · Updated 16 Nov 2026",
  title: "Terms & Privacy Policy",
  description:
    "Please review the User Agreement and Privacy Policy before creating your TutorHub account.",
  tabListLabel: "Choose a document to read",
  highlightsTitle: "Key highlights",
  closeDialog: "Close terms dialog",
  close: "Close",
  accept: "Agree & Continue",
  documents: {
    agreement: {
      tabLabel: "User Agreement",
      highlights: [
        "Accounts are created and verified centrally through the TutorHub OIDC + PKCE identity portal.",
        "You are responsible for protecting your sign-in details and for activity in your own sessions.",
        "Treat teachers and learners with respect in every class and online classroom channel.",
        "Device-locked exams run only in the dedicated Secure Exam app, never as a lockdown in the web browser.",
      ],
      sections: [
        {
          title: "Scope & User Roles",
          paragraphs: [
            "TutorHub is a web-first online learning platform built for multiple organizations: each organization has its own Workspace (tenant) with its own classes, schedules and learning materials.",
            "This agreement applies to every TutorHub user according to the role granted in each Workspace and class: Administrator, Teacher, Teaching Assistant, Student and Guest. What you can do depends on that role.",
          ],
        },
        {
          title: "Accounts & Central Authentication",
          paragraphs: [
            "Sign-up and sign-in happen through the central TutorHub identity portal using OIDC Authorization Code + PKCE. This sign-in page does not send verification codes or store your password itself.",
            "Provide accurate information, keep your sign-in details safe and take responsibility for activity in your own sessions. If you suspect unauthorized access, sign out and notify your Workspace Administrator right away.",
          ],
        },
        {
          title: "Conduct in Classes & Online Classrooms",
          paragraphs: [
            "Treat teachers and learners with respect. Do not harass, impersonate others, or spread malware, spam, or content that infringes copyright or breaks the law through chat, attachments, microphone, camera, screen sharing or the whiteboard.",
            "Share only content that serves learning, and respect the privacy of everyone in your class.",
          ],
        },
        {
          title: "Workspace & Class Administration",
          paragraphs: [
            "Organization Administrators and class-owning Teachers may manage members, invite codes, schedules and online classroom moderation within the permissions they hold.",
            "When rules are broken, they may suspend class participation under the organization's policies. Every access decision is re-checked by the server and never relies on what the browser claims.",
          ],
        },
        {
          title: "Intellectual Property & Learning Materials",
          paragraphs: [
            "Teachers and organizations keep their rights to the curricula, lessons and materials they upload. You may use those materials only within the classes you are allowed to access.",
            "TutorHub's source code, interface and brand belong to the TutorHub project.",
          ],
        },
        {
          title: "Learning Assistant & Secure Exam",
          paragraphs: [
            "When enabled for your Workspace, smart assistance features operate only within the permissions granted to you and the related class.",
            "Exams that require locking the device or operating system run through the dedicated Secure Exam app. TutorHub never locks your computer from the web browser.",
          ],
        },
        {
          title: "Changes to These Terms & Termination",
          paragraphs: [
            "TutorHub may update this agreement as the platform evolves; the latest version and update date are always shown at the top of this dialog.",
            "Accounts involved in serious violations may be suspended or have their access terminated.",
          ],
        },
      ],
    },
    privacy: {
      tabLabel: "Privacy Policy",
      highlights: [
        "We collect only the data needed for your account, classes and schedules.",
        "Data is isolated per organization (tenant) and authorized on the server, denying access by default.",
        "Sign-in sessions use HttpOnly cookies through the BFF; access and refresh tokens are never kept in localStorage.",
        "TutorHub does not sell or share learner data with third parties for advertising.",
      ],
      sections: [
        {
          title: "Personal & Learning Data We Collect",
          paragraphs: [
            "We process your full name, verified email address, language and time zone preferences, organization and class memberships, class schedules, learning conversations and the files you choose to share.",
          ],
        },
        {
          title: "How We Use Your Data",
          paragraphs: [
            "Your data keeps your session authenticated, shows class schedules in your own time zone, powers online classrooms, protects the platform and records audit logs for administrative actions.",
          ],
        },
        {
          title: "Tenant Isolation & Server-side Authorization",
          paragraphs: [
            "Every data query is strictly limited by tenant_id on the server and follows a deny-by-default principle.",
            "People outside your class or organization cannot access your data; roles and permissions are always decided by the server, never by what the browser sends.",
          ],
        },
        {
          title: "Session Cookies & Browser Storage",
          paragraphs: [
            "Your sign-in session uses secure HttpOnly cookies through the BFF layer. TutorHub never stores access tokens or refresh tokens in localStorage or sessionStorage.",
            "The sign-in page never stores your email, username, phone number or one-time codes in the browser's local storage (localStorage, sessionStorage). Any autofill comes from your browser's password manager, as you choose.",
            "Sessions started on the sign-in page stay active on the device for up to 30 days (organization administrator accounts: up to 24 hours); the server decides the final length. Sign out when you use a shared device.",
          ],
        },
        {
          title: "Infrastructure & Sharing Limits",
          paragraphs: [
            "Data is processed only as needed by our core technical infrastructure: the OIDC identity portal, the PostgreSQL database, LiveKit real-time media and Backblaze B2 file storage through short-lived signed URLs issued by the server.",
            "TutorHub does not sell or share learner data with third parties for advertising.",
          ],
        },
        {
          title: "Retention, Audit Logs & Your Rights",
          paragraphs: [
            "Important administrative actions are recorded in a secure audit log that cannot be edited or deleted.",
            "You can view and edit your profile, manage linked sign-in methods or leave a class directly in the app.",
          ],
        },
        {
          title: "Protecting Student Data & Contact",
          paragraphs: [
            "We minimize sensitive data in system logs and do not record students' private content unless it is necessary.",
            "For any privacy questions, contact your Workspace Administrator or our support team at support@tutorhub.local.",
          ],
        },
      ],
    },
  },
};

export const signInLegalContent: Readonly<Record<Language, SignInLegalCopy>> = {
  en,
  vi,
};
