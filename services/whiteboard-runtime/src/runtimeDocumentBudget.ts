export type RuntimeDocumentBudgetErrorCode =
  | "document_budget_missing"
  | "document_too_large"
  | "document_update_too_large"
  | "document_budget_input_invalid";

export class RuntimeDocumentBudgetError extends Error {
  constructor(readonly code: RuntimeDocumentBudgetErrorCode) {
    super(code);
    this.name = "RuntimeDocumentBudgetError";
  }
}

/**
 * Conservative, allocation-free document accounting.
 *
 * Every accepted update consumes its raw byte length until the document is
 * unloaded. This may reject a highly compressible document early, but it never
 * clones or re-encodes the whole Y.Doc on the event loop for each frame and it
 * cannot under-count accepted ingress bytes.
 */
export class RuntimeDocumentBudget {
  private readonly documents = new Map<string, number>();

  constructor(
    private readonly maxDocumentBytes: number,
    private readonly maxUpdateBytes: number,
  ) {
    if (
      !Number.isSafeInteger(maxDocumentBytes) ||
      maxDocumentBytes < 1 ||
      !Number.isSafeInteger(maxUpdateBytes) ||
      maxUpdateBytes < 1 ||
      maxUpdateBytes > maxDocumentBytes
    ) {
      throw new RuntimeDocumentBudgetError("document_budget_input_invalid");
    }
  }

  load(documentName: string, byteLength: number): void {
    this.validateInput(documentName, byteLength, true);
    if (byteLength > this.maxDocumentBytes) {
      throw new RuntimeDocumentBudgetError("document_too_large");
    }
    this.documents.set(documentName, byteLength);
  }

  reserve(documentName: string, updateBytes: number): number {
    this.validateInput(documentName, updateBytes, false);
    if (updateBytes > this.maxUpdateBytes) {
      throw new RuntimeDocumentBudgetError("document_update_too_large");
    }
    const current = this.documents.get(documentName);
    if (current === undefined) {
      throw new RuntimeDocumentBudgetError("document_budget_missing");
    }
    const next = current + updateBytes;
    if (!Number.isSafeInteger(next) || next > this.maxDocumentBytes) {
      throw new RuntimeDocumentBudgetError("document_too_large");
    }
    this.documents.set(documentName, next);
    return next;
  }

  release(documentName: string): void {
    if (!validName(documentName)) {
      throw new RuntimeDocumentBudgetError("document_budget_input_invalid");
    }
    this.documents.delete(documentName);
  }

  size(documentName: string): number | undefined {
    return this.documents.get(documentName);
  }

  private validateInput(
    documentName: string,
    byteLength: number,
    allowZero: boolean,
  ): void {
    if (
      !validName(documentName) ||
      !Number.isSafeInteger(byteLength) ||
      byteLength < (allowZero ? 0 : 1)
    ) {
      throw new RuntimeDocumentBudgetError("document_budget_input_invalid");
    }
  }
}

function validName(value: string): boolean {
  return value.length > 0 && value.length <= 512 && !value.includes("\0");
}
