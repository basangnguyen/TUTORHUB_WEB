# P5-COLLAB-19 closure handoff

> Mục đích: điểm bàn giao ngắn gọn để một task Codex mới có thể tiếp tục P5-COLLAB-19 mà không cần dựa vào lịch sử hội thoại dài.
>
> Kiểm tra gần nhất: **2026-09-17**. Không ghi secret hoặc giá trị từ bất kỳ file `.env*.local` nào vào tài liệu này.

## 1. Trạng thái hiện tại

- Task: **P5-COLLAB-19 — Private alpha**.
- Trạng thái chính xác: **DONE** ngày 2026-09-17; owner đã chấp nhận closure dựa trên lần validation PASS đúng freshness window cùng cleanup PASS sau recovery.
- Git branch: `main`.
- Candidate/deploy commit: `22ebfe1bfecc95d782ee35f4a8049c32f25fdc50`.
- Closure chỉ gồm implementation/tài liệu P5-COLLAB-19; các artifact/probe ngoài phạm vi được bảo toàn và loại khỏi commit.
- P5-COLLAB-20 đã được mở ở `TODO`, nhưng chưa được authorization để ramp/rollback/production.

## 2. Những phần đã hoàn thành hoặc đã xác nhận

- Contract, local regression và disposable runner candidate của P5-COLLAB-19 đã có trong repository.
- `pnpm test:collaboration:p519` đã PASS theo trạng thái dự án hiện được ghi nhận.
- Regression kế thừa từ P5-COLLAB-16/17/18 đã PASS theo trạng thái dự án hiện được ghi nhận.
- Neon disposable preflight đã PASS ở ledger chính xác **`42 false`**.
- Không rollback migration và chưa forward shared staging trong luồng P5-COLLAB-19 này.
- Profile private alpha đã được chấp thuận:
  - một Render Free instance, Singapore;
  - không HA, multi-region hoặc Redis;
  - chấp nhận cold-start và gián đoạn ngắn;
  - hard cap `0 USD`, vượt quota/cost thì whiteboard phải force-off;
  - Object Lock disabled;
  - RPO là durable artifact gần nhất đã xác minh;
  - RTO tối đa 5 phút.
- Owner đã chốt:
  - Primary on-call: Bá Sáng;
  - Backup on-call: Duy Mạnh;
  - Security incident owner: Bá Sáng;
  - Cost owner: Bá Sáng.
- User đã tạo lại Neon disposable sau khi các branch cũ auto-expire, cập nhật B2 key P519 và thêm Render API key vào file local tương ứng.
- Render đã đồng bộ đúng disposable và redeploy: Control `dep-dal159ek1f9s73db1bcg`, Runtime
  `dep-dal15k6k1f9s73db2730`; deploy binding khớp candidate commit.
- Provider run `p519-private-alpha-20260916T035303778Z` chạy đủ 3.600 giây trong cửa sổ
  `2026-09-16T17:08:38.126Z..2026-09-16T18:08:38.126Z`: `6.000/6.000` operation, `120` metrics
  sample, `12` semantic check, `50` reconnect event.
- SLO PASS: join `867 ms`, reconnect `1.083 ms`, convergence `1.134 ms`, acknowledgement `445 ms`,
  artifact `1.503 ms`; toàn bộ reconnect/Control 600s/Neon/B2/rotation/force-off/export/restore/revoke drill PASS.
- Publication, current-run owner sign-off và report validation đều PASS trong provider window.
- Recovery cuối PASS; standalone cleanup PASS zero-residue; ledger `42 false`; whiteboard cuối cùng
  `mode=off`, `runtimeReady=false`, `documents=0`, `editConnections=0`.
- Aggregate local `pnpm test:collaboration:p519` và direct P5-COLLAB-16 matrix đều PASS ngày 2026-09-17.

## 3. Gate đã thực thi và quyết định closure

