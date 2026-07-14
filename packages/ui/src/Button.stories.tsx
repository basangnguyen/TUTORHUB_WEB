import type { Meta, StoryObj } from "@storybook/react-vite";
import { Plus, Settings } from "lucide-react";
import { Button, IconButton } from "./Button";
import "./stories.css";

const meta = {
  title: "Actions/Button",
  component: Button,
  args: {
    children: "Tạo lớp học",
  },
} satisfies Meta<typeof Button>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Primary: Story = {};

export const Variants: Story = {
  render: () => (
    <div className="story-row">
      <Button leadingIcon={<Plus />}>Tạo lớp học</Button>
      <Button variant="secondary">Xem chi tiết</Button>
      <Button variant="quiet">Hủy</Button>
      <Button variant="danger">Xóa lớp</Button>
    </div>
  ),
};

export const Sizes: Story = {
  render: () => (
    <div className="story-row">
      <Button size="sm">Nhỏ</Button>
      <Button size="md">Mặc định</Button>
      <Button size="lg">Lớn</Button>
    </div>
  ),
};

export const Loading: Story = {
  args: {
    loading: true,
    loadingLabel: "Đang tạo lớp",
  },
};

export const IconOnly: Story = {
  render: () => (
    <IconButton label="Mở cấu hình" variant="secondary">
      <Settings />
    </IconButton>
  ),
};
