import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";
import { TextAreaField, TextField } from "./Field";
import { Select } from "./Select";
import "./stories.css";

const meta = {
  title: "Forms/Fields",
  parameters: { layout: "centered" },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

function FormDemo() {
  const [language, setLanguage] = useState("vi");
  return (
    <form className="story-form">
      <TextField
        autoComplete="organization"
        hint="Tên hiển thị cho thành viên trong workspace."
        label="Tên workspace"
        placeholder="Khoa Công nghệ thông tin"
      />
      <TextField
        error="Mã lớp chỉ gồm chữ in hoa, số, gạch ngang hoặc gạch dưới."
        label="Mã lớp"
        value="ATTT 01"
        readOnly
      />
      <TextAreaField
        hint="Tối đa 4.000 ký tự."
        label="Mô tả"
        placeholder="Mục tiêu và nội dung chính của lớp học"
        rows={4}
      />
      <div className="th-field">
        <span className="th-field__label">Ngôn ngữ</span>
        <Select
          ariaLabel="Ngôn ngữ giao diện"
          onValueChange={setLanguage}
          options={[
            { label: "Tiếng Việt", value: "vi" },
            { label: "English", value: "en" },
          ]}
          value={language}
        />
      </div>
    </form>
  );
}

export const CompleteForm: Story = {
  render: () => <FormDemo />,
};
