---
title: TutorHub Core API
emoji: "🎓"
colorFrom: blue
colorTo: gray
sdk: docker
app_port: 7860
---

# TutorHub Core API Space template

Template cho Hugging Face Docker Space miễn phí ở giai đoạn alpha. Space chỉ chạy Go API stateless. Không lưu session, file hoặc queue trên local disk.

Khi tách repository để deploy Space, giữ Docker build context ở root monorepo hoặc điều chỉnh đường dẫn `COPY` tương ứng. Credential chỉ được cấu hình bằng Space Secrets.
