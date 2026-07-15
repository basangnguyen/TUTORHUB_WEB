# TutorHub V2 Design System

## 1. Phạm vi

Design System cung cấp ngôn ngữ thị giác và component nền cho web TutorHub. Nó không
chứa API call, quyền nghiệp vụ, route hoặc state backend. Feature sở hữu nội dung và
luồng; `@tutorhub/ui` sở hữu hành vi UI dùng chung.

## 2. Cấu trúc

| Vùng | Trách nhiệm |
| --- | --- |
| `packages/design-tokens/src/tokens.css` | Semantic tokens, light/dark theme, reduced motion |
| `packages/ui/src` | Component React, CSS `.th-*`, story và unit test |
| `.storybook` | Catalog, docs và accessibility panel |
| `scripts/check-design-token-contrast.mjs` | Gate tự động cho các cặp tương phản chữ chính |
| `apps/web/src` | Layout/feature sử dụng component; không sao chép primitive |

Web phải import theo thứ tự:

```ts
import "@tutorhub/design-tokens/tokens.css";
import "@tutorhub/ui/styles.css";
import "./styles.css";
```

## 3. Nguyên tắc thị giác

- Tông trung tính sáng, điểm nhấn xanh dương mang tính vận hành; màu success/warning/
  danger/info chỉ dùng theo ý nghĩa.
- Mật độ trung bình-cao để phù hợp dashboard giáo dục; control cao 32/40/48 px.
- Radius 4/6/8 px cho control và surface; dialog tối đa 12 px. Không lồng card trang trí.
- Shadow chỉ thể hiện elevation thật như menu, dialog, toast; section bình thường dùng
  đường viền hoặc khoảng cách.
- Font hệ thống Aptos/Segoe UI Variable để tải nhanh, rõ tiếng Việt và không phụ thuộc CDN.
- Motion 80-280 ms, chỉ mô tả thay đổi trạng thái; không dùng animation gây dịch layout.

## 4. Token

Feature chỉ dùng token semantic như `--color-text`, `--color-surface`,
`--color-accent`, `--space-4`, `--radius-sm`, `--shadow-md`. Không dùng hex hoặc tên
màu vật lý nếu giá trị có vai trò dùng chung.

Breakpoint được tài liệu hóa ở 480/768/1024/1280 px. CSS custom properties không dùng
trực tiếp được trong điều kiện media query, vì vậy giá trị số được lặp có kiểm soát tại
media query và phải giữ đồng bộ với token.

Dark theme dùng `data-theme="dark"` trên root. Phase 1 chỉ yêu cầu token và component
render đúng; công tắc theme toàn ứng dụng được triển khai khi product settings có scope.

## 5. Component công khai

- Action: `Button`, `IconButton`.
- Form: `TextField`, `TextAreaField`, `Select`, `SelectField`.
- Navigation: `Tabs`, `Menu` và các subcomponent tương ứng.
- Overlay: `Dialog`, `Drawer`, `Tooltip`, `Toast`.
- Feedback: `Skeleton`, `SkeletonGroup`, `EmptyState`, `ErrorState`,
  `ForbiddenState`, `OfflineState`, `StatusBadge`.

Quy tắc sử dụng:

1. Dùng `Button` cho command; link điều hướng vẫn là link.
2. Dùng `IconButton` chỉ với biểu tượng quen thuộc và luôn truyền `label` đã dịch.
3. Không đặt placeholder thay cho label. Error phải được truyền vào field để có
   `aria-invalid` và accessible description.
4. Dialog cho tác vụ ngắn, cần quyết định; Drawer cho cấu hình phụ cần nhiều chiều cao.
5. Toast chỉ báo kết quả không chặn. Lỗi cần người dùng xử lý phải nằm gần nguồn lỗi.
6. Loading không xóa layout; dùng skeleton có kích thước ổn định hoặc loading state của
   button. Khi gọi mạng, mutation không chạy trên render path.

## 6. Accessibility gate

- Unit test hiện bao phủ loading button, icon-only action, field/select description,
  dialog focus/Escape và tabs arrow navigation.
- `pnpm --filter @tutorhub/design-tokens test` kiểm tra tám cặp light/dark đạt 4.5:1.
- Storybook bật addon a11y với `test: "error"`; story không được có lỗi axe nghiêm trọng.
- Kiểm tra thủ công tối thiểu: Tab/Shift+Tab, Enter/Space, Escape, arrow key, zoom 200%,
  mobile width 320 px, reduced motion và Windows High Contrast khi component liên quan.

## 7. Lệnh phát triển

```powershell
pnpm storybook
pnpm storybook:build
pnpm --filter @tutorhub/ui test
pnpm --filter @tutorhub/design-tokens test
pnpm verify
```

Storybook chạy tại `http://localhost:6006`. Output tĩnh `storybook-static/` chỉ là build
artifact và bị Git-ignore.

## 8. Quy trình thêm component

1. Xác nhận component giải quyết hành vi dùng chung ở ít nhất hai feature hoặc có rủi ro
   accessibility đủ lớn để tập trung hóa.
2. Ưu tiên compose primitive hiện có; chỉ thêm dependency sau ADR hoặc giải thích kỹ thuật.
3. Định nghĩa API semantic, forward ref nếu người dùng cần focus, không expose màu vật lý.
4. Thêm CSS namespaced `.th-*`, story cho trạng thái chính và test hành vi quan trọng.
5. Chạy UI test, token contrast, Storybook build, web test/build và kiểm tra responsive.
6. Cập nhật file này nếu contract hoặc quy tắc sử dụng thay đổi.

## 9. Migration từ giao diện cũ

Migration theo luồng đang được chỉnh sửa, không thay toàn bộ ứng dụng trong một commit.
Ưu tiên route states, app shell, workspace/classroom form và các overlay. Giữ nguyên
contract API, i18n key và permission check; chỉ thay primitive/rendering. CSS cũ chỉ xóa
khi không còn selector sử dụng và đã có kiểm tra trực quan tương đương.
