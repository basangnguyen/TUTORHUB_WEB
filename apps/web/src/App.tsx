import { useCallback, useEffect, useState } from "react";
import { getHealth, type HealthResponse } from "@tutorhub/api-client";
import { StatusBadge } from "@tutorhub/ui";

type ViewState =
  | { status: "loading" }
  | { status: "ready"; health: HealthResponse }
  | { status: "error"; message: string };

export function App() {
  const [viewState, setViewState] = useState<ViewState>({ status: "loading" });

  const checkHealth = useCallback(async () => {
    setViewState({ status: "loading" });

    try {
      const health = await getHealth({
        baseUrl: import.meta.env.VITE_API_BASE_URL ?? "/api",
      });
      setViewState({ status: "ready", health });
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Không thể kiểm tra API.";
      setViewState({ status: "error", message });
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();

    void getHealth({
      baseUrl: import.meta.env.VITE_API_BASE_URL ?? "/api",
      signal: controller.signal,
    })
      .then((health) => {
        setViewState({ status: "ready", health });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) {
          return;
        }
        const message =
          error instanceof Error ? error.message : "Không thể kiểm tra API.";
        setViewState({ status: "error", message });
      });

    return () => controller.abort();
  }, []);

  return (
    <main className="foundation-page">
      <section className="foundation-panel" aria-labelledby="page-title">
        <div className="brand-mark" aria-hidden="true">
          TH
        </div>
        <p className="eyebrow">Engineering foundation</p>
        <h1 id="page-title">TutorHub V2</h1>
        <p className="intro">
          Nền tảng web-first đang được xây dựng trên React, TypeScript và Go.
        </p>

        <div className="health-row" aria-live="polite">
          {viewState.status === "loading" && (
            <StatusBadge tone="neutral">Đang kiểm tra Core API...</StatusBadge>
          )}
          {viewState.status === "ready" && (
            <StatusBadge tone="success">
              TutorHub API đã sẵn sàng · {viewState.health.environment}
            </StatusBadge>
          )}
          {viewState.status === "error" && (
            <StatusBadge tone="danger">{viewState.message}</StatusBadge>
          )}
        </div>

        <dl className="stack-list">
          <div>
            <dt>Web</dt>
            <dd>React + TypeScript + Vite</dd>
          </div>
          <div>
            <dt>Core API</dt>
            <dd>Go modular monolith</dd>
          </div>
          <div>
            <dt>Alpha hosting</dt>
            <dd>Cloudflare Pages + Hugging Face Spaces</dd>
          </div>
        </dl>

        <button
          className="retry-button"
          type="button"
          onClick={() => void checkHealth()}
        >
          Kiểm tra lại API
        </button>
      </section>
    </main>
  );
}
