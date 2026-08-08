# ADR 0027: Presigned B2 transfer capabilities

- Status: Accepted
- Date: 2026-08-08
- Scope: P3-09 direct Backblaze B2 upload/download

## Context

P3-08 established PostgreSQL file authority, opaque object keys, quota reservation and a
fail-closed finalize step. It deliberately did not give a browser an object-storage URL.
P3-09 must let a browser transfer large bodies directly to the private B2 bucket without
turning the Core API into a binary proxy. Provider evidence showed that the P3-08 synchronous
SHA-256 proof cannot be established by B2 S3 presigned PUT, so this ADR also defines the
forward boundary between transfer finalize and P3-10 pre-ready processing.

Backblaze documents S3-compatible presigned PUT and GET support and returns a version ID
for Put/Head Object. Its current public Put/Head Object documentation does not promise the
AWS `x-amz-checksum-sha256` request/response fields, however. The exact transfer mechanism
therefore needs a real provider gate before any end-user upload endpoint can be enabled.

## Decision

### Provider gate comes first

- The first P3-09 deliverable is a disposable B2 contract smoke using the existing AWS SDK
  adapter and a unique `smoke/p3-09-*` key. It presigns an exact PUT, sends a bounded payload,
  reads authoritative metadata, downloads the exact returned version and removes only the
  test object/version.
- PUT signing binds the server-generated key, HTTP method, exact content length and normalized
  content type. The capability expires after at most five minutes. It does not advertise an
  unenforced checksum header.
- Acceptance requires Head Object for the exact client-returned version selector to return
  exact length/type, non-empty ETag and the same version ID. A version-bound presigned GET
  must return the original bytes.
- A browser checksum, unsigned custom metadata or ETag must never be treated as SHA-256 proof.
  Finalize stores no SHA-256 and leaves the file `uploaded`; P3-10 must stream-hash and scan
  that exact version before atomically setting `ready` and storing the verified digest.

### End-user capability contract after provider PASS

- `POST /api/v1/files/{file_id}/upload-capability` requires session, CSRF, expected-tenant
  assertion and `expected_version`. The repository rechecks the active tenant, creator,
  current `file.upload`, `pending` state and intent expiry before signing.
- The response contains only method, short-lived URL, expiry, exact content length and the
  signed headers the browser must supply. It never contains an application key, bucket name
  or a separately reusable raw object key.
- Upload expiry is the earlier of five minutes and the intent expiry. Retry may reuse the
  same exact capability while it is valid; a fresh capability always reauthorizes and never
  extends the 15-minute intent.
- `POST /api/v1/files/{file_id}/download-capability` also requires CSRF and expected-tenant
  binding. It reauthorizes `file.view`, requires a non-deleted `ready` file and signs GET for
  the immutable B2 version persisted by finalize. Download expiry is at most two minutes.
- Download forces a bounded attachment disposition and the stored media type. Active
  content is not rendered inline. Both capability responses use `Cache-Control: no-store`
  and `Referrer-Policy: no-referrer`; URLs and signed query values never enter logs, audit,
  metrics, browser persistence or error bodies.
- Missing/inaccessible/foreign-tenant IDs remain concealed as `404`. Provider signing
  failure is a retriable typed `503` and does not mutate file state.
- Finalize requires `storage_version_id` as a selector, independently HEADs that exact B2
  version, and persists size/type/ETag/version only. Download capability issuance remains
  impossible before the verified P3-10 transition to `ready`.

### Multipart boundary

- Provider evidence established that neither a single-object nor multipart ETag is a
  trustworthy whole-object SHA-256. Multipart therefore follows the same forward contract:
  provider completion returns an immutable version selector, exact-version finalize verifies
  length/type/version, and P3-10 later stream-hashes and scans those bytes before `ready`.
- Forward migration `000028` introduces durable multipart sessions and issued-part manifests.
  The public session UUID is bound to tenant, creator, pending file, expected file version and
  the original intent expiry. The provider upload ID is private database state and is never
  returned, logged or accepted from a client.
