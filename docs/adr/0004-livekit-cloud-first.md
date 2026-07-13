# ADR-0004: LiveKit Cloud cho classroom MVP

- Status: Accepted
- Date: 2026-07-11

## Decision

Dùng LiveKit Cloud trong MVP. Backend TutorHub cấp token/grant; frontend không giữ API secret. Việc self-host chỉ được đánh giá lại sau khi có dữ liệu tải, vùng địa lý, compliance và chi phí.

## Rationale

SFU WebRTC đa khu vực, TURN, nâng cấp và vận hành media là phần có rủi ro cao. Managed service giúp đội tập trung vào classroom UX và authorization trong giai đoạn đầu.

## Alternatives

- Self-host LiveKit: kiểm soát cao nhưng cần năng lực SRE, networking, TURN, autoscaling và multi-region.
- Jitsi/mediasoup: khả thi nhưng yêu cầu tích hợp/vận hành sâu hơn cho mục tiêu hiện tại.
- P2P thuần: không phù hợp lớp nhiều người và moderation/recording.

## Consequences

Cần abstraction mỏng cho room/token policy, telemetry chi phí và export dữ liệu. Recording dùng server-side egress; không tin client làm nguồn sự thật.
