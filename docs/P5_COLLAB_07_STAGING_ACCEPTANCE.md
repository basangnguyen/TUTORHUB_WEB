# P5-COLLAB-07 - Snapshot/import/export/restore worker và B2 acceptance

> Trạng thái: `VERIFY` ngày 2026-08-22. Local/full verify và toàn bộ Neon/B2 disposable gate đã PASS
> ở ledger `40 false`; candidate chưa commit/push và GitHub Verify/Security chưa chạy. Chưa ghi shared
> staging hoặc deploy. Production whiteboard tiếp tục force-off tới P5-COLLAB-17.

## 1. Kết quả local candidate

Core API đã có durable artifact command workflow cho snapshot, export và restore. Whiteboard runtime
claim command bằng lease/fencing, đọc bounded checkpoint, tạo signed artifact envelope rồi ghi đúng
một immutable B2 object version. Restore chỉ atomic swap sang generation mới sau khi worker đã verify
exact object version, checksum, scope, engine/schema version và stage checkpoint đích thành công.

Migration forward-only `000038` thêm bounded latest-generation checkpoint, artifact command queue,
purge queue và bốn maintenance function có pinned `search_path`. Migration `000039` cho phép worker
stage checkpoint của generation đích trước authority swap. Disposable lifecycle phát hiện mâu thuẫn
giữa `ON DELETE SET NULL` và restore-shape constraint; forward migration `000040` sửa đúng invariant:
active restore vẫn bắt buộc exact source, terminal restore cho phép source bị redaction sau retention
purge. PostgreSQL không trở thành operation, history hoặc undo authority; B2 chỉ giữ immutable blob,
còn Core API giữ authorization/catalog và generation lifecycle.

ADR kiến trúc: [ADR-0035](adr/0035-whiteboard-artifact-worker-and-b2-lifecycle.md).

## 2. Boundary và giới hạn bắt buộc

| Boundary                         | Giá trị/behavior                                                     |
| -------------------------------- | -------------------------------------------------------------------- |
| Portable import                  | tối đa `16 MiB`, `2.000` element, `256` file                         |
| Yjs checkpoint                   | tối đa `20 MiB`, đúng một latest checkpoint mỗi generation           |
| Signed artifact envelope         | tối đa `32 MiB`                                                      |
| B2 object key                    | opaque random `192-bit`, không chứa tenant/document/user             |
| B2 binding                       | exact key + exact version ID + bytes + SHA-256 + verification key ID |
| Worker command lease             | bounded attempts, lease token và writer fence                        |
| Maintenance purge               | batch tối đa `25`, lease tối đa `120` giây, `SKIP LOCKED`             |
| Log/audit                        | chỉ bounded reason code/count; không raw scene/Yjs/token/object body  |

Portable Excalidraw scene parser reject prototype keys, path traversal, external URL, active HTML,
SVG/data URL không an toàn, schema/version/hash sai và payload quá giới hạn. `import_validate` worker
command chưa có upload binding trong OpenAPI P5-COLLAB-03 nên fail closed với
`artifact_import_unbound`; manifest validation hiện tại không được coi là đã upload/import artifact.

## 3. Authorization và exact role

Disposable acceptance phải dùng bốn role khác nhau trên cùng branch/database:

1. owner direct `neondb_owner` chỉ migrate/provision;
2. Core API pooled `tutorhub_runtime` chỉ đọc command và insert exact command columns;
3. collaboration worker direct chỉ claim/update command, đọc document/generation, ghi exact snapshot
   columns và latest checkpoint;
4. maintenance direct `tutorhub_poll_maintenance` không có table ACL, chỉ execute claim/complete/fail
   purge functions.

`PUBLIC` bị deny. Core API không được đọc raw checkpoint hoặc xóa artifact; worker không được xóa;
maintenance chỉ nhận opaque object key/version sau khi claim và chỉ xóa catalog row khi exact B2
version deletion đã thành công.

## 4. Local verification đã PASS

