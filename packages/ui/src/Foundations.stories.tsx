import type { Meta, StoryObj } from "@storybook/react-vite";
import "./stories.css";

const meta = {
  title: "Foundations/Semantic tokens",
  parameters: { layout: "padded" },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

const colors = [
  ["Canvas", "--color-canvas"],
  ["Surface", "--color-surface"],
  ["Subtle surface", "--color-surface-subtle"],
  ["Accent", "--color-accent"],
  ["Accent soft", "--color-accent-soft"],
  ["Success", "--color-success-soft"],
  ["Warning", "--color-warning-soft"],
  ["Danger", "--color-danger-soft"],
  ["Info", "--color-info-soft"],
] as const;

export const ColorSystem: Story = {
  render: () => (
    <div className="story-token-grid">
      {colors.map(([label, token]) => (
        <div
          className="story-token"
          key={token}
          style={{ background: `var(${token})` }}
        >
          <strong>{label}</strong>
          <small>{token}</small>
        </div>
      ))}
    </div>
  ),
};

export const Typography: Story = {
  render: () => (
    <div className="story-stack">
      <h1 style={{ fontSize: "var(--font-size-3xl)", margin: 0 }}>
        Lớp học trực tuyến TutorHub
      </h1>
      <h2 style={{ fontSize: "var(--font-size-2xl)", margin: 0 }}>
        Thiết kế rõ ràng cho giáo viên và học sinh
      </h2>
      <p style={{ color: "var(--color-text-muted)", margin: 0 }}>
        Hệ typography dùng Aptos hoặc Segoe UI Variable theo khả năng của hệ
        điều hành, với system fallback ổn định trên web và desktop.
      </p>
      <code style={{ fontFamily: "var(--font-family-mono)" }}>
        classroom.join(sessionId)
      </code>
    </div>
  ),
};