Toàn bộ mục vận hành trong danh sách checkpoint cũ dưới đây đã được thực thi. Lượt wrapper đầu xác minh
report và chạy drill PASS nhưng cleanup trong `finally` lỗi tạm `artifact_queue_unavailable`; recovery +
standalone cleanup sau đó PASS. Lượt wrapper thứ hai bị freshness gate từ chối vì report đã quá 5 phút.
Validator không bị nới lỏng. Owner đã chấp nhận rõ ngày 2026-09-17 dùng lần validation PASS đúng hạn
cùng cleanup PASS sau recovery làm closure, vì vậy P5-COLLAB-19 được chuyển sang `DONE`.

1. Xác minh Render đang dùng đúng disposable mới và redeploy thành công cả Control lẫn Runtime.
2. Chạy lại preflight trên deployment vừa redeploy.
3. Chạy đúng một provider-observed soak đủ 60 phút:
   - preflight: 300 giây;
   - steady soak: 3.000 giây;
   - recovery: 300 giây.
4. Chạy toàn bộ drill matrix:
   - reconnect;
   - sustained control-authority outage đúng 600 giây;
   - Neon outage;
   - B2 outage;
   - credential rotation;
   - force-off;
   - incident/export/restore/revoke.
5. Provider report mới đã được tạo và safe summary có `ok=true`, không có validation error.
6. Owner sign-off đã được xác nhận trong cửa sổ provider run.
7. Chạy cleanup và xác minh:
   - không còn dữ liệu/fixture tạm ngoài phạm vi cho phép;
   - force-off được giữ sau khi hoàn tất;
   - ledger vẫn là `42 false`;
   - không có divergence, data loss hoặc unplanned 5xx.
8. Publication/live notice cho private alpha đã được current report xác nhận:
   - tenant opt-in;
   - hướng dẫn Teacher;
   - thông báo giới hạn và accessibility;
   - support path `/app/settings?source=whiteboard-private-alpha`.
9. Acceptance, backlog, `PROJECT_STATE.md` và master plan đã được cập nhật sau quyết định closure.

## 4. Executor đã bổ sung

- `services/whiteboard-runtime/p519-provider-soak.mjs` thực thi provider-observed workload và sinh report/safe summary.
- Executor theo dõi reconnect schedule, chọn authenticated operation source còn sống và tái kiểm tra semantic barrier sau convergence wait.
- `scripts/run-p519-disposable.mjs` revalidate trusted run/deploy binding trước các report mode.
- `services/whiteboard-runtime/p519-provider-force-off.mjs` đưa runtime về off và chỉ xuất count/boolean an toàn.

## 5. Trình tự đã thực thi

1. Đọc các nguồn chuẩn:
   - `AGENTS.md`;
   - `README.md`;
   - `docs/AGENT_COORDINATION.md`;
   - `docs/PROJECT_STATE.md`;
   - `docs/MASTER_PLAN.md`;
   - `docs/PHASE_5_BACKLOG.md`;
   - `docs/P5_COLLAB_19_PRIVATE_ALPHA_ACCEPTANCE.md`;
   - file handoff này.
2. Kiểm tra `git status -sb` và `git rev-parse --short HEAD`; không ghi đè hoặc xóa artifact chưa hiểu rõ.
3. Chỉ kiểm tra sự tồn tại của `.env.p5-collab-19-disposable.local`; không in nội dung hoặc giá trị.
4. Xác minh runner/provider executor và binding Render hiện tại.
5. Đồng bộ các secret cần thiết từ file local lên đúng hai Render service mà không hiển thị/log giá trị.
6. Redeploy Control và Runtime; xác minh health/readiness.
7. Chạy preflight.
8. Chạy soak đủ 60 phút và thu provider report mới.
9. Chạy drill matrix, owner sign-off và cleanup.
10. Xác nhận force-off, hard cap và final ledger.
11. Cập nhật tài liệu trạng thái rồi mới đánh dấu `DONE`.

