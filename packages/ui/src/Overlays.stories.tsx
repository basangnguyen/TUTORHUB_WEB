import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";
import { Button } from "./Button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  DialogTrigger,
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerTitle,
  DrawerTrigger,
} from "./Dialog";
import { TextField } from "./Field";
import { Toast, ToastProvider, ToastViewport } from "./Toast";
import "./stories.css";

const meta = {
  title: "Overlays/Feedback",
  parameters: { layout: "centered" },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

export const ModalDialog: Story = {
  render: () => (
    <Dialog>
      <DialogTrigger asChild>
        <Button>Mở hộp thoại</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogTitle>Tạo lớp học</DialogTitle>
        <DialogDescription>
          Nhập thông tin cơ bản. Bạn có thể bổ sung thành viên sau.
        </DialogDescription>
        <div style={{ marginTop: "var(--space-5)" }}>
          <TextField label="Tên lớp" placeholder="Cơ sở An toàn thông tin" />
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="secondary">Hủy</Button>
          </DialogClose>
          <Button>Tạo lớp</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  ),
};

export const SideDrawer: Story = {
  render: () => (
    <Drawer>
      <DrawerTrigger asChild>
        <Button variant="secondary">Mở bảng cấu hình</Button>
      </DrawerTrigger>
      <DrawerContent>
        <DrawerTitle>Cấu hình phòng học</DrawerTitle>
        <DrawerDescription>
          Điều chỉnh quyền camera, micro và chia sẻ màn hình.
        </DrawerDescription>
      </DrawerContent>
    </Drawer>
  ),
};

function ToastDemo() {
  const [open, setOpen] = useState(false);
  return (
    <ToastProvider swipeDirection="right">
      <Button onClick={() => setOpen(true)}>Hiện thông báo</Button>
      <Toast
        description="Thay đổi đã được đồng bộ với lớp học."
        onOpenChange={setOpen}
        open={open}
        title="Đã lưu cấu hình"
        tone="success"
      />
      <ToastViewport />
    </ToastProvider>
  );
}

export const ToastMessage: Story = {
  render: () => <ToastDemo />,
};
