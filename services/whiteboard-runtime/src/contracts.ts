export const RUNTIME_VERSIONS = {
  hocuspocus: "4.6.0",
  node: "24.15.0",
  profile: "FREE_PRIVATE_ALPHA",
  yjs: "13.6.27",
} as const;

export type CollaborationCapability = "edit" | "present" | "view";
export type RuntimeMode = "enabled" | "off" | "read_only";

export interface CollaborationScope {
  actorId: string;
  capability: CollaborationCapability;
  documentId: string;
  generation: number;
  providerDocumentName: string;
  sessionId: string;
  tenantId: string;
  writerFence: number;
}

export interface RuntimeAuthorityState {
  mode: RuntimeMode;
}

export interface ControlPlane {
  exchangeGrant(input: {
    documentName: string;
    grant: string;
    origin: string;
  }): Promise<CollaborationScope>;
  probe(): Promise<RuntimeAuthorityState>;
}

export interface StoredCheckpoint {
  checksum: string;
  state: Uint8Array;
  watermark: number;
}

export interface CheckpointStore {
  close(): Promise<void>;
  load(scope: CollaborationScope): Promise<StoredCheckpoint | null>;
  probe(): Promise<void>;
  store(scope: CollaborationScope, state: Uint8Array): Promise<number>;
}

export interface SnapshotArtifact {
  bytes: Uint8Array;
  checksum: string;
  objectKey: string;
}

export interface PortableSnapshotStore {
  put(bytes: Uint8Array): Promise<SnapshotArtifact>;
  probe(): Promise<void>;
}

export type RuntimeDependencyCode =
  "control_plane" | "persistence" | "snapshot";
export type RuntimeEventCode =
  | "checkpoint_failed"
  | "checkpoint_ok"
  | "connection_denied"
  | "connection_ok"
  | "dependency_down"
  | "dependency_up"
  | "drain_complete"
  | "drain_failed"
  | "drain_started"
  | "runtime_started";

export interface SafeLogger {
  event(
    code: RuntimeEventCode,
    outcome: "denied" | "failed" | "ok" | "started",
    durationMs?: number,
  ): void;
}
