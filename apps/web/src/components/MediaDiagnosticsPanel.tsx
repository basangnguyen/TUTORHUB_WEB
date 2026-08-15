import { useState } from "react";
import type { MediaDiagnosticExport } from "@tutorhub/api-client";
import { loadMediaDiagnostics } from "../app/mediaDiagnostics";
import { useI18n } from "../app/i18n";

export function MediaDiagnosticsPanel({ tenantID }: { tenantID: string }) {
  const { t } = useI18n();
  const [status, setStatus] = useState<"idle" | "loading" | "ready" | "error">(
    "idle",
  );
  const [diagnostics, setDiagnostics] = useState<MediaDiagnosticExport | null>(
    null,
  );

  const load = async () => {
    setStatus("loading");
    try {
      setDiagnostics(await loadMediaDiagnostics(tenantID));
      setStatus("ready");
    } catch {
      setStatus("error");
    }
  };

  const download = () => {
    if (!diagnostics) return;
    const url = URL.createObjectURL(
      new Blob([JSON.stringify(diagnostics, null, 2)], {
        type: "application/json",
      }),
    );
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = "tutorhub-media-diagnostics.json";
    anchor.click();
    URL.revokeObjectURL(url);
  };

  return (
    <section
      className="media-p410-diagnostics"
      aria-labelledby="media-p410-title"
    >
      <h2 id="media-p410-title">{t("media.p410.title")}</h2>
      <p>{t("media.p410.privacy")}</p>
      <button
        disabled={status === "loading"}
        onClick={() => void load()}
        type="button"
      >
        {status === "loading" ? t("media.p410.loading") : t("media.p410.load")}
      </button>
      {status === "error" && <p role="alert">{t("media.p410.error")}</p>}
      {diagnostics && (
        <div aria-live="polite">
          <dl>
            <div>
              <dt>{t("media.p410.joinSuccess")}</dt>
              <dd>
                {Math.round(diagnostics.metrics.join_success_rate * 100)}%
              </dd>
            </div>
            <div>
              <dt>{t("media.p410.p95")}</dt>
              <dd>{diagnostics.metrics.p95_time_to_media_ms} ms</dd>
            </div>
            <div>
              <dt>{t("media.p410.reconnect")}</dt>
              <dd>
                {diagnostics.metrics.reconnect_succeeded}/
                {diagnostics.metrics.reconnect_failed}
              </dd>
            </div>
          </dl>
          <button onClick={download} type="button">
            {t("media.p410.download")}
          </button>
        </div>
      )}
    </section>
  );
}
