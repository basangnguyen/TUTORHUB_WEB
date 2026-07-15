import type { Preview } from "@storybook/react-vite";
import "../packages/design-tokens/src/tokens.css";
import "../packages/ui/src/styles.css";
import "./preview.css";

const preview: Preview = {
  parameters: {
    actions: { argTypesRegex: "^on[A-Z].*" },
    a11y: {
      test: "error",
    },
    controls: {
      expanded: true,
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
    layout: "centered",
  },
  tags: ["autodocs"],
};

export default preview;