## 6. Các lệnh gate đã định nghĩa

```powershell
pnpm test:collaboration:p519
pnpm test:integration:collaboration:p519 -- .env.p5-collab-19-disposable.local preflight
pnpm test:integration:collaboration:p519 -- .env.p5-collab-19-disposable.local soak <provider-report.json>
pnpm test:integration:collaboration:p519 -- .env.p5-collab-19-disposable.local drills <provider-report.json>
pnpm test:integration:collaboration:p519 -- .env.p5-collab-19-disposable.local cleanup <provider-report.json>
pnpm test:integration:collaboration:p519 -- .env.p5-collab-19-disposable.local all <provider-report.json>
```

Report hiện có đã quá freshness window; không chạy lại validator để suy diễn closure, không sửa timestamp
và không nới lỏng freshness rule. Chỉ dùng report như lịch sử của run đã hoàn tất.

## 7. Tiêu chí live chính

- Workload chuẩn:
  - 2 documents;
  - 5 clients/document;
  - 500 shapes/document;
  - 120 ops/phút/document;
  - burst tùy chọn 480 ops/phút/document trong 60 giây;
  - reconnect mỗi 600 giây;
  - metrics mỗi 30 giây;
  - semantic hash mỗi 300 giây.
- Ngưỡng:
  - join p95 `<= 7.500 ms`;
  - reconnect p95 `<= 7.500 ms`;
  - convergence p95 `<= 2.500 ms`;
  - acknowledgment p95 `<= 1.000 ms`;
  - artifact p95 `<= 2.500 ms`;
  - recovery `<= 300.000 ms`;
  - cleanup `<= 3.000 ms`;
  - cost `0 USD`;
  - divergence, data loss và unplanned 5xx đều bằng 0;
  - semantic hash được bảo toàn.

## 8. Artifact local phải bảo toàn và loại khỏi commit

Các file sau đang untracked tại lần audit; không tự ý xóa và không đưa vào candidate commit:

- `New Volume (D) - Shortcut.lnk`
- `scripts/.tmp-p519-auth-probe.mjs`
- `scripts/.tmp-p519-contract-rewrite.mjs`
- `scripts/.tmp-p519-rewrite-1.mjs`
- `services/whiteboard-runtime/.tmp-p519-auth-probe.mjs`
- `services/whiteboard-runtime/p519-auth-probe.tmp.mjs`
- `services/whiteboard-runtime/p519-auth-probe2.tmp.mjs`

Luôn loại `.env*.local`, `.lnk`, probe và tmp khỏi staging/commit.

## 9. Provider và CI được ghi nhận gần nhất

Thông tin dưới đây đã được xác minh cho provider run 2026-09-16:

- Render Control deploy: `dep-dal159ek1f9s73db1bcg`.
- Render Runtime deploy: `dep-dal15k6k1f9s73db2730`.
- GitHub Security PASS gần nhất được ghi nhận: run `33863519062`.
- GitHub Verify PASS gần nhất được ghi nhận: run `33863520171`.
- `tmp/p5-collab-19/run-binding.json` và `render-deploy.json` đã được revalidate trong provider run;
  chúng vẫn là local artifact và không được commit.

## 10. Ràng buộc an toàn và phạm vi quyền đã cấp

- Không đọc, in, echo, log hoặc commit giá trị secret trong `.env*.local`.
- Không shared staging trong chuỗi disposable soak/drill này.
- Không rollback migration.
- Không chạy trên production.
- Giữ whiteboard force-off sau khi kết thúc.
- Không đánh dấu `DONE` nếu thiếu provider report mới, drill evidence, owner sign-off hoặc cleanup evidence.
- Làm việc nhanh, chọn giải pháp đơn giản đủ an toàn và kiểm chứng được; tránh lặp lại các bước đã PASS nếu không có dấu hiệu drift.
