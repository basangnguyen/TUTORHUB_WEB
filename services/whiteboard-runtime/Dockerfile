ARG NODE_IMAGE=node:24.15.0-alpine3.23@sha256:d1b3b4da11eefd5941e7f0b9cf17783fc99d9c6fc34884a665f40a06dbdfc94f

FROM ${NODE_IMAGE} AS build

ENV COREPACK_HOME=/opt/corepack \
    PNPM_HOME=/opt/pnpm \
    PATH=/opt/pnpm:$PATH
WORKDIR /workspace

RUN corepack enable --install-directory /usr/local/bin \
    && corepack prepare pnpm@11.7.0 --activate

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig.base.json ./
COPY patches patches
COPY packages/collaboration-client/package.json packages/collaboration-client/package.json
COPY services/whiteboard-runtime/package.json services/whiteboard-runtime/package.json
RUN pnpm install --frozen-lockfile --filter @tutorhub/whiteboard-runtime... --ignore-scripts

COPY packages/collaboration-client/tsconfig.json packages/collaboration-client/tsconfig.json
COPY packages/collaboration-client/src packages/collaboration-client/src
COPY services/whiteboard-runtime/tsconfig.json services/whiteboard-runtime/tsconfig.json
COPY services/whiteboard-runtime/src services/whiteboard-runtime/src
RUN pnpm --filter @tutorhub/whiteboard-runtime build \
    && pnpm --filter @tutorhub/whiteboard-runtime \
      --config.inject-workspace-packages=true deploy --prod /runtime

FROM ${NODE_IMAGE} AS runtime

ENV NODE_ENV=production \
    PORT=3000
WORKDIR /app

# Keep the immutable Node base while requiring the Alpine security release that
# fixes CVE-2026-45447. Later compatible patch releases remain acceptable.
RUN apk add --no-cache --upgrade \
    "libcrypto3>=3.5.7-r0" \
    "libssl3>=3.5.7-r0"

# The runtime starts compiled JavaScript directly. Package managers are build-only
# tooling and are removed from the final image to reduce both attack surface and SBOM.
RUN rm -rf /usr/local/lib/node_modules/npm \
    /usr/local/lib/node_modules/corepack \
    /usr/local/bin/npm \
    /usr/local/bin/npx \
    /usr/local/bin/corepack \
    /usr/local/bin/pnpm \
    /usr/local/bin/pnpx

COPY --from=build --chown=node:node /runtime/package.json ./package.json
COPY --from=build --chown=node:node /runtime/node_modules ./node_modules
COPY --from=build --chown=node:node /runtime/dist ./dist

USER node
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=3s --start-period=30s --retries=3 \
  CMD ["node", "-e", "fetch('http://127.0.0.1:'+(process.env.PORT||3000)+'/livez').then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))"]

CMD ["node", "dist/main.js"]