- Initiate, part-capability, complete and abort all require session, CSRF, expected-tenant
  binding and a fresh server-side authorization check. Part capabilities are valid for at
  most five minutes and never extend the original upload intent. Part numbers are bounded to
  `1..10000`; every non-final part is at least 5,000,000 bytes and no part exceeds five GiB.
- Complete accepts only a contiguous ordered manifest exactly matching the issued part
  numbers and lengths. ETags are opaque provider selectors, not checksums. It returns the
  immutable version ID produced by B2; the caller then uses the existing exact-version
  finalize endpoint.
- Only one active multipart session may own a file. A single-PUT capability is denied while
  such a session is active. If provider initiate succeeds but database ownership cannot be
  committed, the Core API attempts a compensating abort; a bucket lifecycle rule remains the
  safety net for crash-orphaned uploads.
- Abort is idempotent for a session already marked aborted and denies any later part or
  complete request. Expired sessions fail closed and are marked expired lazily; B2 must also
  enforce `AbortIncompleteMultipartUpload` for abandoned provider uploads.

### Credentials and CORS

- The runtime app key is limited to the private non-production bucket and TutorHub prefix.
  It needs only the provider capabilities required by accepted operations; it is never sent
  to the browser. Presigning delegates one exact request, not general bucket access.
- The B2 bucket CORS rule is allowlisted to TutorHub web origins, methods and signed headers.
  A broad wildcard origin is not an acceptable production configuration.
- Capability URLs are bearer-like secrets. Diagnostics may report method, gate name,
  status class and request ID, but never the URL, query string, credentials, filename,
  checksum or object key.

## Consequences

- P3-09 cannot accidentally paper over a provider incompatibility already identified by
  P3-08. The provider evidence determines whether the S3-compatible design can proceed.
- Forward migration `000027` removes unverified pre-ready stored checksum claims and changes
  the lifecycle constraint so SHA-256 is mandatory at `ready`, not synchronous finalize.
- Forward migration `000028` adds durable multipart ownership/part state without granting the
  Core API delete access. Provider upload IDs and part manifests remain private metadata.
- The Core API handles only authorization, metadata and small provider control requests;
  file bytes continue to travel directly between the browser and B2.

## Provider evidence — 2026-08-08

The disposable B2 gate failed the checksum requirement while proving the remaining provider
behavior:

- exact presigned PUT succeeded and Head Object returned the expected length/type, non-empty
  ETag and immutable version ID;
- Head Object did not return SHA-256 even with checksum mode enabled;
- B2 accepted same-length wrong bytes when `x-amz-checksum-sha256` was signed;
- B2 also accepted same-length wrong bytes when `x-amz-content-sha256` was included in
  SigV4 signed headers while the presigned payload mode remained unsigned;
- using the actual SHA-256 as the SigV4 presigned payload hash caused B2 to reject both the
  wrong and correct payload with HTTP 403;
- every accepted smoke object was removed by exact version ID and no credential, URL, query,
  object key or checksum was printed.

The owner approved the forward design on 2026-08-08. Migration `000027`, exact-version
finalize, upload/download capability routes and generated client are therefore authorized.
`file_uploads` remains off for end users until P3-10 establishes SHA-256 and scan authority
before `ready`; no browser assertion is promoted into stored proof.

## Alternatives rejected

- Returning an unsigned bucket/key or long-lived application credential: excessive scope.
- Trusting a checksum echoed by the browser or stored as unsigned metadata: does not prove
  the bytes accepted by B2.
- Treating ETag as SHA-256: provider-specific and false for multipart objects.
- Issuing latest-version download URLs: a replacement at the same key could change bytes
  after authorization/finalize.
- Proxying large uploads/downloads through the Core API: violates the accepted data plane
  and adds avoidable memory, bandwidth and availability pressure.
