import { fileURLToPath } from "node:url";
import { preview } from "vite";

const webRoot = fileURLToPath(new URL("..", import.meta.url));

export default async function calendarGlobalSetup() {
  const server = await preview({
    root: webRoot,
    preview: {
      host: "127.0.0.1",
      port: 4175,
      strictPort: true,
    },
  });

  return async () => {
    await server.close();
  };
}
