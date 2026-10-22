# Imagem única: o React é compilado e embutido no binário do Go.

# 1) Telas
FROM node:22-alpine AS telas
WORKDIR /telas
COPY frontend/package*.json ./
RUN npm install --no-audit --no-fund
COPY frontend/ .
RUN npm run build

# 2) Servidor
FROM golang:1.24-alpine AS servidor
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
COPY --from=telas /telas/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /prosa ./cmd/prosa \
 && mkdir -p /vazio/arquivos

# 3) Imagem final mínima (sem shell, sem pacotes, usuário sem privilégios)
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /home/nonroot
# pasta dos anexos quando não há S3 (já com o dono certo para volumes do Docker)
COPY --from=servidor --chown=65532:65532 /vazio/arquivos ./arquivos
COPY --from=servidor /prosa /prosa
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/prosa"]
