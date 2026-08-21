import { describe, expect, it } from "vitest";
import {
  RuntimeDocumentBudget,
  RuntimeDocumentBudgetError,
} from "./runtimeDocumentBudget.js";

describe("RuntimeDocumentBudget", () => {
  it("accounts accepted update bytes without cloning the document", () => {
    const budget = new RuntimeDocumentBudget(1_000, 400);
    budget.load("opaque-document", 200);

    expect(budget.reserve("opaque-document", 300)).toBe(500);
    expect(budget.reserve("opaque-document", 400)).toBe(900);
    expect(budget.size("opaque-document")).toBe(900);
  });

  it("fails closed at the update and durable document boundaries", () => {
    const budget = new RuntimeDocumentBudget(1_000, 400);
    budget.load("opaque-document", 700);

    expect(() => budget.reserve("opaque-document", 401)).toThrowError(
      new RuntimeDocumentBudgetError("document_update_too_large"),
    );
    expect(() => budget.reserve("opaque-document", 301)).toThrowError(
      new RuntimeDocumentBudgetError("document_too_large"),
    );
    expect(budget.size("opaque-document")).toBe(700);
  });

  it("requires a loaded document and releases its accounting idempotently", () => {
    const budget = new RuntimeDocumentBudget(1_000, 400);

    expect(() => budget.reserve("opaque-document", 1)).toThrowError(
      new RuntimeDocumentBudgetError("document_budget_missing"),
    );
    budget.load("opaque-document", 0);
    budget.release("opaque-document");
    budget.release("opaque-document");
    expect(budget.size("opaque-document")).toBeUndefined();
  });

  it("rejects invalid configuration and inputs with bounded codes", () => {
    expect(() => new RuntimeDocumentBudget(10, 11)).toThrowError(
      new RuntimeDocumentBudgetError("document_budget_input_invalid"),
    );
    const budget = new RuntimeDocumentBudget(10, 5);
    expect(() => budget.load("", 0)).toThrowError(
      new RuntimeDocumentBudgetError("document_budget_input_invalid"),
    );
    expect(() => budget.load("opaque-document", 11)).toThrowError(
      new RuntimeDocumentBudgetError("document_too_large"),
    );
  });
});
