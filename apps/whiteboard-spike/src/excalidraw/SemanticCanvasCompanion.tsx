import { forwardRef, useEffect, useMemo, useState, type Ref } from "react";
import type {
  CanonicalElementV1,
  CanonicalExcalidrawSceneV1,
  JsonObject,
  JsonValue,
} from "./canonicalAuthority";
import type { BoardFixtureShape } from "../model";

const PAGE_SIZE = 50;
const TEXT_LIMIT = 240;

export interface SemanticCanvasItem {
  description: string;
  id: string;
  label: string;
  position: string;
  warning?: string;
}

interface SemanticCanvasCompanionProps {
  canvasAvailable: boolean;
  hash: string;
  items: SemanticCanvasItem[];
  onFocusCanvas: () => void;
  pageName: string;
}

export const SemanticCanvasCompanion = forwardRef(
  function SemanticCanvasCompanion(
    {
      canvasAvailable,
      hash,
      items,
      onFocusCanvas,
      pageName,
    }: SemanticCanvasCompanionProps,
    headingRef: Ref<HTMLHeadingElement>,
  ) {
    const [pageIndex, setPageIndex] = useState(0);
    const pageCount = Math.max(1, Math.ceil(items.length / PAGE_SIZE));
    useEffect(() => {
      setPageIndex((current) => Math.min(current, pageCount - 1));
    }, [pageCount]);
    const visibleItems = useMemo(
      () => items.slice(pageIndex * PAGE_SIZE, (pageIndex + 1) * PAGE_SIZE),
      [items, pageIndex],
    );

    return (
      <section
        className="semantic-canvas"
        aria-labelledby="semantic-canvas-title"
        data-testid="semantic-canvas"
      >
        <div>
          <p className="eyebrow">Semantic canvas fallback</p>
          <h2 id="semantic-canvas-title" ref={headingRef} tabIndex={-1}>
            Nội dung bảng có thể truy cập
          </h2>
          <p>
            Canvas là bề mặt trực quan. Danh sách có cấu trúc này là phương án
            đọc thay thế từ cùng canonical authority, không phải document
            authority thứ hai.
          </p>
        </div>
        <dl className="semantic-summary">
          <div>
            <dt>Trang</dt>
            <dd>{pageName}</dd>
          </div>
          <div>
            <dt>Tổng phần tử</dt>
            <dd>{items.length.toLocaleString("vi-VN")}</dd>
          </div>
        </dl>
        <p
          className="semantic-live-summary"
          data-semantic-hash={hash}
          data-testid="semantic-page-status"
          aria-live="polite"
          aria-atomic="true"
        >
          Đang hiển thị trang {pageIndex + 1} trên {pageCount}, tối đa{" "}
          {PAGE_SIZE} phần tử mỗi trang.
        </p>
        <div className="semantic-toolbar" aria-label="Điều hướng nội dung bảng">
          <button
            type="button"
            disabled={pageIndex === 0}
            onClick={() => setPageIndex((current) => Math.max(0, current - 1))}
          >
            Trang trước
          </button>
          <button
            type="button"
            disabled={pageIndex >= pageCount - 1}
            onClick={() =>
              setPageIndex((current) => Math.min(pageCount - 1, current + 1))
            }
          >
            Trang sau
          </button>
          <button
            type="button"
            disabled={!canvasAvailable}
            onClick={onFocusCanvas}
          >
            Chuyển tiêu điểm tới canvas
          </button>
        </div>
        {visibleItems.length === 0 ? (
          <p className="semantic-empty">Bảng hiện chưa có nội dung.</p>
        ) : (
          <ol className="semantic-list" start={pageIndex * PAGE_SIZE + 1}>
            {visibleItems.map((item) => (
              <li key={item.id} className="semantic-item">
                <h3>{item.label}</h3>
                <p>{item.description}</p>
                <p className="semantic-position">{item.position}</p>
                {item.warning ? (
                  <p className="semantic-warning">Lưu ý: {item.warning}</p>
                ) : null}
              </li>
            ))}
          </ol>
        )}
      </section>
    );
  },
);

