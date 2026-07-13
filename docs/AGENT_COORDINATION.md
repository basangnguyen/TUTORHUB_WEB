# TutorHub V2 - Multi-Agent Coordination

> Tài liệu điều phối chung cho con người và mọi AI agent làm việc trên TutorHub V2. Không dùng lịch sử hội thoại làm nguồn trạng thái duy nhất.

## 1. Repository chính thức

| Thuộc tính | Giá trị |
|---|---|
| GitHub | `https://github.com/basangnguyen/TUTORHUB_WEB` |
| Clone URL | `https://github.com/basangnguyen/TUTORHUB_WEB.git` |
| Remote chuẩn | `origin` |
| Nhánh mặc định | `main` |
| Thư mục local chuẩn | `D:\TutorHub_V2` |
| V1 tham chiếu, chỉ đọc | `D:\Ban_sao_du_an` |
| Monorepo | React/TypeScript, shared packages, Go Core API, infrastructure và docs |

Không tạo repository V2 khác nếu chưa có ADR và quyết định của chủ dự án. Không đưa mã V1, secret hoặc dữ liệu production vào repository này.

## 2. Nguồn sự thật và thứ tự đọc

Trước khi nhận việc, agent phải đọc theo thứ tự:

1. `AGENTS.md` - quy tắc bắt buộc.
2. Tài liệu này - ownership, Git workflow, checklist và bàn giao.
3. `docs/PROJECT_STATE.md` - snapshot thực tế mới nhất.
4. `docs/MASTER_PLAN.md` - kiến trúc và lộ trình tổng thể.
5. `docs/PHASE_1_BACKLOG.md` hoặc backlog phase hiện tại.
6. ADR và tài liệu chuyên đề liên quan task.

Khi thông tin mâu thuẫn: ADR Accepted -> `PROJECT_STATE.md` mới nhất -> tài liệu điều phối -> master plan -> backlog/tài liệu chuyên đề cũ. Phải sửa tài liệu lỗi thời trong cùng task; không âm thầm chọn một phiên bản.

## 3. Trạng thái điều phối hiện tại

| Thuộc tính | Trạng thái |
|---|---|
| Phase hoàn thành | Phase 0 |
| Phase hiện tại | Phase 1 - Engineering foundation |
| Task hoàn thành gần nhất | P1-02 Web shell trên nhánh `codex/p1-02-web-shell` |
| Task ưu tiên kế tiếp | Review/merge P1-02, sau đó nhận P1-04 Core API foundation |
| Initial commit | `33af851` - `chore(bootstrap): initialize TutorHub V2 foundation` |
| CI trên GitHub | `Verify` thành công ngày 2026-07-13 |
| Cloud staging | Chưa tạo |

## 4. Ownership công việc đang hoạt động

Sau khi repository có initial push, **GitHub Issue được gán owner là nguồn khóa nhận việc trực tiếp**. Mỗi task có một issue tạo từ `.github/ISSUE_TEMPLATE/task.yml`, ghi branch và phạm vi tệp dự kiến. PR phải liên kết issue tương ứng.

Trước initial push hoặc khi GitHub không khả dụng, dùng bảng dưới làm cơ chế fallback. Sau mỗi merge, cập nhật bảng thành snapshot cấp workstream; không yêu cầu mọi commit trung gian cùng sửa tệp này. Không nhận task có issue hoặc dòng `IN_PROGRESS` trùng phạm vi nếu chưa thống nhất với owner.

| Task | Trạng thái | Owner/agent | Branch | Phạm vi tệp dự kiến | Bắt đầu | Ghi chú |
|---|---|---|---|---|---|---|
| Bootstrap repository | DONE | Codex | `main` | Toàn bộ baseline ban đầu, GitHub templates và tài liệu điều phối | 2026-07-13 | Commit `33af851`, push `origin/main`, workflow `Verify` thành công |
| P1-02 Web shell | REVIEW | Codex | `codex/p1-02-web-shell` | `apps/web/src`, `apps/web/package.json`, `pnpm-lock.yaml`, task docs | 2026-07-13 | Issue #1; router/query/i18n/route states/responsive shell đã verify cục bộ |
| P1-04 Core API foundation | TODO | Chưa gán | Chưa tạo | `services/core-api`, OpenAPI liên quan | - | Có thể làm song song nếu không sửa cùng contract |

