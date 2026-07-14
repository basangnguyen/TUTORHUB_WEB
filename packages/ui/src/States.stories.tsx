import type { Meta, StoryObj } from "@storybook/react-vite";
import { Button } from "./Button";
import { Skeleton, SkeletonGroup } from "./Skeleton";
import {
  EmptyState,
  ErrorState,
  ForbiddenState,
  OfflineState,
} from "./StateView";
import "./stories.css";

const meta = {
  title: "Feedback/Page states",
  parameters: { layout: "padded" },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

export const Empty: Story = {
  render: () => (
    <EmptyState
      actions={<Button>Tạo lớp đầu tiên</Button>}
      description="Workspace này chưa có lớp học nào."
      title="Chưa có lớp học"
    />
  ),
};

export const Error: Story = {
  render: () => (
    <ErrorState
      actions={<Button variant="secondary">Thử lại</Button>}
      description="Không thể tải dữ liệu lớp học vào lúc này."
      title="Đã xảy ra lỗi"
    />
  ),
};

export const Forbidden: Story = {
  render: () => (
    <ForbiddenState
      description="Tài khoản hiện tại không có quyền xem nội dung này."
      title="Không đủ quyền truy cập"
    />
  ),
};

export const Offline: Story = {
  render: () => (
    <OfflineState
      actions={<Button variant="secondary">Kiểm tra lại</Button>}
      description="Kết nối mạng đã bị gián đoạn. Một số thao tác tạm thời không khả dụng."
      title="Bạn đang ngoại tuyến"
    />
  ),
};

export const Loading: Story = {
  render: () => (
    <SkeletonGroup className="story-panel" label="Đang tải danh sách lớp">
      <Skeleton height={18} width="38%" />
      <Skeleton height={14} width="72%" />
      <Skeleton height={72} width="100%" />
      <Skeleton height={72} width="100%" />
    </SkeletonGroup>
  ),
};
