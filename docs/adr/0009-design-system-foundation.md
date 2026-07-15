# ADR-0009: Nền tảng Design System cho web

- Status: Accepted
- Date: 2026-07-14

## Context

TutorHub V2 đã có web shell, xác thực, workspace, lớp học và phòng LiveKit nhưng
giao diện vẫn dùng nhiều phần tử HTML/CSS cục bộ. Khi số module tăng, cách này dễ làm
sai khác focus state, trạng thái loading/error, kích thước control, màu ngữ nghĩa và
keyboard navigation. P1-03 cần một nền tảng thống nhất nhưng không được khóa giao diện
vào theme của thư viện bên thứ ba.

Sản phẩm là phần mềm giáo dục vận hành thường xuyên, vì vậy mật độ thông tin, độ rõ,
khả năng truy cập và sự ổn định quan trọng hơn hiệu ứng trang trí. Theme sáng là mặc
định; dark theme có token tương ứng nhưng không chặn Phase 1.

## Decision

1. `@tutorhub/design-tokens` là nguồn token CSS ngữ nghĩa duy nhất cho màu, chữ,
   khoảng cách, hình dạng, elevation, motion, z-index và breakpoint.
2. `@tutorhub/ui` chứa component React dùng chung. Component tương tác phức tạp dùng
   Radix Primitives unstyled để nhận focus management, keyboard interaction và ARIA
   behavior, sau đó áp dụng CSS/TutorHub token riêng.
3. Dùng Lucide React làm bộ icon nét thống nhất. Icon-only action bắt buộc có accessible
   name; không chèn SVG tải động từ mạng.
4. Storybook React-Vite là catalog và môi trường review độc lập. Addon accessibility
   chạy ở chế độ lỗi; Vitest kiểm tra hành vi bàn phím/ARIA, còn script token kiểm tra
   các cặp tương phản chữ chính theo WCAG AA.
5. Component API dùng semantic variant (`primary`, `secondary`, `quiet`, `danger`),
   không nhận màu tùy ý ở từng feature. Feature vẫn sở hữu layout nghiệp vụ; Design
   System chỉ sở hữu primitive, state và quy tắc thị giác dùng chung.

## Rationale

- Radix giải quyết phần khó của dialog, select, menu, tabs, tooltip và toast mà vẫn
  để TutorHub toàn quyền về DOM composition và CSS. Điều này phù hợp hơn việc tự viết
  focus trap/roving tabindex, đồng thời ít áp đặt thương hiệu hơn component suite đã
  định sẵn giao diện.
- CSS custom properties hoạt động trực tiếp trong Vite, Storybook và các web surface
  sau này; không cần runtime theme engine hoặc build pipeline riêng.
- Storybook tách review component khỏi dữ liệu/backend, giúp nhiều agent kiểm tra cùng
  một contract UI trước khi tích hợp vào feature.
- Lucide có API React, kích thước nhỏ theo import và ngôn ngữ hình ảnh nhất quán.

## Alternatives

- **MUI/Material:** component đầy đủ nhưng theme và interaction mang nhận diện Material,
  bundle và lớp override lớn hơn nhu cầu hiện tại.
- **Fluent UI:** phù hợp sản phẩm doanh nghiệp và accessibility tốt nhưng tạo cảm giác
  gần Microsoft Teams; việc tùy biến sâu làm tăng chi phí migration CSS hiện có.
- **Tailwind + headless component recipes:** nhanh khi xây mới nhưng thêm một ngôn ngữ
  styling thứ hai, trong khi repository đã có shared CSS token và CSS module theo scope.
- **Tự viết toàn bộ primitive:** kiểm soát tối đa nhưng rủi ro cao ở focus trap, portal,
  Escape, roving focus, screen reader semantics và kiểm thử đa trình duyệt.

## Accessibility constraints

- Mọi control có accessible name; field nối label, hint và error bằng `for`/ARIA.
- Dialog giữ focus, đóng bằng Escape và trả focus về trigger.
- Tabs/menu/select dùng bàn phím theo WAI-ARIA pattern do Radix cung cấp.
- Focus indicator không bị tắt. Cặp chữ chính đạt tối thiểu 4.5:1.
- Animation tôn trọng `prefers-reduced-motion`; forced-colors vẫn hiển thị viền/focus.
- Không truyền thông tin chỉ bằng màu; trạng thái phải có text hoặc icon kèm nhãn.

## Consequences

- Thêm Radix, Lucide, Storybook và test utilities vào workspace; lockfile tăng đáng kể,
  chủ yếu là tooling development không đi vào application chunk.
- Feature cũ được chuyển dần, không big-bang rewrite. CSS cục bộ còn tồn tại cho layout
  đặc thù và phải dùng token thay vì literal khi được chạm tới.
- Mỗi component mới phải có story, trạng thái disabled/loading/error nếu phù hợp và
  test hành vi quan trọng trước khi export công khai.
- Cần theo dõi bundle application; Storybook bundle không được dùng làm thước đo bundle
  production của web.

## Official references

- Radix Primitives overview: https://www.radix-ui.com/primitives/docs/overview/introduction
- Radix accessibility: https://www.radix-ui.com/primitives/docs/overview/accessibility
- Storybook React with Vite: https://storybook.js.org/docs/get-started/frameworks/react-vite/
- WAI-ARIA Authoring Practices: https://www.w3.org/WAI/ARIA/apg/
- WCAG 2.2 contrast minimum: https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html