Giá trị trạng thái hợp lệ: `TODO`, `READY`, `IN_PROGRESS`, `BLOCKED`, `REVIEW`, `DONE`.

Nhãn GitHub đề xuất: `phase-1`, `frontend`, `backend`, `contract`, `database`, `infrastructure`, `security`, `blocked`, `agent-active`. Chỉ một issue `agent-active` được sở hữu một vùng file/module tại một thời điểm.

## 5. Checklist Phase 1 cấp cao

Checklist chi tiết có thẩm quyền nằm tại `docs/PHASE_1_BACKLOG.md`. Bảng này chỉ dùng để điều phối nhanh.

| Workstream | Trạng thái | Điều kiện hoàn thành kế tiếp |
|---|---|---|
| P1-01 Repository và toolchain | DONE | Initial commit, push và GitHub Actions Linux đã xanh |
| P1-02 Web shell | REVIEW | Issue #1 và nhánh `codex/p1-02-web-shell`; lint, typecheck, 6 tests, build và UI responsive đã đạt |
| P1-03 Design system | TODO | Token đầy đủ, component nền, Storybook, accessibility |
| P1-04 Go Core API | PARTIAL | `/api/v1`, Problem Details, status recorder, config validation, observability |
| P1-05 Contract và database | PARTIAL | Generated client, migration Neon/PostgreSQL và tenant-scoped repository |
| P1-06 Authentication | TODO | OIDC/BFF, cookie session, CSRF, `/api/v1/me` |
| P1-07 LiveKit spike | TODO | Prejoin, token tối thiểu quyền, room/reconnect telemetry |
| P1-08 CI/CD và security | PARTIAL | CI thực chạy, scan, branch protection, preview/staging deploy |
| P1-09 Local developer experience | PARTIAL | Docker services, seed, troubleshooting hoàn chỉnh |
| P1-10 Cloud foundation | TODO | Neon/B2/Cloudflare/HF staging và runbook rollback |

`PARTIAL` không được coi là hoàn thành exit gate.

## 6. Git workflow bắt buộc

### 6.1 Trước khi bắt đầu

```powershell
cd D:\TutorHub_V2
git status --short
git remote -v
git fetch origin --prune
git switch main
git pull --ff-only origin main
```

Nếu repository chưa có commit đầu tiên, chỉ agent được giao bootstrap mới thao tác trên `main`. Sau bootstrap, mỗi task dùng branch riêng.

### 6.2 Quy ước branch

```text
<agent>/<task-id>-<mo-ta-ngan>
```

Ví dụ:

- `codex/p1-02-web-shell`
- `claude/p1-04-problem-details`
- `gemini/p1-03-storybook`

Không dùng chung một branch cho hai phiên agent đang hoạt động. Không force-push `main`. Không commit trực tiếp lên `main` sau bootstrap, trừ hotfix đã được chủ dự án chấp thuận.

### 6.3 Commit và Pull Request

- Mỗi commit giải quyết một thay đổi có thể review; không trộn format toàn repo với tính năng.
- Commit message đề xuất: `<type>(<scope>): <mô tả>`, ví dụ `feat(web): add protected app shell`.
- PR phải nêu task, phạm vi, test, migration/contract, rủi ro, rollback và tài liệu đã cập nhật.
- OpenAPI, generated client, migration và code sử dụng phải nằm cùng PR khi có liên quan.
- Trước merge phải đồng bộ `main`, xử lý conflict có chủ đích và chạy `pnpm verify`.

## 7. Vùng dễ xung đột

Các tệp sau cần ownership rõ; chỉ một task sửa tại một thời điểm nếu chưa chia ranh giới:

