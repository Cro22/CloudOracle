FROM node:20-alpine AS frontend
WORKDIR /src
COPY web/package.json web/package-lock.json ./web/
RUN cd web && npm ci
COPY web ./web
RUN cd web && npm run build


FROM golang:1.25-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=frontend /src/internal/api/dist ./internal/api/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/cloudoracle ./cmd/oracle


FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S app -G app
WORKDIR /app
COPY --from=backend /out/cloudoracle /app/cloudoracle
USER app
EXPOSE 8080
ENTRYPOINT ["/app/cloudoracle"]
CMD ["serve", "--port", "8080"]
