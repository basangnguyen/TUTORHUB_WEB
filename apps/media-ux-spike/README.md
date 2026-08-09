# P4-MEDIA-UX-00 isolated UX harness

Prototype này kiểm chứng các quyết định UX có thể chạy hoàn toàn bằng dữ liệu giả lập xác định:

- ba layout `Grid`, `Active speaker`, `Presentation` với fixture 2/5/25/50 người;
- Grid bị chặn ở 12/6/4 tile cho desktop/trung bình/hẹp; Active speaker rail là 6/5/3 và
  Presentation rail là 6/4/3; pin cục bộ thắng active speaker;
- hand queue hội tụ theo server sequence khi duplicate hoặc out-of-order;
- reaction candidate theo allowlist `👍 👏 ❤️ 🎉 😂 😮`, TTL 10 giây, gom burst 750ms/tối đa
  ba nhóm, mỗi người 3/5 giây + 20/phút và phòng 100/5 giây;
- automated reflow ở 320 CSS px, forced colors và reduced motion; 200% zoom thật thuộc P4-05/P4-11;
- bộ chọn `None`/blur/ba nền curated cùng capability/degradation UX.

## Ranh giới an toàn

- Không được nối prototype này vào route production.
- Không gọi Core API, LiveKit, camera, microphone hoặc speaker.
- Không xin permission, đọc device label, lưu lựa chọn hoặc tạo external/provider network request.
- Preview effect chỉ là hình CSS để kiểm chứng nội dung/trạng thái/fallback. Nó **không** thực hiện
  person segmentation và không phải bằng chứng hiệu năng processor.
- Active speaker, server sequence, rate limit và clock đều là dữ liệu mock; Core API vẫn là authority
  khi triển khai thật và `CanPublishData=false` không thay đổi.

## Chạy kiểm tra

```powershell
pnpm --filter @tutorhub/media-ux-spike lint
pnpm --filter @tutorhub/media-ux-spike typecheck
pnpm --filter @tutorhub/media-ux-spike test
pnpm --filter @tutorhub/media-ux-spike build
pnpm --filter @tutorhub/media-ux-spike e2e
pnpm --filter @tutorhub/media-ux-spike dev
```

Mở `http://127.0.0.1:4176`. Kết quả prototype không thay thế matrix browser/device thật, đo
360p/540p/720p, NVDA thủ công hoặc acceptance với LiveKit của P4-03/P4-05/P4-11.
