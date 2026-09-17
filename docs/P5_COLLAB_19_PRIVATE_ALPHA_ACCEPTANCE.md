# P5-COLLAB-19 — Private alpha acceptance

- Trạng thái: `DONE` — provider soak, drill matrix, publication/sign-off và final cleanup đều PASS; owner đã chấp nhận closure
- Ngày cập nhật: 2026-09-17
- Quyết định kiến trúc áp dụng: [ADR-0034](adr/0034-whiteboard-engine-document-authority-and-collaboration-topology.md), [ADR-0037](adr/0037-whiteboard-feature-quota-and-operations.md)
- Task hạ nguồn: P5-COLLAB-20 đang `IN PROGRESS — preparation only`; mọi provider mutation/ramp/rollback/production action cần authorization riêng

## 1. Phạm vi và safety profile

Private alpha dùng profile đã được owner phê duyệt: một Render Free instance tại Singapore, không HA,
không multi-region và không Redis. Cold-start hoặc gián đoạn ngắn được chấp nhận. Hard cap là
`0 USD`; khi quota hoặc cost boundary không còn an toàn, whiteboard phải force-off. Object Lock vẫn
disabled; RPO là last verified durable artifact và RTO tối đa 5 phút.

Whiteboard vẫn globally force-off theo mặc định. Chỉ tenant đã opt-in và được server-side allowlist
mới có thể tham gia. Tenant khác phải fail closed; browser không được tự quyết định tenant,
capability, quota, generation hoặc provider document.

Owner giữ nguyên:

| Trách nhiệm             | Owner    |
| ----------------------- | -------- |
| Primary on-call         | Bá Sáng  |
| Backup on-call          | Duy Mạnh |
| Security incident owner | Bá Sáng  |
| Cost owner              | Bá Sáng  |

## 2. Publication bắt buộc

Trước khi bật private alpha, tenant opt-in, hướng dẫn Teacher, limitation/accessibility notice và
support path `/app/settings?source=whiteboard-private-alpha` phải được công bố bằng UI đã xác thực.
Thông báo phải nói rõ profile miễn phí có cold-start, không HA, quota thấp, khả năng force-off và
semantic text fallback cho người dùng screen reader. Không được ghi email, token, credential URL,
raw board content hoặc dữ liệu học sinh vào evidence.

## 3. Provider-observed soak contract

Local chỉ kiểm tra schema và regression; local PASS không thay thế bằng chứng provider. Exit gate
yêu cầu đúng một **60-minute provider-observed soak** (`60 phút`) trên disposable Neon/B2 và runtime
quan sát được từ provider. Cửa sổ chạy gồm:

| Phase       | Thời lượng | Mục đích                                 |
| ----------- | ---------- | ---------------------------------------- |
| `preflight` | 300 giây   | xác nhận ledger, role, bucket và runtime |
| `soak`      | 3.000 giây | tải ổn định và thu thập telemetry        |
| recovery    | 300 giây   | reconnect, recovery và ổn định sau drill |

Workload chính xác là 2 document, 5 client/document, tổng 10 connection và 500 shape/document.
Mỗi document chạy 120 operation/phút; burst tùy chọn 480 operation/phút/document trong 60 giây.
Reconnect được kích hoạt mỗi 600 giây, metrics lấy mỗi 30 giây và semantic hash kiểm tra mỗi 300 giây.

Threshold bắt buộc: join p95 <= 7.500 ms, reconnect p95 <= 7.500 ms, convergence p95 <= 2.500 ms,
acknowledgement p95 <= 1.000 ms, artifact p95 <= 2.500 ms, recovery <= 300.000 ms, cleanup <= 3.000 ms
và cost = `0 USD`. Divergence, data loss và unplanned 5xx phải bằng zero; semantic hash trước/sau
recovery phải bằng nhau.

## 4. Drill matrix

Provider report phải chứa evidence hiện tại cho toàn bộ `drills` sau:

- reconnect: existing document phục hồi và giữ semantic hash;
- sustained control-authority outage đúng 600 giây: document đang mở phục hồi, document mới fail closed;
- Neon outage: mutation fail closed và phục hồi sau khi Neon trở lại;
- B2 outage: last-good artifact vẫn đọc được và durable writes phục hồi;
- credential rotation: credential cũ bị từ chối, credential mới được chấp nhận;
- force-off: grant mới bị từ chối, phiên đang hoạt động đóng hoặc chuyển read-only an toàn;
- incident, export, restore và revoke: portable export hợp lệ, generation restore giữ semantic hash,
  revoked grant/credential bị từ chối và RTO không quá 5 phút.

## 5. Evidence freshness và sign-off

Mọi item trong `evidence.fresh` phải có `source` và `verifiedAt` nằm trong `startedAt..endedAt` của
chính lần chạy. Evidence kế thừa chỉ được dùng khi có `source`, `verifiedAt` và
`validityRationale` tối thiểu 20 ký tự giải thích vì sao vẫn hợp lệ. Empty object, timestamp cũ,
evidence ngoài cửa sổ hoặc raw secret phải fail closed.

Owner sign-off phải ghi đúng bốn owner ở mục 1 và có `approvedAt` trong cùng cửa sổ provider run;
authorization trước đó không thay thế sign-off của current run.

## 6. Disposable runner

