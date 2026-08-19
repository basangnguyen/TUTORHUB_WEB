import { resolve } from "node:path";
import { preview, type PreviewServer } from "vite";
import { createGateServer } from "../server/gateServer";

export default async function globalSetup() {
  const gateServer = createGateServer({
    port: 4179,
    dataDir: resolve("../../test-results/whiteboard-spike-gate-data"),
  });
  let previewServer: PreviewServer | null = null;
  await gateServer.start();
  try {
    previewServer = await preview({
      root: process.cwd(),
      preview: {
        host: "127.0.0.1",
        port: 4178,
        strictPort: true,
      },
      logLevel: "silent",
    });
  } catch (error) {
    await gateServer.stop();
    throw error;
  }

  return async () => {
    if (previewServer) {
      if ("closeAllConnections" in previewServer.httpServer) {
        previewServer.httpServer.closeAllConnections();
      }
      await new Promise<void>((resolveClose, rejectClose) => {
        previewServer?.httpServer.close((error) => {
          if (error) rejectClose(error);
          else resolveClose();
        });
      });
    }
    await gateServer.stop();
  };
}
