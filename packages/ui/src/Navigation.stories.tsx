import type { Meta, StoryObj } from "@storybook/react-vite";
import { MoreHorizontal, Settings } from "lucide-react";
import { Button, IconButton } from "./Button";
import {
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuSeparator,
  MenuTrigger,
} from "./Menu";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "./Tabs";
import { Tooltip, TooltipProvider } from "./Tooltip";
import "./stories.css";

const meta = {
  title: "Navigation/Patterns",
  parameters: { layout: "centered" },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

export const TabSet: Story = {
  render: () => (
    <div className="story-panel">
      <Tabs defaultValue="overview">
        <TabsList aria-label="Thông tin lớp học">
          <TabsTrigger value="overview">Tổng quan</TabsTrigger>
          <TabsTrigger value="members">Thành viên</TabsTrigger>
          <TabsTrigger value="files">Tài liệu</TabsTrigger>
        </TabsList>
        <TabsContent value="overview">
          Lịch học, thông báo và hoạt động gần đây.
        </TabsContent>
        <TabsContent value="members">
          Danh sách giáo viên và học sinh.
        </TabsContent>
        <TabsContent value="files">
          Tài liệu được chia sẻ trong lớp.
        </TabsContent>
      </Tabs>
    </div>
  ),
};

export const ActionMenu: Story = {
  render: () => (
    <Menu>
      <MenuTrigger asChild>
        <IconButton label="Thao tác khác" variant="secondary">
          <MoreHorizontal />
        </IconButton>
      </MenuTrigger>
      <MenuContent align="end">
        <MenuLabel>Quản lý lớp</MenuLabel>
        <MenuItem>Chỉnh sửa thông tin</MenuItem>
        <MenuItem>Quản lý thành viên</MenuItem>
        <MenuSeparator />
        <MenuItem tone="danger">Lưu trữ lớp</MenuItem>
      </MenuContent>
    </Menu>
  ),
};

export const HelpfulTooltip: Story = {
  render: () => (
    <TooltipProvider>
      <Tooltip content="Cấu hình thiết bị và quyền trong phòng học">
        <Button leadingIcon={<Settings />} variant="secondary">
          Cấu hình
        </Button>
      </Tooltip>
    </TooltipProvider>
  ),
};
