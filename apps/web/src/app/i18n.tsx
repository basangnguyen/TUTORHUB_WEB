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
    "classroom.title": "Lớp học",
    "classroom.description":
      "Quản lý các lớp thuộc workspace đang hoạt động và mở thông tin chi tiết của từng lớp.",
    "classroom.createAction": "Tạo lớp học",
    "classroom.listTitle": "Danh sách lớp",
    "classroom.listDescription":
      "Dữ liệu được giới hạn theo workspace và quyền trong phiên hiện tại.",
    "classroom.loadingList": "Đang tải danh sách lớp",
    "classroom.loadingDetail": "Đang tải thông tin lớp học",
    "classroom.classCount": "{count} lớp",
    "classroom.createTitle": "Tạo lớp học mới",
    "classroom.createDescription":
      "Lớp được tạo ở trạng thái bản nháp trong workspace hiện tại.",
    "classroom.closeCreate": "Đóng biểu mẫu tạo lớp",
    "classroom.codeLabel": "Mã lớp",
    "classroom.codePlaceholder": "Ví dụ: SEC101",
    "classroom.codeHelp":
      "Dùng 3–32 chữ cái, chữ số, dấu gạch ngang hoặc gạch dưới.",
    "classroom.titleLabel": "Tên lớp",
    "classroom.titlePlaceholder": "Ví dụ: Cơ sở An toàn thông tin",
    "classroom.descriptionLabel": "Mô tả",
    "classroom.descriptionPlaceholder":
      "Thông tin ngắn giúp thành viên nhận biết lớp học.",
    "classroom.codeError": "Mã lớp chưa đúng định dạng.",
    "classroom.titleError": "Tên lớp phải có từ 1 đến 200 ký tự.",
    "classroom.descriptionError": "Mô tả không được vượt quá 4.000 ký tự.",
    "classroom.duplicateCodeError":
      "Mã lớp đã tồn tại trong workspace này. Hãy chọn mã khác.",
    "classroom.createForbiddenError":
      "Vai trò hiện tại không có quyền tạo lớp học.",
    "classroom.createError": "Chưa thể tạo lớp học. Hãy thử lại.",
    "classroom.cancelAction": "Hủy",
    "classroom.creating": "Đang tạo...",
    "classroom.createSubmit": "Tạo lớp",
    "classroom.emptyTitle": "Workspace chưa có lớp học",
    "classroom.emptyDescription":
      "Lớp đầu tiên sẽ xuất hiện tại đây sau khi được tạo.",
    "classroom.createFirstAction": "Tạo lớp đầu tiên",
    "classroom.noDescription": "Chưa có mô tả",
    "classroom.statusDraft": "Bản nháp",
    "classroom.statusActive": "Đang hoạt động",
    "classroom.statusArchived": "Đã lưu trữ",
    "classroom.updatedShort": "Cập nhật {date}",
    "classroom.backToList": "← Danh sách lớp",
    "classroom.informationTitle": "Thông tin lớp học",
    "classroom.workspaceLabel": "Workspace",
    "classroom.ownerLabel": "Người phụ trách",
    "classroom.ownerYou": "Bạn",
    "classroom.ownerMember": "Thành viên workspace",
    "classroom.createdLabel": "Ngày tạo",
    "classroom.updatedLabel": "Cập nhật gần nhất",
    "classroom.forbiddenTitle": "Bạn chưa có quyền xem lớp học",
    "classroom.forbiddenDescription":
      "Quyền truy cập được xác định từ membership trong workspace hiện tại.",
    "classroom.notFoundTitle": "Không tìm thấy lớp học",
    "classroom.notFoundDescription":
      "Lớp không tồn tại hoặc không thuộc workspace đang hoạt động.",
    "classroom.errorTitle": "Chưa thể tải dữ liệu lớp học",
    "classroom.errorDescription":
      "Kiểm tra kết nối rồi thử lại. Nếu lỗi tiếp diễn, cần kiểm tra Core API.",
    "classroom.joinRoomAction": "Vào phòng học trực tuyến",
    "media.prejoin.backToClass": "← Quay lại lớp học",
    "media.prejoin.kicker": "Kiểm tra trước khi vào phòng",
    "media.prejoin.title": "Phòng học trực tuyến",
    "media.prejoin.description":
      "Kiểm tra camera, micro và loa trước khi kết nối. Thiết lập thiết bị không được lưu trên trình duyệt.",
    "media.prejoin.classError": "Chưa thể tải thông tin lớp học.",
    "media.prejoin.unsupported":
      "Trình duyệt này không cung cấp đầy đủ API camera và micro. Hãy dùng phiên bản Chrome, Edge, Firefox hoặc Safari mới.",
    "media.prejoin.camera": "Camera",
    "media.prejoin.microphone": "Micro",
    "media.prejoin.displayName": "Tên hiển thị",
    "media.prejoin.join": "Vào phòng học",
    "media.prejoin.joining": "Đang kết nối...",
    "media.prejoin.joinError":
      "Chưa thể cấp quyền vào phòng. Kiểm tra kết nối rồi thử lại.",
    "media.prejoin.invalidClass": "Đường dẫn lớp học không hợp lệ.",
    "media.prejoin.cannotJoin": "Chưa thể vào phòng học",
    "media.prejoin.checkTitle": "Kiểm tra nhanh",
    "media.prejoin.checkHeading": "Thiết bị và kết nối",
    "media.prejoin.checkCamera": "Hình ảnh camera hiển thị rõ và đúng chiều.",
    "media.prejoin.checkMicrophone": "Mức âm thanh thay đổi khi bạn nói.",
    "media.prejoin.checkNetwork": "Kết nối mạng ổn định trước khi bắt đầu.",
    "media.prejoin.speakerTest": "Kiểm tra loa",
    "media.prejoin.speakerPlaying": "Đang phát âm kiểm tra...",
    "media.prejoin.speakerError": "Trình duyệt chưa thể phát âm kiểm tra.",
    "media.prejoin.listenOnlyTitle": "Tham gia ở chế độ chỉ nghe",
    "media.prejoin.listenOnlyDescription":
      "Vai trò hiện tại có thể xem và nghe phòng học nhưng không được phát camera, micro hoặc chia sẻ màn hình.",
    "media.prejoin.unavailableTitle": "Phòng học chưa sẵn sàng",
    "media.room.title": "Phòng học TutorHub",
    "media.room.participantCount": "{count} người tham gia",
    "media.room.participantGrid": "Khu vực video người tham gia",
    "media.room.listenOnly": "Chế độ chỉ nghe",
    "media.room.connecting": "Đang kết nối",
    "media.room.connected": "Đã kết nối",
    "media.room.reconnecting": "Đang khôi phục kết nối",
    "media.room.disconnected": "Đã ngắt kết nối",
    "media.room.failed": "Kết nối thất bại",
    "media.room.connectionError":
      "Không thể kết nối tới phòng học. Hãy quay lại bước kiểm tra và thử lại.",
    "media.room.deviceError":
      "Camera hoặc micro không khả dụng. Kiểm tra quyền của trình duyệt và thiết bị đang dùng.",
    "media.room.dismiss": "Đóng thông báo",
    "media.room.credentialMissing":
      "Thông tin kết nối đã hết hạn hoặc không còn trong bộ nhớ. TutorHub không lưu token phòng học trên trình duyệt.",
    "media.room.recoveryKicker": "Cần xác nhận lại thiết bị",
    "media.room.recoveryTitle": "Quay lại bước kiểm tra trước khi vào phòng",
    "media.room.returnToPrejoin": "Mở kiểm tra thiết bị",
    "media.room.disconnectedKicker": "Phiên phòng học đã kết thúc",
    "media.room.disconnectedTitle": "Bạn đã rời phòng học",
    "media.room.disconnectedDescription":
      "Bạn có thể vào lại bằng token mới hoặc quay về trang thông tin lớp.",
    "media.room.rejoin": "Vào lại phòng",
    "media.room.backToClass": "Quay về lớp học",
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
    "classroom.title": "Classrooms",
    "classroom.description":
      "Manage classes in the active workspace and open the details for each class.",
    "classroom.createAction": "Create class",
    "classroom.listTitle": "Class list",
    "classroom.listDescription":
      "Data is limited by the active workspace and current session permissions.",
    "classroom.loadingList": "Loading the class list",
    "classroom.loadingDetail": "Loading class information",
    "classroom.classCount": "{count} classes",
    "classroom.createTitle": "Create a class",
    "classroom.createDescription":
      "The class starts as a draft in the current workspace.",
    "classroom.closeCreate": "Close the create-class form",
    "classroom.codeLabel": "Class code",
    "classroom.codePlaceholder": "Example: SEC101",
    "classroom.codeHelp": "Use 3–32 letters, numbers, hyphens, or underscores.",
    "classroom.titleLabel": "Class name",
    "classroom.titlePlaceholder": "Example: Information Security Foundations",
    "classroom.descriptionLabel": "Description",
    "classroom.descriptionPlaceholder":
      "Add a short note that helps members identify this class.",
    "classroom.codeError": "The class code format is invalid.",
    "classroom.titleError": "The class name must contain 1–200 characters.",
    "classroom.descriptionError":
      "The description cannot exceed 4,000 characters.",
    "classroom.duplicateCodeError":
      "This class code already exists in the workspace. Choose another code.",
    "classroom.createForbiddenError":
      "Your current role cannot create classes.",
    "classroom.createError": "We could not create the class. Try again.",
    "classroom.cancelAction": "Cancel",
    "classroom.creating": "Creating...",
    "classroom.createSubmit": "Create class",
    "classroom.emptyTitle": "This workspace has no classes",
    "classroom.emptyDescription":
      "The first class will appear here after it is created.",
    "classroom.createFirstAction": "Create the first class",
    "classroom.noDescription": "No description",
    "classroom.statusDraft": "Draft",
    "classroom.statusActive": "Active",
    "classroom.statusArchived": "Archived",
    "classroom.updatedShort": "Updated {date}",
    "classroom.backToList": "← Class list",
    "classroom.informationTitle": "Class information",
    "classroom.workspaceLabel": "Workspace",
    "classroom.ownerLabel": "Owner",
    "classroom.ownerYou": "You",
    "classroom.ownerMember": "Workspace member",
    "classroom.createdLabel": "Created",
    "classroom.updatedLabel": "Last updated",
    "classroom.forbiddenTitle": "You cannot view these classes",
    "classroom.forbiddenDescription":
      "Access is determined by your membership in the active workspace.",
    "classroom.notFoundTitle": "Class not found",
    "classroom.notFoundDescription":
      "The class does not exist or is outside the active workspace.",
    "classroom.errorTitle": "We could not load classroom data",
    "classroom.errorDescription":
      "Check the connection and retry. If this continues, review the Core API.",
    "classroom.joinRoomAction": "Join live classroom",
    "media.prejoin.backToClass": "← Back to classroom",
    "media.prejoin.kicker": "Prejoin check",
    "media.prejoin.title": "Live classroom",
    "media.prejoin.description":
      "Check your camera, microphone and speaker before connecting. Device choices are not stored in the browser.",
    "media.prejoin.classError":
      "Classroom information is temporarily unavailable.",
    "media.prejoin.unsupported":
      "This browser does not provide the required camera and microphone APIs. Use a current Chrome, Edge, Firefox or Safari release.",
    "media.prejoin.camera": "Camera",
    "media.prejoin.microphone": "Microphone",
    "media.prejoin.displayName": "Display name",
    "media.prejoin.join": "Join classroom",
    "media.prejoin.joining": "Connecting...",
    "media.prejoin.joinError":
      "A room credential could not be issued. Check your connection and retry.",
    "media.prejoin.invalidClass": "The classroom address is invalid.",
    "media.prejoin.cannotJoin": "Unable to join the classroom",
    "media.prejoin.checkTitle": "Quick check",
    "media.prejoin.checkHeading": "Devices and connection",
    "media.prejoin.checkCamera":
      "The camera preview is clear and correctly oriented.",
    "media.prejoin.checkMicrophone": "The audio level changes when you speak.",
    "media.prejoin.checkNetwork":
      "Your network is stable before the session starts.",
    "media.prejoin.speakerTest": "Test speaker",
    "media.prejoin.speakerPlaying": "Playing test sound...",
    "media.prejoin.speakerError": "The browser could not play the test sound.",
    "media.prejoin.listenOnlyTitle": "Join in listen-only mode",
    "media.prejoin.listenOnlyDescription":
      "Your current role can watch and listen but cannot publish a camera, microphone or screen share.",
    "media.prejoin.unavailableTitle": "The classroom is not ready",
    "media.room.title": "TutorHub classroom",
    "media.room.participantCount": "{count} participants",
    "media.room.participantGrid": "Participant video area",
    "media.room.listenOnly": "Listen-only mode",
    "media.room.connecting": "Connecting",
    "media.room.connected": "Connected",
    "media.room.reconnecting": "Restoring connection",
    "media.room.disconnected": "Disconnected",
    "media.room.failed": "Connection failed",
    "media.room.connectionError":
      "The classroom could not be reached. Return to the prejoin check and try again.",
    "media.room.deviceError":
      "The camera or microphone is unavailable. Check browser permission and the selected device.",
    "media.room.dismiss": "Dismiss",
    "media.room.credentialMissing":
      "The room credential expired or is no longer in memory. TutorHub does not persist room tokens in browser storage.",
    "media.room.recoveryKicker": "Device confirmation required",
    "media.room.recoveryTitle": "Return to the prejoin check",
    "media.room.returnToPrejoin": "Open device check",
    "media.room.disconnectedKicker": "The classroom session has ended",
    "media.room.disconnectedTitle": "You left the classroom",
    "media.room.disconnectedDescription":
      "You can rejoin with a new token or return to the class information page.",
    "media.room.rejoin": "Rejoin room",
    "media.room.backToClass": "Back to classroom",
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
