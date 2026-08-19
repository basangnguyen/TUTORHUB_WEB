ARG NODE_IMAGE=node:24.15.0-bookworm-slim@sha256:4e6b70dd6cbfc88c8157ba19aa3d9f9cce6ba4703576d55459e45efcbc9c5f5d

FROM ${NODE_IMAGE} AS build

ENV COREPACK_HOME=/opt/corepack \
    PNPM_HOME=/opt/pnpm \
    PATH=/opt/pnpm:$PATH
WORKDIR /workspace

RUN corepack enable --install-directory /usr/local/bin \
    && corepack prepare pnpm@11.7.0 --activate

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig.base.json ./
COPY services/whiteboard-runtime/package.json services/whiteboard-runtime/package.json
RUN pnpm install --frozen-lockfile --filter @tutorhub/whiteboard-runtime... --ignore-scripts

COPY services/whiteboard-runtime/tsconfig.json services/whiteboard-runtime/tsconfig.json
COPY services/whiteboard-runtime/src services/whiteboard-runtime/src
RUN pnpm --filter @tutorhub/whiteboard-runtime build \
    && pnpm --filter @tutorhub/whiteboard-runtime \
      --config.inject-workspace-packages=true deploy --prod /runtime

FROM ${NODE_IMAGE} AS runtime

ENV NODE_ENV=production \
    PORT=3000
WORKDIR /app

COPY --from=build --chown=node:node /runtime/package.json ./package.json
COPY --from=build --chown=node:node /runtime/node_modules ./node_modules
COPY --from=build --chown=node:node /runtime/dist ./dist

USER node
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=3s --start-period=30s --retries=3 \
  CMD ["node", "-e", "fetch('http://127.0.0.1:'+(process.env.PORT||3000)+'/readyz').then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))"]

CMD ["node", "dist/main.js"]