- `package.json`, `pnpm-lock.yaml`, `pnpm-workspace.yaml`, `turbo.json`.
- `go.work`, `services/core-api/go.mod`.
- `openapi/tutorhub.yaml` và generated API client.
- Migration database và seed data.
- `apps/web/src` app shell, route tree và global styles.
- `packages/design-tokens` và public exports của `packages/ui`.
- `.github/workflows`, deployment manifests và Dockerfile.
- `docs/PROJECT_STATE.md`, checklist phase và ADR.

Không giải quyết conflict bằng cách chọn toàn bộ “ours” hoặc “theirs”. Phải đọc và hợp nhất ý nghĩa của cả hai phía.

## 8. Quy trình nhận việc và bàn giao

### Nhận việc

1. Đồng bộ `main` và đọc nguồn sự thật.
2. Chọn một task `READY`/`TODO` không trùng ownership.
3. Nhận GitHub Issue, gán owner/label `agent-active`, branch và phạm vi tệp. Chỉ dùng bảng fallback khi GitHub chưa khả dụng.
4. Ghi kế hoạch ngắn: mục tiêu, file sửa, contract/module, rủi ro và cách test.
5. Chỉ bắt đầu sửa sau khi ranh giới task rõ.

### Kết thúc hoặc tạm dừng

1. Chạy verification phù hợp và ghi kết quả thật.
2. Cập nhật backlog, `PROJECT_STATE.md` và tài liệu/ADR liên quan.
3. Chuyển issue/task thành `REVIEW`, `BLOCKED` hoặc `DONE`; gỡ `agent-active` khi bàn giao và không để `IN_PROGRESS` mồ côi.
4. Ghi commit cuối, branch/PR, file đã sửa, việc còn lại và rủi ro.
5. Không tuyên bố hoàn thành nếu exit gate hoặc test bắt buộc chưa đạt.

### Mẫu bàn giao phiên

```markdown
## Handoff <task-id>

- Owner/agent:
- Branch / commit / PR:
- Mục tiêu:
- Đã hoàn thành:
- File đã sửa:
- Contract/migration thay đổi:
- Lệnh verify và kết quả:
- Phần chưa hoàn thành:
- Rủi ro hoặc quyết định mở:
- Bước tiếp theo chính xác:
```

## 9. Verification chuẩn

```powershell
corepack enable
pnpm install --frozen-lockfile
$env:PATH = "$(Resolve-Path '.tools\go\bin');$env:PATH" # chỉ khi dùng Go local trong .tools
pnpm verify
```

Ngoài verification chung, task phải chạy test theo rủi ro: component/E2E, integration PostgreSQL, authorization matrix, migration hoặc deployment smoke test tương ứng.

## 10. Bảo mật và dữ liệu

- Không commit secret, token, `.env`, dữ liệu người dùng hoặc credential V1.
- Không dán secret vào issue, PR, log, screenshot hoặc tài liệu bàn giao.
- Credential local/staging/production phải tách biệt và lưu trong secret store.
- Nếu phát hiện secret trong Git history, dừng merge, báo chủ dự án và rotate trước khi tiếp tục.
- Không để agent tự thay quyết định bảo mật/kiến trúc Accepted mà không có ADR superseding.

## 11. Quy tắc duy trì tài liệu này

- Cập nhật bảng ownership ngay khi nhận, bàn giao hoặc hủy task.
- Cập nhật snapshot khi phase/task ưu tiên thay đổi.
- Checklist chi tiết chỉ chỉnh trong backlog phase; không sao chép thêm một backlog đầy đủ vào đây.
- Khi repository, default branch hoặc workflow thay đổi, cập nhật đồng thời `README.md`, `AGENTS.md`, `PROJECT_STATE.md` và tài liệu này.
- Mọi trạng thái phải có ngày, owner và bằng chứng kiểm tra; không ghi “đã xong” chỉ dựa trên hội thoại.
