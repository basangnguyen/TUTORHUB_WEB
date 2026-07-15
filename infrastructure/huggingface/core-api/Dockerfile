FROM golang:1.26.5-alpine AS build

WORKDIR /src
COPY services/core-api/go.mod services/core-api/go.mod
WORKDIR /src/services/core-api
RUN go mod download

WORKDIR /src
COPY services/core-api services/core-api
WORKDIR /src/services/core-api
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/tutorhub-core-api ./cmd/api

FROM alpine:3.23
RUN addgroup -S tutorhub && adduser -S -G tutorhub tutorhub
USER tutorhub
WORKDIR /app
COPY --from=build /out/tutorhub-core-api /app/tutorhub-core-api
ENV APP_ENV=staging
ENV PORT=7860
EXPOSE 7860
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -q -T 2 -O - http://127.0.0.1:7860/health > /dev/null || exit 1
ENTRYPOINT ["/app/tutorhub-core-api"]
