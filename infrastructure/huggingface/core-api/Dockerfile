FROM golang:1.26.6-alpine AS build

WORKDIR /src
COPY services/core-api/go.mod services/core-api/go.mod
WORKDIR /src/services/core-api
RUN go mod download

WORKDIR /src
COPY services/core-api services/core-api
WORKDIR /src/services/core-api
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/tutorhub-core-api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/tutorhub-worker ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/tutorhub-calendar-rsvp-fixture ./cmd/calendar-rsvp-fixture

FROM alpine:3.24
RUN addgroup -S tutorhub && adduser -S -G tutorhub tutorhub
USER tutorhub
WORKDIR /app
COPY --from=build /out/tutorhub-core-api /app/tutorhub-core-api
COPY --from=build /out/tutorhub-worker /app/tutorhub-worker
COPY --from=build /out/tutorhub-calendar-rsvp-fixture /app/tutorhub-calendar-rsvp-fixture
ENV APP_ENV=staging
ENV PORT=7860
EXPOSE 7860
CMD ["/app/tutorhub-core-api"]