```powershell
node --test scripts/run-p507-disposable.test.mjs
pnpm --filter @tutorhub/collaboration-client test
pnpm --filter @tutorhub/whiteboard-runtime test
pnpm --filter @tutorhub/whiteboard-runtime lint
pnpm --filter @tutorhub/whiteboard-runtime typecheck
go test ./services/core-api/internal/modules/collaboration
go test -tags=integration ./services/core-api/internal/modules/collaboration `
  -run TestProvisionWhiteboardArtifactWorkerExactACL -count=1
node scripts/check-whiteboard-runtime-oci.mjs
node --test scripts/check-whiteboard-runtime-sbom.test.mjs
pnpm api:check
pnpm verify
git diff --check
```

Runtime local suite PASS `115` test; real B2/lifecycle integration chỉ chạy khi có exact disposable
confirmation. Collaboration-client portable scene suite, Core API collaboration, integration-tag
compile, OpenAPI generated client, OCI dependency/SBOM, lint, typecheck, build, Storybook, bundle
security, Go test/vet và full `pnpm verify` đều PASS sau migration `000040`.

## 5. Disposable `VERIFY -> DONE`

Tạo `.env.p5-collab-07-disposable.local` đã được Git ignore với đúng tên biến sau, không chia sẻ giá
trị trong chat hoặc log:

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
P5_COLLAB_07_DISPOSABLE_CONFIRM=I_UNDERSTAND_P5_COLLAB_07_DISPOSABLE_ONLY
```

Thứ tự gate, không rollback:

1. `node scripts/run-p507-disposable.mjs .env.p5-collab-07-disposable.local preflight`.
2. Owner preflight và forward-only migration: `000038` PASS; `38 false -> 39 false -> 39 false`, sau
   đó `39 false -> 40 false -> 40 false`. Final aggregate rerun giữ `40 false -> 40 false -> 40 false`.
3. `node scripts/run-p507-disposable.mjs .env.p5-collab-07-disposable.local acl` để provision và
   kiểm tra exact four-role ACL.
4. `node scripts/run-p507-disposable.mjs .env.p5-collab-07-disposable.local b2` để PUT/HEAD/versioned
   GET/checksum/tamper binding/exact version DELETE trên bucket disposable.
5. Chạy worker snapshot/export/restore fixture: catalog chỉ publish sau B2 verify; restore tạo đúng
   một generation mới; corrupt/incompatible artifact bị quarantine và không swap authority.
6. Chạy hai maintenance claimant song song để xác nhận `SKIP LOCKED`, bounded batch/retry và exact
   version purge; postflight phải không còn object/queue/fixture row.
7. Review diff/no-secret, commit candidate, push và yêu cầu GitHub Verify/Security PASS — **còn mở**.

### Kết quả disposable ngày 2026-08-22

- Exact four-role PostgreSQL ACL và `PUBLIC` deny: **PASS**.
- B2 PUT/HEAD/versioned GET/checksum/tamper binding/exact version DELETE: **PASS**.
- Worker snapshot/export, corrupt restore quarantine, valid restore staging và one-generation authority
  swap: **PASS**.
- Hai maintenance claimant, `SKIP LOCKED`, exact-version purge, bounded failure/retry và fixture/object
  cleanup postflight: **PASS**.
- Gate được chạy lại bằng `all` ở final ledger `40 false`; toàn chuỗi **PASS** trong `65.7s`.
- Không rollback, không shared-staging write, không deploy và không log credential.

Không xóa Neon branch hoặc B2 bucket/key disposable trước khi lưu đủ kết quả. Không forward shared
staging hay deploy trước báo cáo disposable PASS và xin quyền riêng.

## 6. Deferred, không suy rộng

- OpenAPI hiện chỉ có manifest validation cho portable import; upload intent, artifact ingestion và
  download delivery/presigned export chưa được công bố cho browser. Không tuyên bố end-user
  import/download đã hoàn chỉnh trong P5-COLLAB-07.
- Reconnect/compaction/last-good recovery end-to-end thuộc P5-COLLAB-08; test matrix đầy đủ của
  snapshot/import/export/restore thuộc P5-COLLAB-13.
- Không có shared-staging migration, Render deploy, B2 production credential hoặc production feature
  enable trong candidate này.
