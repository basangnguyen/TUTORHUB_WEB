import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { Settings } from "lucide-react";
import { Button, IconButton } from "./Button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from "./Dialog";
import { TextField } from "./Field";
import { SelectField } from "./Select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "./Tabs";

describe("Button", () => {
  it("announces and disables its loading state", () => {
    render(
      <Button loading loadingLabel="Đang lưu">
        Lưu
      </Button>,
    );

    const button = screen.getByRole("button", { name: "Đang lưu" });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("aria-busy", "true");
  });

  it("renders an accessible icon-only action without hiding its icon", () => {
    render(
      <IconButton label="Mở cấu hình">
        <Settings data-testid="settings-icon" />
      </IconButton>,
    );

    expect(screen.getByRole("button", { name: "Mở cấu hình" })).toBeVisible();
    expect(screen.getByTestId("settings-icon")).toBeVisible();
  });
});

describe("TextField", () => {
  it("connects label, hint and validation error", () => {
    render(
      <TextField
        error="Tên không hợp lệ"
        hint="Tối đa 120 ký tự"
        label="Tên lớp"
      />,
    );

    const input = screen.getByLabelText("Tên lớp");
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(input).toHaveAccessibleDescription(
      "Tối đa 120 ký tự Tên không hợp lệ",
    );
    expect(screen.getByRole("alert")).toHaveTextContent("Tên không hợp lệ");
  });
});

describe("SelectField", () => {
  it("connects its trigger to hint and validation error", () => {
    render(
      <SelectField
        ariaLabel="Vai trò"
        error="Hãy chọn vai trò"
        hint="Quyền có thể thay đổi sau"
        label="Vai trò"
        options={[{ label: "Giáo viên", value: "teacher" }]}
      />,
    );

    const trigger = screen.getByRole("combobox", { name: "Vai trò" });
    expect(trigger).toHaveAttribute("aria-invalid", "true");
    expect(trigger).toHaveAccessibleDescription(
      "Quyền có thể thay đổi sau Hãy chọn vai trò",
    );
  });
});

describe("Dialog", () => {
  it("moves focus into the dialog and closes with Escape", async () => {
    const user = userEvent.setup();
    render(
      <Dialog>
        <DialogTrigger asChild>
          <button type="button">Mở cấu hình</button>
        </DialogTrigger>
        <DialogContent>
          <DialogTitle>Cấu hình lớp học</DialogTitle>
          <DialogDescription>Thay đổi cài đặt phòng.</DialogDescription>
          <button type="button">Lưu</button>
        </DialogContent>
      </Dialog>,
    );

    await user.click(screen.getByRole("button", { name: "Mở cấu hình" }));
    expect(screen.getByRole("dialog")).toBeVisible();
    expect(screen.getByRole("button", { name: "Đóng" })).toBeVisible();

    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Mở cấu hình" })).toHaveFocus();
  });
});

describe("Tabs", () => {
  it("supports arrow-key navigation", async () => {
    const user = userEvent.setup();
    render(
      <Tabs defaultValue="overview">
        <TabsList aria-label="Nội dung lớp học">
          <TabsTrigger value="overview">Tổng quan</TabsTrigger>
          <TabsTrigger value="members">Thành viên</TabsTrigger>
        </TabsList>
        <TabsContent value="overview">Nội dung tổng quan</TabsContent>
        <TabsContent value="members">Danh sách thành viên</TabsContent>
      </Tabs>,
    );

    const overview = screen.getByRole("tab", { name: "Tổng quan" });
    await user.click(overview);
    await user.keyboard("{ArrowRight}");

    expect(screen.getByRole("tab", { name: "Thành viên" })).toHaveFocus();
    expect(screen.getByText("Danh sách thành viên")).toBeVisible();
  });
});
