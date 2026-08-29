# Build the static tearoom binary, then copy it into a minimal runtime.
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/tearoom ./cmd/tearoom

FROM alpine:3.21
RUN adduser -D -u 10001 tearoom
USER tearoom
COPY --from=build /out/tearoom /usr/local/bin/tearoom
EXPOSE 8787
# Default to the hosted server so a plain `docker run` (or a platform
# service without a start command) serves it; $DATABASE_URL is the only
# required variable. A platform start command overrides this entrypoint.
ENTRYPOINT ["sh", "-c", "exec /usr/local/bin/tearoom hosted serve --database \"$DATABASE_URL\" --address :8787"]
