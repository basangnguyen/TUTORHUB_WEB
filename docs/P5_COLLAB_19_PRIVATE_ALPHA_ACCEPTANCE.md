# P5-COLLAB-19 — Private alpha acceptance

- Trạng thái: `VERIFY` — contract, local regression và disposable runner candidate đã sẵn sàng; live evidence chưa hoàn tất
- Ngày cập nhật: 2026-08-25
- Quyết định kiến trúc áp dụng: [ADR-0034](adr/0034-whiteboard-engine-and-sync-topology.md), [ADR-0037](adr/0037-whiteboard-feature-quota-and-operations.md)
- Task hạ nguồn: P5-COLLAB-20 chỉ được mở sau khi P5-COLLAB-19 đạt `DONE`

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

| Gate                                       | Trạng thái | Bằng chứng/việc còn lại                                      |
| ------------------------------------------ | ---------- | ------------------------------------------------------------ |
| Static contract và report validator        | `VERIFY`   | Candidate local; focused tests phải PASS                     |
| Inherited outage/canary regression         | `VERIFY`   | P5-16/P5-18 aggregate được runner gọi lại                    |
| Disposable preflight                       | `PENDING`  | Cần exact Neon/B2 disposable ở ledger `42 false`             |
| Provider-observed 60 phút                  | `PENDING`  | Chưa có current-run provider report                          |
| Full outage/rotation/restore/revoke drills | `PENDING`  | Phải chạy trong/đính kèm current provider window             |
| Publication và tenant opt-in               | `PENDING`  | UI/support notice cần live acceptance                        |
| Owner sign-off và final cleanup            | `PENDING`  | Chỉ ký sau current-run evidence và zero-residue cleanup PASS |

## 8. Exact exit gate

- [ ] Tenant opt-in, teacher guidance, limitation/accessibility notice và support path được công bố.
- [ ] 60-minute provider-observed soak trong declared cap không vi phạm convergence, latency, error hoặc cost budget.
- [ ] Incident/export/restore/revoke drill, current-run evidence, owner sign-off và final cleanup đều PASS.

## 9. Quyết định hiện tại

P5-COLLAB-19 ở `VERIFY`, chưa phải `DONE`. Contract và runner fail closed đã được chuẩn bị để kiểm tra
đúng duration/workload/threshold, evidence freshness, owner sign-off và zero-residue cleanup. Chưa có
authorization nào trong task này cho shared staging hoặc production ramp. P5-COLLAB-20 vẫn bị chặn
cho tới khi publication, disposable/provider run 60 phút, drill matrix và owner sign-off đều PASS.
