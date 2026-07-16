# ADR-0012: Quản trị `main` trong giai đoạn một người duy trì

- Trạng thái: Accepted
- Ngày: 2026-07-16
- Phạm vi: development, staging và private alpha

## Bối cảnh

TutorHub V2 hiện do một coding agent duy trì theo chỉ đạo trực tiếp của chủ dự án.
GitHub chủ yếu được dùng để lưu mã nguồn, lịch sử thay đổi và chạy hậu kiểm CI. Quy
trình pull request bắt buộc, approval độc lập và required checks trước merge làm tăng
thời gian thao tác nhưng chưa tạo được review độc lập thực chất khi chỉ có một người
duy trì.

Kiểm tra ngày 2026-07-16 cho thấy repository công khai chưa có ruleset. Không có đủ
bằng chứng để khẳng định secret scanning, push protection hoặc branch protection đã
được bật ở cấp repository. Trạng thái này phải được ghi nhận rõ, không được mô tả như
một kiểm soát đã tồn tại.

## Quyết định

Trong phạm vi development, staging và private alpha:

1. Cho phép commit và push trực tiếp vào `main`.
2. Không bắt buộc pull request hoặc approval cho mỗi thay đổi.
3. `pnpm verify` là quality gate cục bộ bắt buộc trước khi push.
4. Workflow `Verify` và `Security` phải chạy trên mọi push vào `main` để tạo bằng
   chứng hậu kiểm.
5. Không force-push, không xóa `main` và không đưa secret vào Git.
6. Dùng nhánh tạm cho migration phá vỡ tương thích, thay đổi dependency lớn hoặc
   thay đổi hạ tầng có khả năng làm mất dữ liệu.
7. Mọi ngoại lệ test phải được ghi trong `docs/PROJECT_STATE.md`; không được đánh dấu
   task hoàn thành nếu gate liên quan chưa đạt.

## Kiểm soát bù

- Pre-commit hooks chạy format/lint và các kiểm tra phù hợp với thay đổi.
- `pnpm verify` bao phủ format, generated contract, lint, typecheck, unit,
  integration, build, Storybook, Go test/vet và client-bundle secret scan.
- Workflow `Security` bao phủ Gitleaks, dependency review, CodeQL và Trivy.
- Dependabot theo dõi dependency và GitHub Actions.
- Secret chỉ nằm trong file local bị Git-ignore hoặc secret store của nhà cung cấp.
- Mỗi checkpoint hoàn chỉnh được commit riêng để có thể truy vết và hoàn nguyên.

## Hệ quả

Quy trình nhanh hơn cho một người duy trì nhưng không ngăn được commit lỗi đi vào
`main` trước khi CI chạy. Đây là rủi ro được chấp nhận có thời hạn, không phải baseline
phù hợp cho nhóm nhiều người hoặc production.

## Điều kiện hết hiệu lực

ADR này phải được thay thế bằng quy trình pull request và ruleset bảo vệ `main` trước
điều kiện nào đến trước trong các điều kiện sau:

- có người duy trì thứ hai;
- bắt đầu pilot với người dùng thật;
- xử lý dữ liệu cá nhân production;
- phát hành public beta;
- yêu cầu kiểm soát thay đổi độc lập từ tổ chức hoặc kiểm toán.

Khi đó tối thiểu phải bật required pull request, required `Verify`/`Security` checks,
chặn force-push/xóa nhánh, secret scanning và push protection theo khả năng của gói
GitHub.

## Phương án đã cân nhắc

### Bắt buộc pull request ngay từ hiện tại

An toàn quy trình cao hơn nhưng không tạo review độc lập khi cùng một người vừa viết,
vừa duyệt và vừa merge. Phương án này được hoãn đến khi có trigger hết hiệu lực ở trên.

### Tắt toàn bộ CI và chỉ dùng kiểm tra cục bộ

Không chấp nhận vì mất bằng chứng trên môi trường sạch và bỏ lớp kiểm tra độc lập với
máy phát triển.
