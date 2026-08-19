import { resolve } from "node:path";
import { preview, type PreviewServer } from "vite";
import { startSpikeServer } from "../src/collab/hocuspocusHarness";
import { startAuthorizedExcalidrawServer } from "../server/excalidrawAuthorizationHarness";

export default async function globalSetup() {
  const providerServer = await startSpikeServer();
  const authorizationServer = await startAuthorizedExcalidrawServer();
  process.env.P5_GATE_B_PROVIDER_URL = providerServer.url;
  process.env.P5_GATE_C_CONTROL_URL = authorizationServer.controlUrl;
  process.env.P5_GATE_C_PROVIDER_URL = authorizationServer.providerUrl;
  const previewServer: PreviewServer = await preview({
    root: process.cwd(),
    configFile: resolve("vite.excalidraw.config.ts"),
    preview: {
      host: "127.0.0.1",
      port: 4180,
      strictPort: true,
    },
    logLevel: "silent",
  });

  return async () => {
    if ("closeAllConnections" in previewServer.httpServer) {
      previewServer.httpServer.closeAllConnections();
    }
    await new Promise<void>((resolveClose, rejectClose) => {
      previewServer.httpServer.close((error) => {
        if (error) rejectClose(error);
        else resolveClose();
      });
    });
    await authorizationServer.destroy();
    await providerServer.destroy();
  };
}
