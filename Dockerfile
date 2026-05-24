# Cronix Dockerfile - Multi-stage build
# Build: docker build -t cronix .
# Run: docker run -d -p 8080:8080 -v $(pwd)/data:/app/data -v $(pwd)/config.yaml:/app/config.yaml cronix

FROM node:20-alpine AS frontend
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.23-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /cronix .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata curl
COPY --from=backend /cronix /usr/local/bin/cronix
RUN mkdir -p /app/data
WORKDIR /app
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD curl -f http://localhost:8080/api/health || exit 1
ENTRYPOINT ["cronix"]
CMD ["serve"]
