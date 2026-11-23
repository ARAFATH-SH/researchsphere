# ResearchSphere sync server (slice 22, docs/sync.md): Hocuspocus and Yjs on Node.js 22. Node
# runs the TypeScript sources directly (type stripping), so there is no build step. Build from
# the repository root: docker compose build sync.

FROM node:22.23.3-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts --no-audit --no-fund

FROM node:22.23.3-alpine
ENV NODE_ENV=production SYNC_HOST=0.0.0.0 SYNC_PORT=8091
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY package.json ./
COPY src ./src
USER node
EXPOSE 8091
# SIGTERM drains: no more edits, every document stored, then exit (allow 30 s to stop).
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8091/health || exit 1
CMD ["node", "src/main.ts"]
