FROM node:22-alpine AS builder

WORKDIR /app

COPY package*.json tsconfig.json ./
RUN npm install && npm install --save-dev typescript @types/node

COPY src/ ./src/
RUN ./node_modules/.bin/tsc

# ── Runtime ────────────────────────────────────────────────────────────────────
FROM node:22-alpine

WORKDIR /app

COPY package*.json ./
RUN npm install --omit=dev

COPY --from=builder /app/dist ./dist
COPY skills/ ./skills/

ENV SKILLS_ROOT=/app/skills
ENV TRANSPORT=http
ENV PORT=3030

EXPOSE 3030

CMD ["node", "dist/index.js"]
