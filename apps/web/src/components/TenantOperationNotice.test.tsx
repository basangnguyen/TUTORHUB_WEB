import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { TenantOperationNotice } from "./TenantOperationNotice";

describe("TenantOperationNotice", () => {
  afterEach(cleanup);

  it("offers a retry for a rate-limited operation", () => {
    const retry = vi.fn();
    render(
      <I18nProvider initialLanguage="en">
        <TenantOperationNotice
          availability={{ available: false, reason: "rate_limited" }}
          onRetry={retry}
        />
      </I18nProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(retry).toHaveBeenCalledOnce();
  });

  it("keeps loading fail-closed without a premature retry", () => {
    render(
      <I18nProvider initialLanguage="en">
        <TenantOperationNotice
          availability={{
            available: false,
            reason: "capabilities_loading",
          }}
          onRetry={() => undefined}
        />
      </I18nProvider>,
    );

    expect(screen.getByRole("status")).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});