export function canonicalSceneToSemanticItems(
  scene: CanonicalExcalidrawSceneV1,
): SemanticCanvasItem[] {
  return scene.elements.map((element, index) => ({
    description: describeElement(element),
    id: element.id,
    label: `${index + 1}. ${elementTypeLabel(element.type)}`,
    position: describePosition(element),
    warning: describeWarning(element),
  }));
}

export function fixturesToSemanticItems(
  fixtures: BoardFixtureShape[],
): SemanticCanvasItem[] {
  return fixtures.map((fixture, index) => ({
    description: `Nhãn: ${truncate(fixture.label)}`,
    id: fixture.id,
    label: `${index + 1}. Hình chữ nhật`,
    position: `Vị trí x ${fixture.x}, y ${fixture.y}; kích thước ${fixture.width} × ${fixture.height}.`,
  }));
}

function describeElement(element: CanonicalElementV1): string {
  const text = firstString(
    element.text,
    element.originalText,
    nestedString(element.label, "text"),
    nestedString(element.customData, "accessibleLabel"),
    element.altText,
  );
  const relation = describeRelation(element);
  const link = typeof element.link === "string" ? truncate(element.link) : null;
  const details = [
    text ? `Nội dung: ${truncate(text)}` : null,
    relation,
    link ? `Liên kết: ${link}` : null,
  ].filter((value): value is string => value !== null);
  return details.length > 0 ? details.join(". ") : "Không có nội dung văn bản.";
}

function describePosition(element: CanonicalElementV1): string {
  return `Vị trí x ${rounded(element.x)}, y ${rounded(element.y)}; kích thước ${rounded(element.width)} × ${rounded(element.height)}.`;
}

function describeRelation(element: CanonicalElementV1): string | null {
  const start = nestedString(element.startBinding, "elementId");
  const end = nestedString(element.endBinding, "elementId");
  if (!start && !end) return null;
  if (start && end) return `Kết nối từ ${truncate(start)} đến ${truncate(end)}`;
  return `Kết nối với ${truncate(start ?? end ?? "phần tử không xác định")}`;
}

function describeWarning(element: CanonicalElementV1): string | undefined {
  if (
    element.type === "image" &&
    !firstString(
      element.altText,
      nestedString(element.customData, "accessibleLabel"),
    )
  ) {
    return "Hình ảnh chưa có mô tả thay thế.";
  }
  return undefined;
}

function elementTypeLabel(type: string): string {
  const labels: Record<string, string> = {
    arrow: "Mũi tên",
    diamond: "Hình thoi",
    ellipse: "Hình elip",
    embeddable: "Nội dung nhúng",
    frame: "Khung",
    freedraw: "Nét vẽ tự do",
    iframe: "Khung nội dung",
    image: "Hình ảnh",
    line: "Đường thẳng",
    magicframe: "Khung thông minh",
    rectangle: "Hình chữ nhật",
    text: "Văn bản",
  };
  return labels[type] ?? `Phần tử ${truncate(type)}`;
}

function nestedString(
  value: JsonValue | undefined,
  key: string,
): string | null {
  if (!isJsonObject(value)) return null;
  const nested = value[key];
  return typeof nested === "string" && nested.trim() ? nested : null;
}

function firstString(
  ...values: Array<JsonValue | null | undefined>
): string | null {
  for (const value of values) {
    if (typeof value === "string" && value.trim()) return value;
  }
  return null;
}

function isJsonObject(value: JsonValue | undefined): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function truncate(value: string): string {
  const normalized = value.replace(/\s+/g, " ").trim();
  return normalized.length <= TEXT_LIMIT
    ? normalized
    : `${normalized.slice(0, TEXT_LIMIT - 1)}…`;
}

function rounded(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(1);
}