Runner mặc định chỉ nạp `.env.p5-collab-19-disposable.local`, không log giá trị và không migrate,
rollback hoặc chạm shared staging. File local phải có bốn Neon URL đúng role, scoped B2 disposable
và confirmation:

```dotenv
DATABASE_MIGRATION_URL=
DATABASE_POOL_URL=
DATABASE_COLLABORATION_URL=
DATABASE_POLL_MAINTENANCE_URL=
B2_ENDPOINT=
B2_REGION=
B2_BUCKET=
B2_KEY_ID=
B2_APPLICATION_KEY=
P5_COLLAB_19_DISPOSABLE_CONFIRM=I_UNDERSTAND_P5_COLLAB_19_DISPOSABLE_ONLY
```

Exact final ledger là `42 false`. Các lệnh kiểm tra:

```powershell
pnpm test:collaboration:p519
pnpm test:integration:collaboration:p519 -- .env.p5-collab-19-disposable.local preflight
pnpm test:integration:collaboration:p519 -- .env.p5-collab-19-disposable.local soak <provider-report.json>
pnpm test:integration:collaboration:p519 -- .env.p5-collab-19-disposable.local drills <provider-report.json>
pnpm test:integration:collaboration:p519 -- .env.p5-collab-19-disposable.local cleanup <provider-report.json>
pnpm test:integration:collaboration:p519 -- .env.p5-collab-19-disposable.local all <provider-report.json>
```

Runner luôn xác minh `cleanup` cuối: database rows, runtime connections/documents, B2 current objects,
versions và multipart uploads đều zero; ledger vẫn `42 false`.

## 7. Evidence log hiện tại

Candidate dịch vụ được nghiệm thu ở commit `22ebfe1bfecc95d782ee35f4a8049c32f25fdc50`, với Render
Control deploy `dep-dal159ek1f9s73db1bcg` và Runtime deploy `dep-dal15k6k1f9s73db2730`. Binding
commit/deploy/manifest/target fingerprint và disposable Neon/B2 đã được runner tái xác minh trước run.

Provider report `p519-private-alpha-20260916T035303778Z` có `source=provider-observed`, cửa sổ
`2026-09-16T17:08:38.126Z..2026-09-16T18:08:38.126Z`, đúng 3.600 giây. Hai document nhận lần lượt
`6.000/6.000` operation; thu `120` metrics sample, `12` semantic check và `50` reconnect event.

| Gate                                       | Trạng thái | Bằng chứng                                                                                             |
| ------------------------------------------ | ---------- | ------------------------------------------------------------------------------------------------------ |
| Static contract và report validator        | `PASS`     | Local aggregate PASS; report validator PASS trong freshness window của lượt wrapper đầu                |
| Inherited outage/canary regression         | `PASS`     | P5-16 trực tiếp PASS runtime `20/20`, outage `8/8`, client `9/9` và Core API                           |
| Disposable preflight                       | `PASS`     | Exact Neon/B2 disposable; ledger `42 false`                                                            |
| Provider-observed 60 phút                  | `PASS`     | Join `867 ms`; reconnect `1.083 ms`; convergence `1.134 ms`; ack `445 ms`; artifact `1.503 ms`         |
| Full outage/rotation/restore/revoke drills | `PASS`     | Reconnect, Control outage `600s`, Neon, B2, rotation, force-off, incident/export/restore/revoke        |
| Publication và tenant opt-in               | `PASS`     | Opt-in, Teacher guidance, limitation, accessibility và support path đều được current report xác nhận   |
| Owner sign-off và final cleanup            | `PASS`     | Current-run sign-off PASS; provider cleanup `674 ms`; final standalone cleanup zero-residue PASS       |
| Closure decision                           | `PASS`     | Owner chấp nhận ngày 2026-09-17 dùng lần validation PASS đúng freshness cùng cleanup PASS sau recovery |

Lượt wrapper đầu tiên đã xác minh report và hoàn tất toàn bộ drill matrix nhưng bước cleanup trong
`finally` gặp lỗi tạm thời `artifact_queue_unavailable`. Recovery sau đó PASS, standalone cleanup được
chạy lại và PASS; lần xác minh cuối có database/runtime/B2 current/version/multipart đều bằng zero,
ledger `42 false`. Lượt wrapper thứ hai không được dùng làm closure vì report đã quá freshness window;
validator không bị nới lỏng và không có receipt bền được ghi ở lần PASS đầu.

## 8. Exact exit gate

- [x] Tenant opt-in, teacher guidance, limitation/accessibility notice và support path được công bố.
- [x] 60-minute provider-observed soak trong declared cap không vi phạm convergence, latency, error hoặc cost budget.
- [x] Incident/export/restore/revoke drill, current-run evidence, owner sign-off và final cleanup đều PASS.
- [x] Closure được owner chấp nhận dựa trên lần validation PASS trong freshness window dù wrapper đó không exit `0`.

## 9. Quyết định hiện tại

P5-COLLAB-19 chuyển `VERIFY -> DONE` ngày 2026-09-17 sau khi owner chấp nhận dùng lần validation PASS
đúng freshness window cùng recovery/standalone cleanup PASS làm closure. Whiteboard được trả về
`mode=off`, `runtimeReady=false`, `documents=0`, `editConnections=0`; không rollback, không chạm shared
staging/production và không ghi secret vào evidence. P5-COLLAB-20 được mở ở `TODO`, nhưng mọi hành động
ramp, rollback hoặc production vẫn cần authorization riêng.
