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
    "nav.workspace": "Workspace",
    "nav.settings": "Thiết lập",
    "profile.kicker": "Tài khoản cá nhân",
    "profile.title": "Hồ sơ và danh tính",
    "profile.description":
      "Quản lý thông tin hiển thị và các phương thức đăng nhập được liên kết với tài khoản TutorHub.",
    "profile.loading": "Đang tải hồ sơ cá nhân",
    "profile.loadError": "Không thể tải hồ sơ cá nhân. Hãy thử lại.",
    "profile.loadErrorTitle": "Chưa thể mở hồ sơ",
    "profile.detailsTitle": "Thông tin hồ sơ",
    "profile.detailsDescription":
      "Tên hiển thị và múi giờ được dùng nhất quán trong lớp học, lịch và thông báo.",
    "profile.displayNameLabel": "Tên hiển thị",
    "profile.displayNameHint": "Tối đa 120 ký tự Unicode.",
    "profile.displayNameRequired": "Hãy nhập tên hiển thị.",
    "profile.displayNameTooLong": "Tên hiển thị không được vượt quá 120 ký tự.",
    "profile.localeLabel": "Ngôn ngữ ưu tiên",
    "profile.localeVietnamese": "Tiếng Việt",
    "profile.localeEnglish": "English",
    "profile.timezoneLabel": "Múi giờ",
    "profile.timezoneHint": "Dùng tên múi giờ IANA, ví dụ Asia/Ho_Chi_Minh.",
    "profile.timezoneRequired": "Hãy nhập múi giờ.",
    "profile.timezoneInvalid": "Múi giờ chưa đúng định dạng IANA.",
    "profile.avatarTitle": "Ảnh đại diện",
    "profile.avatarDescription":
      "Ảnh được lưu trong kho đối tượng; Core API chỉ lưu khóa tham chiếu, không lưu dữ liệu ảnh.",
    "profile.avatarPresent": "Đã có ảnh đại diện",
    "profile.avatarEmpty": "Chưa có ảnh đại diện",
    "profile.avatarRemove": "Gỡ ảnh",
    "profile.save": "Lưu thay đổi",
    "profile.saving": "Đang lưu...",
    "profile.saved": "Đã cập nhật hồ sơ.",
    "profile.saveError":
      "Không thể cập nhật hồ sơ. Hãy kiểm tra dữ liệu rồi thử lại.",
    "profile.reauthRequired":
      "Phiên xác thực không còn đủ mới cho thao tác bảo mật này.",
    "profile.identityTitle": "Danh tính đăng nhập",
    "profile.identityDescription":
      "Liên kết thêm nhà cung cấp danh tính hoặc thu hồi phương thức không còn sử dụng.",
    "profile.identityLink": "Liên kết danh tính",
    "profile.identityLinking": "Đang chuẩn bị liên kết...",
    "profile.identityLoading": "Đang tải danh tính liên kết",
    "profile.identityLoadError": "Không thể tải danh sách danh tính.",
    "profile.identityLoadErrorTitle": "Chưa thể tải danh tính",
    "profile.identityEmpty": "Chưa có danh tính liên kết",
    "profile.identityEmptyDescription":
      "Liên kết một nhà cung cấp danh tính để bảo vệ quyền truy cập tài khoản.",
    "profile.identityVerified": "Đã xác minh",
    "profile.identityUnverified": "Chưa xác minh",
    "profile.identityLastUsed": "Dùng gần nhất {date}",
    "profile.identityUnlink": "Hủy liên kết",
    "profile.identityUnlinking": "Đang hủy liên kết...",
    "profile.identityLastProtected":
      "Không thể hủy phương thức đăng nhập cuối cùng.",
    "profile.identityUnlinked": "Đã hủy liên kết danh tính.",
    "profile.identityActionError":
      "Không thể hoàn tất thao tác với danh tính. Hãy thử lại.",
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
    "workspace.createAnotherAction": "Tạo workspace mới",
    "workspace.createAnotherTitle": "Tạo thêm workspace",
    "workspace.createAnotherDescription":
      "Tạo một phạm vi tổ chức độc lập. Bạn sẽ trở thành quản trị viên và được chuyển sang workspace mới sau khi hoàn tất.",
    "workspace.createAnotherSuccess": "Đã tạo và chuyển sang workspace mới.",
    "workspace.createCloseLabel": "Đóng biểu mẫu tạo workspace",
    "workspace.selectTitle": "Chọn workspace để tiếp tục",
    "workspace.selectDescription":
      "Mọi lớp học và quyền thao tác sẽ được giới hạn trong workspace đang chọn.",
    "workspace.selectError":
      "Chưa thể chuyển workspace. Hãy thử lại hoặc kiểm tra membership.",
    "workspace.switching": "Đang chuyển workspace...",
    "workspace.activeLabel": "Workspace đang hoạt động",
    "workspace.noActive": "Bạn chưa có workspace đang hoạt động để chọn.",
    "workspace.managementLoading": "Đang tải thông tin workspace",
    "workspace.managementForbiddenTitle": "Bạn chưa thể xem workspace này",
    "workspace.managementForbiddenDescription":
      "Membership hiện tại không có quyền xem workspace đang hoạt động.",
    "workspace.managementLoadErrorTitle": "Chưa thể tải workspace",
    "workspace.managementLoadErrorDescription":
      "Kiểm tra kết nối rồi thử tải lại thông tin workspace.",
    "workspace.managementKicker": "Phạm vi tổ chức",
    "workspace.managementTitle": "Thông tin workspace",
    "workspace.managementDescription":
      "Xem phạm vi dữ liệu đang hoạt động, vai trò của bạn và cấu hình tổ chức.",
    "workspace.statusActive": "Đang hoạt động",
    "workspace.statusSuspended": "Tạm ngưng",
    "workspace.statusArchived": "Đã lưu trữ",
    "workspace.overviewTitle": "Tổng quan workspace",
    "workspace.overviewDescription":
      "Thông tin này được Core API giới hạn theo workspace đang hoạt động.",
    "workspace.localeLabel": "Ngôn ngữ mặc định",
    "workspace.timezoneLabel": "Múi giờ mặc định",
    "workspace.timezoneHelp": "Dùng tên múi giờ IANA, ví dụ Asia/Ho_Chi_Minh.",
    "workspace.roleLabel": "Vai trò của bạn",
    "workspace.updatedLabel": "Cập nhật gần nhất",
    "workspace.manageRestrictedTitle": "Chỉ quản trị viên được chỉnh sửa",
    "workspace.manageRestrictedDescription":
      "Bạn vẫn có thể xem thông tin workspace; thay đổi và lưu trữ cần quyền quản trị tổ chức.",
    "workspace.editTitle": "Cấu hình workspace",
    "workspace.editDescription":
      "Cập nhật tên, địa chỉ ngắn, ngôn ngữ và múi giờ mặc định.",
    "workspace.nameValidation": "Tên phải có từ 2 đến 120 ký tự.",
    "workspace.slugValidation":
      "Địa chỉ phải có 3–63 chữ thường, chữ số hoặc dấu gạch ngang.",
    "workspace.timezoneValidation": "Hãy nhập một múi giờ IANA hợp lệ.",
    "workspace.updateAction": "Lưu cấu hình",
    "workspace.updating": "Đang lưu...",
    "workspace.updateSuccess": "Đã cập nhật workspace.",
    "workspace.updateError": "Chưa thể cập nhật workspace. Hãy thử lại.",
    "workspace.updateForbidden":
      "Phiên hiện tại không còn quyền cập nhật workspace này.",
    "workspace.updateConflict":
      "Workspace đã được thay đổi ở nơi khác. Tải bản mới nhất trước khi lưu lại.",
    "workspace.reloadLatest": "Tải bản mới nhất",
    "workspace.archiveTitle": "Lưu trữ workspace",
    "workspace.archiveDescription":
      "Lưu trữ sẽ chặn thao tác nghiệp vụ mới nhưng không xóa dữ liệu lịch sử.",
    "workspace.archiveAction": "Lưu trữ workspace",
    "workspace.archiveCloseLabel": "Đóng xác nhận lưu trữ",
    "workspace.archiveConfirmTitle": "Xác nhận lưu trữ workspace",
    "workspace.archiveConfirmDescription":
      "Bạn sắp lưu trữ {name}. Hành động này sẽ xoay phiên và bỏ workspace khỏi phạm vi đang hoạt động.",
    "workspace.archiveWarning":
      "Bạn phải còn ít nhất một workspace đang hoạt động khác mà mình quản trị.",
    "workspace.archiveConfirmAction": "Xác nhận lưu trữ",
    "workspace.archiving": "Đang lưu trữ...",
    "workspace.archiveError": "Chưa thể lưu trữ workspace. Hãy thử lại.",
    "workspace.archiveForbidden":
      "Phiên hiện tại không có quyền lưu trữ workspace này.",
    "workspace.archiveConflict":
      "Workspace đã thay đổi hoặc đây là workspace quản trị cuối cùng. Tải dữ liệu mới nhất để kiểm tra.",
    "workspace.cancelAction": "Hủy",
    "workspace.listTitle": "Workspace của bạn",
    "workspace.listDescription":
      "Danh sách membership và trạng thái workspace thuộc tài khoản hiện tại.",
    "workspace.listLoading": "Đang tải danh sách workspace",
    "workspace.listErrorTitle": "Chưa thể tải danh sách workspace",
    "workspace.listErrorDescription":
      "Danh sách membership hiện không sẵn sàng. Hãy thử lại.",
    "workspace.listEmptyTitle": "Chưa có workspace",
    "workspace.listEmptyDescription":
      "Workspace sẽ xuất hiện tại đây sau khi membership được tạo.",
    "workspace.activeShort": "Đang chọn",
    "workspace.auditLink": "Xem nhật ký kiểm toán",
    "workspace.auditLinkDescription":
      "Theo dõi các thao tác quản trị nhạy cảm trong workspace này.",
    "audit.backToWorkspace": "← Quay lại workspace",
    "audit.kicker": "Bảo mật workspace",
    "audit.title": "Nhật ký hoạt động",
    "audit.description":
      "Lịch sử bất biến giúp quản trị viên truy vết thao tác nhạy cảm theo request và tài nguyên.",
    "audit.refresh": "Làm mới",
    "audit.forbiddenTitle": "Chỉ quản trị viên được xem nhật ký",
    "audit.forbiddenDescription":
      "Phiên hiện tại không có quyền audit.view trong workspace đang hoạt động.",
    "audit.filterTitle": "Bộ lọc nhật ký",
    "audit.filterDescription":
      "Thu hẹp theo thời gian, hành động, tài nguyên hoặc kết quả. Thời gian nhập theo múi giờ của thiết bị.",
    "audit.occurredFromLabel": "Từ thời điểm",
    "audit.occurredToLabel": "Đến trước thời điểm",
    "audit.actionFilterLabel": "Hành động",
    "audit.actionAll": "Tất cả hành động",
    "audit.outcomeFilterLabel": "Kết quả",
    "audit.outcomeAll": "Tất cả kết quả",
    "audit.outcomeSucceeded": "Thành công",
    "audit.outcomeDenied": "Bị từ chối",
    "audit.outcomeFailed": "Thất bại",
    "audit.resourceTypeLabel": "Loại tài nguyên",
    "audit.resourceTypeHint":
      "Ví dụ: tenant, class, class_enrollment hoặc class_member.",
    "audit.resourceIDLabel": "ID tài nguyên",
    "audit.resourceIDHint":
      "Nhập UUID cùng loại tài nguyên để tìm đúng đối tượng.",
    "audit.applyFilters": "Áp dụng bộ lọc",
    "audit.clearFilters": "Xóa bộ lọc",
    "audit.timeRangeError":
      "Khoảng thời gian không hợp lệ; thời điểm bắt đầu phải sớm hơn thời điểm kết thúc.",
    "audit.resourceTypeError":
      "Loại tài nguyên phải bắt đầu bằng chữ thường và chỉ chứa chữ, số, dấu gạch dưới hoặc dấu chấm.",
    "audit.resourceIDNeedsType":
      "Hãy nhập loại tài nguyên trước khi lọc theo ID.",
    "audit.resourceIDError": "ID tài nguyên phải là UUID hợp lệ.",
    "audit.resultsTitle": "Sự kiện kiểm toán",
    "audit.resultsDescription":
      "Sự kiện mới nhất được hiển thị trước và luôn giới hạn trong workspace hiện tại.",
    "audit.loadedCount": "Đã tải {count} sự kiện",
    "audit.loading": "Đang tải nhật ký hoạt động",
    "audit.errorTitle": "Chưa thể tải nhật ký",
    "audit.errorDescription":
      "Kiểm tra kết nối hoặc quyền truy cập rồi thử lại.",
    "audit.refreshError":
      "Chưa thể làm mới. Các sự kiện đã tải trước đó có thể chưa còn cập nhật.",
    "audit.emptyTitle": "Chưa có sự kiện kiểm toán",
    "audit.emptyDescription":
      "Các thao tác quản trị nhạy cảm sẽ xuất hiện tại đây sau khi được ghi nhận.",
    "audit.filteredEmptyTitle": "Không có sự kiện phù hợp",
    "audit.filteredEmptyDescription":
      "Hãy nới rộng khoảng thời gian hoặc xóa bớt bộ lọc.",
    "audit.tableCaption": "Danh sách sự kiện kiểm toán của workspace",
    "audit.timeColumn": "Thời gian",
    "audit.actorColumn": "Người thực hiện",
    "audit.actionColumn": "Hành động",
    "audit.resourceColumn": "Tài nguyên",
    "audit.outcomeColumn": "Kết quả",
    "audit.requestIDColumn": "Request ID",
    "audit.systemActor": "Hệ thống",
    "audit.unknownActor": "Người dùng không còn hiển thị",
    "audit.resourceUnavailable": "Không có ID",
    "audit.loadMore": "Tải thêm sự kiện",
    "audit.loadingMore": "Đang tải thêm...",
    "audit.loadMoreError":
      "Chưa thể tải trang tiếp theo. Các sự kiện hiện tại vẫn được giữ lại.",
    "audit.resource.tenant": "Workspace",
    "audit.resource.membershipInvitation": "Lời mời thành viên",
    "audit.resource.class": "Lớp học",
    "audit.resource.classEnrollment": "Enrollment lớp",
    "audit.resource.classInviteCode": "Mã mời lớp",
    "audit.resource.classMember": "Thành viên lớp",
    "audit.action.tenantCreate": "Tạo workspace",
    "audit.action.tenantUpdate": "Cập nhật workspace",
    "audit.action.tenantArchive": "Lưu trữ workspace",
    "audit.action.tenantSwitch": "Chuyển workspace đang hoạt động",
    "audit.action.membershipInvitationCreate": "Tạo lời mời thành viên",
    "audit.action.membershipInvitationRevoke": "Thu hồi lời mời thành viên",
    "audit.action.membershipInvitationAccept": "Chấp nhận lời mời thành viên",
    "audit.action.membershipInvitationExpire": "Lời mời thành viên hết hạn",
    "audit.action.classCreate": "Tạo lớp học",
    "audit.action.classUpdate": "Cập nhật lớp học",
    "audit.action.classArchive": "Lưu trữ lớp học",
    "audit.action.classRestore": "Khôi phục lớp học",
    "audit.action.classTransferOwnership": "Chuyển quyền sở hữu lớp",
    "audit.action.classEnrollmentEnroll": "Thêm thành viên vào lớp",
    "audit.action.classEnrollmentSuspend": "Tạm ngưng thành viên lớp",
    "audit.action.classEnrollmentRemove": "Xóa thành viên khỏi lớp",
    "audit.action.classEnrollmentJoin": "Tham gia lớp",
    "audit.action.classEnrollmentLeave": "Rời lớp",
    "audit.action.classEnrollmentUpdateRole": "Đổi vai trò trong lớp",
    "audit.action.classRosterBulk": "Thay đổi roster hàng loạt",
    "audit.action.classInviteCodeCreate": "Tạo mã mời lớp",
    "audit.action.classInviteCodeRevoke": "Thu hồi mã mời lớp",
    "audit.action.classInviteCodeExpire": "Mã mời lớp hết hạn",
    "audit.action.classInviteCodeExhaust": "Mã mời lớp hết lượt dùng",
    "invitation.adminTitle": "Lời mời thành viên",
    "invitation.adminDescription":
      "Mời thành viên vào workspace bằng liên kết dùng một lần có thời hạn.",
    "invitation.createAction": "Mời thành viên",
    "invitation.createTitle": "Tạo lời mời thành viên",
    "invitation.createDescription":
      "Chọn email và vai trò. Liên kết chấp nhận chỉ được hiển thị sau khi tạo.",
    "invitation.emailLabel": "Email người được mời",
    "invitation.emailValidation": "Hãy nhập một địa chỉ email hợp lệ.",
    "invitation.roleLabel": "Vai trò trong workspace",
    "invitation.createConfirmAction": "Tạo lời mời",
    "invitation.creating": "Đang tạo lời mời...",
    "invitation.createSuccess":
      "Đã tạo lời mời. Hãy sao chép liên kết trước khi đóng cửa sổ này.",
    "invitation.acceptURLLabel": "Liên kết chấp nhận dùng một lần",
    "invitation.copyAction": "Sao chép liên kết",
    "invitation.copySuccess": "Đã sao chép liên kết.",
    "invitation.copyManual":
      "Trình duyệt không cho phép sao chép tự động. Liên kết đã được chọn để bạn sao chép thủ công.",
    "invitation.listLoading": "Đang tải danh sách lời mời",
    "invitation.listEmptyTitle": "Chưa có lời mời",
    "invitation.listEmptyDescription":
      "Tạo lời mời đầu tiên để thêm giáo viên, học viên hoặc khách vào workspace.",
    "invitation.listErrorTitle": "Chưa thể tải lời mời",
    "invitation.listErrorDescription":
      "Kiểm tra kết nối rồi thử tải lại danh sách lời mời.",
    "invitation.listForbiddenTitle": "Bạn không còn quyền xem lời mời",
    "invitation.listForbiddenDescription":
      "Phiên hiện tại không có quyền quản lý thành viên của workspace này.",
    "invitation.statusPending": "Đang chờ",
    "invitation.statusAccepted": "Đã chấp nhận",
    "invitation.statusRevoked": "Đã thu hồi",
    "invitation.statusExpired": "Đã hết hạn",
    "invitation.expiresLabel": "Hết hạn:",
    "invitation.revokeAction": "Thu hồi",
    "invitation.revokeFor": "Thu hồi lời mời của {email}",
    "invitation.revokeConfirmTitle": "Thu hồi lời mời?",
    "invitation.revokeConfirmDescription":
      "Liên kết dành cho {email} sẽ không thể được sử dụng sau khi thu hồi.",
    "invitation.revokeConfirmAction": "Xác nhận thu hồi",
    "invitation.revoking": "Đang thu hồi...",
    "invitation.revokeSuccess": "Đã thu hồi lời mời của {email}.",
    "invitation.cancelAction": "Hủy",
    "invitation.dialogCloseLabel": "Đóng cửa sổ lời mời",
    "invitation.mutationForbidden":
      "Bạn không còn quyền thực hiện thao tác này.",
    "invitation.mutationConflict":
      "Trạng thái lời mời đã thay đổi hoặc email này đã có lời mời đang chờ. Hãy tải lại danh sách.",
    "invitation.mutationRateLimited":
      "Đã tạo quá nhiều lời mời trong thời gian ngắn. Hãy thử lại sau.",
    "invitation.mutationError":
      "Chưa thể hoàn tất thao tác lời mời. Hãy thử lại.",
    "invitation.publicTitle": "Lời mời tham gia workspace",
    "invitation.publicLoading": "Đang kiểm tra lời mời",
    "invitation.publicWorkspaceLabel": "Workspace",
    "invitation.publicEmailLabel": "Email được mời",
    "invitation.publicCheckingSession": "Đang kiểm tra phiên đăng nhập...",
    "invitation.publicSignInDescription":
      "Bạn cần đăng nhập bằng tài khoản phù hợp trước khi chấp nhận lời mời.",
    "invitation.publicSignInAction": "Đăng nhập TutorHub",
    "invitation.publicReopenLink":
      "Vì lý do bảo mật, hãy mở lại liên kết lời mời sau khi đăng nhập.",
    "invitation.publicAcceptAction": "Chấp nhận lời mời",
    "invitation.publicAccepting": "Đang chấp nhận...",
    "invitation.publicRetryAccept": "Thử chấp nhận lại",
    "invitation.publicSwitchAction": "Chuyển sang workspace này",
    "invitation.publicUseAnotherAccount": "Dùng tài khoản khác",
    "invitation.publicMismatch":
      "Tài khoản đang đăng nhập không khớp với email của lời mời này.",
    "invitation.publicSessionExpired":
      "Phiên đăng nhập đã hết hạn. Hãy đăng nhập rồi mở lại liên kết lời mời.",
    "invitation.publicAcceptedSessionExpired":
      "Phiên đăng nhập đã hết hạn. Hãy đăng nhập lại để chọn workspace vừa tham gia.",
    "invitation.publicAcceptError": "Chưa thể chấp nhận lời mời. Hãy thử lại.",
    "invitation.publicUnavailableTitle": "Lời mời không còn khả dụng",
    "invitation.publicUnavailableDescription":
      "Liên kết không hợp lệ, đã hết hạn, đã được dùng hoặc đã bị thu hồi.",
    "invitation.publicLoadErrorTitle": "Chưa thể kiểm tra lời mời",
    "invitation.publicLoadErrorDescription":
      "Kiểm tra kết nối rồi thử tải lại lời mời.",
    "invitation.publicOfflineDescription":
      "Kết nối mạng để kiểm tra và chấp nhận lời mời này.",
    "invitation.publicAcceptedTitle": "Đã tham gia workspace",
    "invitation.publicAcceptedDescription":
      "Tài khoản của bạn đã được thêm vào {tenant}. Bạn có thể tiếp tục vào TutorHub và chọn workspace này.",
    "invitation.publicWorkspaceFallback": "workspace được mời",
    "invitation.publicContinueAction": "Tiếp tục vào TutorHub",
    "classInvitation.title": "Tham gia lớp học",
    "classInvitation.description":
      "Liên kết bảo mật này cho phép bạn tham gia một lớp trong workspace đang hoạt động.",
    "classInvitation.checkingSession": "Đang kiểm tra phiên đăng nhập",
    "classInvitation.signInDescription":
      "Đăng nhập trước khi tham gia lớp. Vì lý do bảo mật, hãy mở lại liên kết sau khi đăng nhập.",
    "classInvitation.signInAction": "Đăng nhập TutorHub",
    "classInvitation.reopenLink":
      "TutorHub không lưu mã mời trong trình duyệt hoặc đưa mã vào URL đăng nhập.",
    "classInvitation.workspaceRequired":
      "Hãy chọn workspace phù hợp, sau đó mở lại liên kết mời.",
    "classInvitation.joinAction": "Tham gia lớp",
    "classInvitation.joining": "Đang tham gia...",
    "classInvitation.retryJoin": "Thử tham gia lại",
    "classInvitation.openDialog": "Tham gia bằng mã",
    "classInvitation.closeDialog": "Đóng biểu mẫu tham gia lớp",
    "classInvitation.tokenLabel": "Mã hoặc liên kết tham gia",
    "classInvitation.tokenHint":
      "Dán mã bắt đầu bằng thciv1_ hoặc liên kết TutorHub có mã trong phần #token. Mã chỉ được gửi trong nội dung yêu cầu và không được lưu trong trình duyệt.",
    "classInvitation.tokenPlaceholder": "thciv1_… hoặc liên kết tham gia",
    "classInvitation.tokenValidation":
      "Mã hoặc liên kết tham gia chưa đúng định dạng.",
    "classInvitation.joinedSuccess": "Đã tham gia lớp {title}.",
    "classInvitation.openJoinedClass": "Mở lớp vừa tham gia",
    "classInvitation.sessionExpired":
      "Phiên đăng nhập đã hết hạn. Hãy đăng nhập rồi mở lại liên kết.",
    "classInvitation.forbidden":
      "Workspace hiện tại không cho phép bạn dùng liên kết này.",
    "classInvitation.rateLimited":
      "Bạn đã thử quá nhiều lần. Hãy đợi một lúc rồi thử lại.",
    "classInvitation.joinError":
      "Chưa thể tham gia lớp. Hãy kiểm tra kết nối rồi thử lại.",
    "classInvitation.unavailableTitle": "Liên kết tham gia không khả dụng",
    "classInvitation.unavailableDescription":
      "Liên kết không hợp lệ, đã hết hạn, bị thu hồi, đã hết lượt hoặc lớp không còn hoạt động.",
    "classInvitation.offlineDescription":
      "Kết nối Internet để kiểm tra và tham gia lớp học.",
    "classRoster.title": "Danh sách lớp",
    "classRoster.description":
      "Tìm thành viên, xem vai trò và quản lý quyền trong phạm vi lớp học.",
    "classRoster.loadedCount": "Đã tải {count} thành viên",
    "classRoster.archivedNotice":
      "Lớp đã lưu trữ: danh sách vẫn xem được nhưng mọi thay đổi đều bị khóa.",
    "classRoster.searchLabel": "Tìm thành viên",
    "classRoster.searchPlaceholder": "Tên hiển thị hoặc email",
    "classRoster.searchAction": "Tìm kiếm",
    "classRoster.statusFilter": "Lọc theo trạng thái",
    "classRoster.statusAll": "Tất cả trạng thái",
    "classRoster.statusInvited": "Đã mời",
    "classRoster.statusActive": "Đang hoạt động",
    "classRoster.statusSuspended": "Tạm đình chỉ",
    "classRoster.statusLeft": "Đã rời lớp",
    "classRoster.statusRemoved": "Đã xóa",
    "classRoster.roleOwner": "Chủ lớp",
    "classRoster.roleCoTeacher": "Đồng giảng viên",
    "classRoster.roleTeachingAssistant": "Trợ giảng",
    "classRoster.roleStudent": "Học viên",
    "classRoster.loading": "Đang tải danh sách lớp",
    "classRoster.errorTitle": "Chưa thể tải danh sách lớp",
    "classRoster.errorDescription":
      "Hãy thử lại; dữ liệu đã tải trước đó sẽ không được dùng để quyết định quyền.",
    "classRoster.forbiddenTitle": "Bạn không còn quyền xem danh sách lớp",
    "classRoster.forbiddenDescription":
      "Quyền quản lý enrollment của bạn đã thay đổi.",
    "classRoster.pinnedOwner": "Chủ lớp được ghim",
    "classRoster.emptyTitle": "Chưa có thành viên enrollment",
    "classRoster.emptyDescription":
      "Chủ lớp được hiển thị riêng; hãy thêm học viên hoặc tạo liên kết tham gia.",
    "classRoster.filteredEmptyDescription":
      "Không có thành viên nào khớp từ khóa và trạng thái hiện tại.",
    "classRoster.bulkTitle": "Thao tác hàng loạt",
    "classRoster.selectedCount": "Đã chọn {count}/50",
    "classRoster.bulkActionLabel": "Chọn thao tác hàng loạt",
    "classRoster.assignCoTeacher": "Gán đồng giảng viên",
    "classRoster.assignTeachingAssistant": "Gán trợ giảng",
    "classRoster.assignStudent": "Gán học viên",
    "classRoster.suspendAction": "Tạm đình chỉ",
    "classRoster.removeAction": "Xóa khỏi lớp",
    "classRoster.applyBulk": "Áp dụng",
    "classRoster.selectionLimit": "Đã đạt giới hạn 50 thành viên mỗi lần.",
    "classRoster.selectColumn": "Chọn",
    "classRoster.memberColumn": "Thành viên",
    "classRoster.roleColumn": "Vai trò lớp",
    "classRoster.statusColumn": "Trạng thái",
    "classRoster.actionsColumn": "Thao tác",
    "classRoster.selectMember": "Chọn {name}",
    "classRoster.changeRoleFor": "Đổi vai trò lớp của {name}",
    "classRoster.noActions": "Không có thao tác hợp lệ",
    "classRoster.loadMore": "Tải thêm",
    "classRoster.loadingMore": "Đang tải thêm...",
    "classRoster.loadMoreError": "Chưa thể tải trang tiếp theo.",
    "classRoster.confirmTitle": "Xác nhận thay đổi danh sách lớp",
    "classRoster.confirmSingle": "Áp dụng “{action}” cho {name}?",
    "classRoster.confirmBulk": "Áp dụng “{action}” cho {count} thành viên?",
    "classRoster.confirmAction": "Xác nhận",
    "classRoster.cancelAction": "Hủy",
    "classRoster.closeDialog": "Đóng cửa sổ xác nhận danh sách lớp",
    "classRoster.applying": "Đang áp dụng...",
    "classRoster.roleUpdated": "Đã cập nhật vai trò lớp.",
    "classRoster.unchanged": "Vai trò đã ở trạng thái yêu cầu.",
    "classRoster.bulkResult":
      "Kết quả: {updated} cập nhật, {unchanged} không đổi, {failed} thất bại.",
    "classRoster.mutationForbidden":
      "Bạn không còn quyền thực hiện thay đổi này.",
    "classRoster.mutationNotFound":
      "Thành viên không còn tồn tại trong phạm vi lớp hiện tại.",
    "classRoster.mutationConflict":
      "Vai trò hoặc trạng thái đã thay đổi. Danh sách sẽ được tải lại.",
    "classRoster.mutationError":
      "Chưa thể hoàn tất thao tác. Danh sách sẽ được tải lại trước khi thử lại.",
    "classEnrollment.title": "Thành viên và mã mời",
    "classEnrollment.description":
      "Thêm học viên đang có trong workspace hoặc tạo liên kết tham gia có giới hạn.",
    "classEnrollment.inactiveDescription":
      "Kích hoạt lớp để thêm học viên hoặc tạo mã mời mới.",
    "classEnrollment.directTitle": "Thêm học viên trực tiếp",
    "classEnrollment.directDescription":
      "Nhập email của một thành viên đang hoạt động trong workspace.",
    "classEnrollment.emailLabel": "Email thành viên",
    "classEnrollment.emailValidation": "Hãy nhập một địa chỉ email hợp lệ.",
    "classEnrollment.enrollAction": "Thêm vào lớp",
    "classEnrollment.enrolling": "Đang thêm...",
    "classEnrollment.enrollSuccess": "Đã thêm học viên vào lớp.",
    "classEnrollment.inviteTitle": "Liên kết tham gia",
    "classEnrollment.inviteDescription":
      "Mỗi liên kết có thời hạn và số lượt sử dụng hữu hạn.",
    "classEnrollment.createAction": "Tạo liên kết",
    "classEnrollment.createTitle": "Tạo liên kết tham gia lớp",
    "classEnrollment.createDescription":
      "TutorHub chỉ hiển thị liên kết đầy đủ một lần sau khi tạo.",
    "classEnrollment.ttlLabel": "Thời hạn",
    "classEnrollment.ttlOneDay": "1 ngày",
    "classEnrollment.ttlSevenDays": "7 ngày",
    "classEnrollment.ttlThirtyDays": "30 ngày",
    "classEnrollment.usageLabel": "Số lượt sử dụng tối đa",
    "classEnrollment.usageValidation": "Số lượt phải từ 1 đến 1.000.",
    "classEnrollment.createConfirm": "Tạo liên kết",
    "classEnrollment.creating": "Đang tạo...",
    "classEnrollment.createSuccess":
      "Liên kết đã được tạo. Hãy sao chép trước khi đóng cửa sổ.",
    "classEnrollment.linkLabel":
      "Liên kết tham gia (chỉ hiển thị trong lần này)",
    "classEnrollment.copyAction": "Sao chép liên kết",
    "classEnrollment.copySuccess": "Đã sao chép liên kết.",
    "classEnrollment.copyManual":
      "Không thể sao chép tự động. Liên kết đã được chọn để bạn sao chép thủ công.",
    "classEnrollment.listLoading": "Đang tải mã mời",
    "classEnrollment.listEmptyTitle": "Chưa có liên kết tham gia",
    "classEnrollment.listEmptyDescription":
      "Tạo liên kết đầu tiên khi cần mời học viên vào lớp.",
    "classEnrollment.listErrorTitle": "Chưa thể tải mã mời",
    "classEnrollment.listErrorDescription":
      "Kiểm tra kết nối hoặc quyền truy cập rồi thử lại.",
    "classEnrollment.listForbiddenTitle": "Bạn không còn quyền quản lý lớp",
    "classEnrollment.listForbiddenDescription":
      "Quyền quản lý thành viên của lớp đã thay đổi.",
    "classEnrollment.statusActive": "Đang hoạt động",
    "classEnrollment.statusExhausted": "Đã hết lượt",
    "classEnrollment.statusExpired": "Đã hết hạn",
    "classEnrollment.statusRevoked": "Đã thu hồi",
    "classEnrollment.usageCount": "Đã dùng {used}/{limit} lượt",
    "classEnrollment.expiresLabel": "Hết hạn:",
    "classEnrollment.revokeAction": "Thu hồi",
    "classEnrollment.revokeCodeAction":
      "Thu hồi liên kết hết hạn lúc {expires}",
    "classEnrollment.revokeConfirmTitle": "Thu hồi liên kết?",
    "classEnrollment.revokeConfirmDescription":
      "Học viên chưa tham gia sẽ không thể tiếp tục dùng liên kết này.",
    "classEnrollment.revokeConfirm": "Xác nhận thu hồi",
    "classEnrollment.revoking": "Đang thu hồi...",
    "classEnrollment.revokeSuccess": "Đã thu hồi liên kết tham gia.",
    "classEnrollment.cancelAction": "Hủy",
    "classEnrollment.closeDialog": "Đóng cửa sổ quản lý liên kết",
    "classEnrollment.mutationForbidden":
      "Bạn không còn quyền quản lý thành viên của lớp.",
    "classEnrollment.mutationConflict":
      "Trạng thái lớp, học viên hoặc mã mời đã thay đổi. Hãy tải lại.",
    "classEnrollment.mutationNotFound":
      "Không tìm thấy thành viên hoặc mã mời trong lớp hiện tại.",
    "classEnrollment.mutationRateLimited":
      "Bạn đã thao tác quá nhanh. Hãy đợi rồi thử lại.",
    "classEnrollment.mutationError": "Chưa thể hoàn tất thao tác. Hãy thử lại.",
    "classEnrollment.leaveAction": "Rời lớp",
    "classEnrollment.leaveTitle": "Rời lớp học?",
    "classEnrollment.leaveDescription":
      "Lớp sẽ biến mất khỏi danh sách của bạn. Bạn cần liên kết còn hiệu lực hoặc giáo viên thêm lại để tham gia lại.",
    "classEnrollment.leaveConfirm": "Xác nhận rời lớp",
    "classEnrollment.leaving": "Đang rời lớp...",
    "classEnrollment.leaveForbidden":
      "Bạn không còn quyền rời lớp trong workspace hiện tại.",
    "classEnrollment.leaveNotFound":
      "Không tìm thấy enrollment đang hoạt động của bạn trong lớp.",
    "classEnrollment.leaveConflict":
      "Trạng thái enrollment đã thay đổi. Hãy tải lại lớp rồi thử lại.",
    "classEnrollment.leaveError": "Chưa thể rời lớp. Hãy thử lại.",
    "classroom.title": "Lớp học",
    "classroom.description":
      "Quản lý các lớp thuộc workspace đang hoạt động và mở thông tin chi tiết của từng lớp.",
    "classroom.createAction": "Tạo lớp học",
    "classroom.listTitle": "Danh sách lớp",
    "classroom.listDescription":
      "Dữ liệu được giới hạn theo workspace và quyền trong phiên hiện tại.",
    "classroom.loadingList": "Đang tải danh sách lớp",
    "classroom.loadingDetail": "Đang tải thông tin lớp học",
    "classroom.classCount": "Đã tải {count} lớp",
    "classroom.statusFilterLabel": "Lọc theo trạng thái",
    "classroom.statusFilterAll": "Tất cả trạng thái",
    "classroom.loadMore": "Tải thêm lớp",
    "classroom.loadingMore": "Đang tải thêm...",
    "classroom.loadMoreError":
      "Chưa thể tải trang tiếp theo. Danh sách hiện tại vẫn được giữ nguyên.",
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
    "classroom.timezoneLabel": "Múi giờ lớp học",
    "classroom.timezoneHelp": "Dùng tên múi giờ IANA, ví dụ Asia/Ho_Chi_Minh.",
    "classroom.descriptionLabel": "Mô tả",
    "classroom.descriptionPlaceholder":
      "Thông tin ngắn giúp thành viên nhận biết lớp học.",
    "classroom.codeError": "Mã lớp chưa đúng định dạng.",
    "classroom.titleError": "Tên lớp phải có từ 1 đến 200 ký tự.",
    "classroom.timezoneError": "Múi giờ chưa đúng định dạng IANA.",
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
    "classroom.emptyFilteredTitle": "Không có lớp phù hợp bộ lọc",
    "classroom.emptyFilteredDescription":
      "Chọn trạng thái khác để xem các lớp còn lại trong workspace.",
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
    "classroom.archivedLabel": "Ngày lưu trữ",
    "classroom.editTitle": "Chỉnh sửa lớp học",
    "classroom.editDescription":
      "Cập nhật thông tin lớp hoặc kích hoạt bản nháp. Thay đổi được bảo vệ bằng phiên bản dữ liệu.",
    "classroom.statusLabel": "Trạng thái lớp",
    "classroom.statusHelp":
      "Lớp đang hoạt động mới có thể cấp quyền vào phòng học trực tuyến.",
    "classroom.updateAction": "Lưu thay đổi",
    "classroom.updating": "Đang lưu...",
    "classroom.updateSuccess": "Đã cập nhật lớp học.",
    "classroom.updateConflict":
      "Lớp đã được thay đổi ở nơi khác. Tải bản mới nhất trước khi sửa tiếp.",
    "classroom.updateForbidden":
      "Phiên hiện tại không có quyền chỉnh sửa lớp này.",
    "classroom.updateError": "Chưa thể cập nhật lớp học. Hãy thử lại.",
    "classroom.reloadLatest": "Tải bản mới nhất",
    "classroom.archiveTitle": "Lưu trữ lớp học",
    "classroom.archiveDescription":
      "Đóng lớp khỏi hoạt động mới nhưng vẫn giữ danh sách thành viên và lịch sử.",
    "classroom.archiveAction": "Lưu trữ lớp",
    "classroom.archiveCloseLabel": "Đóng xác nhận lưu trữ lớp",
    "classroom.archiveConfirmTitle": "Xác nhận lưu trữ lớp",
    "classroom.archiveConfirmDescription":
      "Bạn sắp lưu trữ {name}. Thành viên sẽ không thể vào phòng học mới.",
    "classroom.archiveWarning":
      "Invite code mới và quyền vào LiveKit sẽ bị đóng cho đến khi lớp được khôi phục.",
    "classroom.archiveConfirmAction": "Xác nhận lưu trữ",
    "classroom.archiving": "Đang lưu trữ...",
    "classroom.archiveError": "Chưa thể lưu trữ lớp. Hãy thử lại.",
    "classroom.restoreTitle": "Khôi phục lớp học",
    "classroom.restoreDescription":
      "Mở lại lớp đã lưu trữ để tiếp tục chỉnh sửa và kích hoạt.",
    "classroom.restoreAction": "Khôi phục lớp",
    "classroom.restoreCloseLabel": "Đóng xác nhận khôi phục lớp",
    "classroom.restoreConfirmTitle": "Xác nhận khôi phục lớp",
    "classroom.restoreConfirmDescription":
      "Bạn sắp khôi phục {name} về trạng thái trước khi lưu trữ.",
    "classroom.restoreWarning":
      "Quyền vào phòng chỉ mở lại nếu lớp được khôi phục ở trạng thái đang hoạt động.",
    "classroom.restoreConfirmAction": "Xác nhận khôi phục",
    "classroom.restoring": "Đang khôi phục...",
    "classroom.restoreError": "Chưa thể khôi phục lớp. Hãy thử lại.",
    "classroom.lifecycleConflict":
      "Trạng thái lớp đã thay đổi. Hãy tải bản mới nhất.",
    "classroom.lifecycleForbidden":
      "Chỉ chủ lớp hoặc quản trị viên workspace được thực hiện thao tác này.",
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
    "media.prejoin.loadingClass": "Đang kiểm tra trạng thái lớp học",
    "media.prejoin.classError": "Chưa thể tải thông tin lớp học.",
    "media.prejoin.classInactive":
      "Chỉ lớp đang hoạt động mới có thể mở phòng học trực tuyến.",
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
    "nav.workspace": "Workspace",
    "nav.settings": "Settings",
    "profile.kicker": "Personal account",
    "profile.title": "Profile and identities",
    "profile.description":
      "Manage the profile shown across TutorHub and the sign-in methods linked to your account.",
    "profile.loading": "Loading your profile",
    "profile.loadError": "Your profile could not be loaded. Please try again.",
    "profile.loadErrorTitle": "Profile unavailable",
    "profile.detailsTitle": "Profile details",
    "profile.detailsDescription":
      "Your display name and timezone are used consistently in classes, calendars, and notifications.",
    "profile.displayNameLabel": "Display name",
    "profile.displayNameHint": "Up to 120 Unicode characters.",
    "profile.displayNameRequired": "Enter a display name.",
    "profile.displayNameTooLong":
      "The display name cannot exceed 120 characters.",
    "profile.localeLabel": "Preferred language",
    "profile.localeVietnamese": "Tiếng Việt",
    "profile.localeEnglish": "English",
    "profile.timezoneLabel": "Timezone",
    "profile.timezoneHint": "Use an IANA timezone, for example Europe/London.",
    "profile.timezoneRequired": "Enter a timezone.",
    "profile.timezoneInvalid": "Enter a valid IANA timezone.",
    "profile.avatarTitle": "Profile picture",
    "profile.avatarDescription":
      "The image is stored in object storage; Core API only keeps its object key.",
    "profile.avatarPresent": "Profile picture set",
    "profile.avatarEmpty": "No profile picture",
    "profile.avatarRemove": "Remove picture",
    "profile.save": "Save changes",
    "profile.saving": "Saving...",
    "profile.saved": "Profile updated.",
    "profile.saveError":
      "The profile could not be updated. Check the fields and try again.",
    "profile.reauthRequired":
      "Your authentication is no longer recent enough for this security-sensitive action.",
    "profile.identityTitle": "Sign-in identities",
    "profile.identityDescription":
      "Link another identity provider or revoke a sign-in method you no longer use.",
    "profile.identityLink": "Link identity",
    "profile.identityLinking": "Preparing link...",
    "profile.identityLoading": "Loading linked identities",
    "profile.identityLoadError": "The linked identities could not be loaded.",
    "profile.identityLoadErrorTitle": "Identities unavailable",
    "profile.identityEmpty": "No linked identities",
    "profile.identityEmptyDescription":
      "Link an identity provider to secure access to your account.",
    "profile.identityVerified": "Verified",
    "profile.identityUnverified": "Unverified",
    "profile.identityLastUsed": "Last used {date}",
    "profile.identityUnlink": "Unlink",
    "profile.identityUnlinking": "Unlinking...",
    "profile.identityLastProtected":
      "The last sign-in method cannot be removed.",
    "profile.identityUnlinked": "Identity unlinked.",
    "profile.identityActionError":
      "The identity action could not be completed. Please try again.",
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
    "workspace.createAnotherAction": "Create workspace",
    "workspace.createAnotherTitle": "Create another workspace",
    "workspace.createAnotherDescription":
      "Create an independent organization boundary. You will become its administrator and switch to it when creation finishes.",
    "workspace.createAnotherSuccess":
      "The new workspace was created and selected.",
    "workspace.createCloseLabel": "Close the create-workspace form",
    "workspace.selectTitle": "Choose a workspace to continue",
    "workspace.selectDescription":
      "Classes and permissions are always limited to the selected workspace.",
    "workspace.selectError":
      "We could not switch workspaces. Try again or check your membership.",
    "workspace.switching": "Switching workspace...",
    "workspace.activeLabel": "Active workspace",
    "workspace.noActive": "You do not have an active workspace to select.",
    "workspace.managementLoading": "Loading workspace information",
    "workspace.managementForbiddenTitle": "You cannot view this workspace",
    "workspace.managementForbiddenDescription":
      "Your current membership cannot view the active workspace.",
    "workspace.managementLoadErrorTitle": "Workspace unavailable",
    "workspace.managementLoadErrorDescription":
      "Check the connection and retry loading the workspace information.",
    "workspace.managementKicker": "Organization boundary",
    "workspace.managementTitle": "Workspace information",
    "workspace.managementDescription":
      "Review the active data boundary, your role, and organization settings.",
    "workspace.statusActive": "Active",
    "workspace.statusSuspended": "Suspended",
    "workspace.statusArchived": "Archived",
    "workspace.overviewTitle": "Workspace overview",
    "workspace.overviewDescription":
      "Core API limits this information to the active workspace.",
    "workspace.localeLabel": "Default language",
    "workspace.timezoneLabel": "Default timezone",
    "workspace.timezoneHelp":
      "Use an IANA timezone, for example Europe/London.",
    "workspace.roleLabel": "Your role",
    "workspace.updatedLabel": "Last updated",
    "workspace.manageRestrictedTitle": "Administrators manage this workspace",
    "workspace.manageRestrictedDescription":
      "You can still view workspace information; editing and archiving require organization administrator access.",
    "workspace.editTitle": "Workspace settings",
    "workspace.editDescription":
      "Update the name, short address, default language, and timezone.",
    "workspace.nameValidation": "The name must contain 2–120 characters.",
    "workspace.slugValidation":
      "The address must use 3–63 lowercase letters, numbers, or hyphens.",
    "workspace.timezoneValidation": "Enter a valid IANA timezone.",
    "workspace.updateAction": "Save settings",
    "workspace.updating": "Saving...",
    "workspace.updateSuccess": "Workspace updated.",
    "workspace.updateError": "The workspace could not be updated. Try again.",
    "workspace.updateForbidden":
      "Your current session can no longer update this workspace.",
    "workspace.updateConflict":
      "The workspace changed elsewhere. Load the latest version before saving again.",
    "workspace.reloadLatest": "Load latest version",
    "workspace.archiveTitle": "Archive workspace",
    "workspace.archiveDescription":
      "Archiving blocks new business activity without deleting historical data.",
    "workspace.archiveAction": "Archive workspace",
    "workspace.archiveCloseLabel": "Close archive confirmation",
    "workspace.archiveConfirmTitle": "Confirm workspace archive",
    "workspace.archiveConfirmDescription":
      "You are about to archive {name}. This rotates the session and removes the workspace from the active context.",
    "workspace.archiveWarning":
      "You must retain at least one other active workspace that you administer.",
    "workspace.archiveConfirmAction": "Confirm archive",
    "workspace.archiving": "Archiving...",
    "workspace.archiveError": "The workspace could not be archived. Try again.",
    "workspace.archiveForbidden":
      "Your current session cannot archive this workspace.",
    "workspace.archiveConflict":
      "The workspace changed or it is your final managed workspace. Load the latest data to check.",
    "workspace.cancelAction": "Cancel",
    "workspace.listTitle": "Your workspaces",
    "workspace.listDescription":
      "Memberships and workspace statuses associated with your account.",
    "workspace.listLoading": "Loading workspace list",
    "workspace.listErrorTitle": "Workspace list unavailable",
    "workspace.listErrorDescription":
      "Your membership list is currently unavailable. Try again.",
    "workspace.listEmptyTitle": "No workspaces",
    "workspace.listEmptyDescription":
      "Workspaces appear here after a membership is created.",
    "workspace.activeShort": "Selected",
    "workspace.auditLink": "View audit log",
    "workspace.auditLinkDescription":
      "Review security-sensitive administrative actions in this workspace.",
    "audit.backToWorkspace": "← Back to workspace",
    "audit.kicker": "Workspace security",
    "audit.title": "Activity audit log",
    "audit.description":
      "Immutable history helps administrators trace sensitive actions by request and resource.",
    "audit.refresh": "Refresh",
    "audit.forbiddenTitle": "Audit history is restricted to administrators",
    "audit.forbiddenDescription":
      "The current session does not have audit.view in the active workspace.",
    "audit.filterTitle": "Audit filters",
    "audit.filterDescription":
      "Narrow results by time, action, resource or outcome. Times use the device timezone.",
    "audit.occurredFromLabel": "Occurred from",
    "audit.occurredToLabel": "Occurred before",
    "audit.actionFilterLabel": "Action",
    "audit.actionAll": "All actions",
    "audit.outcomeFilterLabel": "Outcome",
    "audit.outcomeAll": "All outcomes",
    "audit.outcomeSucceeded": "Succeeded",
    "audit.outcomeDenied": "Denied",
    "audit.outcomeFailed": "Failed",
    "audit.resourceTypeLabel": "Resource type",
    "audit.resourceTypeHint":
      "Examples: tenant, class, class_enrollment or class_member.",
    "audit.resourceIDLabel": "Resource ID",
    "audit.resourceIDHint":
      "Enter a UUID together with its resource type to find one object.",
    "audit.applyFilters": "Apply filters",
    "audit.clearFilters": "Clear filters",
    "audit.timeRangeError":
      "The time range is invalid; the start must be earlier than the end.",
    "audit.resourceTypeError":
      "Resource type must start with a lowercase letter and contain only letters, numbers, underscores or dots.",
    "audit.resourceIDNeedsType":
      "Enter a resource type before filtering by its ID.",
    "audit.resourceIDError": "Resource ID must be a valid UUID.",
    "audit.resultsTitle": "Audit events",
    "audit.resultsDescription":
      "Newest events appear first and always remain inside the active workspace.",
    "audit.loadedCount": "{count} events loaded",
    "audit.loading": "Loading activity audit history",
    "audit.errorTitle": "Audit history unavailable",
    "audit.errorDescription":
      "Check the connection or current access, then try again.",
    "audit.refreshError":
      "Refresh failed. Previously loaded events may no longer be current.",
    "audit.emptyTitle": "No audit events yet",
    "audit.emptyDescription":
      "Security-sensitive administrative actions will appear after they are recorded.",
    "audit.filteredEmptyTitle": "No events match these filters",
    "audit.filteredEmptyDescription":
      "Expand the time range or clear one or more filters.",
    "audit.tableCaption": "Workspace audit event list",
    "audit.timeColumn": "Time",
    "audit.actorColumn": "Actor",
    "audit.actionColumn": "Action",
    "audit.resourceColumn": "Resource",
    "audit.outcomeColumn": "Outcome",
    "audit.requestIDColumn": "Request ID",
    "audit.systemActor": "System",
    "audit.unknownActor": "Unavailable user",
    "audit.resourceUnavailable": "No ID",
    "audit.loadMore": "Load more events",
    "audit.loadingMore": "Loading more...",
    "audit.loadMoreError":
      "The next page could not be loaded. Current events remain available.",
    "audit.resource.tenant": "Workspace",
    "audit.resource.membershipInvitation": "Member invitation",
    "audit.resource.class": "Class",
    "audit.resource.classEnrollment": "Class enrollment",
    "audit.resource.classInviteCode": "Class invitation code",
    "audit.resource.classMember": "Class member",
    "audit.action.tenantCreate": "Create workspace",
    "audit.action.tenantUpdate": "Update workspace",
    "audit.action.tenantArchive": "Archive workspace",
    "audit.action.tenantSwitch": "Switch active workspace",
    "audit.action.membershipInvitationCreate": "Create member invitation",
    "audit.action.membershipInvitationRevoke": "Revoke member invitation",
    "audit.action.membershipInvitationAccept": "Accept member invitation",
    "audit.action.membershipInvitationExpire": "Expire member invitation",
    "audit.action.classCreate": "Create class",
    "audit.action.classUpdate": "Update class",
    "audit.action.classArchive": "Archive class",
    "audit.action.classRestore": "Restore class",
    "audit.action.classTransferOwnership": "Transfer class ownership",
    "audit.action.classEnrollmentEnroll": "Enroll class member",
    "audit.action.classEnrollmentSuspend": "Suspend class member",
    "audit.action.classEnrollmentRemove": "Remove class member",
    "audit.action.classEnrollmentJoin": "Join class",
    "audit.action.classEnrollmentLeave": "Leave class",
    "audit.action.classEnrollmentUpdateRole": "Update class role",
    "audit.action.classRosterBulk": "Bulk roster change",
    "audit.action.classInviteCodeCreate": "Create class invitation code",
    "audit.action.classInviteCodeRevoke": "Revoke class invitation code",
    "audit.action.classInviteCodeExpire": "Expire class invitation code",
    "audit.action.classInviteCodeExhaust": "Exhaust class invitation code",
    "invitation.adminTitle": "Member invitations",
    "invitation.adminDescription":
      "Invite members to the workspace with an expiring one-time link.",
    "invitation.createAction": "Invite member",
    "invitation.createTitle": "Create member invitation",
    "invitation.createDescription":
      "Choose an email and role. The acceptance link is shown only after creation.",
    "invitation.emailLabel": "Invitee email",
    "invitation.emailValidation": "Enter a valid email address.",
    "invitation.roleLabel": "Workspace role",
    "invitation.createConfirmAction": "Create invitation",
    "invitation.creating": "Creating invitation...",
    "invitation.createSuccess":
      "Invitation created. Copy the link before closing this dialog.",
    "invitation.acceptURLLabel": "One-time acceptance link",
    "invitation.copyAction": "Copy link",
    "invitation.copySuccess": "Link copied.",
    "invitation.copyManual":
      "Automatic copy is unavailable. The link is selected so you can copy it manually.",
    "invitation.listLoading": "Loading member invitations",
    "invitation.listEmptyTitle": "No invitations yet",
    "invitation.listEmptyDescription":
      "Create the first invitation to add a teacher, student, or guest to this workspace.",
    "invitation.listErrorTitle": "Invitations unavailable",
    "invitation.listErrorDescription":
      "Check the connection and retry loading the invitation list.",
    "invitation.listForbiddenTitle": "You can no longer view invitations",
    "invitation.listForbiddenDescription":
      "The current session cannot manage members in this workspace.",
    "invitation.statusPending": "Pending",
    "invitation.statusAccepted": "Accepted",
    "invitation.statusRevoked": "Revoked",
    "invitation.statusExpired": "Expired",
    "invitation.expiresLabel": "Expires:",
    "invitation.revokeAction": "Revoke",
    "invitation.revokeFor": "Revoke invitation for {email}",
    "invitation.revokeConfirmTitle": "Revoke invitation?",
    "invitation.revokeConfirmDescription":
      "The link issued to {email} cannot be used after it is revoked.",
    "invitation.revokeConfirmAction": "Confirm revoke",
    "invitation.revoking": "Revoking...",
    "invitation.revokeSuccess": "Invitation for {email} revoked.",
    "invitation.cancelAction": "Cancel",
    "invitation.dialogCloseLabel": "Close invitation dialog",
    "invitation.mutationForbidden":
      "You no longer have permission to perform this action.",
    "invitation.mutationConflict":
      "The invitation changed or this email already has a pending invitation. Reload the list.",
    "invitation.mutationRateLimited":
      "Too many invitations were created recently. Try again later.",
    "invitation.mutationError":
      "The invitation action could not be completed. Try again.",
    "invitation.publicTitle": "Workspace invitation",
    "invitation.publicLoading": "Checking invitation",
    "invitation.publicWorkspaceLabel": "Workspace",
    "invitation.publicEmailLabel": "Invited email",
    "invitation.publicCheckingSession": "Checking sign-in status...",
    "invitation.publicSignInDescription":
      "Sign in with the matching account before accepting this invitation.",
    "invitation.publicSignInAction": "Sign in to TutorHub",
    "invitation.publicReopenLink":
      "For security, reopen the invitation link after signing in.",
    "invitation.publicAcceptAction": "Accept invitation",
    "invitation.publicAccepting": "Accepting...",
    "invitation.publicRetryAccept": "Retry acceptance",
    "invitation.publicSwitchAction": "Switch to this workspace",
    "invitation.publicUseAnotherAccount": "Use another account",
    "invitation.publicMismatch":
      "The signed-in account does not match this invitation's email.",
    "invitation.publicSessionExpired":
      "Your session expired. Sign in, then reopen the invitation link.",
    "invitation.publicAcceptedSessionExpired":
      "Your session expired. Sign in again to choose the workspace you joined.",
    "invitation.publicAcceptError":
      "The invitation could not be accepted. Try again.",
    "invitation.publicUnavailableTitle": "Invitation unavailable",
    "invitation.publicUnavailableDescription":
      "The link is invalid, expired, already used, or revoked.",
    "invitation.publicLoadErrorTitle": "Invitation check unavailable",
    "invitation.publicLoadErrorDescription":
      "Check the connection and retry loading the invitation.",
    "invitation.publicOfflineDescription":
      "Connect to the internet to check and accept this invitation.",
    "invitation.publicAcceptedTitle": "Workspace joined",
    "invitation.publicAcceptedDescription":
      "Your account was added to {tenant}. Continue to TutorHub and select this workspace.",
    "invitation.publicWorkspaceFallback": "the invited workspace",
    "invitation.publicContinueAction": "Continue to TutorHub",
    "classInvitation.title": "Join a class",
    "classInvitation.description":
      "This secure link lets you join a class in your active workspace.",
    "classInvitation.checkingSession": "Checking sign-in status",
    "classInvitation.signInDescription":
      "Sign in before joining. For security, reopen this link after sign-in.",
    "classInvitation.signInAction": "Sign in to TutorHub",
    "classInvitation.reopenLink":
      "TutorHub does not store the invite token or place it in the sign-in URL.",
    "classInvitation.workspaceRequired":
      "Select the matching workspace, then reopen the invitation link.",
    "classInvitation.joinAction": "Join class",
    "classInvitation.joining": "Joining...",
    "classInvitation.retryJoin": "Retry joining",
    "classInvitation.openDialog": "Join with a code",
    "classInvitation.closeDialog": "Close the join-class form",
    "classInvitation.tokenLabel": "Join code or link",
    "classInvitation.tokenHint":
      "Paste a code beginning with thciv1_ or a TutorHub link whose fragment contains #token. The code is sent only in the request body and is never stored by the browser.",
    "classInvitation.tokenPlaceholder": "thciv1_… or a join link",
    "classInvitation.tokenValidation": "Enter a valid join code or link.",
    "classInvitation.joinedSuccess": "You joined {title}.",
    "classInvitation.openJoinedClass": "Open the joined class",
    "classInvitation.sessionExpired":
      "Your session expired. Sign in, then reopen the link.",
    "classInvitation.forbidden":
      "Your current workspace does not allow this invitation.",
    "classInvitation.rateLimited":
      "Too many attempts were made. Wait a moment and retry.",
    "classInvitation.joinError":
      "The class could not be joined. Check the connection and retry.",
    "classInvitation.unavailableTitle": "Class invitation unavailable",
    "classInvitation.unavailableDescription":
      "The link is invalid, expired, revoked, exhausted, or the class is no longer active.",
    "classInvitation.offlineDescription":
      "Connect to the internet to check and join the class.",
    "classRoster.title": "Class roster",
    "classRoster.description":
      "Find members, review roles, and manage permissions within this class.",
    "classRoster.loadedCount": "{count} members loaded",
    "classRoster.archivedNotice":
      "This class is archived: the roster remains readable, but all changes are locked.",
    "classRoster.searchLabel": "Find a member",
    "classRoster.searchPlaceholder": "Display name or email",
    "classRoster.searchAction": "Search",
    "classRoster.statusFilter": "Filter by status",
    "classRoster.statusAll": "All statuses",
    "classRoster.statusInvited": "Invited",
    "classRoster.statusActive": "Active",
    "classRoster.statusSuspended": "Suspended",
    "classRoster.statusLeft": "Left",
    "classRoster.statusRemoved": "Removed",
    "classRoster.roleOwner": "Owner",
    "classRoster.roleCoTeacher": "Co-teacher",
    "classRoster.roleTeachingAssistant": "Teaching assistant",
    "classRoster.roleStudent": "Student",
    "classRoster.loading": "Loading class roster",
    "classRoster.errorTitle": "Class roster unavailable",
    "classRoster.errorDescription":
      "Retry the request; previously loaded data is never used to decide permissions.",
    "classRoster.forbiddenTitle": "You can no longer view this roster",
    "classRoster.forbiddenDescription":
      "Your enrollment-management access has changed.",
    "classRoster.pinnedOwner": "Pinned class owner",
    "classRoster.emptyTitle": "No enrollment-backed members",
    "classRoster.emptyDescription":
      "The owner is shown separately; add a learner or create a join link.",
    "classRoster.filteredEmptyDescription":
      "No members match the current search and status filter.",
    "classRoster.bulkTitle": "Bulk actions",
    "classRoster.selectedCount": "{count}/50 selected",
    "classRoster.bulkActionLabel": "Choose a bulk action",
    "classRoster.assignCoTeacher": "Assign co-teacher",
    "classRoster.assignTeachingAssistant": "Assign teaching assistant",
    "classRoster.assignStudent": "Assign student",
    "classRoster.suspendAction": "Suspend",
    "classRoster.removeAction": "Remove from class",
    "classRoster.applyBulk": "Apply",
    "classRoster.selectionLimit": "The 50-member operation limit is reached.",
    "classRoster.selectColumn": "Select",
    "classRoster.memberColumn": "Member",
    "classRoster.roleColumn": "Class role",
    "classRoster.statusColumn": "Status",
    "classRoster.actionsColumn": "Actions",
    "classRoster.selectMember": "Select {name}",
    "classRoster.changeRoleFor": "Change the class role for {name}",
    "classRoster.noActions": "No valid actions",
    "classRoster.loadMore": "Load more",
    "classRoster.loadingMore": "Loading more...",
    "classRoster.loadMoreError": "The next page could not be loaded.",
    "classRoster.confirmTitle": "Confirm roster change",
    "classRoster.confirmSingle": "Apply “{action}” to {name}?",
    "classRoster.confirmBulk": "Apply “{action}” to {count} members?",
    "classRoster.confirmAction": "Confirm",
    "classRoster.cancelAction": "Cancel",
    "classRoster.closeDialog": "Close roster confirmation",
    "classRoster.applying": "Applying...",
    "classRoster.roleUpdated": "The class role was updated.",
    "classRoster.unchanged": "The role already matched the requested value.",
    "classRoster.bulkResult":
      "Result: {updated} updated, {unchanged} unchanged, {failed} failed.",
    "classRoster.mutationForbidden":
      "You can no longer make this roster change.",
    "classRoster.mutationNotFound":
      "The member is no longer available in this class scope.",
    "classRoster.mutationConflict":
      "The role or enrollment state changed. The roster will be refreshed.",
    "classRoster.mutationError":
      "The action could not be completed. The roster will refresh before a retry.",
    "classEnrollment.title": "Members and invite links",
    "classEnrollment.description":
      "Add an existing workspace member or create a bounded class join link.",
    "classEnrollment.inactiveDescription":
      "Activate the class before adding learners or creating a new link.",
    "classEnrollment.directTitle": "Add a learner directly",
    "classEnrollment.directDescription":
      "Enter the email of an active member in this workspace.",
    "classEnrollment.emailLabel": "Member email",
    "classEnrollment.emailValidation": "Enter a valid email address.",
    "classEnrollment.enrollAction": "Add to class",
    "classEnrollment.enrolling": "Adding...",
    "classEnrollment.enrollSuccess": "The learner was added to the class.",
    "classEnrollment.inviteTitle": "Join links",
    "classEnrollment.inviteDescription":
      "Every link has a finite lifetime and usage limit.",
    "classEnrollment.createAction": "Create link",
    "classEnrollment.createTitle": "Create a class join link",
    "classEnrollment.createDescription":
      "TutorHub shows the complete link only once after creation.",
    "classEnrollment.ttlLabel": "Lifetime",
    "classEnrollment.ttlOneDay": "1 day",
    "classEnrollment.ttlSevenDays": "7 days",
    "classEnrollment.ttlThirtyDays": "30 days",
    "classEnrollment.usageLabel": "Maximum uses",
    "classEnrollment.usageValidation":
      "The usage limit must be between 1 and 1,000.",
    "classEnrollment.createConfirm": "Create link",
    "classEnrollment.creating": "Creating...",
    "classEnrollment.createSuccess":
      "The link is ready. Copy it before closing this dialog.",
    "classEnrollment.linkLabel": "Class join link (shown only this time)",
    "classEnrollment.copyAction": "Copy link",
    "classEnrollment.copySuccess": "Link copied.",
    "classEnrollment.copyManual":
      "Automatic copy is unavailable. The link is selected for manual copy.",
    "classEnrollment.listLoading": "Loading invite codes",
    "classEnrollment.listEmptyTitle": "No class join links",
    "classEnrollment.listEmptyDescription":
      "Create the first link when learners need to join this class.",
    "classEnrollment.listErrorTitle": "Invite codes unavailable",
    "classEnrollment.listErrorDescription":
      "Check the connection or your access, then retry.",
    "classEnrollment.listForbiddenTitle": "You can no longer manage this class",
    "classEnrollment.listForbiddenDescription":
      "Your class enrollment-management access changed.",
    "classEnrollment.statusActive": "Active",
    "classEnrollment.statusExhausted": "Exhausted",
    "classEnrollment.statusExpired": "Expired",
    "classEnrollment.statusRevoked": "Revoked",
    "classEnrollment.usageCount": "{used}/{limit} uses consumed",
    "classEnrollment.expiresLabel": "Expires:",
    "classEnrollment.revokeAction": "Revoke",
    "classEnrollment.revokeCodeAction": "Revoke link expiring at {expires}",
    "classEnrollment.revokeConfirmTitle": "Revoke this link?",
    "classEnrollment.revokeConfirmDescription":
      "Learners who have not joined will no longer be able to use it.",
    "classEnrollment.revokeConfirm": "Confirm revoke",
    "classEnrollment.revoking": "Revoking...",
    "classEnrollment.revokeSuccess": "The class join link was revoked.",
    "classEnrollment.cancelAction": "Cancel",
    "classEnrollment.closeDialog": "Close class invitation dialog",
    "classEnrollment.mutationForbidden":
      "You can no longer manage class enrollments.",
    "classEnrollment.mutationConflict":
      "The class, enrollment, or invite-code state changed. Reload and retry.",
    "classEnrollment.mutationNotFound":
      "The member or invite code was not found in this class.",
    "classEnrollment.mutationRateLimited":
      "Actions are being made too quickly. Wait, then retry.",
    "classEnrollment.mutationError":
      "The action could not be completed. Please retry.",
    "classEnrollment.leaveAction": "Leave class",
    "classEnrollment.leaveTitle": "Leave this class?",
    "classEnrollment.leaveDescription":
      "The class will disappear from your list. You will need an active link or a teacher to add you again.",
    "classEnrollment.leaveConfirm": "Confirm leave",
    "classEnrollment.leaving": "Leaving...",
    "classEnrollment.leaveForbidden":
      "You can no longer leave this class in the active workspace.",
    "classEnrollment.leaveNotFound":
      "Your active enrollment could not be found in this class.",
    "classEnrollment.leaveConflict":
      "Your enrollment state changed. Reload the class, then retry.",
    "classEnrollment.leaveError": "The class could not be left. Please retry.",
    "classroom.title": "Classrooms",
    "classroom.description":
      "Manage classes in the active workspace and open the details for each class.",
    "classroom.createAction": "Create class",
    "classroom.listTitle": "Class list",
    "classroom.listDescription":
      "Data is limited by the active workspace and current session permissions.",
    "classroom.loadingList": "Loading the class list",
    "classroom.loadingDetail": "Loading class information",
    "classroom.classCount": "{count} classes loaded",
    "classroom.statusFilterLabel": "Filter by status",
    "classroom.statusFilterAll": "All statuses",
    "classroom.loadMore": "Load more classes",
    "classroom.loadingMore": "Loading more...",
    "classroom.loadMoreError":
      "The next page could not be loaded. The current list is still available.",
    "classroom.createTitle": "Create a class",
    "classroom.createDescription":
      "The class starts as a draft in the current workspace.",
    "classroom.closeCreate": "Close the create-class form",
    "classroom.codeLabel": "Class code",
    "classroom.codePlaceholder": "Example: SEC101",
    "classroom.codeHelp": "Use 3–32 letters, numbers, hyphens, or underscores.",
    "classroom.titleLabel": "Class name",
    "classroom.titlePlaceholder": "Example: Information Security Foundations",
    "classroom.timezoneLabel": "Class timezone",
    "classroom.timezoneHelp":
      "Use an IANA timezone name, for example Asia/Ho_Chi_Minh.",
    "classroom.descriptionLabel": "Description",
    "classroom.descriptionPlaceholder":
      "Add a short note that helps members identify this class.",
    "classroom.codeError": "The class code format is invalid.",
    "classroom.titleError": "The class name must contain 1–200 characters.",
    "classroom.timezoneError": "Enter a valid IANA timezone.",
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
    "classroom.emptyFilteredTitle": "No classes match this filter",
    "classroom.emptyFilteredDescription":
      "Choose another status to see the remaining classes in this workspace.",
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
    "classroom.archivedLabel": "Archived",
    "classroom.editTitle": "Edit class",
    "classroom.editDescription":
      "Update class information or activate a draft. Data versions protect concurrent changes.",
    "classroom.statusLabel": "Class status",
    "classroom.statusHelp":
      "Only active classes can issue live-classroom access.",
    "classroom.updateAction": "Save changes",
    "classroom.updating": "Saving...",
    "classroom.updateSuccess": "The class was updated.",
    "classroom.updateConflict":
      "This class changed elsewhere. Load the latest version before continuing.",
    "classroom.updateForbidden": "Your current session cannot edit this class.",
    "classroom.updateError": "The class could not be updated. Try again.",
    "classroom.reloadLatest": "Load latest version",
    "classroom.archiveTitle": "Archive class",
    "classroom.archiveDescription":
      "Close the class to new activity while preserving its roster and history.",
    "classroom.archiveAction": "Archive class",
    "classroom.archiveCloseLabel": "Close class archive confirmation",
    "classroom.archiveConfirmTitle": "Confirm class archive",
    "classroom.archiveConfirmDescription":
      "You are about to archive {name}. Members will no longer be able to enter a new room.",
    "classroom.archiveWarning":
      "New invite codes and LiveKit access remain closed until the class is restored.",
    "classroom.archiveConfirmAction": "Confirm archive",
    "classroom.archiving": "Archiving...",
    "classroom.archiveError": "The class could not be archived. Try again.",
    "classroom.restoreTitle": "Restore class",
    "classroom.restoreDescription":
      "Reopen an archived class so it can be edited and activated again.",
    "classroom.restoreAction": "Restore class",
    "classroom.restoreCloseLabel": "Close class restore confirmation",
    "classroom.restoreConfirmTitle": "Confirm class restore",
    "classroom.restoreConfirmDescription":
      "You are about to restore {name} to its pre-archive state.",
    "classroom.restoreWarning":
      "Room access only reopens when the restored class is active.",
    "classroom.restoreConfirmAction": "Confirm restore",
    "classroom.restoring": "Restoring...",
    "classroom.restoreError": "The class could not be restored. Try again.",
    "classroom.lifecycleConflict":
      "The class status changed. Load the latest version.",
    "classroom.lifecycleForbidden":
      "Only the class owner or a workspace administrator can do this.",
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
    "media.prejoin.loadingClass": "Checking classroom status",
    "media.prejoin.classError":
      "Classroom information is temporarily unavailable.",
    "media.prejoin.classInactive":
      "Only active classes can open a live classroom.",
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
