import {
  TLSyncErrorCloseEventCode,
  type TLPersistentClientSocket,
  type TLSocketStatusChangeEvent,
} from "@tldraw/sync-core";

export type GateCapability = "view" | "edit" | "present";

interface GrantProtocolSocketOptions {
  baseUrl: string;
  tenantId: string;
  documentId: string;
  actorId: string;
  requestedCapability: GateCapability;
  sessionId: string;
  storeId: string;
}

interface GrantResponse {
  grant: string;
  generation: number;
}

const PROTOCOL_NAME = "tutorhub-sync-v1";
const GRANT_PROTOCOL_PREFIX = "tutorhub-grant.";
const RECONNECT_DELAYS_MS = [100, 250, 500, 1_000, 2_000] as const;

export class GrantProtocolSocket implements TLPersistentClientSocket<
  object,
  object
> {
  connectionStatus: "error" | "offline" | "online" = "offline";

  private socket: WebSocket | null = null;
  private closed = false;
  private manuallyOffline = false;
  private attempt = 0;
  private reconnectTimer: number | null = null;
  private connectionEpoch = 0;
  private expectedGeneration: number | undefined;
  private readonly messageListeners = new Set<(message: object) => void>();
  private readonly statusListeners = new Set<
    (event: TLSocketStatusChangeEvent) => void
  >();

  constructor(private readonly options: GrantProtocolSocketOptions) {
    this.connect();
  }

  sendMessage(message: object): void {
    if (this.socket?.readyState !== WebSocket.OPEN) return;
    for (const part of chunkProtocolMessage(JSON.stringify(message))) {
      this.socket.send(part);
    }
  }

  onReceiveMessage(callback: (message: object) => void): () => void {
    this.messageListeners.add(callback);
    return () => this.messageListeners.delete(callback);
  }

  onStatusChange(
    callback: (event: TLSocketStatusChangeEvent) => void,
  ): () => void {
    this.statusListeners.add(callback);
    return () => this.statusListeners.delete(callback);
  }

  restart(): void {
    if (this.closed) return;
    this.manuallyOffline = false;
    this.attempt = 0;
    this.disconnectSocket();
    this.connect();
  }

  goOffline(): void {
    if (this.closed) return;
    this.manuallyOffline = true;
    this.cancelReconnect();
    this.connectionEpoch += 1;
    this.disconnectSocket();
    this.setStatus("offline");
  }

  goOnline(): void {
    if (this.closed) return;
    this.manuallyOffline = false;
    this.attempt = 0;
    this.connect();
  }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.cancelReconnect();
    this.connectionEpoch += 1;
    this.disconnectSocket();
    this.setStatus("offline");
    this.messageListeners.clear();
    this.statusListeners.clear();
  }

  private async connect(): Promise<void> {
    if (
      this.closed ||
      this.manuallyOffline ||
      this.socket?.readyState === WebSocket.OPEN ||
      this.socket?.readyState === WebSocket.CONNECTING
    ) {
      return;
    }

    const epoch = ++this.connectionEpoch;
    try {
      const response = await fetch(`${this.options.baseUrl}/gate/grants`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          tenantId: this.options.tenantId,
          documentId: this.options.documentId,
          actorId: this.options.actorId,
          requestedCapability: this.options.requestedCapability,
          ...(this.expectedGeneration === undefined
            ? {}
            : { expectedGeneration: this.expectedGeneration }),
        }),
      });
      if (!response.ok) {
        throw new Error(`grant_http_${response.status}`);
      }
      const payload = (await response.json()) as GrantResponse;
      if (!/^[A-Za-z0-9_-]{40,64}$/.test(payload.grant)) {
        throw new Error("grant_response_invalid");
      }
      if (!Number.isInteger(payload.generation) || payload.generation < 1) {
        throw new Error("grant_generation_invalid");
      }
      this.expectedGeneration = payload.generation;
      if (
        epoch !== this.connectionEpoch ||
        this.closed ||
        this.manuallyOffline
      ) {
        return;
      }

      const socketUrl = new URL(
        `/connect/${encodeURIComponent(this.options.documentId)}`,
        this.options.baseUrl.replace(/^http/, "ws"),
      );
      socketUrl.searchParams.set("sessionId", this.options.sessionId);
      socketUrl.searchParams.set("storeId", this.options.storeId);
      const socket = new WebSocket(socketUrl, [
        PROTOCOL_NAME,
        `${GRANT_PROTOCOL_PREFIX}${payload.grant}`,
      ]);
      this.socket = socket;

      socket.addEventListener("open", () => {
        if (this.socket !== socket) return;
        this.attempt = 0;
        this.setStatus("online");
      });
      socket.addEventListener("message", (event) => {
        if (this.socket !== socket || typeof event.data !== "string") return;
        try {
          const message = JSON.parse(event.data) as object;
          for (const listener of this.messageListeners) listener(message);
        } catch {
          this.restart();
        }
      });
      socket.addEventListener("close", (event) => {
        if (this.socket !== socket) return;
        this.socket = null;
        if (event.code === TLSyncErrorCloseEventCode) {
          this.setStatus("error", event.reason || "UNKNOWN_ERROR");
        } else {
          this.setStatus("offline");
        }
        this.scheduleReconnect();
      });
      socket.addEventListener("error", () => {
        if (this.socket !== socket) return;
        this.setStatus("offline");
      });
    } catch (error) {
      if (
        epoch !== this.connectionEpoch ||
        this.closed ||
        this.manuallyOffline
      ) {
        return;
      }
      const reason =
        error instanceof Error ? error.message : "connection_failed";
      this.setStatus("error", reason);
      this.scheduleReconnect();
    }
  }

  private scheduleReconnect(): void {
    if (this.closed || this.manuallyOffline || this.reconnectTimer !== null)
      return;
    const delay =
      RECONNECT_DELAYS_MS[
        Math.min(this.attempt, RECONNECT_DELAYS_MS.length - 1)
      ];
    this.attempt += 1;
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null;
      this.connect();
    }, delay);
  }

  private cancelReconnect(): void {
    if (this.reconnectTimer !== null) {
      window.clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }

  private disconnectSocket(): void {
    const socket = this.socket;
    this.socket = null;
    if (socket && socket.readyState < WebSocket.CLOSING) {
      socket.close(1000, "CLIENT_RESTART");
    }
  }

  private setStatus(
    status: "error" | "offline" | "online",
    reason = "connection_failed",
  ): void {
    if (this.connectionStatus === status) return;
    this.connectionStatus = status;
    const event: TLSocketStatusChangeEvent =
      status === "error" ? { status, reason } : { status };
    for (const listener of this.statusListeners) listener(event);
  }
}

// Mirrors the public TLSync wire format while keeping each frame below the gate limit.
function chunkProtocolMessage(message: string): string[] {
  const maxCharacters = 128 * 1024;
  if (message.length < maxCharacters) return [message];
  const parts: string[] = [];
  let remaining = message.length;
  let index = 0;
  while (remaining > 0) {
    const prefix = `${index}_`;
    const size = Math.max(
      Math.min(maxCharacters - prefix.length, remaining),
      1,
    );
    parts.unshift(prefix + message.slice(remaining - size, remaining));
    remaining -= size;
    index += 1;
  }
  return parts;
}
